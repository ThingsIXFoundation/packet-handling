package forwarder

import (
	"encoding/base64"
	"encoding/hex"

	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend"
	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend/events"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/packet-handling/gateway/file"
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
	gws, err := file.NewFileGatewayStore("gateways.keys")
	if err != nil {
		return nil, err
	}

	fw := &Forwarder{
		backend:        backend,
		onlineGateways: mapset.New[lorawan.EUI64](),
		routerEvents:   make(chan *routerEvent, 4096),
		gatewayStore:   gws,
		mapperClient:   nil,
	}

	rp, err := NewRouterPool(fw)
	if err != nil {
		return nil, err
	}

	fw.routerPool = rp

	mf, err := NewMapperForwarder(fw, fw.gatewayStore)
	if err != nil {
		return nil, err
	}

	fw.mapperClient = mf

	fw.setup()

	return fw, nil
}

// Start the forwarder in the background. This waits for received router events
// and forwards them to to correct gateway. Events coming from gateways are
// received by the backend that publishes them through the forwader that calls
// the callbacks on the router client that sends the event to the router.
func (fw *Forwarder) Start() {
	// TODO Fix handling err
	go fw.routerPool.Start()
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
		"gateway":  hex.EncodeToString(frame.RxInfo.GatewayId),
		"rssi":     frame.RxInfo.Rssi,
		"snr":      frame.RxInfo.LoraSnr,
		"freq":     frame.TxInfo.Frequency,
		"sf":       frame.TxInfo.GetLoraModulationInfo().GetSpreadingFactor(),
		"pol":      frame.TxInfo.GetLoraModulationInfo().GetPolarizationInversion(),
		"coderate": frame.TxInfo.GetLoraModulationInfo().GetCodeRate(),
		"payload":  base64.RawStdEncoding.EncodeToString(frame.PhyPayload),
	}).Info("received uplink from gateway")

	gw, err := fw.gatewayStore.GatewayByLocalID(frame.RxInfo.GatewayId)
	if err != nil {
		logrus.WithError(err).Errorf("could not lookup gateway")
		return
	} else if gw == nil {
		logrus.WithFields(logrus.Fields{
			"gateway": hex.EncodeToString(frame.RxInfo.GatewayId),
		}).Errorf("could not find gateway in store")
		return
	}

	frame, err = fw.updateUplinkFrameToNetworkFormat(gw, frame)
	if err != nil {
		logrus.WithError(err).Errorf("could update uplink frame to network format")
		return
	}

	err = phy.UnmarshalBinary(frame.PhyPayload)
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
			router.client.DeliverDataUp(gw, frame)
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
			router.client.DeliverJoin(gw, frame)
		}
	}
}

func (fw *Forwarder) DownlinkTxAck(txack gw.DownlinkTXAck) {
	logrus.WithFields(logrus.Fields{
		"gateway":     hex.EncodeToString(txack.GatewayId),
		"downlink_id": txack.DownlinkId,
	}).Info("received txack from gateway")

	gw, err := fw.gatewayStore.GatewayByLocalID(txack.GatewayId)
	if err != nil {
		logrus.WithError(err).Errorf("could not lookup gateway")
		return
	} else if gw == nil {
		logrus.WithFields(logrus.Fields{
			"gateway": hex.EncodeToString(txack.GatewayId),
		}).Errorf("could not find gateway in store")
		return
	}

	txack, err = fw.updateDownlinkTxAckToNetworkFormat(gw, txack)
	if err != nil {
		logrus.WithError(err).Errorf("could update txack to network format")
		return
	}

	// TODO: Lookup downlink router from cache based on id and deliver to right router

}

func (fw *Forwarder) SubscribeEvent(event events.Subscribe) {
	gw, err := fw.gatewayStore.GatewayByLocalID(event.GatewayID[:])
	if err != nil {
		logrus.WithError(err).Errorf("could not lookup gateway")
		return
	} else if gw == nil {
		logrus.WithFields(logrus.Fields{
			"gateway": hex.EncodeToString(event.GatewayID[:]),
		}).Errorf("could not find gateway in store")
		return
	}

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
	for routerAddress, router := range routers {
		logrus.WithFields(logrus.Fields{
			"router":             routerAddress,
			"gateway-local-id":   gw.LocalGatewayID,
			"gateway-network-id": gw.NetworkGatewayID,
			"online":             event.Subscribe,
		}).Info("develivering status to router")
		router.client.DeliverGatewayStatus(gw, event.Subscribe)
	}

}

func (fw *Forwarder) SendDownlinkFrameFromRouter(router *Router, frame gw.DownlinkFrame) {
	logrus.Infof("frame: %s", frame.GatewayId)
	logrus.WithFields(logrus.Fields{
		"gateway": hex.EncodeToString(frame.GatewayId),
		"payload": base64.RawStdEncoding.EncodeToString(frame.PhyPayload),
		"router":  router.id,
	}).Info("received downlink from router for gateway")

	gw, err := fw.gatewayStore.GatewayByNetworkID(frame.GatewayId)
	if err != nil {
		logrus.WithError(err).Errorf("could not lookup gateway")
		return
	} else if gw == nil {
		logrus.WithFields(logrus.Fields{
			"gateway": hex.EncodeToString(frame.GatewayId),
		}).Errorf("could not find gateway in store")
		return
	}

	frame, err = fw.updateDownlinkFrameFromNetworkFormat(gw, frame)
	if err != nil {
		logrus.WithError(err).Error("could not update downlink frame, dropping packet")
		return
	}

	// TODO: Keep track of router so we know to what router to send dowlink ack

	err = fw.backend.SendDownlinkFrame(frame)
	if err != nil {
		logrus.WithError(err).Error("could not send downlink frame, dropping packet")
		return
	}
}

func (fw *Forwarder) SendDownlinkFrame(frame gw.DownlinkFrame) {
	logrus.WithFields(logrus.Fields{
		"gateway":       hex.EncodeToString(frame.GatewayId),
		"frequency":     frame.Items[0].GetTxInfo().Frequency,
		"pol":           frame.Items[0].GetTxInfo().GetLoraModulationInfo().PolarizationInversion,
		"payload":       base64.RawStdEncoding.EncodeToString(frame.PhyPayload),
		"payload (hex)": hex.EncodeToString(frame.PhyPayload),
	}).Info("received downlink for gateway")

	gw, err := fw.gatewayStore.GatewayByNetworkID(frame.GatewayId)
	if err != nil {
		logrus.WithError(err).Errorf("could not lookup gateway")
		return
	} else if gw == nil {
		logrus.WithFields(logrus.Fields{
			"gateway": hex.EncodeToString(frame.GatewayId),
		}).Errorf("could not find gateway in store")
		return
	}

	frame, err = fw.updateDownlinkFrameFromNetworkFormat(gw, frame)
	if err != nil {
		logrus.WithError(err).Error("could not update downlink frame, dropping packet")
		return
	}

	err = fw.backend.SendDownlinkFrame(frame)
	if err != nil {
		logrus.WithError(err).Error("could not send downlink frame, dropping packet")
		return
	}
}

func (fw *Forwarder) GatewayStats(gwstats gw.GatewayStats) {
	logrus.Debugf("received gateway stats from gateway: %s", hex.EncodeToString(gwstats.GatewayId))
}

func (fw *Forwarder) updateUplinkFrameToNetworkFormat(gw *gateway.Gateway, frame gw.UplinkFrame) (gw.UplinkFrame, error) {
	frame.RxInfo.GatewayId = gw.NetworkGatewayID.Bytes()
	return frame, nil
}

func (fw *Forwarder) updateDownlinkTxAckToNetworkFormat(gw *gateway.Gateway, txack gw.DownlinkTXAck) (gw.DownlinkTXAck, error) {
	txack.GatewayId = gw.NetworkGatewayID.Bytes()
	return txack, nil
}

func (fw *Forwarder) updateDownlinkFrameFromNetworkFormat(gw *gateway.Gateway, frame gw.DownlinkFrame) (gw.DownlinkFrame, error) {
	gatewayID := gw.LocalGatewayID.Bytes()
	frame.GatewayId = gatewayID
	if frame.TxInfo != nil {
		frame.TxInfo.GatewayId = gatewayID
	}
	for i := range frame.Items {
		frame.Items[i].TxInfo.GatewayId = gatewayID
	}

	return frame, nil
}
