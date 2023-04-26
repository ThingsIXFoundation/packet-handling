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

	gnsssystemtime "github.com/ThingsIXFoundation/gnss-system-time"
	h3light "github.com/ThingsIXFoundation/h3-light"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/ThingsIXFoundation/types"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/ThingsIXFoundation/coverage-api/go/mapper"
	"github.com/ThingsIXFoundation/packet-handling/mapperpacket"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type MapperForwarder struct {
	gatewayStore      gateway.GatewayStore
	exchange          *Exchange
	mapperRegionCache *lru.Cache[string, string]
	coverageClient    *CoverageClient
}

func NewMapperForwarder(cfg *Config, exchange *Exchange, gatewayStore gateway.GatewayStore) (*MapperForwarder, error) {
	mapperRegionCache, err := lru.New[string, string](64)
	if err != nil {
		return nil, err
	}
	coverageClient, err := NewCoverageClient(cfg)
	if err != nil {
		return nil, err
	}

	return &MapperForwarder{exchange: exchange, gatewayStore: gatewayStore, mapperRegionCache: mapperRegionCache, coverageClient: coverageClient}, nil
}

func IsMaybeMapperPacket(frame *gw.UplinkFrame, payload *lorawan.MACPayload) bool {
	if frame.TxInfo.GetModulation().GetLora().GetSpreadingFactor() != 7 {
		return false
	}

	if frame.TxInfo.GetModulation().GetLora().GetBandwidth() != 125000 {
		return false
	}

	if payload.FPort == nil {
		return false
	}

	if *payload.FPort != 0x01 && *payload.FPort != 0x02 {
		return false
	}

	if payload.FHDR.DevAddr[0] != 0x02 {
		return false
	}

	return true
}

func (mc *MapperForwarder) handleDiscoveryPacket(frame *gw.UplinkFrame, mac *lorawan.MACPayload) {
	gateway, err := mc.gatewayStore.ByNetworkIDString(frame.RxInfo.GatewayId)
	if err != nil || gateway == nil {
		logrus.WithFields(logrus.Fields{
			"network_gateway_id": frame.RxInfo.GatewayId,
		}).Error("unknown gateway, dropping mapper packet")
		return

	}
	frameLog := logrus.WithFields(logrus.Fields{
		"gw_local_id":   gateway.LocalID,
		"gw_network_id": gateway.NetworkID,
		"rssi":          frame.GetRxInfo().GetRssi(),
		"snr":           frame.GetRxInfo().GetSnr(),
		"freq":          frame.GetTxInfo().GetFrequency(),
		"sf":            frame.GetTxInfo().GetModulation().GetLora().GetSpreadingFactor(),
		"pol":           frame.GetTxInfo().GetModulation().GetLora().GetPolarizationInversion(),
		"coderate":      frame.GetTxInfo().GetModulation().GetLora().GetCodeRate(),
		"bandwidth":     frame.GetTxInfo().GetModulation().GetLora().GetBandwidth(),
	})

	dp, _ := mapperpacket.NewDiscoveryPacketFromBytes(frame.PhyPayload)

	h := sha256.Sum256(frame.PhyPayload[0:22])
	sig := frame.PhyPayload[22:]
	sig[64] -= 27

	pkb, err := crypto.Ecrecover(h[:], sig)
	if err != nil {
		frameLog.WithError(err).Error("could not recover public key from mapper signature, malformed packet?")
		return
	}

	pk, err := crypto.UnmarshalPubkey(pkb)
	if err != nil {
		frameLog.WithError(err).Error("could not recover public key from mapper signature, malformed packet?")
		return
	}

	mapperAddress := crypto.PubkeyToAddress(*pk)
	mapperID := types.ID(utils.DeriveThingsIxID(pk))

	frameLog = frameLog.WithFields(logrus.Fields{
		"mapper_id": mapperID,
	})

	if !crypto.VerifySignature(pkb, h[:], sig[0:64]) {
		frameLog.Error("invalid mapper signature, malformed packet?")
		return
	}
	lat, lon := dp.LatLonFloat()
	mapTime := gnsssystemtime.GalileoTowToTime(dp.TOW(), time.Now().Add(1*time.Minute), 18)
	region := h3light.LatLonToCell(lat, lon, 1)

	frameLog = frameLog.WithFields(logrus.Fields{
		"latitude":      lat,
		"longitude":     lon,
		"region":        region,
		"map_time":      mapTime,
		"map_time_diff": time.Since(mapTime),
	})

	frameLog.Info("received mapper discovery packet, signing and delivering to coverage-mapping-service")

	mc.mapperRegionCache.Add(mapperAddress.String(), region.String())

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
		frameLog.WithError(err).Error("could not marshal packet receipt")
		return
	}
	dprbh := sha256.Sum256(dprb)
	gwsig, err := crypto.Sign(dprbh[:], gateway.PrivateKey)
	if err != nil {
		frameLog.WithError(err).Error("could not sign packet receipt: error while signing packet")
		return
	}

	dpr.GatewaySignature = gwsig

	// Deliver in goroutine as the response will take almost 1 second in optimal case
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		resp, err := mc.coverageClient.DeliverDiscoveryPacketReceipt(ctx, region, dpr)
		if err != nil {
			frameLog.WithError(err).Error("could not deliver packet receipt")
			return
		}
		dtr := resp.GetDownlinkTransmitRequest()
		if dtr == nil {
			frameLog.Info("gateway was not selected for downlink")
			return
		} else {
			frameLog.Info("gateway was selected as winner!")
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
							Bandwidth:             dtr.Bandwidth,
							SpreadingFactor:       dtr.SpreadingFactor,
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

		downlinkLegacyId := uuid.New()

		df := gw.DownlinkFrame{
			DownlinkId:       utils.RandUint32(), //
			DownlinkIdLegacy: downlinkLegacyId[:],
			Items:            []*gw.DownlinkFrameItem{&dfi},
			GatewayId:        frame.RxInfo.GatewayId,
		}

		dfe := router.DownlinkFrameEvent{
			DownlinkFrame: &df,
		}

		mc.exchange.handleDownlinkFrame(&dfe)
	}()
}

func (mc *MapperForwarder) handleDownlinkConfirmationPacket(frame *gw.UplinkFrame, mac *lorawan.MACPayload) {
	gateway, err := mc.gatewayStore.ByNetworkIDString(frame.RxInfo.GatewayId)
	if err != nil || gateway == nil {
		logrus.WithFields(logrus.Fields{
			"network_gateway_id": frame.RxInfo.GatewayId,
		}).Error("unknown gateway, dropping mapper packet")
		return
	}

	logrus.Info("Received downlink confirmation message")

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

	regionStr, ok := mc.mapperRegionCache.Get(address.String())
	if !ok {
		logrus.Warn("dropping packet because we didn't see the discovery packet before")
		return
	}

	region, err := h3light.CellFromString(regionStr)
	if err != nil {
		logrus.WithError(err).Error("error decoding region string")
		return
	}

	dcpr := &mapper.DownlinkConfirmationPacketReceipt{
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

	dprb, err := proto.Marshal(dcpr)
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

	dcpr.GatewaySignature = gwsig

	go func() {
		logrus.Debug("sending downlink confirmation packet")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := mc.coverageClient.DeliverDownlinkConfirmationPacketReceipt(ctx, region, dcpr)
		if err != nil {
			logrus.WithError(err).Error("could not deliver packet receipt")
			return
		}
	}()
}

func (mc *MapperForwarder) HandleMapperPacket(frame *gw.UplinkFrame, mac *lorawan.MACPayload) {
	if *mac.FPort == 0x01 {
		mc.handleDiscoveryPacket(frame, mac)
	} else if *mac.FPort == 0x02 {
		mc.handleDownlinkConfirmationPacket(frame, mac)
	}
}

func (mc *MapperForwarder) Run(ctx context.Context) {
	mc.coverageClient.Run(ctx)
}
