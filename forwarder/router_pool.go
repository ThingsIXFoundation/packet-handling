package forwarder

import (
	"encoding/binary"
	"sync"

	"github.com/FastFilter/xorfilter"
	"github.com/brocaar/lorawan"
)

type RouterPool struct {
	routers     map[string]*Router
	routerMutex sync.RWMutex
}

func NewRouterPool() (*RouterPool, error) {
	rp := &RouterPool{}

	return rp, nil
}

// TODO:
// Discover routers from smart contract
// Poll routers to get latest routing data

func (rp *RouterPool) Start() error {
	return nil
}

func (rp *RouterPool) Stop() error {
	return nil
}

func (rp *RouterPool) GetRoutersForDataUp(devAddr lorawan.DevAddr) ([]*Router, error) {
	rp.routerMutex.RLock()
	defer rp.routerMutex.RUnlock()

	ret := []*Router{}
	for _, router := range rp.routers {
		if router.AcceptsDevAddr(devAddr) {
			ret = append(ret, router)
		}
	}

	return ret, nil
}

func (rp *RouterPool) GetRoutersForJoin(joinEUI lorawan.EUI64) ([]*Router, error) {
	rp.routerMutex.RLock()
	defer rp.routerMutex.RUnlock()

	ret := []*Router{}
	for _, router := range rp.routers {
		if router.AcceptsJoin(joinEUI) {
			ret = append(ret, router)
		}
	}

	return ret, nil
}

func (rp *RouterPool) GetConnectedRouters() ([]*Router, error) {
	ret := []*Router{}
	for _, router := range rp.routers {
		// TODO: Check connection status
		ret = append(ret, router)
	}

	return ret, nil
}

type Router struct {
	id         string
	client     *RouterClient
	uri        string
	netIDs     []lorawan.NetID
	joinFilter xorfilter.Xor8
}

func (r *Router) AcceptsDevAddr(devAddr lorawan.DevAddr) bool {
	for _, netID := range r.netIDs {
		if devAddr.IsNetID(netID) {
			return true
		}
	}

	return false
}

func (r *Router) AcceptsJoin(joinEUI lorawan.EUI64) bool {
	return r.joinFilter.Contains(binary.BigEndian.Uint64(joinEUI[:]))
}
