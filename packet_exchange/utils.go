package packetexchange

import (
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/brocaar/lorawan"
)

// localUplinkFrameToNetwork converts the given frame that was received from a gateway
// into a frame that can be send anto the network on behalf of the given gw.
func localUplinkFrameToNetwork(gw *Gateway, frame gw.UplinkFrame) (gw.UplinkFrame, error) {
	copy(frame.RxInfo.GatewayId, gw.NetworkID[:])
	return frame, nil
}

func localDownlinkTxAckToNetwork(gw *Gateway, txack gw.DownlinkTXAck) (gw.DownlinkTXAck, error) {
	txack.GatewayId = gw.NetworkID[:]
	return txack, nil
}

func IsMaybeMapperPacket(payload *lorawan.MACPayload) bool {
	return payload.FHDR.DevAddr[0] == 0x02
}

func GatewayIDBytesToLoraEUID(id []byte) lorawan.EUI64 {
	var lid lorawan.EUI64
	copy(lid[:], id)
	return lid
}

func networkDownlinkFrameToLocal(gw *Gateway, frame *gw.DownlinkFrame) *gw.DownlinkFrame {
	frame.GatewayId = gw.LocalID[:]
	if frame.TxInfo != nil {
		frame.TxInfo.GatewayId = gw.LocalID[:]
	}
	for i := range frame.Items {
		frame.Items[i].TxInfo.GatewayId = gw.LocalID[:]
	}
	return frame
}
