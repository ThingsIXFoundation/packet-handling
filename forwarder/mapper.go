package forwarder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ThingsIXFoundation/coverage-api/go/mapper"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang/protobuf/proto"
	"github.com/sirupsen/logrus"
	"github.com/uber/h3-go"
)

type MapperForwarder struct {
	gatewayStore gateway.GatewayStore
	forwarder    *Forwarder
}

func NewMapperForwarder(forwarder *Forwarder) (*MapperForwarder, error) {
	return &MapperForwarder{forwarder: forwarder}, nil
}

func IsMaybeMapperPacket(payload *lorawan.MACPayload) bool {
	// Check if devaddr matches mapper
	return false
}

func (mc *MapperForwarder) HandleMapperPacket(frame gw.UplinkFrame, mac *lorawan.MACPayload) {
	macb, err := mac.MarshalBinary()
	if err != nil {
		logrus.WithError(err).Error("could not marshal mapper packet")
		return
	}

	h := sha256.Sum256(macb[0:16])
	sig := macb[16:]

	pk, err := crypto.Ecrecover(h[:], sig)
	if err != nil {
		logrus.WithError(err).Error("could not recover public key from mapper signature, malformed packet?")
		return
	}

	if !crypto.VerifySignature(pk, h[:], sig) {
		logrus.Error("invalid mapper signature, malformed packet?")
		return
	}

	region, err := regionForPacket(macb)
	if err != nil {
		logrus.WithError(err).Error("could not retrieve location from packet, malformed packet?")
		return
	}

	dpr := &mapper.DiscoveryPacketReceipt{
		Frequency:       frame.TxInfo.Frequency,
		Rssi:            frame.RxInfo.Rssi,
		LoraSnr:         frame.RxInfo.LoraSnr,
		SpreadingFactor: frame.TxInfo.GetLoraModulationInfo().GetSpreadingFactor(),
		Bandwidth:       frame.TxInfo.GetLoraModulationInfo().Bandwidth,
		CodeRate:        frame.TxInfo.GetLoraModulationInfo().CodeRate,
		Phy:             frame.PhyPayload,
		Time:            frame.RxInfo.Time,
	}

	dprb, err := proto.Marshal(dpr)
	if err != nil {
		logrus.WithError(err).Error("could not marshal packet receipt")
		return
	}
	dprbh := sha256.Sum256(dprb)
	gwkey := mc.gatewayStore.PrivateKeyForGateway(frame.RxInfo.GatewayId)
	if gwkey == nil {
		logrus.Errorf("could not sign packet receipt gateway with id: %s not found in store", hex.EncodeToString(frame.RxInfo.GatewayId))
		return
	}
	gwsig, err := crypto.Sign(dprbh[:], gwkey)
	if err != nil {
		logrus.WithError(err).Error("could not sign packet receipt: error while signing packet")
		return
	}

	dpr.GatewaySignature = gwsig

	coverageClient, err := mapperClientForRegion(region)

	// Deliver in goroutine as the response will take almost 1 second in optimal case
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		resp, err := coverageClient.DeliverDiscoveryPacketReceipt(ctx, dpr)
		cancel()
		if err != nil {
			logrus.WithError(err).Error("could not deliver packet receipt")
			return
		}
		dtr := resp.GetDownlinkTransmitRequest()
		if dtr == nil {
			logrus.Info("gateway was not selected for downlink")
			return
		}

		dfi := gw.DownlinkFrameItem{
			TxInfo: &gw.DownlinkTXInfo{
				GatewayId: frame.RxInfo.GatewayId,
				Frequency: dtr.Frequency,
				Power:     dtr.Power,
				Timing:    gw.DownlinkTiming_IMMEDIATELY,
				TimingInfo: &gw.DownlinkTXInfo_ImmediatelyTimingInfo{
					ImmediatelyTimingInfo: &gw.ImmediatelyTimingInfo{},
				},
				Context: frame.RxInfo.Context,
			},
		}

		df := gw.DownlinkFrame{
			DownlinkId: nil, //
			Items:      []*gw.DownlinkFrameItem{&dfi},
			GatewayId:  frame.RxInfo.GatewayId,
		}

		mc.forwarder.SendDownlinkFrame(df)

	}()

}

func regionForPacket(macb []byte) (h3.H3Index, error) {
	return 0, fmt.Errorf("not yet implemented")
}

func mapperClientForRegion(region h3.H3Index) (mapper.CoverageV1Client, error) {
	return nil, nil
}
