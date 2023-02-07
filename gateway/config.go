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

package gateway

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type GatewayStoreType uint

const (
	NoGatewayStoreType GatewayStoreType = iota
	YamlFileGatewayStore
	PostgresqlGatewayStore
)

type Config struct {
	BlockChain struct {
		Endpoint      string
		ChainID       uint64
		Confirmations uint64
	} `mapstructure:"blockchain"`
}

type ForwarderGatewayRecordUnknownConfig struct {
	// File points to a file on the local file system where unknown gateways
	// that connect are recorded.
	File string

	// Postgresql if non nil indicates that unknown gateways must be recorded to
	// a postgresql database.
	Postgresql *bool `mapstructure:"postgresql"`
}

type StoreConfig struct {
	// RefreshInterval indicates how often the gateway store is reloaded from
	// the backend store. If this is a nil ptr hot reloads are disabled.
	RefreshInterval *time.Duration `mapstructure:"refresh"`

	// YamlStorePath indicates that gateways are stored in a
	// YAML based file store located on the local file system.
	// Only data for gateways in the store is forwarded.
	YamlStorePath *string `mapstructure:"file"`

	// Use a PGSQL database to store gateways.
	Postgresql *bool `mapstructure:"postgresql"`
}

func (sc StoreConfig) Type() GatewayStoreType {
	if sc.Postgresql != nil && *sc.Postgresql {
		return PostgresqlGatewayStore
	}
	if sc.YamlStorePath != nil {
		return YamlFileGatewayStore
	}
	return NoGatewayStoreType
}

// RegistrySyncAPIConfig retrieve gateway information from the ThingsIX gateway
// registry through the HTTP API.
type RegistrySyncAPIConfig struct {
	Endpoint string `mapstructure:"endpoint"`
}

// RegistrySyncOnChainConfig retrieve gateway information from the ThingsIX
// gateway registry from the smart contract.
type RegistrySyncOnChainConfig struct {
	Endpoint     string
	Confirmation uint64
	ChainID      uint64
	Address      common.Address `mapstructure:"address"`
}

// RegistrySyncConfig describes how gateway information is retrieved from the
// ThingsIX gateway registry.
type RegistrySyncConfig struct {
	ThingsIxApi RegistrySyncAPIConfig      `mapstructure:"thingsix_api"`
	OnChain     *RegistrySyncOnChainConfig `mapstructure:"on_chain"`
}
