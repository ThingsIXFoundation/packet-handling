package packetexchange

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/mitchellh/mapstructure"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type ExchangeGatewayBackendConfig map[string]interface{}

type BackendType string

const (
	SemtechUDPBackendType    BackendType = "semtech_udp"
	BasicStationBackendType  BackendType = "basic_station"
	ConcentratorDBackendType BackendType = "concentratord"
)

func (b ExchangeGatewayBackendConfig) Type() BackendType {
	fields := map[string]interface{}(b)
	return BackendType(fields["type"].(string))
}

type semtechBackendConfig struct {
	UDPBind      string
	SkipCRCCheck bool
	FakeRxTime   bool
}

func (b ExchangeGatewayBackendConfig) SemtechUDPConfig() (*semtechBackendConfig, error) {
	var cfg semtechBackendConfig
	if str, ok := b["udp_bind"].(string); ok {
		cfg.UDPBind = str
	} else {
		return nil, fmt.Errorf("invalid udp_bind")
	}
	if b, ok := b["skip_crc_check"].(bool); ok {
		cfg.SkipCRCCheck = b
	}
	if b, ok := b["fake_rx_time"].(bool); ok {
		cfg.FakeRxTime = b
	}
	return &cfg, nil
}

type AccountingStrategy struct{}

func (strat *AccountingStrategy) Strategy() Accounter {
	if strat == nil { // user has not configured accounting, disable it
		return NewNoAccountingStrategy()
	}
	return NewNoAccountingStrategy() // TODO: support other accounting in the future
}

type Config struct {
	Log struct {
		Level     logrus.Level
		Timestamp bool
	}

	BlockChain struct {
		Endpoint      string
		ChainID       uint64
		Confirmations uint64
	} `mapstructure:"blockchain"`

	Gateways struct {
	}

	PacketExchange struct {
		// Backend represents a backend configuration as suported by the Chirpstack project.
		// Unfornutaly chirpstack uses inline struct definitions making it impossible for us
		// to reuse them and forceing use to make a mapping from our own config.
		Backend ExchangeGatewayBackendConfig

		// Optional account strategy configuration, if not specified no account is used meaning
		// that all packets are exchanged between gateway and routers.
		Accounting *AccountingStrategy

		// TrustedGateways is the list of gateways that are allowed to connect
		TrustedGateways []*Gateway `mapstructure:"gateways"`
	} `mapstructure:"packet_exchange"`

	Routers struct {
		// Default routers that will receive all gateway data unfiltered
		Default []*Router
		// Interval indicates how often the routes are refreshed
		UpdateInterval time.Duration `mapstructure:"update_interval"`

		// ThingsIXApi indicates when non-nil that router information must be
		// fetched from the ThingsIX API
		ThingsIXApi *struct {
			Endpoint *string
		} `mapstructure:"thingsix_api"`

		// RegistryContract indicates when non-nil that router information must
		// be fetched from the registry smart contract (required blockchain cfg)
		RegistryContract *common.Address `mapstructure:"registry"`
	}

	Metrics *struct {
		Prometheus *struct {
			Host string
			Port uint16
			Path string
		}
	}
}

func (cfg Config) PrometheusEnabled() bool {
	return cfg.Metrics != nil &&
		cfg.Metrics.Prometheus != nil
}

func (cfg Config) MetricsPrometheusAddress() string {
	var (
		host        = "localhost"
		port uint16 = 8080
	)

	if cfg.Metrics.Prometheus.Host != "" {
		host = cfg.Metrics.Prometheus.Host
	}
	if cfg.Metrics.Prometheus.Port != 0 {
		port = cfg.Metrics.Prometheus.Port
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func (cfg Config) MetricsPrometheusPath() string {
	path := "/metrics"
	if cfg.Metrics.Prometheus.Path != "" {
		path = cfg.Metrics.Prometheus.Path
	}
	return path
}

func mustLoadConfig() *Config {
	viper.SetConfigName("forwarder")        // name of config file (without extension)
	viper.SetConfigType("yaml")             // REQUIRED if the config file does not have the extension in the name
	viper.AddConfigPath("/etc/thingsix/")   // path to look for the config file in
	viper.AddConfigPath("$HOME/.forwarder") // call multiple times to add many search paths
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
		utils.StringToLogrusLevel(),
		decodeTrustedGatewayHook,
		decodeExchangeGatewayBackend,
	))); err != nil {
		logrus.WithError(err).Fatal("unable to load configuration")
	}

	logrus.SetLevel(cfg.Log.Level)
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: cfg.Log.Timestamp})

	// set the Default flag on the defaultRouters to distinct them from routes
	// loaded from ThingsIX
	for _, r := range cfg.Routers.Default {
		r.Default = true
	}

	return &cfg
}

func decodeExchangeGatewayBackend(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.Map || t != reflect.TypeOf(ExchangeGatewayBackendConfig{}) {
		return data, nil
	}

	fields := data.(map[string]interface{})
	if typ, ok := fields["type"].(string); ok {
		switch BackendType(strings.ToLower(typ)) {
		case SemtechUDPBackendType:
			return ExchangeGatewayBackendConfig(fields), nil
		case BasicStationBackendType, ConcentratorDBackendType:
			return nil, fmt.Errorf("backend '%s' not (yet) implemented", typ)
		}
	}
	return nil, fmt.Errorf("unsupported backend '%s'", fields["type"])
}

func decodeTrustedGatewayHook(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.Map || t != reflect.TypeOf(Gateway{}) {
		return data, nil
	}

	var (
		fields     = data.(map[string]interface{})
		localID    = fields["local_id"]
		privateKey = fields["private_key"]
		gw         = Gateway{
			Owner: common.HexToAddress(fields["owner"].(string)),
		}
	)

	if localIDStr, ok := localID.(string); ok {
		id, err := hex.DecodeString(localIDStr)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted gateway local id")
		}
		if len(gw.LocalID) != len(id) {
			return nil, fmt.Errorf("invalid trusted gateway local id")
		}
		copy(gw.LocalID[:], id)
	} else {
		return nil, fmt.Errorf("invalid trusted gateway local id")
	}

	if privateKeyStr, ok := privateKey.(string); ok {
		key, err := crypto.HexToECDSA(privateKeyStr)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted gateway private key")
		}
		gw.privateKey = key
	} else {
		return nil, fmt.Errorf("invalid trusted gateway private key")
	}

	var (
		pub      = gw.privateKey.PublicKey
		pubBytes = crypto.CompressPubkey(&pub)
		// compressed ThingsIX public keys always start with 0x02.
		// therefore don't use it and use bytes [1:] to derive the id
		h = sha256.Sum256(pubBytes[1:])
	)

	gw.CompressedPublicKeyBytes = pubBytes
	copy(gw.NetworkID[:], h[:8])

	return &gw, nil
}
