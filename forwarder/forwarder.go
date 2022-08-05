package forwarder

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend"
	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend/events"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/brocaar/lorawan"
	"github.com/sirupsen/logrus"
	"github.com/zyedidia/generic/mapset"
)

type Forwarder struct {
	backend        backend.Backend
	mapperClient   *MapperForwarder
	routerPool     *RouterPool
	gatewayStore   gateway.GatewayStore
	onlineGateways mapset.Set[lorawan.EUI64]
	routerEvents   chan *routerEvent
}

func NewForwarder(backend backend.Backend) (*Forwarder, error) {
	fw := &Forwarder{
		backend:        backend,
		onlineGateways: mapset.New[lorawan.EUI64](),
		routerEvents:   make(chan *routerEvent, 4096),
	}

	rp, err := NewRouterPool()
	if err != nil {
		return nil, err
	}

	fw.routerPool = rp

	fw.setup()

	return fw, nil
}

// Run the forwarder in the background. This waits for received router events
// and forwards them to to correct gateway. Events coming from gateways are
// received by the backend that publishes them through the forwader that calls
// the callbacks on the router client that sends the event to the router.
func (fw *Forwarder) Run() {
	go func() {
		for ev := range fw.routerEvents {
			fw.handleRouterEvent(ev)
		}
	}()
}

func (fw *Forwarder) handleRouterEvent(event *routerEvent) {
	if frame := event.ev.GetDownlinkFrameEvent(); frame != nil {
		fw.SendDownlinkFrameFromRouter(event.router, *frame.DownlinkFrame)
	} else if airtimePayment := event.ev.GetAirtimePaymentEvent(); airtimePayment != nil {
		logrus.Info("TODO: handle router received airtime payment")
	}
}

func (fw *Forwarder) setup() error {
	fw.backend.SetUplinkFrameFunc(fw.UplinkFrame)
	fw.backend.SetDownlinkTxAckFunc(fw.DownlinkTxAck)
	fw.backend.SetGatewayStatsFunc(fw.GatewayStats)
	fw.backend.SetSubscribeEventFunc(fw.SubscribeEvent)

	fw.backend.SetRawPacketForwarderEventFunc(nil)

	return nil
}

func (fw *Forwarder) UplinkFrame(frame gw.UplinkFrame) {
	var phy lorawan.PHYPayload

	logrus.WithFields(logrus.Fields{
		"gateway": hex.EncodeToString(frame.RxInfo.GatewayId),
		"rssi":    frame.RxInfo.Rssi,
		"freq":    frame.TxInfo.Frequency,
		"payload": base64.RawStdEncoding.EncodeToString(frame.PhyPayload),
	}).Info("reveived packet from gateway")

	err := phy.UnmarshalBinary(frame.PhyPayload)
	if err != nil {
		logrus.WithError(err).Error("could not decode lorawan packet, dropping packet")
		return
	}

	if phy.MHDR.MType == lorawan.ConfirmedDataUp || phy.MHDR.MType == lorawan.UnconfirmedDataUp {
		// Filter by NetID
		mac, ok := phy.MACPayload.(*lorawan.MACPayload)
		if !ok {
			logrus.Error("invalid packet: data-up but no mac-payload, dropping packet")
			return
		}

		// TODO: Handle mapper mac and forward to mapping service
		if IsMaybeMapperPacket(mac) {
			fw.mapperClient.HandleMapperPacket(frame, mac)
			return
		}

		routers, err := fw.routerPool.GetRoutersForDataUp(mac.FHDR.DevAddr)
		if err != nil {
			logrus.WithError(err).Error("error while getting routers for uplink, dropping packet")
			return
		}

		if len(routers) == 0 {
			logrus.Infof("no routers for packet found, dropping packet")
		}

		for _, router := range routers {
			router.client.DeliverDataUp(frame)
		}
	} else if phy.MHDR.MType == lorawan.JoinRequest || phy.MHDR.MType == lorawan.RejoinRequest {
		// Filter by Xor8 filter on joinEUI
		jr, ok := phy.MACPayload.(*lorawan.JoinRequestPayload)
		if !ok {
			logrus.Error("invalid packet: join but no join-payload, dropping packet")
			return
		}

		routers, err := fw.routerPool.GetRoutersForJoin(jr.JoinEUI)
		if err != nil {
			logrus.WithError(err).Error("error while getting routers for join, dropping packet")
			return
		}

		if len(routers) == 0 {
			logrus.Infof("no routers for packet found, dropping packet")
		}

		for _, router := range routers {
			router.client.DeliverJoin(frame)
		}
	}
}

func (fw *Forwarder) DownlinkTxAck(txack gw.DownlinkTXAck) {
}

func (fw *Forwarder) SubscribeEvent(event events.Subscribe) {
	if event.Subscribe {
		if !fw.onlineGateways.Has(event.GatewayID) {
			logrus.Infof("gateway is online: %s", hex.EncodeToString(event.GatewayID[:]))
		}

		fw.onlineGateways.Put(event.GatewayID)
	} else {
		if fw.onlineGateways.Has(event.GatewayID) {
			logrus.Infof("gateway is offline: %s", hex.EncodeToString(event.GatewayID[:]))
		}

		fw.onlineGateways.Remove(event.GatewayID)
	}

	routers, err := fw.routerPool.GetConnectedRouters()
	if err != nil {
		logrus.WithError(err).Error("error while getting routers to report hotspot status to, dropping status update")
		return
	}
	for _, router := range routers {
		router.client.DeliverGatewayStatus(fw.gatewayStore.NetworkGatewayIdForGateway(event.GatewayID[:]), event.Subscribe)
	}

}

func (fw *Forwarder) SendDownlinkFrameFromRouter(router *Router, frame gw.DownlinkFrame) {
	frame, err := fw.updateDownlinkFrameFromNetworkFormat(frame)
	if err != nil {
		logrus.WithError(err).Error("could not send downlink frame, dropping packet")
		return
	}

	// TODO: Cache router so we know to what router to send dowlink ack

	err = fw.backend.SendDownlinkFrame(frame)
	if err != nil {
		logrus.WithError(err).Error("could not send downlink frame, dropping packet")
		return
	}
}

func (fw *Forwarder) SendDownlinkFrame(frame gw.DownlinkFrame) {
	err := fw.backend.SendDownlinkFrame(frame)
	if err != nil {
		logrus.WithError(err).Error("could not send downlink frame, dropping packet")
		return
	}
}

func (fw *Forwarder) GatewayStats(gwstats gw.GatewayStats) {
	logrus.Debugf("received gateway stats from gateway: %s", hex.EncodeToString(gwstats.GatewayId))
}

func (fw *Forwarder) updateUplinkFrameToNetworkFormat(frame gw.UplinkFrame) (gw.UplinkFrame, error) {
	networkGatewayId := fw.gatewayStore.NetworkGatewayIdForGateway(frame.RxInfo.GatewayId)
	if networkGatewayId == nil {
		return gw.UplinkFrame{}, fmt.Errorf("gateway not found in keystore: %s", hex.EncodeToString(frame.RxInfo.GatewayId))
	}

	frame.RxInfo.GatewayId = networkGatewayId
	return frame, nil
}

func (fw *Forwarder) updateDownlinkTxAckToNetworkFormat(txack gw.DownlinkTXAck) (gw.DownlinkTXAck, error) {
	networkGatewayId := fw.gatewayStore.NetworkGatewayIdForGateway(txack.GatewayId)
	if networkGatewayId == nil {
		return gw.DownlinkTXAck{}, fmt.Errorf("gateway not found in keystore: %s", hex.EncodeToString(txack.GatewayId))
	}

	txack.GatewayId = networkGatewayId

	return txack, nil
}

func (fw *Forwarder) updateDownlinkFrameFromNetworkFormat(frame gw.DownlinkFrame) (gw.DownlinkFrame, error) {
	gatewayId := fw.gatewayStore.GatewayIdForNetworkGatewayId(frame.GatewayId)
	frame.GatewayId = gatewayId
	frame.TxInfo.GatewayId = gatewayId
	for i := range frame.Items {
		frame.Items[i].TxInfo.GatewayId = gatewayId
	}

	return frame, nil
}
