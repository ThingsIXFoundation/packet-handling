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
		// Backend represents a backend configuration as suported by the Chirpstack project.
		// Unfornutaly chirpstack uses inline struct definitions making it impossible for us
		// to reuse them and forceing use to make a mapping from our own config.
		Backend struct {
			SemtechUDP *struct {
				UDPBind      *string `mapstructure:"udp_bind"`
				SkipCRCCheck *bool   `mapstructure:"skip_crc_check"`
				FakeRxTime   *bool   `mapstructure:"fake_rx_time"`
			} `mapstructure:"semtech_udp"`

			BasicStation  *struct{} `mapstructure:"basic_station"`
			Concentratord *struct{} `mapstructure:"concentratord"`
		}

		Gateways struct {
			Store struct {
				YamlStorePath *string `mapstructure:"file"`
			}

			RecordUnknown *struct {
				File string
			} `mapstructure:"record_unknown"`
			RegistryAddress *common.Address `mapstructure:"gateway_registry"`
		}

		Routers struct {
			// Default routers that will receive all gateway data unfiltered
			Default []*Router

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
		Accounting *struct {
		}
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
		logrus.Infof("set config file: %s", configFile)
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
