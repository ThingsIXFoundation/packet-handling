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
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// ThingsIXRegistry provides access to ThingsIX gateway registry.
type ThingsIXRegistry interface {
	// GatewayDetails retrieves gateway details from the ThingsIX registry.
	// Depending on the implementation it can use a cache, set force=true to
	// disable this cache and force a new request.
	GatewayDetails(ctx context.Context, gatewayID ThingsIxID, force bool) (common.Address, uint8, *GatewayDetails, error)
}

// NewThingsIXGatewayRegistry builds a new ThingsIX gateway registry client.
func NewThingsIXGatewayRegistry(cfg *RegistrySyncConfig) (ThingsIXRegistry, error) {
	if cfg != nil && cfg.OnChain != nil {
		return buildThingsIXRegistryOnChainSyncer(cfg.OnChain)
	}
	if cfg != nil && cfg.ThingsIxApi.Endpoint != "" {
		return buildThingsIXRegistryApiSyncer(cfg.ThingsIxApi)
	}
	return nil, ErrGatewayRegistryConfigMissing
}

type cachedData struct {
	Owner   common.Address
	Version uint8
	Details *GatewayDetails
	Err     error
	When    time.Time
}
