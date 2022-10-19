package gateway

import (
	"errors"

	gateway_registry "github.com/ThingsIXFoundation/gateway-registry-go"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	Cmd = &cobra.Command{
		Use:   "gateway",
		Short: "gateway related commands",
	}

	listGatewayCmd = &cobra.Command{
		Use:   "list <gateway-store-file>",
		Short: "Add gateway to gateway store",
		Args:  cobra.ExactArgs(1),
		Run:   listGatewayStore,
	}

	addGatewayCmd = &cobra.Command{
		Use:   "add <gateway-store-file> <local-id>",
		Short: "Add gateway to gateway store",
		Args:  cobra.ExactArgs(2),
		Run:   addGatewayToStore,
	}

	gatewayDetailsCmd = &cobra.Command{
		Use:   "details <gateway-store-file> <thingsix-id/local-id/network-id>",
		Short: "Show gateway details",
		Args:  cobra.RangeArgs(2, 3),
		Run:   gatewayDetails,
	}

	rpcEndpoint     string
	registryAddress string
)

func init() {
	listGatewayCmd.PersistentFlags().StringVar(&rpcEndpoint, "rpc.endpoint", "", "RPC endpoint")
	listGatewayCmd.PersistentFlags().StringVar(&registryAddress, "registry.address", "", "Gateway registry address")
	gatewayDetailsCmd.PersistentFlags().StringVar(&rpcEndpoint, "rpc.endpoint", "", "RPC endpoint")
	gatewayDetailsCmd.PersistentFlags().StringVar(&registryAddress, "registry.address", "", "Gateway registry address")

	Cmd.AddCommand(listGatewayCmd)
	Cmd.AddCommand(addGatewayCmd)
	Cmd.AddCommand(gatewayDetailsCmd)
}

func gatewayDetails(cmd *cobra.Command, args []string) {
	var (
		store, err = LoadGatewayYamlFileStore(args[0])
		id         = args[1]
		registry   *gateway_registry.GatewayRegistry
		gw         *Gateway
	)
	if err != nil {
		logrus.WithError(err).Fatal("unable to open gateway store")
	}
	if len(id) == 16 {
		gw, err = store.GatewayByLocalID(mustDecodeGatewayID(id))
		if errors.Is(err, ErrNotFound) {
			gw, err = store.GatewayByNetworkID(mustDecodeGatewayID(id))
		}
	} else {
		gw, err = store.GatewayByThingsIxID(mustDecodeThingsIXID(id))
	}
	if err != nil {
		logrus.WithError(err).Fatal("gateway not found in store")
	}

	if rpcEndpoint != "" {
		client, err := ethclient.Dial(rpcEndpoint)
		if err != nil {
			logrus.WithError(err).Error("unable to dial RPC node")
		}
		defer client.Close()
		registry, err = gateway_registry.NewGatewayRegistry(common.HexToAddress(registryAddress), client)
		if err != nil {
			logrus.WithError(err).Error("unable to instantiate gateway registry bindings")
		}
	}

	printGatewaysAsTable([]*Gateway{gw}, registry)
}

func listGatewayStore(cmd *cobra.Command, args []string) {
	var registry *gateway_registry.GatewayRegistry
	store, err := LoadGatewayYamlFileStore(args[0])
	if err != nil {
		logrus.WithError(err).Fatal("unable to open gateway store")
	}

	if rpcEndpoint != "" {
		client, err := ethclient.Dial(rpcEndpoint)
		if err != nil {
			logrus.WithError(err).Error("unable to dial RPC node")
		}
		defer client.Close()
		registry, err = gateway_registry.NewGatewayRegistry(common.HexToAddress(registryAddress), client)
		if err != nil {
			logrus.WithError(err).Error("unable to instantiate gateway registry bindings")
		}
	}

	printGatewaysAsTable(store.Gateways(), registry)
}

func addGatewayToStore(cmd *cobra.Command, args []string) {
	var (
		store, err = LoadGatewayYamlFileStore(args[0])
		localID    = mustDecodeGatewayID(args[1])
	)
	if err != nil {
		logrus.WithError(err).Fatal("unable to open gateway store")
	}

	// generate new ThingsIX gateway key
	gateway, err := GenerateNewGateway(localID)
	if err != nil {
		logrus.WithError(err).Fatal("unable to generate gateway entry")
	}

	if err := store.AddGateway(gateway.LocalGatewayID, gateway.PrivateKey); err != nil {
		logrus.WithError(err).Fatal("unable to add gateway to store")
	}

	gw, err := store.GatewayByLocalID(gateway.LocalGatewayID)
	if err != nil {
		logrus.WithError(err).Fatal("unable to retrieve added gateway: %s", localID)
	}
	printGatewaysAsTable([]*Gateway{gw}, nil)
}
