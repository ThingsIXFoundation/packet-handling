// Copyright 2023 Stichting ThingsIX Foundation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package gateway

import (
	"context"
	"fmt"
	"math/big"
	"net/url"

	"github.com/ThingsIXFoundation/frequency-plan/go/frequency_plan"
	h3light "github.com/ThingsIXFoundation/h3-light"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/sirupsen/logrus"
)

type GatewayThingsIXSmartContract struct {
	// Endpoint is the RPC endpoint for the blockchain node
	Endpoint string
	// Confirmations holds the block confirmations
	Confirmations uint64
	// Addr holds the gateway registry address
	Addr common.Address
}

func buildThingsIXRegistryOnChainSyncer(cfg *RegistrySyncOnChainConfig) (*GatewayThingsIXSmartContract, error) {
	if _, err := url.Parse(cfg.Endpoint); err != nil {
		return nil, fmt.Errorf("invalid RPC endpoint for smart contract integration: %s", cfg.Endpoint)
	}
	if cfg.Address == (common.Address{}) {
		return nil, fmt.Errorf("gateway ThingsIX registry syncer missing registry smart contract address")
	}

	logrus.WithFields(logrus.Fields{
		"registry":      cfg.Address,
		"confirmations": cfg.Confirmation,
	}).Info("sync with ThingsIX gateway registry on-chain")

	return &GatewayThingsIXSmartContract{
		Endpoint:      cfg.Endpoint,
		Confirmations: cfg.Confirmation,
		Addr:          cfg.Address,
	}, nil
}

func (sync *GatewayThingsIXSmartContract) GatewayDetails(ctx context.Context, gatewayID ThingsIxID, force bool) (common.Address, uint8, *GatewayDetails, error) {
	client, err := ethclient.DialContext(ctx, sync.Endpoint)
	if err != nil {
		logrus.WithError(err).Warn("unable to connect to blockchain RPC node")
		return common.Address{}, 0, nil, err
	}
	defer client.Close()

	registry, err := NewGatewayRegistryCaller(sync.Addr, client)
	if err != nil {
		return common.Address{}, 0, nil, err
	}

	head, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return common.Address{}, 0, nil, err
	}

	if head.Number.Uint64() < sync.Confirmations {
		return common.Address{}, 0, nil, fmt.Errorf("chain too short for confirmed blocks")
	}

	opts := bind.CallOpts{
		BlockNumber: new(big.Int).SetUint64(head.Number.Uint64() - sync.Confirmations),
		Context:     ctx,
	}

	gateway, err := registry.Gateways(&opts, gatewayID)
	if err != nil {
		return common.Address{}, 0, nil, err
	}

	var (
		owner       = gateway.Owner
		version     = gateway.Version
		altitude    = uint16(gateway.Altitude) * 3
		antennaGain = fmt.Sprintf("%.1f", float32(gateway.AntennaGain)/10.0)
		band        = string(frequency_plan.FromBlockchain(frequency_plan.BlockchainFrequencyPlan(gateway.FrequencyPlan)))
		location    = h3light.Cell(gateway.Location).String()
		details     *GatewayDetails
	)
	if gateway.AntennaGain != 0 {
		details = &GatewayDetails{
			AntennaGain: &antennaGain,
			Altitude:    &altitude,
			Band:        &band,
			Location:    &location,
		}
	}

	return owner, version, details, nil
}
