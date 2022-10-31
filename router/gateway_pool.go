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

package router

import (
	"fmt"
	"sync"

	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gofrs/uuid"
	"github.com/sirupsen/logrus"
)

type forwarderManagedGateway struct {
	forwarderID uuid.UUID
	gatewayID   lorawan.EUI64
	forwarder   chan<- *router.RouterToGatewayEvent
}

type GatewayPool struct {
	gatewaysMu sync.Mutex
	gateways   map[lorawan.EUI64]*forwarderManagedGateway
}

func NewGatewayPool() (*GatewayPool, error) {
	return &GatewayPool{
		gatewaysMu: sync.Mutex{},
		gateways:   make(map[lorawan.EUI64]*forwarderManagedGateway),
	}, nil
}

// SetOnline must be called when the forwarder has a gateway connected and is
// able to deliver packets to it.
func (gp *GatewayPool) SetOnline(forwarderID uuid.UUID, gatewayID lorawan.EUI64, gatewayOwner common.Address, forwarderEventSender chan<- *router.RouterToGatewayEvent) {
	gp.gatewaysMu.Lock()
	defer gp.gatewaysMu.Unlock()

	gp.gateways[gatewayID] = &forwarderManagedGateway{
		forwarderID: forwarderID,
		gatewayID:   gatewayID,
		forwarder:   forwarderEventSender,
	}

	logrus.WithFields(logrus.Fields{
		"forwarder": forwarderID,
		"gateway":   gatewayID,
		"owner":     gatewayOwner}).Info("gateway online")
}

// SetOffline must be called when a forwarder detects one of its gateways is
// offline so it won't be able to deliver packages to it.
func (gp *GatewayPool) SetOffline(forwarderID uuid.UUID, gatewayID lorawan.EUI64) {
	gp.gatewaysMu.Lock()
	defer gp.gatewaysMu.Unlock()

	delete(gp.gateways, gatewayID)

	logrus.WithFields(logrus.Fields{
		"forwarder": forwarderID,
		"gateway":   gatewayID}).Debug("gateway offline")
}

// AllOffline must be called whan a forwarder goes offline and all its gateways
// are therefore immediatly offline.
func (gp *GatewayPool) AllOffline(forwarderID uuid.UUID) {
	gp.gatewaysMu.Lock()
	defer gp.gatewaysMu.Unlock()

	for gatewayID, gateway := range gp.gateways {
		if gateway.forwarderID == forwarderID {
			delete(gp.gateways, gatewayID)
			logrus.WithFields(logrus.Fields{
				"forwarder": forwarderID,
				"gateway":   gatewayID}).Debug("gateway offline")
		}
	}
}

// DownlinkFrame is a callback that must be called by the integration layer if
// it wants to send a downlink frame to a gateway through the forwarder it is
// connected to.
func (gp *GatewayPool) DownlinkFrame(frame gw.DownlinkFrame) {
	event := &router.RouterToGatewayEvent{
		Event: &router.RouterToGatewayEvent_DownlinkFrameEvent{
			DownlinkFrameEvent: &router.DownlinkFrameEvent{
				DownlinkFrame: &frame,
			},
		},
	}
	gp.sendDownlink(frame.GatewayId, event)
}

func (gp *GatewayPool) sendDownlink(addressedGatewayID []byte, event *router.RouterToGatewayEvent) {
	gp.gatewaysMu.Lock()
	defer gp.gatewaysMu.Unlock()

	if gwId, err := utils.BytesToGatewayID(addressedGatewayID); err == nil {
		// send packet to forwarders that have this gateway attached
		for gatewayID, gateway := range gp.gateways {
			if gatewayID == gwId {
				gateway.forwarder <- event
				logrus.WithFields(logrus.Fields{
					"gw_network_id": gatewayID,
					"event_type":    fmt.Sprintf("%T", event.GetEvent()),
					"forwarder_id":  gateway.forwarderID,
				}).Info("sent downlink to forwarder")
			}
		}
	} else {
		logrus.WithError(err).
			WithField("addressed_gw_id", fmt.Sprintf("%x", addressedGatewayID)).
			Error("invalid gateway id")
	}
}
