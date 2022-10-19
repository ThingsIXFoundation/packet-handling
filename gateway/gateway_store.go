package gateway

import (
	"crypto/ecdsa"

	"github.com/brocaar/lorawan"
)

type GatewayStore interface {
	Gateways() ([]*Gateway, error)
	GatewayByLocalID(localGatewayID []byte) (*Gateway, error)
	GatewayByNetworkID(networkGatewayID []byte) (*Gateway, error)
	GatewayByThingsIxID([32]byte) (*Gateway, error)
	AddGateway(localID lorawan.EUI64, key *ecdsa.PrivateKey) error
}
