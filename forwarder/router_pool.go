package forwarder

import "github.com/brocaar/lorawan"

type RouterPool struct {
}

func NewRouterPool() (*RouterPool, error) {
	rp := &RouterPool{}

	return rp, nil
}

func (rp *RouterPool) GetRoutersForDataUp(lorawan.DevAddr) ([]*RouterClient, error) {
	return nil, nil
}

func (rp *RouterPool) GetRoutersForJoin(lorawan.EUI64) ([]*RouterClient, error) {
	return nil, nil
}
