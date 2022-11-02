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
	"os"
	"time"

	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/mitchellh/mapstructure"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Config struct {
	Forwarder struct {
		// Backend holdsconfiguration related to the forwarders gateway
		// endpoint and supported protocol.
		Backend struct {
			SemtechUDP *struct {
				UDPBind    *string `mapstructure:"udp_bind"`
				FakeRxTime *bool   `mapstructure:"fake_rx_time"`
			} `mapstructure:"semtech_udp"`

			BasicStation  *struct{} `mapstructure:"basic_station"`
			Concentratord *struct{} `mapstructure:"concentratord"`
		}

		// Gateways holds configuration related to gateways.
		Gateways struct {
			// Store describes how gateways are stored/loaded in the forwarder.
			Store struct {
				// YamlStorePath indicates that gateways are stored in a
				// YAML based file store located on the local file system.
				// Only data for gateways in the store is forwarded.
				YamlStorePath *string `mapstructure:"file"`
			}

			// RecordUnknown records gateways that connect to the forwarder but
			// are not in the forwarders gateway store. Recorded gateways can
			// be imported later if required.
			RecordUnknown *struct {
				// File points to a file on the local file system where
				// unknown gateways that connect are recorded.
				File string
			} `mapstructure:"record_unknown"`
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

		Routers struct {
			// Default routers that will receive all gateway data unfiltered
			Default []*Router

			// OnChain idicates that ThingsIX routers are loaded from the router
			// registry as deployed on the blockchain.
			OnChain *struct {
				// RegistryContract indicates when non-nil that router information must
				// be fetched from the registry smart contract (required blockchain cfg)
				RegistryContract common.Address `mapstructure:"registry"`

				// Interval indicates how often the routes are refreshed
				UpdateInterval *time.Duration `mapstructure:"interval"`
			} `mapstructure:"on_chain"`

			// ThingsIXApi indicates when non-nil that router information must be
			// fetched from the ThingsIX API
			ThingsIXApi *struct {
				Endpoint *string
				// Interval indicates how often the routes are refreshed
				UpdateInterval *time.Duration `mapstructure:"interval"`
			} `mapstructure:"thingsix_api"`
		}

		// Optional account strategy configuration, if not specified no account is used meaning
		// that all packets are exchanged between gateway and routers.
		Accounting *struct{}
	}

	Log struct {
		Level     logrus.Level
		Timestamp bool
	}

	BlockChain struct {
		Polygon *struct {
			Endpoint      string
			ChainID       uint64 `mapstructure:"chain_id"`
			Confirmations uint64
		}
	}

	Metrics *struct {
		Prometheus *struct {
			Address string
			Path    string
		}
	}
}

func (cfg Config) PrometheusEnabled() bool {
	return cfg.Metrics != nil &&
		cfg.Metrics.Prometheus != nil
}

func (cfg Config) MetricsPrometheusAddress() string {
	if cfg.Metrics.Prometheus.Address != "" {
		return cfg.Metrics.Prometheus.Address
	}
	return "localhost:8080"
}

func (cfg Config) MetricsPrometheusPath() string {
	path := "/metrics"
	if cfg.Metrics.Prometheus.Path != "" {
		path = cfg.Metrics.Prometheus.Path
	}
	return path
}

func mustLoadConfig() *Config {
	viper.SetConfigName("forwarder") // name of config file (without extension)
	viper.SetConfigType("yaml")      // REQUIRED if the config file does not have the extension in the name

	if home, err := os.UserHomeDir(); err == nil {
		viper.AddConfigPath(home) // call multiple times to add many search paths
	}
	viper.AddConfigPath("/etc/thingsix/") // path to look for the config file in
	viper.AddConfigPath(".")

	if configFile := viper.GetString("config"); configFile != "" {
		viper.SetConfigFile(configFile)
	}

	if err := viper.ReadInConfig(); err != nil {
		logrus.WithError(err).Fatal("unable to read config")
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg, viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToSliceHookFunc(","),
		utils.StringToEthereumAddressHook(),
		utils.IntToBigIntHook(),
		utils.HexStringToBigIntHook(),
		utils.StringToHashHook(),
		utils.StringToDuration(),
		utils.StringToLogrusLevel()))); err != nil {
		logrus.WithError(err).Fatal("unable to load configuration")
	}

	logrus.SetLevel(cfg.Log.Level)
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: cfg.Log.Timestamp})

	// ensure user provided polygon blockchain config
	if cfg.BlockChain.Polygon == nil {
		logrus.Fatal("missing Polygon blockchain configuration")
	}

	// set the Default flag on the defaultRouters to distinct them from routes
	// loaded from ThingsIX
	for _, r := range cfg.Forwarder.Routers.Default {
		r.Default = true
	}

	return &cfg
}
