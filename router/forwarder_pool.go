package router

import (
	"fmt"
	"net"
	"sync"

	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
)

type remoteForwarder struct {
	id       uint
	addr     net.Addr  // forwarders external address
	gateways [][8]byte // gateways connected to the forwarder
	send     chan *router.RouterToHotspotEvent
}

type ForwardersPool struct {
	forwardersMu sync.Mutex
	forwarders   map[uint]*remoteForwarder
}

func (fp *ForwardersPool) Add(fwdr *remoteForwarder) error {
	fp.forwardersMu.Lock()
	defer fp.forwardersMu.Unlock()

	if _, found := fp.forwarders[fwdr.id]; found {
		return fmt.Errorf("forwarder already online")
	}

	fp.forwarders[fwdr.id] = fwdr
	return nil
}

func (fp *ForwardersPool) Delete(fwdr *remoteForwarder) {
	fp.forwardersMu.Lock()
	defer fp.forwardersMu.Unlock()
	if f, found := fp.forwarders[fwdr.id]; found {
		close(f.send)
		delete(fp.forwarders, fwdr.id)
	}
}

func (fp *ForwardersPool) send(event *router.RouterToHotspotEvent) {
	fp.forwardersMu.Lock()
	defer fp.forwardersMu.Unlock()

	// send packet to matched forwarders
	for _, fwdr := range fp.forwarders {
		// if fwd.match(event) {
		fwdr.send <- event
		// }
	}
}

func (fp *ForwardersPool) DownlinkFrame(frame gw.DownlinkFrame) {
	event := &router.RouterToHotspotEvent{
		Event: &router.RouterToHotspotEvent_DownlinkFrameEvent{
			DownlinkFrameEvent: &router.DownlinkFrameEvent{
				DownlinkFrame: &frame,
			},
		},
	}
	fp.send(event)
}
