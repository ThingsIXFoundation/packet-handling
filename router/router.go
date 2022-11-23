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
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/integration"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/lorawan"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gofrs/uuid"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Router accepts connections from forwarders and can exchange message between
// gateways and the integration layer this router is configured for. It ensures
// that messages from the integrations layer are send to the correct forwarder
// that can deliver the packet to the targeted gateways.
type Router struct {
	router.UnimplementedRouterV1Server

	// router configuration
	config RouterConfig

	// integrations layer that handles received packages from gateways through
	// their forwarder or can send packages back to gateways when required
	integration integration.Integration

	// gatways keeps track which gateways are online and are connected through
	// which forwarder
	gateways *GatewayPool

	// joinFilterGenerator generates the join filter that is required by gateways
	// to be able to route joins (that don't have NetIds) to the right router
	joinFilterGenerator JoinFilterGenerator
}

var _ router.RouterV1Server = (*Router)(nil)

func NewRouter(cfg *Config, in integration.Integration) (*Router, error) {
	identity, err := loadRouterIdentity(cfg)
	if err != nil {
		return nil, err
	}

	logrus.WithFields(logrus.Fields{
		"router_id": identity.ID,
	}).Info("keyfile loaded")

	pool, err := NewGatewayPool()
	if err != nil {
		return nil, err
	}

	jfg, err := NewJoinFilterGenerator(cfg.Router)
	if err != nil {
		return nil, err
	}

	r := &Router{
		integration:         in,
		gateways:            pool,
		config:              cfg.Router,
		joinFilterGenerator: jfg,
	}

	// callbacks called by the integration layer
	in.SetDownlinkFrameFunc(pool.DownlinkFrame)
	in.SetGatewayConfigurationFunc(r.GatewayConfigurationHandler)
	in.SetRawPacketForwarderCommandFunc(r.RawPacketForwarderCommandHandler)
	in.SetGatewayCommandExecRequestFunc(r.GatewayCommandExecHandler)

	return r, nil
}

func (r *Router) GatewayConfigurationHandler(conf *gw.GatewayConfiguration) {
	logrus.Infof("got gateway configuration handle call: %x", conf.GetGatewayId())
}

func (r *Router) RawPacketForwarderCommandHandler(cmd *gw.RawPacketForwarderCommand) {
	logrus.Infof("got raw packet forwarder command handle call: %x", cmd.GetGatewayId())
}

func (r *Router) GatewayCommandExecHandler(cmdExec *gw.GatewayCommandExecRequest) {
	logrus.Infof("got gateway command exec handle call: %x", cmdExec.GetGatewayId())
}

func (r *Router) MustRun(ctx context.Context) {
	err := r.Run(ctx)
	if err != nil {
		logrus.WithError(err).Fatal("could not run router")
	}
}

func (r *Router) Run(ctx context.Context) error {
	logrus.WithField("addr", r.config.ForwarderListenerAddress()).Info("open forwarder listener")
	lis, err := net.Listen("tcp", r.config.ForwarderListenerAddress())
	if err != nil {
		return fmt.Errorf("unable to bind to endpoint: %w", err)
	}
	defer lis.Close()

	var (
		kaep = keepalive.EnforcementPolicy{
			MinTime:             15 * time.Second, // If a client pings more than once every 15 seconds, terminate the connection
			PermitWithoutStream: true,             // Allow pings even when there are no active streams
		}
		kasp = keepalive.ServerParameters{
			Time:    20 * time.Second, // Ping the client if it is idle for 5 seconds to ensure the connection is still active
			Timeout: 5 * time.Second,  // Wait 5 seconds for the ping ack before assuming the connection is dead
		}
		opts = []grpc.ServerOption{
			grpc.KeepaliveEnforcementPolicy(kaep),
			grpc.KeepaliveParams(kasp),
		}
		grpcSrv        = grpc.NewServer(opts...)
		grpcSrvStopped = make(chan struct{})
	)

	router.RegisterRouterV1Server(grpcSrv, r)

	go func() {
		defer close(grpcSrvStopped)
		if err := grpcSrv.Serve(lis); err != nil {
			logrus.WithError(err).Fatal("unable to start gRPC interface")
		}
		logrus.Info("operator service stopped")
	}()

	// Update the JoinFilter every RenewInterval
	go func() {
		err := r.joinFilterGenerator.UpdateFilter(ctx)
		if err != nil {
			logrus.WithError(err).Error("error while updating JoinFilter")
		}

		renewTicker := time.NewTicker(r.config.JoinFilterGenerator.RenewInterval)
		for {
			select {
			case <-renewTicker.C:
				ctx, cancel := context.WithTimeout(ctx, r.config.JoinFilterGenerator.RenewInterval/2)
				err := r.joinFilterGenerator.UpdateFilter(ctx)
				if err != nil {
					logrus.WithError(err).Error("error while updating JoinFilter")
				}
				// No defer here because the function only returns once
				cancel()
			case <-ctx.Done():
				logrus.Info("stopping JoinFilter update loop")
				return
			}
		}
	}()

	// wait until the context expires and stop the service
	<-ctx.Done()
	grpcSrv.GracefulStop()

	// wait till the service stopped
	<-grpcSrvStopped

	return nil
}

func (r *Router) JoinFilter(ctx context.Context, req *router.JoinFilterRequest) (*router.JoinFilterResponse, error) {
	filter, err := r.joinFilterGenerator.JoinFilter(ctx)
	if err != nil {
		logrus.WithError(err).Error("error while getting join filter")
		return nil, status.Errorf(codes.Internal, "error while getting join filter")
	}
	return &router.JoinFilterResponse{JoinFilter: filter}, nil
}

var (
	connectedForwarders int32
)

// Called by the forwarder to start bi-directional communication stream on which
// events from gateways are send through the forwarder to this router or in
// reverse from the integrations connected to this router to the forwarder and
// eventually to its gateways that the event is targeted for.
func (r *Router) Events(forwarder router.RouterV1_EventsServer) error {
	// generate unique identifier for connected forwarder
	var (
		forwarderID, err = uuid.NewV4()
		fwdlog           = logrus.WithField("forwarder_id", forwarderID)
	)
	if err != nil {
		logrus.WithError(err).Error("unable to generate forwarder id")
		return status.Error(codes.Internal, "interal error")
	}

	// report that forwarder connected
	if p, ok := peer.FromContext(forwarder.Context()); ok {
		fwdlog = fwdlog.WithField("addr", p.Addr)
	}
	fwdlog.Info("forwarder connected")

	connectedForwardersGauge.Set(float64(atomic.AddInt32(&connectedForwarders, 1)))
	defer func() { connectedForwardersGauge.Set(float64(atomic.AddInt32(&connectedForwarders, -1))) }()

	// turn forwarder into a readable event channel on which events from the
	// forwarder can be read. It is closed when the connection closes. It is
	// closed in a background routine that forwarderEventStream starts.
	forwarderEvents := r.forwarderEventStream(forwarderID, forwarder)

	// open a channel to send events received from the integrations layer to
	// the forwarder and its gateways.
	integrationEvents := make(chan *router.RouterToGatewayEvent, 256)
	defer close(integrationEvents)

	for {
		fwdlog.Debug("process events")
		select {
		case fwdEvent, ok := <-forwarderEvents: // wait for forwarder events
			if !ok {
				r.gateways.AllOffline(forwarderID)
				fwdlog.Info("forwarder disconnected")
				return nil
			}

			var (
				info                  = fwdEvent.GetGatewayInformation()
				pubKey                = info.GetPublicKey()
				gatewayNetworkID, err = gateway.GatewayPublicKeyToID(pubKey)
				gatewayOwner          = common.BytesToAddress(info.GetOwner())
				event                 = fwdEvent.GetEvent()
			)

			if err != nil {
				fwdlog.WithError(err).Warn("unable to decode gateway ID from forwarder event")
				continue
			}
			log := fwdlog.WithFields(logrus.Fields{
				"gw_network_id": gatewayNetworkID,
				"gw_owner":      gatewayOwner,
			})

			if uplink, ok := event.(*router.GatewayToRouterEvent_UplinkFrameEvent); ok {
				r.handleUplink(log, gatewayNetworkID, uplink)
			} else if downlinkAck, ok := event.(*router.GatewayToRouterEvent_DownlinkTXAckEvent); ok {
				r.handleDownlinkTxAck(log, gatewayNetworkID, downlinkAck)
			} else if status, ok := event.(*router.GatewayToRouterEvent_StatusEvent); ok {
				r.handleStatus(log, forwarderID, gatewayNetworkID, gatewayOwner, status, integrationEvents)
			} else {
				log.Warn("received unsupported forwarder event")
			}
		case ev, ok := <-integrationEvents: // wait for integration events
			if !ok {
				// TODO: determine if disconnecting is the right thing to do if the integrations layer stopped
				fwdlog.Info("integration events stream closed, disconnect forwarder")
				r.gateways.AllOffline(forwarderID)
				return status.Error(codes.Unavailable, "integration stopped")
			}
			if err := forwarder.Send(ev); err != nil {
				fwdlog.WithError(err).WithField("event", ev.GetEvent()).Warn("unable to send event to forwarder")
				return status.Error(status.Code(err), "unable to send event to forwarder")
			} else {
				fwdlog.Info("event sent to forwarder")
			}
		}
	}
}

// forwarderEventerRWChan turns the given events readable into a readable go
// channel with a reader and writer.
func (r *Router) forwarderEventStream(id uuid.UUID, events router.RouterV1_EventsServer) <-chan *router.GatewayToRouterEvent {
	var (
		log                     = logrus.WithField("forwarder_id", id)
		receivedForwarderEvents = make(chan *router.GatewayToRouterEvent, 4096)
	)

	go func() {
		defer close(receivedForwarderEvents)
		for {
			in, err := events.Recv()
			switch status.Code(err) {
			case codes.OK:
				// got event from forwarder
				receivedForwarderEvents <- in
			case codes.Canceled:
				// forwarder disconnected, logged somewhere else
				return
			default:
				log.WithError(err).Error("received error from forwarder")
			}
		}
	}()
	return receivedForwarderEvents
}

func (r *Router) handleStatus(log *logrus.Entry, forwarderID uuid.UUID, gatewayID lorawan.EUI64, gatewayOwner common.Address, status *router.GatewayToRouterEvent_StatusEvent, integrationEvents chan<- *router.RouterToGatewayEvent) {
	// forwarders send periodically (~30s) an indication if a gateway is still
	// online or when a gateway goes offline
	online := status.StatusEvent.GetOnline()
	if online {
		r.gateways.SetOnline(forwarderID, gatewayID, gatewayOwner, integrationEvents)
	} else {
		r.gateways.SetOffline(forwarderID, gatewayID)
	}
	log.WithField("online", online).Debug("gateway status")
	err := r.integration.SetGatewaySubscription(online, gatewayID)
	if err != nil {
		logrus.WithError(err).Error("could not set gateway subscription")
	}
}

func (r *Router) handleUplink(log *logrus.Entry, gatewayNetworkID lorawan.EUI64, event *router.GatewayToRouterEvent_UplinkFrameEvent) {
	var (
		frame                          = event.UplinkFrameEvent.GetUplinkFrame()
		gatewayNetworkIDFromFrame, err = utils.Eui64FromString(frame.GetRxInfo().GetGatewayId())
		uplinkID                       = frame.GetRxInfo().GetUplinkId()
	)
	log = log.WithFields(logrus.Fields{
		"uplink_id": uplinkID,
	})

	if err != nil {
		log.WithError(err).Error("unable to decode gateway network id from uplink frame, drop uplink")
		return
	}

	if gatewayNetworkID != gatewayNetworkIDFromFrame {
		log.WithField("frame_gw_network-id", gatewayNetworkIDFromFrame).Error("received uplink with gateway info id != frame gateway id, drop uplink")
		return
	}

	if err := r.integration.PublishEvent(gatewayNetworkID, integration.EventUp, uplinkID, frame); err != nil {
		log.WithError(err).WithFields(logrus.Fields{
			"event_type": integration.EventUp,
		}).Error("forwarded uplink event to integrations failed, drop uplink")

		uplinksCounter.WithLabelValues(gatewayNetworkID.String(), "failed").Inc()
		return
	}

	log.WithFields(logrus.Fields{
		"event_type": integration.EventUp,
	}).Info("forwarded uplink event to integration")

	uplinksCounter.WithLabelValues(gatewayNetworkID.String(), "success").Inc()
}

func (r *Router) handleDownlinkTxAck(log *logrus.Entry, gatewayNetworkID lorawan.EUI64, event *router.GatewayToRouterEvent_DownlinkTXAckEvent) {
	var (
		ack                            = event.DownlinkTXAckEvent.GetDownlinkTXAck()
		gatewayNetworkIDFromFrame, err = utils.Eui64FromString(ack.GetGatewayId())
		downlinkId                     = ack.GetDownlinkId()
	)
	log = log.WithField("downlink_id", downlinkId)

	if err != nil {
		log.WithError(err).Error("invalid gatewayID, drop downlink-tx-ack")
		return
	}
	if gatewayNetworkID != gatewayNetworkIDFromFrame {
		log.WithField("frame_gw_network-id", gatewayNetworkIDFromFrame).
			Error("received downlink ack with gateway info id != frame gateway id, drop downlink-tx-ack")
		return
	}

	if err := integration.GetIntegration().PublishEvent(gatewayNetworkID, integration.EventAck, downlinkId, ack); err != nil {
		log.WithError(err).WithField("event_type", integration.EventAck).Error("unable to send downlink ACK to integration")
		downlinksCounter.WithLabelValues(gatewayNetworkID.String(), "failed").Inc()
		return
	}

	log.Info("send gateway downlink ACK to integration")

	downlinksCounter.WithLabelValues(gatewayNetworkID.String(), "success").Inc()
}
