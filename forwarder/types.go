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
	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend/events"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/lorawan"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
)

// Backend defines the interface that a backend must implement.
type Backend interface {
	// Stop closes the backend.
	Stop() error

	// Start starts the backend.
	Start() error

	// SetDownlinkTxAckFunc sets the DownlinkTXAck handler func.
	SetDownlinkTxAckFunc(func(*gw.DownlinkTxAck))

	// SetGatewayStatsFunc sets the GatewayStats handler func.
	SetGatewayStatsFunc(func(*gw.GatewayStats))

	// SetUplinkFrameFunc sets the UplinkFrame handler func.
	SetUplinkFrameFunc(func(*gw.UplinkFrame))

	// SetRawPacketForwarderEventFunc sets the RawPacketForwarderEvent handler func.
	SetRawPacketForwarderEventFunc(func(*gw.RawPacketForwarderEvent))

	// SetSubscribeEventFunc sets the Subscribe handler func.
	SetSubscribeEventFunc(func(events.Subscribe))

	// SendDownlinkFrame sends the given downlink frame.
	SendDownlinkFrame(*gw.DownlinkFrame) error

	// ApplyConfiguration applies the given configuration to the gateway.
	ApplyConfiguration(*gw.GatewayConfiguration) error

	// RawPacketForwarderCommand sends the given raw command to the packet-forwarder.
	RawPacketForwarderCommand(*gw.RawPacketForwarderCommand) error
}

// NetworkEvent represents an event received from the network
type NetworkEvent struct {
	source *Router
	event  *router.RouterToGatewayEvent
}

// GatewayEvent is the decoded event received from a gateway through the backend.
// It contains the raw payload together with some helper function to determine
// what kind of event was received.
type GatewayEvent struct {
	join *struct {
		devEUI lorawan.EUI64
		event  *router.GatewayToRouterEvent
	}
	uplink *struct {
		device lorawan.DevAddr
		event  *router.GatewayToRouterEvent
	}
	subOnlineOfflineEvent *struct {
		event *router.GatewayToRouterEvent
	}
	downlinkAck *struct {
		downlinkID uint32
		event      *router.GatewayToRouterEvent
	}
	receivedFrom *gateway.Gateway
}

// IsUplink returns an indication if the event is an uplink event.
func (ge GatewayEvent) IsUplink() bool {
	return ge.uplink != nil
}

// IsJoin returns an indication if the vent is a join event.
func (ge GatewayEvent) IsJoin() bool {
	return ge.join != nil
}

// IsOnlineOfflineEvent is an indication if a gateway went offline or became online.
func (ge GatewayEvent) IsOnlineOfflineEvent() bool {
	return ge.subOnlineOfflineEvent != nil
}

// IsDownlinkAck returns an indication if the vent is a downlink ACK.
func (ge GatewayEvent) IsDownlinkAck() bool {
	return ge.downlinkAck != nil
}
