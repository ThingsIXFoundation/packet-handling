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
	"fmt"
	"strings"

	"github.com/ThingsIXFoundation/frequency-plan/go/frequency_plan"
	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend/basicstation"
	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend/semtechudp"
	chirpconfig "github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/config"
	"github.com/brocaar/lorawan/band"
	"github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"
)

// buildBackend returns the forwarder Chirpstack backend that was configured
// in the given cfg. Or an error in case of missing configuration or invalid
// configuration.
func buildBackend(cfg *Config) (Backend, error) {
	switch {
	case cfg.Forwarder.Backend.BasicStation != nil && cfg.Forwarder.Backend.BasicStation.Region != "":
		return buildBasicStationBackend(cfg)
	case cfg.Forwarder.Backend.SemtechUDP != nil:
		return buildSemtechUDPBackend(cfg)
	case cfg.Forwarder.Backend.Concentratord != nil:
		return nil, fmt.Errorf("backend concentratord not supported")
	default:
		return nil, fmt.Errorf("invalid backend configuration")
	}
}

// buildSemtechUDPBackend return the Chirpstack UDP backend implementation
// based on the given cfg.
func buildSemtechUDPBackend(cfg *Config) (*semtechudp.Backend, error) {
	var (
		chirpCfg   chirpconfig.Config
		udpBind    = "0.0.0.0:1680" // default
		fakeRxTime = false          // default
	)

	if cfg.Forwarder.Backend.SemtechUDP.UDPBind != nil {
		udpBind = *cfg.Forwarder.Backend.SemtechUDP.UDPBind
	}

	if cfg.Forwarder.Backend.SemtechUDP.FakeRxTime != nil {
		fakeRxTime = *cfg.Forwarder.Backend.SemtechUDP.FakeRxTime
	}

	chirpCfg.Backend.Type = "semtech_udp"
	chirpCfg.Backend.SemtechUDP.UDPBind = udpBind
	chirpCfg.Backend.SemtechUDP.FakeRxTime = fakeRxTime

	logrus.WithFields(logrus.Fields{
		"udp_bind":     chirpCfg.Backend.SemtechUDP.UDPBind,
		"fake_rx_time": chirpCfg.Backend.SemtechUDP.FakeRxTime,
	}).Info("Semtech UDP backend")

	backend, err := semtechudp.NewBackend(chirpCfg)
	if err != nil {
		return nil, fmt.Errorf("unable to instantiate semtech backend: %w", err)
	}
	return backend, nil
}

// buildBasicStationBackend returns the Chirpstack basic station backend
// implementation based on the given cfg.
func buildBasicStationBackend(cfg *Config) (*basicstation.Backend, error) {
	b, err := frequency_plan.GetBand(strings.ToUpper(cfg.Forwarder.Backend.BasicStation.Region))
	if err != nil {
		return nil, fmt.Errorf("unsupported region %s", cfg.Forwarder.Backend.BasicStation.Region)
	}

	var chirpCfg chirpconfig.Config
	chirpCfg.Backend.Type = "basic_station"
	chirpCfg.Backend.BasicStation.Region = b.Name()
	chirpCfg.Backend.BasicStation.CACert = *cfg.Forwarder.Backend.BasicStation.CACert
	chirpCfg.Backend.BasicStation.TLSCert = *cfg.Forwarder.Backend.BasicStation.TLSCert
	chirpCfg.Backend.BasicStation.TLSKey = *cfg.Forwarder.Backend.BasicStation.TLSKey
	chirpCfg.Backend.BasicStation.StatsInterval = *cfg.Forwarder.Backend.BasicStation.StatsInterval
	chirpCfg.Backend.BasicStation.PingInterval = *cfg.Forwarder.Backend.BasicStation.PingInterval
	chirpCfg.Backend.BasicStation.TimesyncInterval = *cfg.Forwarder.Backend.BasicStation.TimesyncInterval
	chirpCfg.Backend.BasicStation.ReadTimeout = *cfg.Forwarder.Backend.BasicStation.ReadTimeout
	chirpCfg.Backend.BasicStation.WriteTimeout = *cfg.Forwarder.Backend.BasicStation.WriteTimeout

	loadBasicStationRegionConfigUplink(b, &chirpCfg)
	loadBasicStationRegionConfigDownlink(b, &chirpCfg)

	logrus.WithFields(logrus.Fields{
		"bind":   chirpCfg.Backend.BasicStation.Bind,
		"region": chirpCfg.Backend.BasicStation.Region,
	}).Info("Basic Station backend")

	backend, err := basicstation.NewBackend(chirpCfg)
	if err != nil {
		return nil, fmt.Errorf("unable to instantiate basic station backend: %w", err)
	}
	return backend, nil
}

func loadBasicStationRegionConfigUplink(b band.Band, cfg *chirpconfig.Config) {
	var concentrator chirpconfig.BasicStationConcentrator

	cfg.Backend.BasicStation.FrequencyMin = ^uint32(0)
	cfg.Backend.BasicStation.FrequencyMax = uint32(0)

	for _, channelIndex := range b.GetEnabledUplinkChannelIndices() {
		channel, err := b.GetUplinkChannel(channelIndex)
		if err != nil {
			logrus.Fatal(err)
		}

		minDr, err := b.GetDataRate(channel.MinDR)
		if err != nil {
			logrus.Fatal(err)
		}
		maxDr, err := b.GetDataRate(channel.MaxDR)
		if err != nil {
			logrus.Fatal(err)
		}
		if maxDr.Modulation == band.LRFHSSModulation {
			for dr := channel.MaxDR; dr > 0; dr-- {
				maxDr, err = b.GetDataRate(dr)
				if err != nil {
					logrus.Fatal(err)
				}

				if maxDr.Modulation == band.LoRaModulation {
					break
				}
			}
		}

		if channel.Frequency < cfg.Backend.BasicStation.FrequencyMin {
			cfg.Backend.BasicStation.FrequencyMin = channel.Frequency
		}
		if channel.Frequency > cfg.Backend.BasicStation.FrequencyMax {
			cfg.Backend.BasicStation.FrequencyMax = channel.Frequency
		}

		if minDr != maxDr {
			concentrator.MultiSF.Frequencies = append(concentrator.MultiSF.Frequencies, channel.Frequency)
		} else if minDr == maxDr && minDr.Modulation == band.LoRaModulation {
			concentrator.LoRaSTD.Bandwidth = uint32(minDr.Bandwidth)
			concentrator.LoRaSTD.Frequency = channel.Frequency
			concentrator.LoRaSTD.SpreadingFactor = uint32(minDr.SpreadFactor)
		} else if minDr == maxDr && minDr.Modulation == band.FSKModulation {
			concentrator.FSK.Frequency = channel.Frequency
		}
	}
	cfg.Backend.BasicStation.Concentrators = append(cfg.Backend.BasicStation.Concentrators, concentrator)
}

func loadBasicStationRegionConfigDownlink(b band.Band, cfg *chirpconfig.Config) {
	var reportedIndexes []int
	for _, channelIndex := range b.GetEnabledUplinkChannelIndices() {
		rx1ChannelIndex, err := b.GetRX1ChannelIndexForUplinkChannelIndex(channelIndex)
		if err != nil {
			logrus.Fatal(err)
		}
		if slices.Contains(reportedIndexes, rx1ChannelIndex) {
			continue
		}

		reportedIndexes = append(reportedIndexes, rx1ChannelIndex)

		channel, err := b.GetDownlinkChannel(rx1ChannelIndex)
		if err != nil {
			logrus.Fatal(err)
		}

		maxDr, err := b.GetDataRate(channel.MaxDR)
		if err != nil {
			logrus.Fatal(err)
		}
		if maxDr.Modulation == band.LRFHSSModulation {
			for dr := channel.MaxDR; dr > 0; dr-- {
				maxDr, err = b.GetDataRate(dr)
				if err != nil {
					logrus.Fatal(err)
				}

				if maxDr.Modulation == band.LoRaModulation {
					break
				}
			}
		}

		if channel.Frequency < cfg.Backend.BasicStation.FrequencyMin {
			cfg.Backend.BasicStation.FrequencyMin = channel.Frequency
		}
		if channel.Frequency > cfg.Backend.BasicStation.FrequencyMax {
			cfg.Backend.BasicStation.FrequencyMax = channel.Frequency
		}
	}

	if b.GetDefaults().RX2Frequency < cfg.Backend.BasicStation.FrequencyMin {
		cfg.Backend.BasicStation.FrequencyMin = b.GetDefaults().RX2Frequency
	}
	if b.GetDefaults().RX2Frequency > cfg.Backend.BasicStation.FrequencyMax {
		cfg.Backend.BasicStation.FrequencyMax = b.GetDefaults().RX2Frequency
	}
}
