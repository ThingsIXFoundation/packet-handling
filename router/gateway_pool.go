package router

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/sirupsen/logrus"
)

type forwarderManagedGateway struct {
	forwarderID uint64
	gatewayID   gateway.GatewayID
	forwarder   chan<- *router.RouterToGatewayEvent
}

type GatewayPool struct {
	gatewaysMu sync.Mutex
	gateways   map[gateway.GatewayID]*forwarderManagedGateway
}

func NewGatewayPool() (*GatewayPool, error) {
	return &GatewayPool{
		gatewaysMu: sync.Mutex{},
		gateways:   make(map[gateway.GatewayID]*forwarderManagedGateway),
	}, nil
}

// SetOnline must be called when the forwarder has a gateway connected and is
// able to deliver packets to it.
func (gp *GatewayPool) SetOnline(forwarderID uint64, gatewayID gateway.GatewayID, forwarderEventSender chan<- *router.RouterToGatewayEvent) {
	gp.gatewaysMu.Lock()
	defer gp.gatewaysMu.Unlock()

	gp.gateways[gatewayID] = &forwarderManagedGateway{
		forwarderID: forwarderID,
		gatewayID:   gatewayID,
		forwarder:   forwarderEventSender,
	}

	logrus.WithFields(logrus.Fields{
		"forwarder": forwarderID,
		"gateway":   gatewayID}).Debug("gateway online")
}

// SetOffline must be called when a forwarder detects one of its gateways is
// offline so it won't be able to deliver packages to it.
func (gp *GatewayPool) SetOffline(forwarderID uint64, gatewayID gateway.GatewayID) {
	gp.gatewaysMu.Lock()
	defer gp.gatewaysMu.Unlock()

	delete(gp.gateways, gatewayID)

	logrus.WithFields(logrus.Fields{
		"forwarder": forwarderID,
		"gateway":   gatewayID}).Debug("gateway offline")
}

// AllOffline must be called whan a forwarder goes offline and all its gateways
// are therefore immediatly offline.
func (gp *GatewayPool) AllOffline(forwarderID uint64) {
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
	gp.send(frame.GatewayId, event)
}

func (gp *GatewayPool) send(addressedGatewayID []byte, event *router.RouterToGatewayEvent) {
	gp.gatewaysMu.Lock()
	defer gp.gatewaysMu.Unlock()

	logrus.Debug("hier")

	// send packet to matched forwarders
	for gatewayID, gateway := range gp.gateways {

		if bytes.Equal(gatewayID[:], addressedGatewayID[:]) {
			gateway.forwarder <- event
			logrus.WithFields(logrus.Fields{
				"gatewayID": gatewayID,
				"eventType": fmt.Sprintf("%T", event.GetEvent()),
			}).Debug("send event to forwarder")
		}
	}
}
