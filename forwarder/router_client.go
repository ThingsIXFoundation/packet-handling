package forwarder

import (
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
)

type RouterClient struct {
	client router.RouterV1Client
}

func NewRouterClient() (*RouterClient, error) {
	return &RouterClient{}, nil
}

func (rc *RouterClient) DeliverDataUp(gw.UplinkFrame) {

}

func (rc *RouterClient) DeliverJoin(gw.UplinkFrame) {

}

func (rc *RouterClient) DeliverGatewayStatus(gatewayId []byte, online bool) {

}
