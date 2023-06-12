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

package mapperpacket

import (
	"log"
	"testing"
)

func TestDiscoveryPacket_LatLon(t *testing.T) {
	tests := []struct {
		name string
		dp   *DiscoveryPacket
		lat  int32
		lon  int32
	}{
		{
			name: "East of Greenwich",
			dp:   MustNewDiscoveryPacketFromBytesFromString("QH+4RAIABVcBAyy5csnYp4AHVAIWNMgpO8eufYA4zIok4W102uksiSPakfMYqMCVf2llaXLDY5nuGJdYjO/1dBbVOytY4yj8yss2bkCItqTQY+FgmOMA"),
			lat:  53262706,
			lon:  -6194345,
		},
		{
			name: "East of Greenwich 2",
			dp:   MustNewDiscoveryPacketFromBytesFromString("QH+4RAIABzUBAyy3S8nY2zgHFAI66zYoTIEv7gHpwHbAd8V22+2EUoRFNcO7GQdnqh9Fi15XQvruPIhurSuVptvApNNRLXaXwB1Kn+yd9Q1RelxCn8IB"),
			lat:  53262155,
			lon:  -6192690,
		},
		{
			name: "Normal",
			dp:   MustNewDiscoveryPacketFromBytesFromString("QHYQdAIAAGYBAxrDFAIP2RgAFAG1uiVjyKFgoqhyxIegCyJhR0G25t7MuKD7fBaLLZ36XpCTEKrGPOcfw7riKlNdQGaJefJkybhFQpB2zKQXEt4r8hoA"),
			lat:  52085524,
			lon:  4324131},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dp := tt.dp
			gotLat, gotLon := dp.LatLon()
			if gotLat != tt.lat {
				t.Errorf("DiscoveryPacket.LatLon() got = %v, want %v", gotLat, tt.lat)
			}
			if gotLon != tt.lon {
				t.Errorf("DiscoveryPacket.LatLon() got1 = %v, want %v", gotLon, tt.lon)
			}
		})
	}
}

func TestLon(t *testing.T) {
	t.SkipNow()
	len := 29
	for i := -180_000_000; i <= 180_000_000; i = i + 10 {
		measure := i * 10
		min := (^0)
		umask := uint32(min)
		mask := umask >> (32 - len)
		transmit := (uint32(measure) / uint32(10)) & mask
		r1 := transmit * 10
		restore := int32(r1)
		diff := measure - int(restore)
		if diff < 0 {
			diff = -1 * diff
		}

		log.Printf("%d: transmit: %032b, mask: %032b, restore: %032b, diff: %d", i, transmit, mask, restore, diff)

		if diff > 10 {
			t.Fatalf("%d != %d", measure, restore)
			t.FailNow()
		}

	}
}

func TestDiscoveryPacket_LatLonFloat(t *testing.T) {
	tests := []struct {
		name string
		dp   *DiscoveryPacket
		lat  float64
		lon  float64
	}{
		{
			name: "East of Greenwich",
			dp:   MustNewDiscoveryPacketFromBytesFromString("QH+4RAIABVcBAyy5csnYp4AHVAIWNMgpO8eufYA4zIok4W102uksiSPakfMYqMCVf2llaXLDY5nuGJdYjO/1dBbVOytY4yj8yss2bkCItqTQY+FgmOMA"),
			lat:  53.262706,
			lon:  -6.194345,
		},
		{
			name: "East of Greenwich 2",
			dp:   MustNewDiscoveryPacketFromBytesFromString("QH+4RAIABzUBAyy3S8nY2zgHFAI66zYoTIEv7gHpwHbAd8V22+2EUoRFNcO7GQdnqh9Fi15XQvruPIhurSuVptvApNNRLXaXwB1Kn+yd9Q1RelxCn8IB"),
			lat:  53.262155,
			lon:  -6.19269,
		},
		{name: "Normal",
			dp:  MustNewDiscoveryPacketFromBytesFromString("QHYQdAIAAGYBAxrDFAIP2RgAFAG1uiVjyKFgoqhyxIegCyJhR0G25t7MuKD7fBaLLZ36XpCTEKrGPOcfw7riKlNdQGaJefJkybhFQpB2zKQXEt4r8hoA"),
			lat: 52.085524,
			lon: 4.324131},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dp := tt.dp
			gotLat, gotLon := dp.LatLonFloat()
			if gotLat != tt.lat {
				t.Errorf("DiscoveryPacket.LatLon() got = %v, want %v", gotLat, tt.lat)
			}
			if gotLon != tt.lon {
				t.Errorf("DiscoveryPacket.LatLon() got1 = %v, want %v", gotLon, tt.lon)
			}
		})
	}
}
