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
	"context"
	"fmt"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"sync"
	"time"

	"github.com/FastFilter/xorfilter"
	"github.com/ThingsIXFoundation/packet-handling/forwarder/broadcast"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
)

// RoutesUpdaterFunc is a callback that retrieves routing information from
// ThingsIX. It returns the set of routers or an error in case the set could
// not be fetched.
type RoutesUpdaterFunc func() ([]*Router, error)

type ID [32]byte

func (id ID) String() string {
	if id == ([32]byte{}) {
		return "default"
	}
	return fmt.Sprintf("0x%x", id[:])
}

type RouterDetails struct {
	// Endpoint is the URI where the router can be reached
	Endpoint string
	// NetIDs is the set of network identifiers this routers wants to receive packets from
	NetIDs []lorawan.NetID
	// Owner is the routers owner
	Owner common.Address
}

type Router struct {
	// ID is the routers identity as its registered in the smart contract
	ThingsIXID ID
	// Endpoint is the URI where the router can be reached
	Endpoint string
	// Default is an indication if the router is configured in the configuration and wants to receive all data
	Default bool
	// Name is an optional name users can appoint to routers that are in the configuration
	Name string
	// NetIDs is the set of network identifiers this routers wants to receive packets from
	NetIDs []lorawan.NetID
	// Owner is the routers owner
	Owner common.Address

	joinFilterMutex sync.RWMutex
	// JoinFilter is the filter of devices that are allowed to join the network this router is part of
	joinFilter *xorfilter.Xor8
	// Accounting keeps track if this router pays for the data is received from the gateways
	accounting Accounter
}

func (r *Router) String() string {
	if r.Name != "" {
		return r.Name
	}
	return r.ThingsIXID.String()
}

func NewRouter(id [32]byte, endpoint string, def bool, netIDs []lorawan.NetID, owner common.Address, accounting Accounter) *Router {
	return &Router{
		ThingsIXID: id,
		Endpoint:   endpoint,
		Default:    def,
		NetIDs:     netIDs,
		Owner:      owner,
		accounting: accounting,
	}
}

func (r *Router) AllowAirtime(owner common.Address, airtime time.Duration) bool {
	return r.Default || r.accounting.Allow(owner, airtime)
}

// InterestedIn returns an indication if router is interested in a message
// from a device with the given devaddr.
func (r *Router) InterestedIn(addr lorawan.DevAddr) bool {
	if r.Default { // default routers receive data from all devices
		return true
	}
	for _, netID := range r.NetIDs {
		if addr.IsNetID(netID) {
			return true
		}
	}
	return false
}

func (r *Router) SetJoinFilter(filter *xorfilter.Xor8) {
	r.joinFilterMutex.Lock()
	r.joinFilter = filter
	r.joinFilterMutex.Unlock()
}

// AcceptsJoin returns an indication if the device that wants to join the
// network is accepted by this router.
func (r *Router) AcceptsJoin(devEUI lorawan.EUI64) bool {
	r.joinFilterMutex.RLock()
	defer r.joinFilterMutex.RUnlock()

	if len(r.joinFilter.Fingerprints) == 0 {
		return r.Default
	}
	return r.joinFilter.Contains(utils.Eui64ToUint64(devEUI))
}

// RoutingTable takes care of the communication between the Packet Exchange and
// external routers. Received data from the packet exchange is routed to routers
// that have expressed interest in it.
type RoutingTable struct {
	// routesFetcher is the callback to retrieve the latest routes
	routesFetcher RoutesUpdaterFunc

	// routesTableBroadcaster emits the set of fresh fetched routers. Router
	// clients receive the fresh set and disconnect if their router is removed
	// from the  list or update their configuration. A seperate routine will
	// spin up new router clients if new registered routers.
	routesTableBroadcaster *broadcast.Broadcaster[[]*Router]

	// routesUpdateInterval holds the interval on which routesFetcher is called
	// to fetch the latest routes. This is dynamic, of the fetch failed this
	// interval is shorted to retry it more often. After a successfull fetch the
	// interval is set to the configured interval.
	routesUpdateInterval    time.Duration
	routesUpdateIntervalCfg time.Duration

	// networkEvents is a stream with messages received from the routers on the
	// netwerk. The router clients will send their data on it so the packet
	// exchange can read from it and send it to the backend that sends it back to
	// the gateway.
	networkEvents chan *NetworkEvent

	// Data received from gateways. Router clients listen on this channel, determine
	// if the event is of interest of the router they are connected to, and forward
	// it if needed.
	gatewayEvents *broadcast.Broadcaster[*GatewayEvent]

	// defaultRoutes contains the set of default routers, these are configured
	// locally and always get send all data that is received from the gateways.
	// These routers don't have to be registered in ThingsIX.
	defaultRoutes []*Router
}

// Run starts the integration with the routers on the ThingsIX network until the
// given context expires.
//
// It fetches the list of registered routers and opens connects with these routers.
// For each router a client is started that maintains the connection with the router
// and exchanges messages with it. Periodically the latest set of registered routers
// is fetched and nieuw router clients are started for fresh registered routers or
// clients are stopped/updated when they are either removed or updated.
func (r *RoutingTable) Run(ctx context.Context) {
	// router clients will subscribe to the routing table broadcaster. Data and
	// events for router clients are broadcasted over this event channel.
	r.routesTableBroadcaster.Run()

	// wait for routing table updates and forward them to the router clients or
	// start/stop clients in case of new routers/deleted routers.
	go r.keepRouteTableUpToDate(ctx)

	// run router clients to default configured routers
	go r.runDefaultRouting(ctx)

	for {
		select {
		case <-time.After(r.routesUpdateInterval):
			// assume failure and retry it in a couple of minutes
			r.routesUpdateInterval = 2 * time.Minute

			// fetch the latest known set of routers from ThingsIX
			routers, err := r.routesFetcher()
			if err != nil {
				logrus.WithError(err).Warn("unable to refresh routers")
				continue
			}

			// try to submit routing information to router clients
			if r.routesTableBroadcaster.TryBroadcast(routers) {
				// successfull, refresh on configured update interval
				r.routesUpdateInterval = r.routesUpdateIntervalCfg
				continue
			}
			logrus.Warn("unable to refresh routing table")
		case <-ctx.Done():
			logrus.Info("routing table stopped")
			return
		}
	}
}

func (r *RoutingTable) keepRouteTableUpToDate(ctx context.Context) {
	var (
		newRoutes       = make(chan []*Router)
		existingRouters = make(map[[32]byte]*struct {
			stop    context.CancelFunc
			details chan *RouterDetails
		})
	)
	// routes table broadcaster emits the latest retrieved routes periodically.
	r.routesTableBroadcaster.Subscribe(newRoutes)
	defer r.routesTableBroadcaster.Unsubscribe(newRoutes)

	for {
		select {
		// new set of routes fetched, determine which one are new and
		// startup a client for them. Or broadcast routing details update
		// for existing routes, or stop routes if the router is removed.
		case routers := <-newRoutes:
			var (
				newRoutesCount      = 0
				existingRoutesCount = 0
				deletedRoutesCount  = 0
			)

			// stop which routers are deleted and stop the client
			for id, client := range existingRouters {
				deleted := true
				for _, r := range routers {
					if r.ThingsIXID == id {
						deleted = false
						break
					}
				}
				if deleted {
					go client.stop()
					deletedRoutesCount++
					delete(existingRouters, id)
				}
			}

			for _, router := range routers {
				// send route details to client for existing routers
				if client, ok := existingRouters[router.ThingsIXID]; ok {
					// existing route, send route details update to client, in case
					// the client is dialing the router this can temporarly block
					// therefore do it in the background.
					go func() {
						client.details <- &RouterDetails{
							Endpoint: router.Endpoint,
							NetIDs:   router.NetIDs,
							Owner:    router.Owner,
						}
					}()
					existingRoutesCount++
				} else {
					// new route, startup client and add it to the routing table
					var (
						clientCtx, clientCancel = context.WithCancel(ctx)
						details                 = make(chan *RouterDetails)
					)
					go NewRouterClient(router, r.routesTableBroadcaster, r.networkEvents, r.gatewayEvents, details).Run(clientCtx)
					existingRouters[router.ThingsIXID] = &struct {
						stop    context.CancelFunc
						details chan *RouterDetails
					}{
						clientCancel,
						details,
					}
					newRoutesCount++
				}
			}

			logrus.WithFields(logrus.Fields{
				"new":      newRoutesCount,
				"existing": existingRoutesCount,
				"deleted":  deletedRoutesCount,
			}).Info("refreshed routing table")
		case <-ctx.Done():
			return
		}
	}
}

// runDefaultRouting start router clients for default configured routers
func (r *RoutingTable) runDefaultRouting(ctx context.Context) {
	var allStopped sync.WaitGroup
	for _, dr := range r.defaultRoutes {
		allStopped.Add(1)
		cpy := dr
		go func() {
			// run router client until ctx expires
			ignore := make(chan *RouterDetails) // default routes are never updated
			NewRouterClient(cpy, r.routesTableBroadcaster, r.networkEvents, r.gatewayEvents, ignore).Run(ctx)
			allStopped.Done()
		}()
	}
	// wait for shutdown signal and all router clients have stopped
	<-ctx.Done()
	allStopped.Wait()
	logrus.Trace("default routers disconnected")
}

// buildRoutingTable constructs a new routing table
func buildRoutingTable(cfg *Config, accounter Accounter) (*RoutingTable, error) {
	routes, interval, err := obtainThingsIXRoutesFunc(cfg, accounter)
	if err != nil {
		return nil, fmt.Errorf("unable to determine method to fetch ThingsIX routes: %w", err)
	}

	return &RoutingTable{
		routesFetcher:           routes,
		routesUpdateInterval:    time.Millisecond, // first time try to fetch routing information immediately
		routesUpdateIntervalCfg: interval,
		routesTableBroadcaster:  broadcast.New[[]*Router](1),
		defaultRoutes:           cfg.Forwarder.Routers.Default,
		networkEvents:           make(chan *NetworkEvent, 1024),
		gatewayEvents:           broadcast.New[*GatewayEvent](1024).Run(),
	}, nil
}

// obtainThingsIXRoutesFunc returns a func that can be used to retrieve the latest set
// of ThingsIX routers from a source that is configured in the given cfg.
func obtainThingsIXRoutesFunc(cfg *Config, accounter Accounter) (RoutesUpdaterFunc, time.Duration, error) {
	if cfg.Forwarder.Routers.OnChain != nil {
		return fetchRoutersFromChain(cfg, accounter)
	}

	if cfg.Forwarder.Routers.ThingsIXApi != nil && cfg.Forwarder.Routers.ThingsIXApi.Endpoint != nil {
		return fetchRoutersFromThingsIXAPI(cfg, accounter)
	}

	// no routes source configured, only use default routers
	logrus.Warn("no ThingsIX routing table source configured, only use default routers from configuration")
	return func() ([]*Router, error) {
		return nil, nil
	}, 24 * time.Hour, nil
}
