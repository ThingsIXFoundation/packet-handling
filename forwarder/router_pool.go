package forwarder

import (
	"context"
	"encoding/binary"
	"sync"
	"time"

	"github.com/FastFilter/xorfilter"
	"github.com/ThingsIXFoundation/packet-handling/cmd/forwarder/config"
	"github.com/brocaar/lorawan"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type RouterPool struct {
	routers     map[string]*Router
	routerMutex sync.RWMutex
	forwarder   *Forwarder
}

func NewRouterPool(forwarder *Forwarder) (*RouterPool, error) {
	rp := &RouterPool{
		routers:   map[string]*Router{},
		forwarder: forwarder,
	}

	if viper.GetString(config.DefaultRouter) != "" {
		logrus.Infof("DEBUG: Default router added: %s", viper.GetString(config.DefaultRouter))
		rp.routers["default"] = &Router{
			id:  "default",
			uri: viper.GetString(config.DefaultRouter),
		}
	}

	return rp, nil
}

// TODO:
// Discover routers from smart contract
// Poll routers to get latest routing data

func (rp *RouterPool) Start() error {
	for _, router := range rp.routers {

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		rc, err := DialRouter(ctx, router.uri)
		if err != nil {
			return err
		}

		router.client = rc
		go func(router *Router) {
			err := rc.Run(context.Background(), router, rp.forwarder.routerEvents)
			if err != nil {
				logrus.WithError(err).Fatal()
			}
		}(router)

	}
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

	if viper.GetString(config.DefaultRouter) != "" {
		ret = append(ret, rp.routers["default"])
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
	if viper.GetString(config.DefaultRouter) != "" {
		ret = append(ret, rp.routers["default"])
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
	if len(r.joinFilter.Fingerprints) == 0 {
		return false
	}
	return r.joinFilter.Contains(binary.BigEndian.Uint64(joinEUI[:]))
}
