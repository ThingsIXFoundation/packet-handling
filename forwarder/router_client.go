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
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/FastFilter/xorfilter"
	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/brocaar/lorawan"

	"github.com/ThingsIXFoundation/frequency-plan/go/frequency_plan"
	"github.com/ThingsIXFoundation/packet-handling/forwarder/broadcast"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

// RouterClient communicates with a remote router and exchanges messages
// between the router and the packet exchange.
type RouterClient struct {
	// router details
	router *Router

	// routeTableBroadcaster emits the routing table when its refreshed
	// client will track it and determine if they need to update their
	// routing data (e.g. router updated its net ids) or disconnect from
	// a router that is dropped from the routing table. New clients are
	// started by the RoutingTable.
	routeTableBroadcaster *broadcast.Broadcaster[[]*Router]

	// routerInfo is a stream of router information that is received from
	// the routing table through the routeTableBroadcaster.
	routerInfo chan []*Router

	// routerEvents is used by this client to send messages received from
	// the router it is connected to, to the packet exchange that can
	// foward these events to the appropiate gateway if required.
	routerEvents chan *NetworkEvent

	// gatewayEvents streams received gateway messages. The client must
	// determine if the event is of interest of the router it is connected
	// to and forward it if necessary.
	gatewayEvents *broadcast.Broadcaster[*GatewayEvent]

	// receives router details
	routerDetails <-chan *RouterDetails

	// Tracks the last time an event for a certain gateway was sent to the router
	// this is used to send additional online events to prevent a timeout
	lastGatewayEvent map[lorawan.EUI64]time.Time
}

// NewRouterClient create a new client that connects to a remote routers and
// handles communication with that router.
func NewRouterClient(router *Router,
	routeTableBroadcaster *broadcast.Broadcaster[[]*Router],
	routerEvents chan *NetworkEvent, gatewayEvents *broadcast.Broadcaster[*GatewayEvent],
	routerDetails <-chan *RouterDetails) *RouterClient {

	routerInfo := make(chan []*Router)
	routeTableBroadcaster.Subscribe(routerInfo)

	return &RouterClient{
		router:                router,
		routerInfo:            routerInfo,
		routeTableBroadcaster: routeTableBroadcaster,
		routerEvents:          routerEvents,
		gatewayEvents:         gatewayEvents,
		routerDetails:         routerDetails,
		lastGatewayEvent:      make(map[lorawan.EUI64]time.Time),
	}
}

// Run the router client until the given context expires.
// This includes connecting to the router and opening a bidirectional stream
// to it to exchange packets.
func (rc *RouterClient) Run(ctx context.Context) {
	var (
		lastConnectAttempt    time.Time
		reconnectInterval     = 5 * time.Second
		nextReconnectInterval = func() time.Duration {
			if reconnectInterval < time.Second {
				reconnectInterval = 5 * time.Second
				return reconnectInterval
			}
			reconnectInterval = reconnectInterval * 2
			if reconnectInterval > (5 * time.Minute) {
				reconnectInterval = 5 * time.Minute
			}
			return reconnectInterval
		}
		log = logrus.WithFields(logrus.Fields{
			"endpoint": rc.router.Endpoint,
			"band":     frequency_plan.FromBlockchain(rc.router.FrequencyPlan),
			"default":  rc.router.Default,
		})
	)

	// stop being interested in router updates since this client stopped
	defer rc.routeTableBroadcaster.Unsubscribe(rc.routerInfo)

	for {
		lastConnectAttempt = time.Now()
		err := rc.run(ctx)
		if err == nil {
			return
		}

		// determine if the context was cancelled since that is the reason the
		// router disconnected
		select {
		case <-ctx.Done():
			log.Info("router disconnected")
			return
		case details := <-rc.routerDetails:
			rc.router.Endpoint = details.Endpoint
			rc.router.NetID = details.NetID
			rc.router.Prefix = details.Prefix
			rc.router.Mask = details.Mask
			rc.router.Owner = details.Owner

			log = logrus.WithFields(logrus.Fields{
				"endpoint": rc.router.Endpoint,
				"default":  rc.router.Default,
			})

			// attempt was more than 1 minute ago this indicates the communication
			// was good for at least a short period, reset reconnect interval so it
			// will retry to connect immediately
			if time.Since(lastConnectAttempt) > time.Minute {
				reconnectInterval = 0
			}
		default: // connection with router dropped for whatever reason last connect
			// attempt was more than 1 minute ago this indicates the communication
			// was good for at least a short period, reset reconnect interval so it
			// will retry to connect immediately
			if time.Since(lastConnectAttempt) > time.Minute {
				reconnectInterval = 0
			}
		}

		log.WithError(err).WithField("reconnect", reconnectInterval).Errorf("router client stopped unexpected")
		wait := true
		retry := time.After(nextReconnectInterval())
		for wait {
			select {
			case <-retry:
				wait = false
			case details := <-rc.routerDetails:
				rc.router.Endpoint = details.Endpoint
				rc.router.NetID = details.NetID
				rc.router.Prefix = details.Prefix
				rc.router.Mask = details.Mask
				rc.router.Owner = details.Owner

				log = logrus.WithFields(logrus.Fields{
					"endpoint": rc.router.Endpoint,
					"default":  rc.router.Default,
				})
			case <-ctx.Done():
				log.Trace("router client stopped")
				return
			}
		}
	}
}

func logRouterDialDetails(router *Router) {
	log := logrus.WithFields(logrus.Fields{
		"router":   router,
		"endpoint": router.Endpoint,
		"default":  router.Default,
		"netid":    router.NetID,
		"prefix":   fmt.Sprintf("%08x/%d", router.Prefix, router.Mask),
		"band":     frequency_plan.FromBlockchain(router.FrequencyPlan),
	})
	if !router.Default {
		log = log.WithField("owner", router.Owner)
	}

	log.Info("connect router")
}

func (rc *RouterClient) run(ctx context.Context) error {
	var (
		log                   = logrus.WithField("router_id", rc.router)
		joinFilterRenewTicker = time.NewTicker(30 * time.Minute)
		pendingDownlinkAcks   = make(map[[32]byte]time.Time)
		dialCtx, cancel       = context.WithTimeout(ctx, 30*time.Second)
		kacp                  = keepalive.ClientParameters{
			Time:                20 * time.Second, // send pings every 20 seconds if there is no activity
			Timeout:             5 * time.Second,  // wait 5 seconds for ping ack before considering the connection dead
			PermitWithoutStream: true,             // send pings even without active streams
		}
	)
	defer cancel()
	logRouterDialDetails(rc.router)

	// connect to the router
	conn, err := grpc.DialContext(dialCtx, rc.router.Endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(kacp))

	if err != nil {
		return fmt.Errorf("unable to dial router: %w", err)
	}
	defer conn.Close()
	log.Info("router connected")

	client := router.NewRouterV1Client(conn)
	eventStream, err := client.Events(ctx)
	if err != nil {
		return fmt.Errorf("unable to open bi-directional event stream with router: %w", err)
	}

	// subscribe to message from the packet exchange
	fromGateway := make(chan *GatewayEvent)
	rc.gatewayEvents.Subscribe(fromGateway)

	// unsubscribe on disconnect
	defer rc.gatewayEvents.Unsubscribe(fromGateway)

	// turn router incoming eventStream into a channel
	fromRouter := routerEventsChan(eventStream)

	// cleanup expired pending downlink acks
	var (
		pendingDownlinkAcksTicker  = time.NewTicker(30 * time.Second)
		pendingDownlinkAckDeadline = 30 * time.Second
	)

	defer pendingDownlinkAcksTicker.Stop()

	// 1. client listens for messages from the packet exchange, if a message is received determine
	// if our router is interested in it, if so send it to the router
	// 2. wait for events from the router and forward them to the packet exchange so it can forward
	// the message to the appropiate gateway
	log.Trace("start router message exchange")
	routersOnlineGauge.WithLabelValues(rc.router.String()).Set(1)
	defer routersOnlineGauge.WithLabelValues(rc.router.String()).Set(0)

	// Get the JoinFilter now and update it later every joinFilterRenewInterval
	go rc.updateJoinFilter(ctx, client)

	for {
		select {
		case <-pendingDownlinkAcksTicker.C:
			// delete expired pending downlink acks
			deadline := time.Now().Add(-pendingDownlinkAckDeadline)
			for id, created := range pendingDownlinkAcks {
				if created.Before(deadline) {
					delete(pendingDownlinkAcks, id)
					log.WithField("downlink_id", id).Warn("delete expired downlink ACK")
				}
			}
		case details := <-rc.routerDetails:
			reconnect := rc.router.Endpoint != details.Endpoint
			rc.router.Endpoint = details.Endpoint
			rc.router.NetID = details.NetID
			rc.router.Prefix = details.Prefix
			rc.router.Mask = details.Mask
			rc.router.Owner = details.Owner
			rc.router.FrequencyPlan = details.FrequencyPlan

			if reconnect {
				log.WithField("new-endpoint", rc.router.Endpoint).Info("reconnect router on new endpoint")
				return nil
			}
		case ev, ok := <-fromGateway:
			if ok {
				if ev.IsUplink() {
					// send event if router is interested in it
					if rc.router.InterestedIn(ev.uplink.device) {
						pktlog := log.WithFields(logrus.Fields{
							"dev_addr":      ev.uplink.device,
							"gw_network_id": ev.receivedFrom.NetworkID,
							"gw_local_id":   ev.receivedFrom.LocalID,
							"uplink_id":     ev.uplink.event.GetUplinkFrameEvent().UplinkFrame.GetRxInfo().GetUplinkId(),
						})

						rxPacketsPerRouterCounter.WithLabelValues(ev.receivedFrom.NetworkID.String(), ev.receivedFrom.LocalID.String(), rc.router.String()).Inc()

						var (
							owner   = rc.router.Owner
							airtime = time.Duration(ev.uplink.event.GetUplinkFrameEvent().GetAirtimeReceipt().GetAirtime()) * time.Millisecond
						)
						if rc.router.AllowAirtime(owner, airtime) {
							if err := eventStream.Send(ev.uplink.event); err != nil {
								return fmt.Errorf("unable to send event to router: %w", err)
							}

							// Update the last gateway event because an event was successfully sent
							rc.lastGatewayEvent[ev.receivedFrom.NetworkID] = time.Now()

							pktlog.Info("forwarded uplink packet to router")
						} else {
							pktlog.Warn("accounting prevents forwarding uplink packet to router, drop packet")
						}
					}
				} else if ev.IsJoin() {
					// send event if router is accepts the join request
					if rc.router.AcceptsJoin(ev.join.devEUI) {
						pktlog := log.WithFields(logrus.Fields{
							"dev_eui":       ev.join.devEUI,
							"gw_network_id": ev.receivedFrom.NetworkID,
							"gw_local_id":   ev.receivedFrom.LocalID,
							"uplink_id":     ev.join.event.GetUplinkFrameEvent().UplinkFrame.GetRxInfo().GetUplinkId(),
						})

						rxPacketsPerRouterCounter.WithLabelValues(ev.receivedFrom.NetworkID.String(), ev.receivedFrom.LocalID.String(), rc.router.String()).Inc()

						var (
							owner   = rc.router.Owner
							airtime = time.Duration(ev.join.event.GetUplinkFrameEvent().GetAirtimeReceipt().GetAirtime()) * time.Millisecond
						)
						if rc.router.AllowAirtime(owner, airtime) {
							if err := eventStream.Send(ev.join.event); err != nil {
								return fmt.Errorf("unable to send event to router: %w", err)
							}

							// Update the last gateway event because an event was successfully sent
							rc.lastGatewayEvent[ev.receivedFrom.NetworkID] = time.Now()

							pktlog.Info("forwarded join packet to router")
						} else {
							pktlog.Warn("accounting prevents forwarding join packet to router, drop packet")
						}
					}
				} else if ev.IsDownlinkAck() {
					downlinkID := sha256.Sum256(binary.BigEndian.AppendUint32(rc.router.ThingsIXID[:], ev.downlinkAck.downlinkID))
					// test if the router this client is connected to asked for the downlink
					if _, ok := pendingDownlinkAcks[downlinkID]; ok {
						// our router ordered the ACK
						delete(pendingDownlinkAcks, downlinkID)

						if err := eventStream.Send(ev.downlinkAck.event); err != nil {
							return fmt.Errorf("unable to send event to router: %w", err)
						}

						log.WithFields(logrus.Fields{
							"downlink_id":   fmt.Sprintf("%x", downlinkID[:8]),
							"gw_network_id": ev.receivedFrom.NetworkID,
							"gw_local_id":   ev.receivedFrom.LocalID,
						}).Info("forwarded downlink-ack to router")
					}
				} else if ev.IsOnlineOfflineEvent() {
					if ev.subOnlineOfflineEvent.event.GetStatusEvent().Online {
						if lastEvent, ok := rc.lastGatewayEvent[ev.receivedFrom.NetworkID]; !ok || time.Since(lastEvent) > 4*time.Minute {
							if err := eventStream.Send(ev.subOnlineOfflineEvent.event); err != nil {
								return fmt.Errorf("unable to send gateway online event to router: %w", err)
							}

							rc.lastGatewayEvent[ev.receivedFrom.NetworkID] = time.Now()

							log.WithFields(logrus.Fields{
								"gw_network_id": ev.receivedFrom.NetworkID,
								"gw_local_id":   ev.receivedFrom.LocalID,
							}).Info("sent online event to router")
						}

					} else {
						if err := eventStream.Send(ev.subOnlineOfflineEvent.event); err != nil {
							return fmt.Errorf("unable to send gateway offline event to router: %w", err)
						}

						// Delete last gateway event because gateway is now reported offline
						delete(rc.lastGatewayEvent, ev.receivedFrom.NetworkID)

						log.WithFields(logrus.Fields{
							"gw_network_id": ev.receivedFrom.NetworkID,
							"gw_local_id":   ev.receivedFrom.LocalID,
						}).Info("sent offline event to router")
					}
				}
			}
		case event, ok := <-fromRouter:
			if !ok {
				return fmt.Errorf("connection with router lost")
			}

			log.Info("received event from router")

			if airtimePayment := event.GetAirtimePaymentEvent(); airtimePayment != nil {
				rc.router.accounting.AddPayment(airtimePayment)
			}

			if downlinkEvent := event.GetDownlinkFrameEvent(); downlinkEvent != nil {
				// router asked the gateway for a confirmation that it transmitted
				// the downlink message. Store the downlink ID so its possible to
				// determine if a downlink ACK must be forwarded to the router this
				// client is connected to.

				downlinkID := sha256.Sum256(binary.BigEndian.AppendUint32(rc.router.ThingsIXID[:], downlinkEvent.GetDownlinkFrame().GetDownlinkId()))
				log.WithField("downlink_id", fmt.Sprintf("%x", downlinkID[:8])).Info("received downlink from router")
				pendingDownlinkAcks[downlinkID] = time.Now()
			}
			rc.routerEvents <- &NetworkEvent{
				source: rc.router,
				event:  event,
			}
		case latestRoutesInfo, ok := <-rc.routerInfo:
			// new router info found, determine if this route is still in the new set,
			// if not stop, or update router info if outdated. If router is default
			// ignore update since its part of this configuration.
			if !rc.router.Default && ok {
				found := false
				for _, router := range latestRoutesInfo {
					if rc.router.ThingsIXID == router.ThingsIXID {
						found = true
						endpointChanged := rc.router.Endpoint != router.Endpoint
						// update router details
						rc.router.Endpoint = router.Endpoint
						rc.router.NetID = router.NetID
						rc.router.Prefix = router.Prefix
						rc.router.Mask = router.Mask
						rc.router.Owner = router.Owner
						if endpointChanged {
							return fmt.Errorf("endpoint changed") // force reconnect
						}
					}
				}

				if !found {
					log.WithField("endpoint", rc.router.Endpoint).Info("disconnect from router - router not part of ThingsIX anymore")
					return nil
				}
			}
		case <-joinFilterRenewTicker.C:
			// Update the join filter from the router every joinFilterRenewInterval
			go rc.updateJoinFilter(ctx, client)
		}
	}
}

func (rc *RouterClient) updateJoinFilter(ctx context.Context, client router.RouterV1Client) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	resp, err := client.JoinFilter(ctx, &router.JoinFilterRequest{})
	if err != nil {
		logrus.WithError(err).WithField("router", rc.router).Error("error while updating JoinFilter for router")
		return
	}

	var bitmap *roaring64.Bitmap
	var filter *xorfilter.Xor8

	if len(resp.GetJoinFilter().GetRoaringBitmap()) > 0 {
		bitmap = roaring64.New()
		err := bitmap.UnmarshalBinary(resp.JoinFilter.RoaringBitmap)
		if err != nil {
			logrus.WithError(err).WithField("router", rc.router).Error("error while updating JoinFilter for router")
			return
		}
	} else if resp.GetJoinFilter().GetXor8() != nil {
		xor := resp.GetJoinFilter().GetXor8()
		filter = &xorfilter.Xor8{}
		filter.Seed = xor.Seed
		filter.Fingerprints = xor.Fingerprints
		filter.BlockLength = xor.Blocklength
	}

	rc.router.SetJoinFilter(filter, bitmap)
	if bitmap != nil {
		logrus.WithField("router", rc.router).Infof("updated the JoinFilter with bitmap with %d items", bitmap.GetCardinality())
	} else if filter != nil {
		logrus.WithField("router", rc.router).Infof("updated the JoinFilter with %d fingerprints", len(filter.Fingerprints))
	}
}

// routerEventsChan turns the given events readable into a readable go channel
// it closes the returned channel when receiving an event fails
func routerEventsChan(events router.RouterV1_EventsClient) <-chan *router.RouterToGatewayEvent {
	receivedRouterEvents := make(chan *router.RouterToGatewayEvent)
	go func() {
		defer close(receivedRouterEvents)
		for {
			in, err := events.Recv()
			statusCode := status.Code(err)
			switch statusCode {
			case codes.OK:
				receivedRouterEvents <- in
			case codes.Canceled, codes.Unavailable, codes.Unknown:
				return
			default:
				logrus.WithError(err).WithFields(logrus.Fields{
					"status": statusCode.String(),
				}).Error("unable to receive router message")
			}
		}
	}()
	return receivedRouterEvents
}
