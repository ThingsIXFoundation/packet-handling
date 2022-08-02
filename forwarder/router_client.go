package forwarder

import "github.com/brocaar/chirpstack-api/go/v3/gw"

type RouterClient struct {
}

func NewRouterClient() (*RouterClient, error) {
	return &RouterClient{}, nil
}

func (rc *RouterClient) DeliverDataUp(gw.UplinkFrame) {

}

func (rc *RouterClient) DeliverJoin(gw.UplinkFrame) {

}
