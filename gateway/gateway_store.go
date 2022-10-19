package gateway

import (
	"crypto/ecdsa"

	"github.com/brocaar/lorawan"
)

type GatewayStore interface {
	Gateways() []*Gateway
	GatewayByLocalID(id lorawan.EUI64) (*Gateway, error)
	GatewayByLocalIDBytes(id []byte) (*Gateway, error)
	GatewayByNetworkID(id lorawan.EUI64) (*Gateway, error)
	GatewayByNetworkIDBytes(id []byte) (*Gateway, error)
	GatewayByThingsIxID([32]byte) (*Gateway, error)
	AddGateway(localID lorawan.EUI64, key *ecdsa.PrivateKey) error
}
