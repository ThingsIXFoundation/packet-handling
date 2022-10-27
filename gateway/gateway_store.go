package gateway

import (
	"crypto/ecdsa"

	"github.com/brocaar/lorawan"
)

// Store defines a gateway store.
type Store interface {
	Gateways() []*Gateway
	GatewayByLocalID(id lorawan.EUI64) (*Gateway, error)
	GatewayByLocalIDBytes(id []byte) (*Gateway, error)
	GatewayByNetworkID(id lorawan.EUI64) (*Gateway, error)
	GatewayByNetworkIDBytes(id []byte) (*Gateway, error)
	GatewayByThingsIxID([32]byte) (*Gateway, error)
	AddGateway(localID lorawan.EUI64, key *ecdsa.PrivateKey) error
}
