package gateway

type GatewayStore interface {
	Gateways() ([]*Gateway, error)
	GatewayByLocalID(localGatewayID []byte) (*Gateway, error)
	GatewayByNetworkID(networkGatewayID []byte) (*Gateway, error)
	AddGateway(gw *Gateway) error
}
