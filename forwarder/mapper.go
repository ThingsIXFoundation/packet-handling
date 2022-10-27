package forwarder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/ThingsIXFoundation/coverage-api/go/mapper"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/packet-handling/mapperpacket"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/chirpstack-api/go/v3/common"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gofrs/uuid"
	"github.com/sirupsen/logrus"
	"github.com/uber/h3-go/v4"
	"google.golang.org/protobuf/proto"
)

type MapperForwarder struct {
	gatewayStore gateway.Store
	exchange     *Exchange
}

func NewMapperForwarder(exchange *Exchange) (*MapperForwarder, error) {
	return &MapperForwarder{exchange: exchange}, nil
}

func IsMaybeMapperPacket(payload *lorawan.MACPayload) bool {
	logrus.Infof("dev_addr=%s", hex.EncodeToString(payload.FHDR.DevAddr[:]))
	return payload.FHDR.DevAddr[0] == 0x02
}

func (mc *MapperForwarder) HandleMapperPacket(frame gw.UplinkFrame, mac *lorawan.MACPayload) {
	gateway, err := mc.gatewayStore.GatewayByNetworkIDBytes(frame.RxInfo.GatewayId)
	if err != nil || gateway == nil {
		logrus.WithFields(logrus.Fields{
			"local_gateway_id": hex.EncodeToString(frame.RxInfo.GatewayId),
		}).Error("unknown gateway, dropping mapper packet")
		return
	}

	dp, _ := mapperpacket.NewDiscoveryPacketFromBytes(frame.PhyPayload)

	h := sha256.Sum256(frame.PhyPayload[0:22])
	sig := frame.PhyPayload[22:]
	sig[64] -= 27

	pkb, err := crypto.Ecrecover(h[:], sig)
	if err != nil {
		logrus.WithError(err).Error("could not recover public key from mapper signature, malformed packet?")
		return
	}

	pk, err := crypto.UnmarshalPubkey(pkb)
	if err != nil {
		logrus.WithError(err).Error("could not recover public key from mapper signature, malformed packet?")
		return
	}

	address := crypto.PubkeyToAddress(*pk)

	logrus.Infof("received packet from mapper: %s", address)

	if !crypto.VerifySignature(pkb, h[:], sig[0:64]) {
		logrus.Error("invalid mapper signature, malformed packet?")
		return
	}
	lat, lon := dp.LatLon()
	logrus.Infof("packet was mapped at: %f, %f", float64(lat)/1000000, float64(lon)/1000000)

	region := h3.LatLngToCell(dp.LatLonGeoCoordinate(), 3)
	logrus.Infof("packet is for region: %s", region)

	dpr := &mapper.DiscoveryPacketReceipt{
		Frequency:        frame.TxInfo.Frequency,
		Rssi:             frame.RxInfo.Rssi,
		LoraSnr:          frame.RxInfo.LoraSnr,
		SpreadingFactor:  frame.TxInfo.GetLoraModulationInfo().GetSpreadingFactor(),
		Bandwidth:        frame.TxInfo.GetLoraModulationInfo().Bandwidth,
		CodeRate:         frame.TxInfo.GetLoraModulationInfo().CodeRate,
		Phy:              frame.PhyPayload,
		Time:             frame.RxInfo.Time,
		GatewaySignature: []byte{},
	}

	dprb, err := proto.Marshal(dpr)
	if err != nil {
		logrus.WithError(err).Error("could not marshal packet receipt")
		return
	}
	dprbh := sha256.Sum256(dprb)
	gwsig, err := crypto.Sign(dprbh[:], gateway.PrivateKey)
	if err != nil {
		logrus.WithError(err).Error("could not sign packet receipt: error while signing packet")
		return
	}

	dpr.GatewaySignature = gwsig

	coverageClient, _ := mc.mapperClientForRegion(region)

	// Deliver in goroutine as the response will take almost 1 second in optimal case
	go func() {
		logrus.Debug("sending discovery packet")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		resp, err := coverageClient.DeliverDiscoveryPacketReceipt(ctx, dpr)
		if err != nil {
			logrus.WithError(err).Error("could not deliver packet receipt")
			return
		}
		dtr := resp.GetDownlinkTransmitRequest()
		if dtr == nil {
			logrus.Info("gateway was not selected for downlink")
			return
		} else {
			logrus.Info("gateway was selected for downlink")
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
				Modulation: common.Modulation_LORA,
				ModulationInfo: &gw.DownlinkTXInfo_LoraModulationInfo{
					LoraModulationInfo: &gw.LoRaModulationInfo{
						Bandwidth:             125,
						SpreadingFactor:       7,
						CodeRate:              "4/5",
						PolarizationInversion: true,
					},
				},
				Context: frame.RxInfo.Context,
			},
			PhyPayload: dtr.GetPhy(),
		}

		df := gw.DownlinkFrame{
			DownlinkId: uuid.Must(uuid.NewV4()).Bytes(), //
			Items:      []*gw.DownlinkFrameItem{&dfi},
			GatewayId:  frame.RxInfo.GatewayId,
			PhyPayload: dtr.GetPhy(),
		}

		dfe := router.DownlinkFrameEvent{
			DownlinkFrame: &df,
		}

		mc.exchange.handleDownlinkFrame(&dfe)
	}()
}

func (m *MapperForwarder) mapperClientForRegion(region h3.Cell) (*CoverageClient, error) {
	return NewCoverageClient()
}
