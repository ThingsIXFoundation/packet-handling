package forwarder

import (
	"encoding/hex"

	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend/events"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/brocaar/lorawan"
	"github.com/sirupsen/logrus"
)

// Backend defines the interface that a backend must implement
type Backend interface {
	// Stop closes the backend.
	Stop() error

	// Start starts the backend.
	Start() error

	// SetDownlinkTxAckFunc sets the DownlinkTXAck handler func.
	SetDownlinkTxAckFunc(func(gw.DownlinkTXAck))

	// SetGatewayStatsFunc sets the GatewayStats handler func.
	SetGatewayStatsFunc(func(gw.GatewayStats))

	// SetUplinkFrameFunc sets the UplinkFrame handler func.
	SetUplinkFrameFunc(func(gw.UplinkFrame))

	// SetRawPacketForwarderEventFunc sets the RawPacketForwarderEvent handler func.
	SetRawPacketForwarderEventFunc(func(gw.RawPacketForwarderEvent))

	// SetSubscribeEventFunc sets the Subscribe handler func.
	SetSubscribeEventFunc(func(events.Subscribe))

	// SendDownlinkFrame sends the given downlink frame.
	SendDownlinkFrame(gw.DownlinkFrame) error

	// ApplyConfiguration applies the given configuration to the gateway.
	ApplyConfiguration(gw.GatewayConfiguration) error

	// RawPacketForwarderCommand sends the given raw command to the packet-forwarder.
	RawPacketForwarderCommand(gw.RawPacketForwarderCommand) error
}

type GatewaySet struct {
	byLocalID   map[lorawan.EUI64]*gateway.Gateway
	byNetworkID map[lorawan.EUI64]*gateway.Gateway
}

func (gs GatewaySet) ByLocalIDBytes(id []byte) (*gateway.Gateway, bool) {
	var lid lorawan.EUI64
	if len(id) != len(lid) {
		logrus.WithField("id", hex.EncodeToString(id)).Warn("search gateway by invalid id")
		return nil, false
	}
	copy(lid[:], id)
	return gs.ByLocalID(lid)
}

func (gs GatewaySet) ByLocalID(id lorawan.EUI64) (*gateway.Gateway, bool) {
	gw, found := gs.byLocalID[id]
	return gw, found
}

func (gs GatewaySet) ByNetworkIDBytes(id []byte) (*gateway.Gateway, bool) {
	var lid lorawan.EUI64
	if len(id) != len(lid) {
		logrus.WithField("id", hex.EncodeToString(id)).Warn("search gateway by invalid id")
		return nil, false
	}
	copy(lid[:], id)
	return gs.ByLocalID(lid)
}

func (gs GatewaySet) ByNetworkID(id lorawan.EUI64) (*gateway.Gateway, bool) {
	gw, found := gs.byNetworkID[id]
	return gw, found
}

// NetworkEvent represents an event received from the network
type NetworkEvent struct {
	source *Router
	event  *router.RouterToGatewayEvent
}

type GatewayEvent struct {
	join *struct {
		joinEUI lorawan.EUI64
		event   *router.GatewayToRouterEvent
	}
	uplink *struct {
		device lorawan.DevAddr
		event  *router.GatewayToRouterEvent
	}
	subOnlineOfflineEvent *struct {
		event *router.GatewayToRouterEvent
	}
	downlinkAck *struct {
		downlinkID []byte
		event      *router.GatewayToRouterEvent
	}
	receivedFrom *gateway.Gateway
}

func (ge GatewayEvent) IsUplink() bool {
	return ge.uplink != nil
}

func (ge GatewayEvent) IsJoin() bool {
	return ge.join != nil
}

func (ge GatewayEvent) IsOnlineOfflineEvent() bool {
	return ge.subOnlineOfflineEvent != nil
}

func (ge GatewayEvent) IsDownlinkAck() bool {
	return ge.downlinkAck != nil
}
