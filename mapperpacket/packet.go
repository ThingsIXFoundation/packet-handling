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
	"fmt"

	"github.com/ThingsIXFoundation/bitoffset"
	"github.com/brocaar/lorawan"
	"github.com/uber/h3-go/v4"
)

type MapperPacket struct {
	b []byte
}

func (mp MapperPacket) SetFType(ftype uint8) {
	bitoffset.SetUint8(mp.b, 0, 3, ftype)
}

func (mp MapperPacket) SetDevAddr(devAddr lorawan.DevAddr) {
	bitoffset.SetUint8(mp.b, 8+24, 8, devAddr[0])
	bitoffset.SetUint8(mp.b, 8+16, 8, devAddr[1])
	bitoffset.SetUint8(mp.b, 8+8, 8, devAddr[2])
	bitoffset.SetUint8(mp.b, 8+0, 8, devAddr[3])
}

func (mp MapperPacket) SetFPort(fport uint8) {
	bitoffset.SetUint8(mp.b, 8*8, 8, fport)
}

func (mp MapperPacket) DevAddr() lorawan.DevAddr {
	return [4]byte{bitoffset.Uint8(mp.b, 8+24, 8), bitoffset.Uint8(mp.b, 8+16, 8), bitoffset.Uint8(mp.b, 8+8, 8), bitoffset.Uint8(mp.b, 8+0, 8)}
}

func (mp MapperPacket) Payload() []byte {
	return mp.b[9:]
}

func (mp MapperPacket) Phy() []byte {
	return mp.b
}

type DiscoveryPacket struct {
	MapperPacket
}

func NewDiscoveryPacketFromBytes(phy []byte) (*DiscoveryPacket, error) {
	if len(phy) != 9+13+65 {
		return nil, fmt.Errorf("invalid packet length: %d", len(phy))
	}
	dp := &DiscoveryPacket{}
	dp.b = phy

	return dp, nil
}

func (dp DiscoveryPacket) LatLon() (int32, int32) {
	lat := int32(bitoffset.Uint32(dp.Payload(), 4, 28))
	lon := int32(bitoffset.Uint32(dp.Payload(), 32, 29))

	return lat, lon
}

func (dp DiscoveryPacket) LatLonFloat() (float64, float64) {
	lat, lon := dp.LatLon()
	return float64(lat) / 1000000, float64(lon) / 1000000
}

func (dp DiscoveryPacket) LatLonGeoCoordinate() h3.LatLng {
	latf, lonf := dp.LatLonFloat()
	return h3.NewLatLng(latf, lonf)
}

type DownlinkTransmitPacket struct {
	MapperPacket
}

func NewDownlinkTransmitPacket() *DownlinkTransmitPacket {
	dtp := &DownlinkTransmitPacket{}
	dtp.b = make([]byte, 9+8)

	return dtp
}

func (dtp DownlinkTransmitPacket) SetChallenge(challenge []byte) {
	for i := 0; i < 8; i++ {
		dtp.Payload()[i] = challenge[i]
	}
}

type DownlinkConfirmationPacket struct {
	MapperPacket
}

func NewDownlinkConfirmationPacketFromBytes(phy []byte) (*DownlinkConfirmationPacket, error) {
	if len(phy) != 9+13+65 {
		return nil, fmt.Errorf("invalid packet length: %d", len(phy))
	}
	dcp := &DownlinkConfirmationPacket{}
	dcp.b = phy

	return dcp, nil
}

func (dcp *DownlinkConfirmationPacket) Challenge() []byte {
	return dcp.Payload()[5 : 5+8]
}

func (dcp *DownlinkConfirmationPacket) Version() uint8 {
	return bitoffset.Uint8(dcp.Payload(), 0, 4)
}

func (dcp *DownlinkConfirmationPacket) Rssi() int {
	return int(bitoffset.Int32(dcp.Payload(), 4, 16))
}

func (dcp *DownlinkConfirmationPacket) Snr() int {
	return int(bitoffset.Int32(dcp.Payload(), 20, 8))
}

func (dcp *DownlinkConfirmationPacket) Battery() uint8 {
	return bitoffset.Uint8(dcp.Payload(), 28, 8)
}

func (dcp *DownlinkConfirmationPacket) Flags() uint8 {
	return bitoffset.Uint8(dcp.Payload(), 36, 4)
}
