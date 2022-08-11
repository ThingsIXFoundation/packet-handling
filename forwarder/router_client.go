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
)

type inProgressHotspotEventSend struct {
	ev  *router.HotspotToRouterEvent
	err chan error
}

type RouterClient struct {
	client        router.RouterV1Client
	conn          *grpc.ClientConn
	hotspotEvents chan *inProgressHotspotEventSend
}

type routerEvent struct {
	router *Router
	ev     *router.RouterToHotspotEvent
}

// DialRouter tries to connect to the router at the given endpoint.
func DialRouter(ctx context.Context, endpoint string) (*RouterClient, error) {
	conn, err := grpc.DialContext(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("unable to dial router: %w", err)
	}

	return &RouterClient{
		client:        router.NewRouterV1Client(conn),
		conn:          conn,
		hotspotEvents: make(chan *inProgressHotspotEventSend),
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
		select {
		// wait for hotspot events that must be emitted to the router
		case hotspotEvent := <-rc.hotspotEvents:
			sendErr := eventStream.Send(hotspotEvent.ev)
			if hotspotEvent.err <- sendErr; sendErr != nil {
				return fmt.Errorf("unable to send hotspot event to router: %w", err)
			}
		// wait for incoming events from the router that must be send to the hotspot
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
func routerEventsChan(events router.RouterV1_EventsClient) <-chan *router.RouterToHotspotEvent {
	receivedRouterEvents := make(chan *router.RouterToHotspotEvent)
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
func (rc *RouterClient) DeliverDataUp(gw *gateway.Gateway, frame gw.UplinkFrame) chan error {
	errChan := make(chan error)

	airtime, err := airtime.UplinkAirtime(frame)
	if err != nil {
		errChan <- err
	}

	// TODO: Keep track of airtime

	event := router.HotspotToRouterEvent{
		GatewayInformation: &router.GatewayInformation{
			Id:        gw.NetworkGatewayID.Bytes(),
			PublicKey: gw.PublicKeyBytes,
			Owner:     gw.Owner,
		},
		Event: &router.HotspotToRouterEvent_UplinkFrameEvent{
			UplinkFrameEvent: &router.UplinkFrameEvent{
				UplinkFrame: &frame,
				AirtimeReceipt: &router.AirtimeReceipt{
					Owner:   gw.Owner,
					Airtime: uint32(airtime.Milliseconds()),
				},
			},
		},
	}

	rc.hotspotEvents <- &inProgressHotspotEventSend{
		ev:  &event,
		err: errChan,
	}

	return errChan
}

// Caller is response to close the returned channel
func (rc *RouterClient) DeliverJoin(gw *gateway.Gateway, frame gw.UplinkFrame) chan error {
	return rc.DeliverDataUp(gw, frame) // Internally joins and data-ups are the same
}

// Caller is response to close the returned channel
func (rc *RouterClient) DeliverGatewayStatus(gw *gateway.Gateway, online bool) chan error {
	event := router.HotspotToRouterEvent{
		GatewayInformation: &router.GatewayInformation{
			Id:        gw.NetworkGatewayID.Bytes(),
			PublicKey: gw.PublicKeyBytes,
			Owner:     gw.Owner,
		},
		Event: &router.HotspotToRouterEvent_StatusEvent{
			StatusEvent: &router.StatusEvent{
				Online: online,
			},
		},
	}

	errChan := make(chan error)
	rc.hotspotEvents <- &inProgressHotspotEventSend{
		ev:  &event,
		err: errChan,
	}

	return errChan
}
