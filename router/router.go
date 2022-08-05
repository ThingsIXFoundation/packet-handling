package router

import (
	"context"

	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/integration"
	"github.com/ThingsIXFoundation/router-api/go/router"
)

type Router struct {
	router.UnimplementedRouterV1Server

	integration integration.Integration
}

var _ router.RouterV1Server = (*Router)(nil)

func NewRouter(int integration.Integration) (*Router, error) {
	return &Router{integration: int}, nil
}

func (r *Router) setup() {

}

func (r *Router) NetIds(ctx context.Context, req *router.NetIdsRequest) (*router.NetIdsResponse, error) {
	panic("not implemented") // TODO: Implement
}

func (r *Router) JoinFilter(ctx context.Context, req *router.JoinFilterRequest) (*router.JoinFilterResponse, error) {
	panic("not implemented") // TODO: Implement
}

func (r *Router) Events(evs router.RouterV1_EventsServer) error {
	panic("not implemented") // TODO: Implement
}
