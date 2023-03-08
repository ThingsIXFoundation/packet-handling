// Copyright 2023 Stichting ThingsIX Foundation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
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
	"time"

	h3light "github.com/ThingsIXFoundation/h3-light"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/chirpstack/chirpstack/api/go/v4/common"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
)

func setChaindataInFrameMetadata(frame *gw.UplinkFrame, gw *gateway.Gateway, airtime time.Duration) {
	frame.RxInfo.Metadata = map[string]string{}
	metadata := frame.RxInfo.Metadata
	metadata["thingsix_gateway_id"] = gw.ID().String()
	metadata["thingsix_airtime_ms"] = fmt.Sprintf("%d", airtime.Milliseconds())

	if gw.Owner != nil {
		metadata["thingsix_owner"] = gw.Owner.String()
	}

	if gw.Details == nil {
		return
	}

	if gw.Details.Location != nil && gw.Details.Altitude != nil {
		metadata["thingsix_location_hex"] = *gw.Details.Location
		c, err := h3light.CellFromString(*gw.Details.Location)
		if err == nil {
			lat, lon := c.LatLon()
			metadata["thingsix_location_latitude"] = fmt.Sprintf("%f", lat)
			metadata["thingsix_location_longitude"] = fmt.Sprintf("%f", lon)
			metadata["thingsix_altitude"] = fmt.Sprintf("%d", gw.Details.Altitude)
			if frame.RxInfo.Location == nil {
				frame.RxInfo.Location = &common.Location{
					Source:    common.LocationSource_CONFIG,
					Latitude:  lat,
					Longitude: lon,
					Altitude:  float64(*gw.Details.Altitude),
					Accuracy:  0,
				}
			}
		}
	}

	if gw.Details.Band != nil {
		metadata["thingsix_frequency_plan"] = *gw.Details.Band
	}

	if gw.Details.AntennaGain != nil {
		metadata["thingsix_antenna_gain"] = *gw.Details.AntennaGain
	}
}
