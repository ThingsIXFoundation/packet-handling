package config

var (
	// address where the routers gRPC listener binds on and accepts connections
	// from forwarders
	GrpcListenAddress        = "grpc.listen.address"
	GrpcListenAddressDefault = "0.0.0.0:3200"
)
