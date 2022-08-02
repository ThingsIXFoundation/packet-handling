package forwarder

import (
	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/brocaar/lorawan"
	"github.com/sirupsen/logrus"
)

type Forwarder struct {
	backend    backend.Backend
	routerPool RouterPool
}

func NewForwarder(backend backend.Backend) (*Forwarder, error) {
	fw := &Forwarder{
		backend: backend,
	}

	fw.setup()

	return fw, nil
}

func (fw *Forwarder) setup() error {
	fw.backend.SetUplinkFrameFunc(fw.UplinkFrame)
	fw.backend.SetDownlinkTxAckFunc(fw.DownlinkTxAck)

	fw.backend.SetSubscribeEventFunc(nil)
	fw.backend.SetGatewayStatsFunc(nil)
	fw.backend.SetRawPacketForwarderEventFunc(nil)

	return nil
}

func (fw *Forwarder) UplinkFrame(frame gw.UplinkFrame) {
	var phy lorawan.PHYPayload

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

		routers, err := fw.routerPool.GetRoutersForDataUp(mac.FHDR.DevAddr)
		if err != nil {
			logrus.WithError(err).Error("error while getting routers for uplink, dropping packet")
			return
		}

		for _, router := range routers {
			router.DeliverDataUp(frame)
		}
	} else if phy.MHDR.MType == lorawan.JoinRequest || phy.MHDR.MType == lorawan.RejoinRequest {
		// Filter by Xor16 filter on
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

		for _, router := range routers {
			router.DeliverJoin(frame)
		}
	}
}

func (fw *Forwarder) DownlinkTxAck(txack gw.DownlinkTXAck) {

}
