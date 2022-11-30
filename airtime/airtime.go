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

package airtime

import (
	"fmt"
	"time"

	"github.com/brocaar/lorawan/airtime"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
)

func UplinkAirtime(frame *gw.UplinkFrame) (time.Duration, error) {
	payload := len(frame.PhyPayload)
	lora := frame.GetTxInfo().GetModulation().GetLora()
	if lora == nil {
		return 0, fmt.Errorf("packet is not LoRa, cannot calculate airtime")
	}
	sf := int(lora.GetSpreadingFactor())
	bandwidth := int(lora.GetBandwidth() / 1000)
	preamble := 8 // always 8 for LoRaWAN
	var codingrate airtime.CodingRate
	switch lora.CodeRate {
	case gw.CodeRate_CR_4_5:
		codingrate = airtime.CodingRate45
	case gw.CodeRate_CR_4_6:
		codingrate = airtime.CodingRate46
	case gw.CodeRate_CR_4_7:
		codingrate = airtime.CodingRate47
	default:
		return 0, fmt.Errorf("invalid code rate: %s", lora.CodeRate)
	}
	headerEnabled := true
	lowDataRateOptimization := (sf >= 11)

	return airtime.CalculateLoRaAirtime(payload, sf, bandwidth, preamble, codingrate, headerEnabled, lowDataRateOptimization)
}

func DownlinkAirtime(frame *gw.DownlinkFrame) (time.Duration, error) {
	payload := len(frame.Items[0].GetPhyPayload())
	lora := frame.Items[0].GetTxInfo().GetModulation().GetLora()
	if lora == nil {
		return 0, fmt.Errorf("packet is not LoRa, cannot calculate airtime")
	}
	sf := int(lora.GetSpreadingFactor())
	bandwidth := int(lora.GetBandwidth() / 1000)
	preamble := 8 // always 8 for LoRaWAN
	var codingrate airtime.CodingRate
	switch lora.CodeRate {
	case gw.CodeRate_CR_4_5:
		codingrate = airtime.CodingRate45
	case gw.CodeRate_CR_4_6:
		codingrate = airtime.CodingRate46
	case gw.CodeRate_CR_4_7:
		codingrate = airtime.CodingRate47
	default:
		return 0, fmt.Errorf("invalid code rate: %s", lora.CodeRate)
	}
	headerEnabled := true
	lowDataRateOptimization := (sf >= 11)

	airtime, err := airtime.CalculateLoRaAirtime(payload, sf, bandwidth, preamble, codingrate, headerEnabled, lowDataRateOptimization)
	if err != nil {
		return 0, err
	}

	return airtime * 8, nil // For downlinks airtime is x 8 because all 8 channels of a gateway are claimed during transmission
}
