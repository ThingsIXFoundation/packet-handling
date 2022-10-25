package gateway

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"

	gateway_registry "github.com/ThingsIXFoundation/gateway-registry-go"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	Cmd = &cobra.Command{
		Use:   "gateway",
		Short: "gateway related commands",
	}

	importGatewayCmd = &cobra.Command{
		Use:   "import <gateway-store-file> <chain-id> <owner> <onboarder-address> <recorded-unknown-gateway-file>",
		Short: "Import gateway to gateway store",
		Args:  cobra.ExactArgs(5),
		Run:   importGatewayStore,
	}

	listGatewayCmd = &cobra.Command{
		Use:   "list <gateway-store-file>",
		Short: "List gateway in gateway store",
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

	Cmd.AddCommand(importGatewayCmd)
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

func importGatewayStore(cmd *cobra.Command, args []string) {
	var (
		owner     = common.HexToAddress(args[2])
		onboarder = common.HexToAddress(args[3])
		log       = logrus.WithField("file", args[4])
		version   = uint8(1)
	)

	chainID, ok := new(big.Int).SetString(args[1], 0)
	if !ok {
		log.Fatal("invalid chain id")
	}
	store, err := LoadGatewayYamlFileStore(args[0])
	if err != nil {
		logrus.WithError(err).Fatal("unable to open gateway store")
	}

	// load list with unknown gateway local ids and generate a new gateway
	// record for each of them
	in, err := os.Open(args[4])
	if err != nil {
		log.WithError(err).Fatal("unable to open recorded gateways file")
	}
	defer in.Close()

	var (
		ids      []lorawan.EUI64
		gateways []*Gateway
	)
	if err := yaml.NewDecoder(in).Decode(&ids); err != nil {
		log.WithError(err).Fatal("unable to decode recorded gateways file")
	}

	// generate for all ids a new gateway key
	for _, id := range ids {
		gw, err := GenerateNewGateway(id)
		if err != nil {
			log.WithError(err).Fatal("unable to generate new gateway")
		}
		gateways = append(gateways, gw)
	}

	type outputGw struct {
		ID        string         `json:"gateway_id"`
		Version   uint8          `json:"version"`
		Local     lorawan.EUI64  `json:"local_id"`
		Network   lorawan.EUI64  `json:"network_id"`
		Address   common.Address `json:"address"`
		ChainID   uint64         `json:"chain_id"`
		Signature string         `json:"gateway_onboard_signature"`
	}

	var output []outputGw
	for _, gw := range gateways {
		sign, err := SignPlainBatchOnboardMessage(chainID, onboarder, owner, version, gw)
		if err != nil {
			log.WithError(err).Fatal("unable to sign gateway onboard message")
		}

		if err := store.AddGateway(gw.LocalGatewayID, gw.PrivateKey); err == nil {
			logrus.Infof("imported gateway %s", gw.LocalGatewayID)
		} else {
			logrus.Infof("gateway %s already in gateway store, skip", gw.LocalGatewayID)
			continue
		}

		output = append(output, outputGw{
			ID:        "0x" + hex.EncodeToString(gw.CompressedPublicKeyBytes),
			Version:   version,
			Local:     gw.LocalGatewayID,
			Network:   gw.NetworkGatewayID,
			ChainID:   chainID.Uint64(),
			Address:   gw.Address(),
			Signature: fmt.Sprintf("0x%x", sign),
		})
	}

	if len(output) > 0 {
		if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
			logrus.WithError(err).Fatal("unable to write gateway onboard message!")
		}
	}
	logrus.Infof("imported %d gateways, don't forget to onboard these", len(output))
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
