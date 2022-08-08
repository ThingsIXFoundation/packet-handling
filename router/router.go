package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/integration"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type Router struct {
	router.UnimplementedRouterV1Server

	integration       integration.Integration
	integrationEvents chan *router.RouterToHotspotEvent
	forwarders        ForwardersPool
}

var _ router.RouterV1Server = (*Router)(nil)

func NewRouter(int integration.Integration) (*Router, error) {
	r := Router{integration: int, integrationEvents: make(chan *router.RouterToHotspotEvent)}
	r.integration.SetDownlinkFrameFunc(r.forwarders.DownlinkFrame)
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

func (r *Router) Events(forwarder router.RouterV1_EventsServer) error {
	fwdr := remoteForwarder{}
	if p, ok := peer.FromContext(forwarder.Context()); ok {
		fwdr.addr = p.Addr
	}

	logrus.WithField("forwarder", fwdr.addr).Info("forwarder connected")

	// turn forwarder into a readable event stream on which events from the
	// forwarder can be read. It is closed when the forwarder connection is
	// closed.
	forwarderEventsReceiver := r.forwarderEventStream(forwarder)

	// subscribe forwarder to integration events
	integrationEvents, unsubscribe, err := r.subscribeToIntegrations(fwdr.addr, forwarder)
	if err != nil {
		logrus.WithError(err).Warn("unable to register forwarder")
		return status.Error(codes.AlreadyExists, "forwarder already registered")
	}
	defer unsubscribe() // remove forwarder from integrations events after disconnect

	for {
		select {
		// events received from the forwarder that must be forwarded to the integrations
		case in, ok := <-forwarderEventsReceiver:
			if !ok {
				return nil
			}

			var (
				info   = in.GetGatewayInformation()
				pubKey = info.GetPublicKey()
				owner  = info.GetOwner()
				event  = in.GetEvent()
			)

			logrus.WithFields(logrus.Fields{
				"pubKey": fmt.Sprintf("%x", pubKey),
				"owner":  fmt.Sprintf("%x", owner),
			}).Debug("received event from forwarder")

			if uplink, ok := event.(*router.HotspotToRouterEvent_UplinkFrameEvent); ok {
				r.handleUplink(uplink)
			} else if downlinkAck, ok := event.(*router.HotspotToRouterEvent_DownlinkTXAckEvent); ok {
				r.handleDownlinkTxAck(downlinkAck)
			}
		// events from the integration that must be send to the forwarder
		case ev, ok := <-integrationEvents:
			if !ok {
				logrus.Info("integration events stream closed")
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

type unsubscribeForwarder func()

// subscribeToIntegrations subscribes the given forwarder to events coming from
// the integrations backend. It returns a channel from which integration events
// can be read and a closer function that must be called when the forwarder
// disconnects. This closer function unsubscribes the forwarder from the
// integrations backend.
func (r *Router) subscribeToIntegrations(addr net.Addr, forwarder router.RouterV1_EventsServer) (<-chan *router.RouterToHotspotEvent, unsubscribeForwarder, error) {
	integrationEvents := make(chan *router.RouterToHotspotEvent, 4096)
	fwd := &remoteForwarder{
		addr:     addr,
		gateways: nil,
		send:     integrationEvents,
	}

	if err := r.forwarders.Add(fwd); err != nil {
		return nil, nil, err
	}

	// Filter events targeted for other forwarders
	return integrationEvents, func() {
		r.forwarders.Delete(fwd)
	}, nil
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

func (r *Router) handleUplink(event *router.HotspotToRouterEvent_UplinkFrameEvent) {
	logrus.Info("got uplink frame") // TODO
}

func (r *Router) handleDownlinkTxAck(event *router.HotspotToRouterEvent_DownlinkTXAckEvent) {
	if ack := event.DownlinkTXAckEvent.GetDownlinkTXAck(); ack != nil {
		logrus.Info("got downlink tx ack") // TODO
	} else if receipt := event.DownlinkTXAckEvent.GetAirtimeReceipt(); receipt != nil {
		logrus.Info("got airtime receipt") // TODO
	}
}
