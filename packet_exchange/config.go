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

type ExchangeAccountingConfig map[string]interface{}

type AccountingType string

func (b ExchangeAccountingConfig) Type() AccountingType {
	fields := map[string]interface{}(b)
	return AccountingType(fields["type"].(string))
}

func (b ExchangeAccountingConfig) Accounter() Accounter {
	switch b.Type() {
	case NoAccountingType:
		return NewNoAccountingStrategy()
	default:
		return NewNoAccountingStrategy()
	}
}

const (
	NoAccountingType AccountingType = "no_accounting"
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

type Config struct {
	Log struct {
		Level     logrus.Level
		Timestamp bool
	}
	PacketExchange struct {
		// Backend represents a backend configuration as suported by the Chirpstack project.
		// Unfornutaly chirpstack uses inline struct definitions making it impossible for us
		// to reuse them and forceing use to make a mapping from our own config.
		Backend ExchangeGatewayBackendConfig

		// Optional account strategy configuration, if not specified no account is used meaning
		// that all packets are exchanged between gateway and routers.
		Accounting *ExchangeAccountingConfig

		// TrustedGateways is the list of gateways that are allowed to connect
		TrustedGateways []*Gateway `mapstructure:"gateways"`
	} `mapstructure:"packet_exchange"`
	Routers struct {
		// Default routers that will receive all gateway data unfiltered
		Default []*Router
		// Interval indicates how often the routes are refreshed
		UpdateInterval time.Duration `mapstructure:"update_interval"`
		ChainID        uint64        `mapstructure:"chain_id"`
		ThingsIXApi    *struct {
			Endpoint *string
		} `mapstructure:"thingsix_api"`
		// SmartContract retrieves routing information direct from the ThingsIX router contract
		SmartContract *struct {
			Confirmations uint64
			Address       common.Address
			Endpoint      *string
		} `mapstructure:"smart_contract"`
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

func mustLoadConfig(args []string) *Config {
	if len(args) == 1 {
		viper.SetConfigFile(args[0])
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
		decodeExchangeAccountingStrategy,
	))); err != nil {
		logrus.WithError(err).Fatal("unable to load configuration")
	}

	logrus.SetLevel(cfg.Log.Level)
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: cfg.Log.Timestamp})

	// if accounting is not specified use the default no accounting
	if cfg.PacketExchange.Accounting == nil {
		cfg.PacketExchange.Accounting = &ExchangeAccountingConfig{
			"type": NoAccountingType,
		}
	}

	// set the Default flag on the defaultRouters to distinct them from routes
	// loaded from ThingsIX
	for _, r := range cfg.Routers.Default {
		r.Default = true
	}

	return &cfg
}

func decodeExchangeAccountingStrategy(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.Map || t != reflect.TypeOf(ExchangeAccountingConfig{}) {
		return data, nil
	}

	fields := data.(map[string]interface{})
	if typ, ok := fields["type"].(string); ok {
		switch strings.ToLower(typ) {
		case string(NoAccountingType):
			if len(fields) != 1 {
				return nil, fmt.Errorf("invalid backend accounting configuration")
			}
			return fields, nil
		}
	} else {
		// use default no accounting if accounting is not configured
		if len(fields) != 0 {
			return nil, fmt.Errorf("invalid backend accounting configuration")
		}
		fields["type"] = NoAccountingType
		return fields, nil
	}
	return nil, fmt.Errorf("unsupported accounting '%s'", fields["type"])
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
