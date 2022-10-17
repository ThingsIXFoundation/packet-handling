package forwarder

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ThingsIXFoundation/packet-handling/airtime"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type RouterClient struct {
	client        router.RouterV1Client
	conn          *grpc.ClientConn
	gatewayEvents chan *router.GatewayToRouterEvent
}

type routerEvent struct {
	router *Router
	ev     *router.RouterToGatewayEvent
}

// DialRouter tries to connect to the router at the given endpoint.
func DialRouter(ctx context.Context, endpoint string) (*RouterClient, error) {
	conn, err := grpc.DialContext(ctx, endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("unable to dial router: %w", err)
	}

	return &RouterClient{
		client:        router.NewRouterV1Client(conn),
		conn:          conn,
		gatewayEvents: make(chan *router.GatewayToRouterEvent),
	}, nil
}

// Run the router until the given context expires and emit reveived router
// events on the given routerEvents channel.
func (rc *RouterClient) Run(ctx context.Context, r *Router, routerEvents chan<- *routerEvent) error {
	// create bi-directory event stream to exchange events with the router
	eventStream, err := rc.client.Events(ctx)
	if err != nil {
		return fmt.Errorf("unable to obtain router event stream: %w", err)
	}

	// turn incoming router events stream into a readable channel
	re := routerEventsChan(eventStream)

	// ensure that connection is closed when stopped
	defer rc.conn.Close()

	for {
		logrus.Debug("waiting for gateway events")
		select {
		// wait for gateway events that must be emitted to the router
		case gatewayEvent := <-rc.gatewayEvents:
			sendErr := eventStream.Send(gatewayEvent)
			if sendErr != nil {
				logrus.WithError(err).Errorf("unable to deliver gateway event")
			}
		// wait for incoming events from the router that must be send to the gateway
		case event, ok := <-re:
			if !ok {
				return nil
			}
			// broadcast router event to forwarder that calls the appropiate callback
			routerEvents <- &routerEvent{
				router: r,
				ev:     event,
			}
		// wait for the context to expire
		case <-ctx.Done():
			return ctx.Err()
		}
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
			if errors.Is(err, io.EOF) {
				logrus.Info("router disconnected")
				return
			}
			if err != nil {
				logrus.WithError(err).Warn("unable to receive events from router")
				return
			}
			receivedRouterEvents <- in
		}
	}()
	return receivedRouterEvents
}

// DeliverDataUp forwards the given uplink frame to the router
// Caller is response to close the returned channel
func (rc *RouterClient) DeliverDataUp(gw *gateway.Gateway, frame gw.UplinkFrame) {

	airtime, _ := airtime.UplinkAirtime(frame)

	// TODO: Keep track of airtime

	event := router.GatewayToRouterEvent{
		GatewayInformation: &router.GatewayInformation{
			PublicKey: gw.CompressedPublicKeyBytes,
			Owner:     gw.Owner.Bytes(),
		},
		Event: &router.GatewayToRouterEvent_UplinkFrameEvent{
			UplinkFrameEvent: &router.UplinkFrameEvent{
				UplinkFrame: &frame,
				AirtimeReceipt: &router.AirtimeReceipt{
					Owner:   gw.Owner.Bytes(),
					Airtime: uint32(airtime.Milliseconds()),
				},
			},
		},
	}

	rc.gatewayEvents <- &event

}

// Caller is response to close the returned channel
func (rc *RouterClient) DeliverJoin(gw *gateway.Gateway, frame gw.UplinkFrame) {
	rc.DeliverDataUp(gw, frame) // Internally joins and data-ups are the same
}

// Caller is response to close the returned channel
func (rc *RouterClient) DeliverGatewayStatus(gw *gateway.Gateway, online bool) {
	event := router.GatewayToRouterEvent{
		GatewayInformation: &router.GatewayInformation{
			PublicKey: gw.CompressedPublicKeyBytes,
			Owner:     gw.Owner.Bytes(),
		},
		Event: &router.GatewayToRouterEvent_StatusEvent{
			StatusEvent: &router.StatusEvent{
				Online: online,
			},
		},
	}

	rc.gatewayEvents <- &event
}
