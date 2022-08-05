package gateway

import "crypto/ecdsa"

type GatewayStore interface {
	Gateways() ([][]byte, error)
	CreateKeyPairForGateway(gatewayId []byte) error
	PrivateKeyForGateway(gatewayId []byte) *ecdsa.PrivateKey
	PublicKeyForGateway(gatewayId []byte) *ecdsa.PublicKey
	NetworkGatewayIdForGateway(gatewayId []byte) []byte
	GatewayIdForNetworkGatewayId(networkGatewayId []byte) []byte
}
