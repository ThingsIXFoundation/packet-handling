package packetexchange

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"time"

	"github.com/ThingsIXFoundation/packet-handling/packet_exchange/broadcast"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gofrs/uuid"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
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
	routerInfo            chan []*Router

	// routerEvents is used by this client to send messages received from
	// the router it is connected to, to the packet exchange that can
	// foward these events to the appropiate gateway if required.
	routerEvents chan *NetworkEvent

	// gatewayEvents streams received gateway messages. The client must
	// determine if the event is of interest of the router it is connected
	// to and forward it if necessary.
	gatewayEvents *broadcast.Broadcaster[*GatewayEvent]
}

func NewRouterClient(ctx context.Context, router *Router,
	routeTableBroadcaster *broadcast.Broadcaster[[]*Router],
	routerEvents chan *NetworkEvent, gatewayEvents *broadcast.Broadcaster[*GatewayEvent]) *RouterClient {

	routerInfo := make(chan []*Router)
	routeTableBroadcaster.Subscribe(routerInfo)

	return &RouterClient{
		router:                router,
		routerInfo:            routerInfo,
		routeTableBroadcaster: routeTableBroadcaster,
		routerEvents:          routerEvents,
		gatewayEvents:         gatewayEvents,
	}
}

// Run the router client until the given context expires.
// This includes connecting to the router and opening a bidirectional stream
// to it to exchange packets.
func (rc *RouterClient) Run(ctx context.Context) {
	var (
		lastConnectAttempt    = time.Now()
		reconnectInterval     = 5 * time.Second
		nextReconnectInterval = func() time.Duration {
			if reconnectInterval < time.Second {
				reconnectInterval = 5 * time.Second
				return reconnectInterval
			}
			reconnectInterval = reconnectInterval * 2
			if reconnectInterval > time.Minute {
				reconnectInterval = time.Minute
			}
			return reconnectInterval
		}
		log = logrus.WithFields(logrus.Fields{
			"endpoint": rc.router.Endpoint,
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
		default: // connection with router dropped for whatever reason last connect
			// attempt was more than 1 minute ago this indicates the communication
			// was good for at least a short period, reset reconnect interval so it
			// will retry to connect immediately
			if time.Since(lastConnectAttempt) > time.Minute {
				reconnectInterval = 0
			}
		}

		log.WithError(err).Errorf("router client stopped unexpected, reconnect in %v", reconnectInterval)
		routersDisconnectedGauge.Add(1)
		select {
		case <-time.After(nextReconnectInterval()):
			routersDisconnectedGauge.Sub(1)
			continue
		case <-ctx.Done():
			log.Trace("router client stopped")
			routersDisconnectedGauge.Sub(1)
			return
		}
	}
}

func (rc *RouterClient) run(ctx context.Context) error {
	var (
		log                 = logrus.WithField("router", rc.router)
		pendingDownlinkAcks = make(map[[32]byte]time.Time)
	)
	log.WithFields(logrus.Fields{
		"router":   rc.router,
		"endpoint": rc.router.Endpoint,
	}).Info("connect to router")

	// connect to the router
	conn, err := grpc.DialContext(ctx, rc.router.Endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("unable to dial router: %w", err)
	}
	defer conn.Close()

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
	routersConnectedGauge.Add(1)
	defer routersConnectedGauge.Sub(1)

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
		case ev, ok := <-fromGateway:
			if ok {
				if ev.IsUplink() {
					// send event if router is interested in it
					if rc.router.InterestedIn(ev.uplink.device) {
						log.WithFields(logrus.Fields{
							"devaddr":       ev.uplink.device,
							"gw-network-id": ev.receivedFrom.NetworkID,
							"gw-local-id":   ev.receivedFrom.LocalID,
							"uplink_id":     uuid.FromBytesOrNil(ev.uplink.event.GetUplinkFrameEvent().UplinkFrame.GetRxInfo().GetUplinkId()),
						}).Info("forward uplink packet")

						var (
							owner   = common.BytesToAddress(ev.uplink.event.GetDownlinkTXAckEvent().GetAirtimeReceipt().GetOwner())
							airtime = time.Duration(ev.uplink.event.GetDownlinkTXAckEvent().GetAirtimeReceipt().GetAirtime()) * time.Millisecond
						)
						if rc.router.AllowAirtime(owner, airtime) {
							if err := eventStream.Send(ev.uplink.event); err != nil {
								return fmt.Errorf("unable to send event to router: %w", err)
							}
						} else {
							log.Warn("accounting prevents uplink, drop packet")
						}
					}
				} else if ev.IsJoin() {
					// send event if router is accepts the join request
					if rc.router.AcceptsJoin(ev.join.joinEUI) {
						log.WithFields(logrus.Fields{
							"joinEUI":       ev.join.joinEUI,
							"gw-network-id": ev.receivedFrom.NetworkID,
							"gw-local-id":   ev.receivedFrom.LocalID,
							"uplink_id":     uuid.FromBytesOrNil(ev.uplink.event.GetUplinkFrameEvent().UplinkFrame.GetRxInfo().GetUplinkId()),
						}).Info("forward join to router")
						if err := eventStream.Send(ev.join.event); err != nil {
							return fmt.Errorf("unable to send event to router: %w", err)
						}
					}
				} else if ev.IsDownlinkAck() {
					downlinkID := sha256.Sum256(append(rc.router.ThingsIXID[:], ev.downlinkAck.downlinkID...))
					// test if the router this client is connected to asked for the ACK
					if _, ok := pendingDownlinkAcks[downlinkID]; ok {
						// our router ordered the ACK
						delete(pendingDownlinkAcks, downlinkID)
						log.WithFields(logrus.Fields{
							"downlink_id":   fmt.Sprintf("%x", downlinkID[:8]),
							"gw-network-id": ev.receivedFrom.NetworkID,
							"gw-local-id":   ev.receivedFrom.LocalID,
						}).Info("forward downlink ACK to router")
						if err := eventStream.Send(ev.downlinkAck.event); err != nil {
							return fmt.Errorf("unable to send event to router: %w", err)
						}
					}
				} else if ev.IsOnlineOfflineEvent() {
					// send to all connected routers
					if err := eventStream.Send(ev.subOnlineOfflineEvent.event); err != nil {
						return fmt.Errorf("unable to send event to router: %w", err)
					}
				}
			}
		case event, ok := <-fromRouter:
			if !ok {
				return fmt.Errorf("connection with router closed or failed")
			}

			log.Info("received event from router")

			if airtimePayment := event.GetAirtimePaymentEvent(); airtimePayment != nil {
				rc.router.accounting.AddPayment(airtimePayment)
			}

			if isDownlinkAckEvent(event) {
				// router asked the end-device for a confirmation that it received
				// the downlink message. Store the downlink ID so its possible to
				// determine if a downlink ACK must be forwarded to the router this
				// client is connected to.
				downlinkID := sha256.Sum256(append(rc.router.ThingsIXID[:], event.GetDownlinkFrameEvent().GetDownlinkFrame().GetDownlinkId()...))
				log.WithField("downlink_id", fmt.Sprintf("%x", downlinkID[:8])).Info("received downlink ACK from router")
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
						// update router details
						if rc.router.Endpoint != router.Endpoint ||
							!reflect.DeepEqual(rc.router.NetIDs, router.NetIDs) ||

							rc.router.Owner != router.Owner {
							rc.router.Endpoint = router.Endpoint
							rc.router.NetIDs = router.NetIDs

							log.WithField("endpoint", rc.router.Endpoint)
							log.Info("updated router details")
						}
					}
				}

				if !found {
					log.Info("disconnect from router - router not part of ThingsIX anymore")
					return nil
				}
			}
		}
	}
}

func isDownlinkAckEvent(event *router.RouterToGatewayEvent) bool {
	// TODO: check if event is a downlink that requires an ACK
	// for now return true if its a downlink
	return event.GetDownlinkFrameEvent().GetDownlinkFrame() != nil

	// var (
	// 	phy   lorawan.PHYPayload
	// 	frame = event.GetDownlinkFrameEvent().GetDownlinkFrame()
	// )
	// logrus.Info("DBG BVK: isDownlinkAckEvent")
	// if frame != nil {
	// 	if err := phy.UnmarshalBinary(frame.PhyPayload); err != nil {
	// 		logrus.WithError(err).Warn("could not decode lorawan downlink frame")
	// 		return false
	// 	}
	// 	return phy.MHDR.MType == 5 //bin 100 MType.ConfirmedDataDown
	// }
	// return false
}

// routerEventsChan turns the given events readable into a readable go channel
// it closes the returned channel when receiving an event fails
func routerEventsChan(events router.RouterV1_EventsClient) <-chan *router.RouterToGatewayEvent {
	receivedRouterEvents := make(chan *router.RouterToGatewayEvent)
	go func() {
		defer close(receivedRouterEvents)
		for {
			in, err := events.Recv()
			if err != nil {
				statusCode := status.Code(err)
				if statusCode == codes.Canceled {
					return
				}
				if statusCode != codes.OK && statusCode != codes.Unknown {
					continue
				}
				if err != nil {
					logrus.WithError(err).Warn("unable to receive events from router")
					continue
				}
			}
			receivedRouterEvents <- in
		}
	}()
	return receivedRouterEvents
}
