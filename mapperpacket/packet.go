package mapperpacket

import (
	"fmt"

	"github.com/ThingsIXFoundation/bitoffset"
	"github.com/brocaar/lorawan"
	"github.com/uber/h3-go"
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

func (dp DiscoveryPacket) LatLonGeoCoordinate() h3.GeoCoord {
	latf, lonf := dp.LatLonFloat()
	return h3.GeoCoord{Latitude: latf, Longitude: lonf}
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
