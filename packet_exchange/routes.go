package packetexchange

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/FastFilter/xorfilter"
	"github.com/ThingsIXFoundation/packet-handling/packet_exchange/broadcast"
	"github.com/brocaar/lorawan"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
)

// RoutesUpdaterFunc is a callback that retrieves routing information from
// ThingsIX. It returns the set of routers or an error in case the set could
// not be fetched.
type RoutesUpdaterFunc func() ([]*Router, error)

type ID [32]byte

func (id ID) String() string {
	if id == ([32]byte{}) {
		return "default"
	}
	return fmt.Sprintf("0x%x", id[:])
}

type Router struct {
	ThingsIXID ID
	Endpoint   string
	Default    bool
	NetIDs     []lorawan.NetID
	Owner      common.Address
	joinFilter xorfilter.Xor8
	accounting Accounter
}

func (r Router) String() string {
	return r.ThingsIXID.String()
}

func NewRouter(id [32]byte, endpoint string, def bool, netIDs []lorawan.NetID, owner common.Address, accounting Accounter) *Router {
	return &Router{
		ThingsIXID: id,
		Endpoint:   endpoint,
		Default:    def,
		NetIDs:     netIDs,
		Owner:      owner,
		accounting: accounting,
	}
}

func (r Router) AllowAirtime(owner common.Address, airtime time.Duration) bool {
	return r.Default || r.accounting.Allow(owner, airtime)
}

// InterestedIn returns an indication if router is interested in a message
// from a device with the given devaddr.
func (r Router) InterestedIn(addr lorawan.DevAddr) bool {
	if r.Default { // default routers receive data from all devices
		return true
	}
	for _, netID := range r.NetIDs {
		if addr.IsNetID(netID) {
			return true
		}
	}
	return false
}

// AcceptsJoin returns an indication if the device that wants to join the
// network is accepted by this router.
func (r Router) AcceptsJoin(joinEUI lorawan.EUI64) bool {
	if len(r.joinFilter.Fingerprints) == 0 {
		return false
	}
	return r.joinFilter.Contains(binary.BigEndian.Uint64(joinEUI[:]))
}

// RoutingTable takes care of the communication between the Packet Exchange and
// external routers. Received data from the packet exchange is routed to routers
// that have expressed interest in it.
type RoutingTable struct {
	// routesFetcher is the callback to retrieve the latest routes
	routesFetcher RoutesUpdaterFunc

	// routesTableBroadcaster emits the set of fresh fetched routers. Router
	// clients receive the fresh set and disconnect if their router is removed
	// from the  list or update their configuration. A seperate routine will
	// spin up new router clients if new registered routers.
	routesTableBroadcaster *broadcast.Broadcaster[[]*Router]

	// routesUpdateInterval holds the interval on which routesFetcher is called
	// to fetch the latest routes. This is dynamic, of the fetch failed this
	// interval is shorted to retry it more often. After a successfull fetch the
	// interval is set to the configured interval.
	routesUpdateInterval    time.Duration
	routesUpdateIntervalCfg time.Duration

	// networkEvents is a stream with messages received from the routers on the
	// netwerk. The router clients will send their data on it so the packet
	// exchange can read from it and send it to the backend that sends it back to
	// the gateway.
	networkEvents chan *NetworkEvent

	// Data received from gateways. Router clients listen on this channel, determine
	// if the event is of interest of the router they are connected to, and forward
	// it if needed.
	gatewayEvents *broadcast.Broadcaster[*GatewayEvent]

	// defaultRoutes contains the set of default routers, these are configured
	// locally and always get send all data that is received from the gateways.
	// These routers don't have to be registered in ThingsIX.
	defaultRoutes []*Router
}

// Run starts the integration with the routers on the ThingsIX network until the
// given context expires.
//
// It fetches the list of registered routers and opens connects with these routers.
// For each router a client is started that maintains the connection with the router
// and exchanges messages with it. Periodically the latest set of registered routers
// is fetched and nieuw router clients are started for fresh registered routers or
// clients are stopped/updated when they are either removed or updated.
func (r *RoutingTable) Run(ctx context.Context) {
	// start the broadcaster for clients to listen on router table updates
	r.routesTableBroadcaster.Run()

	// start a listener that starts new router clients for routes that are
	// added to the routing table.
	go r.keepRouteTableUpToDate(ctx)

	// run router clients to default configured routers
	go r.runDefaultRouting(ctx)

	for {
		select {
		case <-time.After(r.routesUpdateInterval):
			// assume update fails and retry it in 1 minute
			r.routesUpdateInterval = time.Minute

			// fetch the latest known set of routers from ThingsIX
			routers, err := r.routesFetcher()
			if err != nil {
				logrus.WithError(err).Warn("unable to refresh routers")
				continue
			}

			// try to submit routing information to router clients
			if r.routesTableBroadcaster.TryBroadcast(routers) {
				// successfull, refresh on configured update interval
				r.routesUpdateInterval = r.routesUpdateIntervalCfg
			} else {
				logrus.Warn("unable to refresh routing table")
			}
		case <-ctx.Done():
			logrus.Info("networking routing stopped")
			return
		}
	}
}

func (r *RoutingTable) keepRouteTableUpToDate(ctx context.Context) {
	var (
		newRoutes      = make(chan []*Router)
		existingRoutes = mapset.NewThreadUnsafeSet[[32]byte]()
	)
	// routes table broadcaster emits the latest retrieved routes
	// periodically.
	r.routesTableBroadcaster.Subscribe(newRoutes)
	defer r.routesTableBroadcaster.Unsubscribe(newRoutes)

	for {
		select {
		case routers := <-newRoutes:
			// new set of routes fetched, determine which one are new
			// ans startup a client for them. This client will from
			// then on process new routes and determine if it need to
			// stop (route deleted) or update its configuration (route
			// updated)
			latestRoutes := mapset.NewThreadUnsafeSet[[32]byte]()
			for _, r := range routers {
				latestRoutes.Add(r.ThingsIXID)
			}

			// determine which routes are new and start a client. Existing
			// routes are managed by a client that is subscribed to the
			// new routes broadcaster and will either stop if they are
			// dropped in the routing table or update their routing info
			// if its updated. Therefore only start clients for new routes.
			newRoutesCount := 0
			latestRoutes.Each(func(id [32]byte) bool {
				if !existingRoutes.Contains(id) {
					for _, router := range routers {
						if router.ThingsIXID == id {
							go NewRouterClient(ctx, router, r.routesTableBroadcaster, r.networkEvents, r.gatewayEvents).Run(ctx)
							existingRoutes.Add(id) // add to routing table
							break
						}
					}
					newRoutesCount++
				}
				return false // continue iterating over set
			})

			logrus.WithField("new-routes", newRoutesCount).Info("refreshed routing table")

			// make the latest routes the new route table
			existingRoutes = latestRoutes
		case <-ctx.Done():
			return
		}
	}
}

// runDefaultRouting start router clients for default configured routers
func (r *RoutingTable) runDefaultRouting(ctx context.Context) {
	var allStopped sync.WaitGroup
	for _, dr := range r.defaultRoutes {
		allStopped.Add(1)
		cpy := dr
		go func() {
			// run router client until ctx expires
			NewRouterClient(ctx, cpy, r.routesTableBroadcaster, r.networkEvents, r.gatewayEvents).Run(ctx)
			allStopped.Done()
		}()
	}
	// wait for shutdown signal and all router clients have stopped
	<-ctx.Done()
	allStopped.Wait()
	logrus.Trace("default routers disconnected")
}

func buildRoutingTable(cfg *Config) (*RoutingTable, error) {
	routes, err := obtainThingsIXRoutesFunc(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to determine method to fetch ThingsIX routes: %w", err)
	}

	return &RoutingTable{
		routesFetcher:           routes,
		routesUpdateInterval:    time.Second, // first time try to fetch routing information allmost immediatly
		routesUpdateIntervalCfg: cfg.Routes.UpdateInterval,
		routesTableBroadcaster:  broadcast.New[[]*Router](1),
		defaultRoutes:           cfg.Routes.Default,
		networkEvents:           make(chan *NetworkEvent, 1024),
		gatewayEvents:           broadcast.New[*GatewayEvent](1024).Run(),
	}, nil
}

func obtainThingsIXRoutesFunc(cfg *Config) (RoutesUpdaterFunc, error) {
	accounter := cfg.PacketExchange.Accounting.Accounter()

	if cfg.Routes.SmartContract != nil && cfg.Routes.SmartContract.Address != (common.Address{}) {
		return func() ([]*Router, error) {
			// TODO: add routers from ThingsIX smart contract to routes array
			return nil, fmt.Errorf("refresh from smart contract not implemented")
		}, nil
	}

	if cfg.Routes.ThingsIXApi != nil && cfg.Routes.ThingsIXApi.Endpoint != nil {
		return func() ([]*Router, error) {
			resp, err := http.Get(*cfg.Routes.ThingsIXApi.Endpoint)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()

			snapshot := struct {
				BlockNumber uint64
				ChainID     uint64
				Routers     []struct {
					Endpoint string
					ID       string
					Manager  common.Address
					Networks []uint32
				}
			}{}

			if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
				return nil, err
			}
			if snapshot.ChainID != cfg.Routes.ChainID {
				return nil, fmt.Errorf("invalid routes snapshot, got %d, want %d", snapshot.ChainID, cfg.Routes.ChainID)
			}

			// convert from snapshot to internal format
			routers := make([]*Router, len(snapshot.Routers))
			for i, r := range snapshot.Routers {
				var (
					id     [32]byte
					netids = make([]lorawan.NetID, len(r.Networks))
				)

				copy(id[:], r.ID)
				for i, id := range r.Networks {
					var netid [4]byte
					binary.LittleEndian.PutUint32(netid[:], id)
					netids[i] = lorawan.NetID{netid[0], netid[1], netid[2]}
				}
				routers[i] = NewRouter(id, r.Endpoint, false, netids, r.Manager, accounter)
			}
			logrus.WithField("#-routers", len(routers)).Info("fetched routing table from ThingsIX API")
			return routers, nil
		}, nil
	}

	// no routes source configured, only use default routers
	logrus.Warn("no ThingsIX routing table source configured, only use routers from configuration")
	return func() ([]*Router, error) {
		return nil, nil
	}, nil
}
