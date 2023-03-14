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

	"github.com/ThingsIXFoundation/frequency-plan/go/frequency_plan"
	"github.com/ThingsIXFoundation/packet-handling/database"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/mitchellh/mapstructure"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

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

func getNetConfig(net string) *Config {
	var cfg = Config{}
	if net == "" {
		return &cfg
	}
	cfg.Forwarder = ForwarderConfig{}
	cfg.Forwarder.Backend = ForwarderBackendConfig{}
	cfg.Forwarder.Backend.SemtechUDP = &ForwarderBackendSemtechUDPConfig{}
	cfg.Forwarder.Backend.SemtechUDP.UDPBind = utils.Ptr("0.0.0.0:1680")
	cfg.Forwarder.Backend.SemtechUDP.FakeRxTime = utils.Ptr(false)
	cfg.Forwarder.Backend.BasicStation = &BasicStationBackendConfig{}
	cfg.Forwarder.Backend.BasicStation.Bind = utils.Ptr("0.0.0.0:8887")
	cfg.Forwarder.Backend.BasicStation.TLSCert = utils.Ptr("/etc/thingsix-forwarder/basic_station/cert.pem")
	cfg.Forwarder.Backend.BasicStation.TLSKey = utils.Ptr("/etc/thingsix-forwarder/basic_station/private_key.pem")
	cfg.Forwarder.Backend.BasicStation.CACert = utils.Ptr("/etc/thingsix-forwarder/basic_station/ca_cert.pem")
	cfg.Forwarder.Backend.BasicStation.StatsInterval = utils.Ptr(30 * time.Second)
	cfg.Forwarder.Backend.BasicStation.PingInterval = utils.Ptr(time.Minute)
	cfg.Forwarder.Backend.BasicStation.TimesyncInterval = utils.Ptr(time.Hour)
	cfg.Forwarder.Backend.BasicStation.ReadTimeout = utils.Ptr(65 * time.Second)
	cfg.Forwarder.Backend.BasicStation.WriteTimeout = utils.Ptr(time.Second)
	cfg.Forwarder.Gateways = ForwarderGatewayConfig{}
	cfg.Forwarder.Gateways.HttpAPI.Address = "127.0.0.1:8080"
	cfg.Forwarder.Gateways.Store = gateway.StoreConfig{}
	cfg.Forwarder.Gateways.Store.DefaultGatewayFrequencyPlan = frequency_plan.Invalid
	cfg.Forwarder.Gateways.Store.RefreshInterval = utils.Ptr(30 * time.Minute)
	cfg.Forwarder.Gateways.Store.YamlStorePath = utils.Ptr("/etc/thingsix-forwarder/gateways.yaml")
	cfg.Forwarder.Gateways.RecordUnknown = &gateway.ForwarderGatewayRecordUnknownConfig{}
	cfg.Forwarder.Gateways.RecordUnknown.File = "/etc/thingsix-forwarder/unknown_gateways.yaml"
	cfg.Forwarder.Routers = ForwarderRoutersConfig{}
	cfg.Forwarder.Routers.ThingsIXApi = &ForwarderRoutersThingsIXAPIConfig{}
	cfg.Forwarder.Routers.ThingsIXApi.UpdateInterval = utils.Ptr(30 * time.Minute)
	cfg.BlockChain = BlockchainConfig{}
	cfg.BlockChain.Polygon = &BlockchainPolygonConfig{}
	cfg.BlockChain.Polygon.Confirmations = 128
	cfg.Log = LogConfig{}
	cfg.Log.Level = logrus.InfoLevel
	cfg.Log.Timestamp = true
	cfg.Metrics = &MetricsConfig{}
	cfg.Metrics.Prometheus = &MetricsPrometheusConfig{}
	cfg.Metrics.Prometheus.Address = "0.0.0.0:8888"
	cfg.Metrics.Prometheus.Path = "/metrics"

	if net == "main" {
		cfg.Forwarder.Gateways.BatchOnboarder.Address = common.Address{} // TODO, once available
		cfg.Forwarder.Gateways.ThingsIXOnboardEndpoint = "https://api.thingsix.com/gateways/v1/onboards/{onboarder}/{owner}"
		cfg.Forwarder.Gateways.Registry.ThingsIxApi.Endpoint = "https://api.thingsix.com/gateways/v1/{id}"
		cfg.Forwarder.Routers.ThingsIXApi.Endpoint = utils.Ptr("https://api.thingsix.com/routers/v1/snapshot")
		cfg.BlockChain.Polygon.Endpoint = "https://polygon-rpc.com"
		cfg.BlockChain.Polygon.ChainID = 137
		return &cfg
	}
	if net == "test" {
		cfg.Forwarder.Gateways.BatchOnboarder.Address = common.HexToAddress("0xe685A0826419Bc982c9278eA7798143Fe7CF9f11")
		cfg.Forwarder.Gateways.ThingsIXOnboardEndpoint = "https://api-testnet.thingsix.com/gateways/v1/onboards/{onboarder}/{owner}"
		cfg.Forwarder.Gateways.Registry.ThingsIxApi.Endpoint = "https://api-testnet.thingsix.com/gateways/v1/{id}"
		cfg.Forwarder.Routers.ThingsIXApi.Endpoint = utils.Ptr("https://api-testnet.thingsix.com/routers/v1/snapshot")
		cfg.BlockChain.Polygon.Endpoint = "https://rpc.ankr.com/polygon_mumbai"
		cfg.BlockChain.Polygon.ChainID = 80001
		return &cfg
	}
	if net == "dev" {
		cfg.Forwarder.Gateways.BatchOnboarder.Address = common.HexToAddress("0xC7Dc48Ae9ED3e095f58ecF5320dE33F43A06cfC1")
		cfg.Forwarder.Gateways.ThingsIXOnboardEndpoint = "https://api-devnet.thingsix.com/gateways/v1/onboards/{onboarder}/{owner}"
		cfg.Forwarder.Gateways.Registry.ThingsIxApi.Endpoint = "https://api-devnet.thingsix.com/gateways/v1/{id}"
		cfg.Forwarder.Routers.ThingsIXApi.Endpoint = utils.Ptr("https://api-devnet.thingsix.com/routers/v1/snapshot")
		cfg.BlockChain.Polygon.Endpoint = "https://rpc.ankr.com/polygon_mumbai"
		cfg.BlockChain.Polygon.ChainID = 80001
		return &cfg
	}

	logrus.Fatalf("invalid net: %s, valid options are: main, test, dev and \"\"", net)
	return nil
}

// if ignoreLogLevel is true the log level is not set from config.
func mustLoadConfig(ignoreLogLevel bool) *Config {
	viper.SetConfigName("config") // name of config file (without extension)
	viper.SetConfigType("yaml")   // REQUIRED if the config file does not have the extension in the name

	net := viper.GetString("net")
	cfg := getNetConfig(net)

	if configFile := viper.GetString("config"); configFile != "" {
		viper.SetConfigFile(configFile)

		if err := viper.ReadInConfig(); err != nil {
			logrus.WithError(err).WithField("file", configFile).Fatal("unable to read config")
		}

		if err := viper.Unmarshal(cfg, viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToSliceHookFunc(","),
			utils.StringToEthereumAddressHook(),
			utils.IntToBigIntHook(),
			utils.HexStringToBigIntHook(),
			utils.StringToHashHook(),
			utils.StringToDuration(),
			utils.StringToLogrusLevel()))); err != nil {
			logrus.WithError(err).Fatal("unable to load configuration")
		}
	} else if net == "" {
		logrus.Fatal("neither a default network nor a config-file where provided. Provide at least one.")
	}

	if !ignoreLogLevel {
		logrus.SetLevel(cfg.Log.Level)
		logrus.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:    true,
			DisableTimestamp: !cfg.Log.Timestamp,
		})
	}

	if defaultGatewayFreqPlan := viper.GetString("default_frequency_plan"); defaultGatewayFreqPlan != "" {
		band, err := frequency_plan.GetBand(defaultGatewayFreqPlan)
		if err != nil {
			logrus.WithField("frequency_plan", defaultGatewayFreqPlan).Fatal("invalid default gateway frequency plan provided")
		}
		cfg.Forwarder.Gateways.Store.DefaultGatewayFrequencyPlan = frequency_plan.BandName(band.Name())
	}

	if net != "" {
		logrus.Infof("***Starting ThingsIX Forwarder connected to %snet***", net)
	} else {
		logrus.Info("***Starting ThingsIX Forwarder connected to unknown net***")
	}
	logrus.Infof("Version: %s", utils.Version())

	// ensure user provided polygon blockchain config
	if cfg.BlockChain.Polygon == nil {
		logrus.Fatal("missing Polygon blockchain configuration")
	}

	// if one of the config options require postgresql ensure that the user
	// configured postgresql.
	useDB := (cfg.Forwarder.Gateways.Store.Postgresql != nil && *cfg.Forwarder.Gateways.Store.Postgresql) ||
		(cfg.Forwarder.Gateways.RecordUnknown.Postgresql != nil && *cfg.Forwarder.Gateways.RecordUnknown.Postgresql)

	if useDB && cfg.Database != nil && cfg.Database.Postgresql != nil {
		database.MustInit(*cfg.Database.Postgresql)
	} else if useDB {
		logrus.Fatal("missing database postgresql configuration")
	}

	// set the Default flag on the defaultRouters to distinct them from routes
	// loaded from ThingsIX
	for _, r := range cfg.Forwarder.Routers.Default {
		r.Default = true
	}

	// work-around to prevent circular dependencies
	if cfg.BlockChain.Polygon != nil && cfg.Forwarder.Gateways.Registry.OnChain != nil {
		cfg.Forwarder.Gateways.Registry.OnChain.Endpoint = cfg.BlockChain.Polygon.Endpoint
		cfg.Forwarder.Gateways.Registry.OnChain.Confirmation = cfg.BlockChain.Polygon.Confirmations
		cfg.Forwarder.Gateways.Registry.OnChain.ChainID = cfg.BlockChain.Polygon.ChainID
	}

	return cfg
}
