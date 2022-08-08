package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"

	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/integration"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
)

// Router accepts connections from forwarders and can exchange message between
// gateways and the integration layer this router is configured for. It ensures
// that messages from the integrations layer are send to the correct forwarder
// that can deliver the packet to the targeted gateways.
type Router struct {
	router.UnimplementedRouterV1Server

	// integrations layer that handles received packages from gateways through
	// their forwarder or can send packages back to gateways when required
	integration integration.Integration

	// gatways keeps track which gateways are online and are connected through
	// which forwarder
	gateways GatewayPool

	// atomic counter that is incremented each time a forwarder connects and used
	// internally to give each forwarder an unique id. This id is used when the
	// forwarder disconnects to remove all online gateways from the gateways
	// pool that are connected though the disconnected forwarder.
	forwarderID uint64
}

var _ router.RouterV1Server = (*Router)(nil)

func NewRouter(int integration.Integration) (*Router, error) {
	r := Router{integration: int}
	r.integration.SetDownlinkFrameFunc(r.gateways.DownlinkFrame)
	// TODO: additional callbacks
	return &r, nil
}

func (r *Router) Run(ctx context.Context, endpoint string) error {
	lis, err := net.Listen("tcp", endpoint)
	if err != nil {
		return fmt.Errorf("unable to bind to endpoint: %w", err)
	}
	defer lis.Close()

	var (
		opts           = []grpc.ServerOption{}
		grpcSrv        = grpc.NewServer(opts...)
		grpcSrvStopped = make(chan struct{})
	)

	router.RegisterRouterV1Server(grpcSrv, r)

	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			logrus.WithError(err).Fatal("unable to start gRPC interface")
		}
		logrus.Info("operator service stopped")
		close(grpcSrvStopped)
	}()

	// wait until the context expires and stop the service
	<-ctx.Done()
	grpcSrv.GracefulStop()

	// wait till the service stopped
	<-grpcSrvStopped

	return nil
}

func (r *Router) NetIds(ctx context.Context, req *router.NetIdsRequest) (*router.NetIdsResponse, error) {
	panic("not implemented") // TODO: Implement
}

func (r *Router) JoinFilter(ctx context.Context, req *router.JoinFilterRequest) (*router.JoinFilterResponse, error) {
	panic("not implemented") // TODO: Implement
}

// Called by the forwarder to start bi-directional communication stream on which
// events from gateways are send through the forwarder to this router or in
// reverse from the integrations connected to this router to the forwarder and
// eventually to its gateways that the event is targeted for.
func (r *Router) Events(forwarder router.RouterV1_EventsServer) error {
	// generate unique identifier for connected forwarder
	forwarderID := atomic.AddUint64(&r.forwarderID, 1)

	// report that forwarder connected
	if p, ok := peer.FromContext(forwarder.Context()); ok {
		logrus.WithFields(logrus.Fields{
			"forwarder": p.Addr,
			"ID":        forwarderID}).Info("forwarder connected")
	} else {
		logrus.WithField("ID", forwarderID).Info("forwarder connected")
	}

	// turn forwarder into a readable event channel on which events from the
	// forwarder can be read. It is closed when the forwarder connection is
	// closed.
	forwarderEventsReceiver := r.forwarderEventStream(forwarder)

	// channel that the integration layer uses to forward messages to gateways
	// that are managed by this forwarder.
	integrationEvents := make(chan *router.RouterToHotspotEvent, 256)
	defer close(integrationEvents)

	for {
		select {
		// events received from the forwarder that must be forwarded to the integrations
		case in, ok := <-forwarderEventsReceiver:
			if !ok {
				// unable to retrieve events from forwarder, probably disconnected
				logrus.WithField("ID", forwarderID).Info("forwarder disconnected")
				r.gateways.AllOffline(forwarderID)
				return nil
			}

			var (
				info      = in.GetGatewayInformation()
				pubKey    = info.GetPublicKey()
				gatewayID = info.GetGatewayId() // TODO: as GatewayID
				owner     = info.GetOwner()
				event     = in.GetEvent()
			)

			logrus.WithFields(logrus.Fields{
				"pubKey":  fmt.Sprintf("%x", pubKey),
				"owner":   fmt.Sprintf("%x", owner),
				"ID":      forwarderID,
				"gateway": gatewayID,
			}).Debug("received event from forwarder")

			if uplink, ok := event.(*router.HotspotToRouterEvent_UplinkFrameEvent); ok {
				r.handleUplink(uplink)
			} else if downlinkAck, ok := event.(*router.HotspotToRouterEvent_DownlinkTXAckEvent); ok {
				r.handleDownlinkTxAck(downlinkAck)
			} else if status, ok := event.(*router.HotspotToRouterEvent_StatusEvent); ok {
				r.handleStatus(forwarderID, gatewayID, status, integrationEvents)
			}
		// events from the integrations layer that must be send to a gateway through
		// the connected forwarder
		case ev, ok := <-integrationEvents:
			if !ok {
				// TODO: determine if disconnecting is the right thing to do if the integrations layer stopped
				logrus.Info("integration events stream closed")
				logrus.WithField("ID", forwarderID).Info("forwarder disconnected")
				r.gateways.AllOffline(forwarderID)
				return nil
			}
			if err := forwarder.Send(ev); err != nil {
				logrus.WithError(err).WithField("event", ev.GetEvent()).Warn("unable to send event to forwarder")
			} else {
				logrus.WithField("event", ev.GetEvent()).Info("send event to forwarder")
			}
		}
	}
}

// forwarderEventerRWChan turns the given events readable into a readable go
// channel with a reader and writer.
func (r *Router) forwarderEventStream(events router.RouterV1_EventsServer) <-chan *router.HotspotToRouterEvent {
	receivedForwarderEvents := make(chan *router.HotspotToRouterEvent, 4096)
	go func() {
		defer close(receivedForwarderEvents)
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
			receivedForwarderEvents <- in
		}
	}()
	return receivedForwarderEvents
}

func (r *Router) handleStatus(forwarderID uint64, gatewayID GatewayID, status *router.HotspotToRouterEvent_StatusEvent, integrationEvents chan<- *router.RouterToHotspotEvent) {
	// forwarders send periodically (~30s) an indication if a gateway is still
	// online or when a gateway goes offline
	online := status.StatusEvent.GetOnline()
	if online {
		r.gateways.SetOnline(forwarderID, gatewayID, integrationEvents)
	} else {
		r.gateways.SetOffline(forwarderID, gatewayID)
	}
}

func (r *Router) handleUplink(event *router.HotspotToRouterEvent_UplinkFrameEvent) {
	logrus.Info("got uplink frame") // TODO: send to integrations layer
}

func (r *Router) handleDownlinkTxAck(event *router.HotspotToRouterEvent_DownlinkTXAckEvent) {
	if ack := event.DownlinkTXAckEvent.GetDownlinkTXAck(); ack != nil {
		logrus.Info("got downlink tx ack") // TODO: send to integrations layer
	} else if receipt := event.DownlinkTXAckEvent.GetAirtimeReceipt(); receipt != nil {
		logrus.Info("got airtime receipt") // TODO: send to integrations layer
	}
}
