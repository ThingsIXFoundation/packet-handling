package router

import (
	"fmt"
	"sync"

	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/brocaar/lorawan"
	"github.com/sirupsen/logrus"
)

type GatewayID lorawan.EUI64

func (gid GatewayID) String() string {
	return fmt.Sprintf("%s", gid)
}

type forwarderManagedGateway struct {
	forwarderID uint64
	gatewayID   GatewayID
	forwarder   chan<- *router.RouterToHotspotEvent
}

type GatewayPool struct {
	gatewaysMu sync.Mutex
	gateways   map[GatewayID]*forwarderManagedGateway
}

// SetOnline must be called when the forwarder has a gateway connected and is
// able to deliver packets to it.
func (gp *GatewayPool) SetOnline(forwarderID uint64, gatewayID GatewayID, forwarderEventSender chan<- *router.RouterToHotspotEvent) {
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
func (gp *GatewayPool) SetOffline(forwarderID uint64, gatewayID GatewayID) {
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
	event := &router.RouterToHotspotEvent{
		Event: &router.RouterToHotspotEvent_DownlinkFrameEvent{
			DownlinkFrameEvent: &router.DownlinkFrameEvent{
				DownlinkFrame: &frame,
			},
		},
	}
	gp.send(event)
}

func (gp *GatewayPool) send(event *router.RouterToHotspotEvent) {
	gp.gatewaysMu.Lock()
	defer gp.gatewaysMu.Unlock()

	// send packet to matched forwarders
	for gatewayID, gateway := range gp.gateways {

		if true { // TODO: only send event to forwarder that has reported the gateway is online
			gateway.forwarder <- event
			logrus.WithFields(logrus.Fields{
				"gatewayID": gatewayID,
				"eventType": fmt.Sprintf("%T", event.GetEvent()),
			}).Debug("send event to forwarder")
		}
	}
}
