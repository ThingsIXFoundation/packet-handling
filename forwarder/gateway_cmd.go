// Copyright 2022 Stichting ThingsIX Foundation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package forwarder

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"time"

	gateway_registry "github.com/ThingsIXFoundation/gateway-registry-go"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	GatewayCmds = &cobra.Command{
		Use:   "gateway",
		Short: "gateway related commands",
	}

	importGatewayCmd = &cobra.Command{
		Use:   "import <chain-id> <owner> <onboarder-address>",
		Short: "Import recorded unknown gateways in gateway store",
		Args:  cobra.ExactArgs(3),
		Run:   importGatewayStore,
	}

	listGatewayCmd = &cobra.Command{
		Use:   "list",
		Short: "List gateway in gateway store",
		Args:  cobra.NoArgs,
		Run:   listGatewayStore,
	}

	addGatewayCmd = &cobra.Command{
		Use:   "add <local-id>",
		Short: "Add gateway to gateway store",
		Args:  cobra.ExactArgs(1),
		Run:   addGatewayToStore,
	}

	gatewayDetailsCmd = &cobra.Command{
		Use:   "details <thingsix-id/local-id/network-id>",
		Short: "Show gateway details",
		Args:  cobra.ExactArgs(1),
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

	GatewayCmds.AddCommand(importGatewayCmd)
	GatewayCmds.AddCommand(listGatewayCmd)
	GatewayCmds.AddCommand(addGatewayCmd)
	GatewayCmds.AddCommand(gatewayDetailsCmd)
}

func gatewayDetails(cmd *cobra.Command, args []string) {
	var (
		cfg        = mustLoadConfig()
		store, err = gateway.NewGatewayStore(context.Background(), &cfg.Forwarder.Gateways.Store)
		id         = args[0]
		registry   *gateway_registry.GatewayRegistry
		gw         *gateway.Gateway
	)
	if err != nil {
		logrus.WithError(err).Fatal("unable to open gateway store")
	}
	if len(id) == 16 {
		gw, err = store.ByLocalID(mustDecodeGatewayID(id))
		if errors.Is(err, gateway.ErrNotFound) {
			gw, err = store.ByNetworkID(mustDecodeGatewayID(id))
		}
	} else {
		gw, err = store.ByThingsIxID(mustDecodeThingsIXID(id))
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

	printGatewaysAsTable([]*gateway.Gateway{gw}, registry)
}

func importGatewayStore(cmd *cobra.Command, args []string) {
	var (
		cfg         = mustLoadConfig()
		store, err  = gateway.NewGatewayStore(context.Background(), &cfg.Forwarder.Gateways.Store)
		owner       = common.HexToAddress(args[1])
		onboarder   = common.HexToAddress(args[2])
		version     = uint8(1)
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		gateways    []*gateway.Gateway
	)
	defer cancel()

	if err != nil {
		logrus.WithError(err).Fatal("unable to load gateway store")
	}

	chainID, ok := new(big.Int).SetString(args[0], 0)
	if !ok {
		logrus.Fatal("invalid chain id")
	}

	recordedLocalIDs, err := GetAllRecordedUnknownGateways(cfg.Forwarder.Gateways.RecordUnknown)
	if err != nil {
		logrus.WithError(err).Fatal("unable to retrieve recorded unknown gateways")
	}

	// generate for all recorded id's a new gateway key
	for _, id := range recordedLocalIDs {
		gw, err := gateway.GenerateNewGateway(id)
		if err != nil {
			logrus.WithError(err).Fatal("unable to generate new gateway")
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
		sign, err := gateway.SignPlainBatchOnboardMessage(chainID, onboarder, owner, version, gw)
		if err != nil {
			logrus.WithError(err).Fatal("unable to sign gateway onboard message")
		}

		if _, err := store.Add(ctx, gw.LocalID, gw.PrivateKey); err == nil {
			logrus.Infof("imported gateway %s", gw.LocalID)
		} else {
			logrus.Infof("gateway %s already in gateway store, skip", gw.LocalID)
			continue
		}

		output = append(output, outputGw{
			ID:        "0x" + hex.EncodeToString(gw.ThingsIxID[:]),
			Version:   version,
			Local:     gw.LocalID,
			Network:   gw.NetID,
			ChainID:   chainID.Uint64(),
			Address:   gw.Address(),
			Signature: fmt.Sprintf("0x%x", sign),
		})
	}

	if len(output) > 0 {
		if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
			logrus.WithError(err).Fatal("unable to write gateway onboard message!")
		}
		logrus.Infof("imported %d gateways, don't forget to onboard these with the above JSON message", len(output))
	} else {
		logrus.Infof("imported 0 gateways")
	}
}

func listGatewayStore(cmd *cobra.Command, args []string) {
	var (
		registry   *gateway_registry.GatewayRegistry
		cfg        = mustLoadConfig()
		store, err = gateway.NewGatewayStore(context.Background(), &cfg.Forwarder.Gateways.Store)
	)
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

	var collector GatewayCollector
	store.Range(&collector)
	printGatewaysAsTable(collector.Gateways, registry)
}

func addGatewayToStore(cmd *cobra.Command, args []string) {
	var (
		localID     = mustDecodeGatewayID(args[0])
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		cfg         = mustLoadConfig()
		store, err  = gateway.NewGatewayStore(context.Background(), &cfg.Forwarder.Gateways.Store)
	)
	if err != nil {
		logrus.WithError(err).Fatal("unable to open gateway store")
	}
	defer cancel()

	if err != nil {
		logrus.WithError(err).Fatal("unable to open gateway store")
	}

	// generate new ThingsIX gateway key
	newGateway, err := gateway.GenerateNewGateway(localID)
	if err != nil {
		logrus.WithError(err).Fatal("unable to generate gateway entry")
	}

	gw, err := store.Add(ctx, newGateway.LocalID, newGateway.PrivateKey)
	if err != nil {
		logrus.WithError(err).Fatal("unable to add gateway to store")
	}

	printGatewaysAsTable([]*gateway.Gateway{gw}, nil)
}
