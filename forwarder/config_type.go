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
	"time"

	"github.com/ThingsIXFoundation/packet-handling/database"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
)

type ForwarderBackendSemtechUDPConfig struct {
	UDPBind    *string `mapstructure:"udp_bind"`
	FakeRxTime *bool   `mapstructure:"fake_rx_time"`
}

type ForwarderBackendConfig struct {
	SemtechUDP    *ForwarderBackendSemtechUDPConfig `mapstructure:"semtech_udp"`
	BasicStation  *struct{}                         `mapstructure:"basic_station"`
	Concentratord *struct{}                         `mapstructure:"concentratord"`
}

type ForwarderGatewayConfig struct {
	// Store describes how gateways are stored/loaded in the forwarder.
	Store gateway.StoreConfig
	// RecordUnknown records gateways that connect to the forwarder but
	// are not in the forwarders gateway store. Recorded gateways can
	// be imported later if required.
	RecordUnknown *gateway.ForwarderGatewayRecordUnknownConfig `mapstructure:"record_unknown"`
	// RegistryAddress holds the address where the gateway registry is
	// deployed on chain. It is used to retrieve gateway details to
	// determine which gateways in the store are onboarded on ThingsIX
	// and have their details. Once token support is integrated to ThingsIX
	// only data for these gateways will be forwarded.
	RegistryAddress *common.Address `mapstructure:"gateway_registry"`

	// Refresh indicates the interval in which the gateway registry is
	// used to check if gateways from the forwarders store are onboarded
	// have their gateways set.
	Refresh *time.Duration
}

type ForwarderRoutersOnChainConfig struct {
	// RegistryContract indicates when non-nil that router information must
	// be fetched from the registry smart contract (required blockchain cfg)
	RegistryContract common.Address `mapstructure:"registry"`

	// Interval indicates how often the routes are refreshed
	UpdateInterval *time.Duration `mapstructure:"interval"`
}

type ForwarderRoutersThingsIXAPIConfig struct {
	Endpoint *string
	// Interval indicates how often the routes are refreshed
	UpdateInterval *time.Duration `mapstructure:"interval"`
}

type ForwarderRoutersConfig struct {
	// Default routers that will receive all gateway data unfiltered
	Default []*Router

	// OnChain idicates that ThingsIX routers are loaded from the router
	// registry as deployed on the blockchain.
	OnChain *ForwarderRoutersOnChainConfig `mapstructure:"on_chain"`

	// ThingsIXApi indicates when non-nil that router information must be
	// fetched from the ThingsIX API
	ThingsIXApi *ForwarderRoutersThingsIXAPIConfig `mapstructure:"thingsix_api"`
}

type ForwarderConfig struct {
	// Backend holdsconfiguration related to the forwarders gateway
	// endpoint and supported protocol.
	Backend ForwarderBackendConfig

	// Gateways holds configuration related to gateways.
	Gateways ForwarderGatewayConfig

	Routers ForwarderRoutersConfig

	// Optional account strategy configuration, if not specified no account is used meaning
	// that all packets are exchanged between gateway and routers.
	Accounting *struct{}
}

type LogConfig struct {
	Level     logrus.Level
	Timestamp bool
}

type BlockchainPolygonConfig struct {
	Endpoint      string
	ChainID       uint64 `mapstructure:"chain_id"`
	Confirmations uint64
}

type BlockchainConfig struct {
	Polygon *BlockchainPolygonConfig
}

type MetricsPrometheusConfig struct {
	Address string
	Path    string
}

type MetricsConfig struct {
	Prometheus *MetricsPrometheusConfig
}

type Config struct {
	Forwarder  ForwarderConfig
	Log        LogConfig
	BlockChain BlockchainConfig
	Database   *struct {
		Postgresql *database.Config
	}
	Metrics *MetricsConfig
}
