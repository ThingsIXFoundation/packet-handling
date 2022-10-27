package forwarder

import (
	"time"

	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
)

// buildAccounter returns the Accounter module as configured in the given cfg.
// Or an error in case of an invalid configuration.
func buildAccounter(cfg *Config) (Accounter, error) {
	return NewNoAccountingStrategy(), nil // not yet supported
}

// Accounter is implemented by account strategies that determine if a
// packet must be forwarded to a router or not because it hasn't paid
// for the service gateways connected to the packet exchange provide.
type Accounter interface {
	// Allow returns an indication if the user is allowed to receive
	// the packet that took the given amount of airtime from the gateway.
	Allow(user common.Address, airtime time.Duration) bool

	// AddPayment must be called each time a router sends an airtime
	// payment to the forwarder. The accounter will store/track these
	// and determines if the router is allowed to receive more data.
	AddPayment(payment *router.AirtimePaymentEvent)
}

type NoAccounting struct {
}

// NewNoAccountingStrategy returns an Accounter that allows all data to
// be forwarded to the router and ignore router payments.
func NewNoAccountingStrategy() *NoAccounting {
	logrus.Info("disable packet accounting")
	return &NoAccounting{}
}

// Allow all data to the given user
func (a NoAccounting) Allow(user common.Address, airtime time.Duration) bool {
	logrus.Debug("allow all for no-accounting")
	return true
}

// AddPayment ignores the given payment
func (a NoAccounting) AddPayment(payment *router.AirtimePaymentEvent) {
	logrus.Debug("ignore airtime payment for no-accounting")
}
