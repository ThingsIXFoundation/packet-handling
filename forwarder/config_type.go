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

type BasicStationBackendConfig struct {
	Bind             *string        `mapstructure:"bind"`
	TLSCert          *string        `mapstructure:"tls_cert"`
	TLSKey           *string        `mapstructure:"tls_key"`
	CACert           *string        `mapstructure:"ca_cert"`
	StatsInterval    *time.Duration `mapstructure:"stats_interval"`
	PingInterval     *time.Duration `mapstructure:"ping_interval"`
	TimesyncInterval *time.Duration `mapstructure:"timesync_interval"`
	ReadTimeout      *time.Duration `mapstructure:"read_timeout"`
	WriteTimeout     *time.Duration `mapstructure:"write_timeout"`
	Region           string         `mapstructure:"region"`
}

type ForwarderBackendConfig struct {
	SemtechUDP    *ForwarderBackendSemtechUDPConfig `mapstructure:"semtech_udp"`
	BasicStation  *BasicStationBackendConfig        `mapstructure:"basic_station"`
	Concentratord *struct{}                         `mapstructure:"concentratord"`
}

type ForwarderHttpApiConfig struct {
	Address string `mapstructure:"address"`
}

type ForwarderGatewayConfig struct {
	// BatchOnboarder configures the gateway batch onboarder smart contract plugin.
	BatchOnboarder struct {
		// Address is the smart contract chain address
		Address common.Address `mapstructure:"address"`
	} `mapstructure:"batch_onboarder"`

	// BatchOnboarder configures the gateway early adopter onboarder smart contract plugin.
	EarlyAdopter struct {
		// Address is the smart contract chain address
		Address common.Address `mapstructure:"-"`
	}

	// Store describes how gateways are stored/loaded in the forwarder.
	Store gateway.StoreConfig

	// RecordUnknown records gateways that connect to the forwarder but
	// are not in the forwarders gateway store. Recorded gateways can
	// be imported later if required.
	RecordUnknown *gateway.ForwarderGatewayRecordUnknownConfig `mapstructure:"record_unknown"`

	// Registry describes how gateway data is retrieved from the ThingsIX
	// gateway registry.
	Registry gateway.RegistrySyncConfig `mapstructure:"registry"`

	// HttpAPI configures the private Forwarder HTTP API
	HttpAPI ForwarderHttpApiConfig `mapstructure:"api"`

	// ThingsIXOnboardEndpoint accepts gateway onboard messages for easy onboarding
	ThingsIXOnboardEndpoint string
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

type ForwarderMappingThingsIXAPIConfig struct {
	IndexEndpoint *string `mapstructure:"index_endpoint"`
	// Interval indicates how often the coverage-mapping-indexes are refreshed
	UpdateInterval *time.Duration `mapstructure:"interval"`
}

type ForwarderMappingConfig struct {
	ThingsIXApi *ForwarderMappingThingsIXAPIConfig `mapstructure:"thingsix_api"`
}

type ForwarderConfig struct {
	// Backend holdsconfiguration related to the forwarders gateway
	// endpoint and supported protocol.
	Backend ForwarderBackendConfig

	// Gateways holds configuration related to gateways.
	Gateways ForwarderGatewayConfig

	Routers ForwarderRoutersConfig

	Mapping ForwarderMappingConfig

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
	ChainID       uint64 `mapstructure:"-"`
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
