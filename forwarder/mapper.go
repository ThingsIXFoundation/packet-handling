// Copyright 2022 Stichting ThingsIX Foundation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package forwarder

import (
	"context"
	"crypto/sha256"
	"time"

	h3light "github.com/ThingsIXFoundation/h3-light"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"

	"github.com/ThingsIXFoundation/coverage-api/go/mapper"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/packet-handling/mapperpacket"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sirupsen/logrus"
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
	return payload.FHDR.DevAddr[0] == 0x02
}

func (mc *MapperForwarder) HandleMapperPacket(frame *gw.UplinkFrame, mac *lorawan.MACPayload) {
	gateway, err := mc.gatewayStore.GatewayByNetworkIDString(frame.RxInfo.GatewayId)
	if err != nil || gateway == nil {
		logrus.WithFields(logrus.Fields{
			"local_gateway_id": frame.RxInfo.GatewayId,
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
	lat, lon := dp.LatLonFloat()
	logrus.Infof("packet was mapped at: %f, %f", float64(lat)/1000000, float64(lon)/1000000)

	region := h3light.LatLonToRes0ToCell(lat, lon)
	logrus.Infof("packet is for region: %s", region)

	dpr := &mapper.DiscoveryPacketReceipt{
		Frequency:        frame.TxInfo.Frequency,
		Rssi:             frame.RxInfo.Rssi,
		LoraSnr:          float64(frame.RxInfo.Snr),
		SpreadingFactor:  frame.TxInfo.GetModulation().GetLora().GetSpreadingFactor(),
		Bandwidth:        frame.TxInfo.GetModulation().GetLora().GetBandwidth(),
		CodeRate:         frame.TxInfo.GetModulation().GetLora().GetCodeRate().String(),
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
			TxInfo: &gw.DownlinkTxInfo{
				Frequency: dtr.Frequency,
				Power:     dtr.Power,
				Timing: &gw.Timing{
					Parameters: &gw.Timing_Immediately{
						Immediately: &gw.ImmediatelyTimingInfo{},
					},
				},
				Modulation: &gw.Modulation{
					Parameters: &gw.Modulation_Lora{
						Lora: &gw.LoraModulationInfo{
							Bandwidth:             125,
							SpreadingFactor:       7,
							CodeRateLegacy:        "4/5",
							CodeRate:              gw.CodeRate_CR_4_5,
							PolarizationInversion: true,
						},
					},
				},
				Context: frame.RxInfo.Context,
			},
			PhyPayload: dtr.GetPhy(),
		}

		df := gw.DownlinkFrame{
			DownlinkId: utils.RandUint32(), //
			Items:      []*gw.DownlinkFrameItem{&dfi},
			GatewayId:  frame.RxInfo.GatewayId,
		}

		dfe := router.DownlinkFrameEvent{
			DownlinkFrame: &df,
		}

		mc.exchange.handleDownlinkFrame(&dfe)
	}()
}

func (m *MapperForwarder) mapperClientForRegion(region h3light.Cell) (*CoverageClient, error) {
	return NewCoverageClient()
}
