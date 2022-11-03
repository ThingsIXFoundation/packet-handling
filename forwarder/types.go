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
	"encoding/hex"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"sync"

	"context"
	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend/events"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/lorawan"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
	"github.com/sirupsen/logrus"
	"time"
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

// GatewaySet sync a gateway store periodically with the gateway registry and
// keeps track of gateways in the store that are onboarded and have their details
// set in the ThingsIX gateway registry.
//
// It also maps from gateways their local id to the ThingsIX network id.
type GatewaySet struct {
	config *Config
	store  gateway.Store

	mu          sync.RWMutex
	byLocalID   map[lorawan.EUI64]*gateway.Gateway
	byNetworkID map[lorawan.EUI64]*gateway.Gateway
}

func NewGatewaySet(cfg *Config, store gateway.Store, local map[lorawan.EUI64]*gateway.Gateway, network map[lorawan.EUI64]*gateway.Gateway) *GatewaySet {
	return &GatewaySet{
		config:      cfg,
		store:       store,
		byLocalID:   local,
		byNetworkID: network,
	}
}

/**
 * Refresh polls until the given ctx expires periodically (interval is configurable)
 * the gateway registry and refreshes its internal gateway sets with gateways that
 * are onboarded and have their details set.
 */
func (gs *GatewaySet) Refresh(ctx context.Context) {
	if gs.config.Forwarder.Gateways.RegistryAddress == nil {
		// don't refresh loaded gateway store with on-chain
		// gateway registry. RegistryAddress must be made mandatory
		// once Gateways can be onboarded/updated in ThingsIX
		// (TODO: e.g. token support is added)
		return
	}

	var (
		refresh  = 5 * time.Minute  // check refresh interval gateway registry for updates
		interval = time.Duration(0) // run instant first time
	)
	if *gs.config.Forwarder.Gateways.Refresh < time.Minute {
		refresh = time.Minute
	} else if gs.config.Forwarder.Gateways.Refresh != nil {
		refresh = *gs.config.Forwarder.Gateways.Refresh
	}

	logrus.WithFields(logrus.Fields{
		"address": gs.config.Forwarder.Gateways.RegistryAddress,
		"refresh": refresh,
	}).Info("gateway registry")

	for {
		select {
		case <-time.After(interval):
			local, network, err := onboardedAndRegisteredGateways(gs.config, gs.store)
			if err == nil {
				gs.mu.Lock()
				gs.byLocalID = local
				gs.byNetworkID = network
				gs.mu.Unlock()
				logrus.WithField("#-gateways", len(local)).Info("refreshed gateways with registry")
			} else {
				logrus.WithError(err).Error("unable to refresh gateway store")
			}
			interval = refresh
		case <-ctx.Done():
			return
		}
	}
}

func (gs *GatewaySet) ByLocalIDString(id string) (*gateway.Gateway, bool) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	lid, err := utils.Eui64FromString(id)
	if err != nil {
		logrus.WithError(err).WithField("id", id).Warn("search gateway by invalid id")
		return nil, false
	}

	return gs.ByLocalID(lid)
}

func (gs *GatewaySet) ByLocalID(id lorawan.EUI64) (*gateway.Gateway, bool) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	gw, found := gs.byLocalID[id]
	return gw, found
}

func (gs *GatewaySet) ByNetworkIDBytes(id []byte) (*gateway.Gateway, bool) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	var lid lorawan.EUI64
	if len(id) != len(lid) {
		logrus.WithField("id", hex.EncodeToString(id)).Warn("search gateway by invalid id")
		return nil, false
	}
	copy(lid[:], id)
	return gs.ByLocalID(lid)
}

func (gs *GatewaySet) ByNetworkID(id lorawan.EUI64) (*gateway.Gateway, bool) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	gw, found := gs.byNetworkID[id]
	return gw, found
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
