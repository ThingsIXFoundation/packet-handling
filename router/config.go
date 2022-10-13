package router

import (
	"fmt"
	"time"

	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/mitchellh/mapstructure"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type RouterConfig struct {
	Forwarder struct {
		Endpoint struct {
			Host string
			Port uint16
		}
	}

	Integration struct {
		Marshaler string `mapstructure:"marshaler"`

		MQTT *struct {
			EventTopicTemplate      string        `mapstructure:"event_topic_template"`
			CommandTopicTemplate    string        `mapstructure:"command_topic_template"`
			StateTopicTemplate      string        `mapstructure:"state_topic_template"`
			StateRetained           bool          `mapstructure:"state_retained"`
			KeepAlive               time.Duration `mapstructure:"keep_alive"`
			MaxReconnectInterval    time.Duration `mapstructure:"max_reconnect_interval"`
			TerminateOnConnectError bool          `mapstructure:"terminate_on_connect_error"`
			MaxTokenWait            time.Duration `mapstructure:"max_token_wait"`

			Auth *struct {
				Generic *struct {
					Server       string   `mapstructure:"server"`
					Servers      []string `mapstructure:"servers"`
					Username     string   `mapstructure:"username"`
					Password     string   `mapstrucure:"password"`
					CACert       string   `mapstructure:"ca_cert"`
					TLSCert      string   `mapstructure:"tls_cert"`
					TLSKey       string   `mapstructure:"tls_key"`
					QOS          uint8    `mapstructure:"qos"`
					CleanSession bool     `mapstructure:"clean_session"`
					ClientID     string   `mapstructure:"client_id"`
				} `mapstructure:"generic"`

				GCPCloudIoTCore *struct {
					Server        string        `mapstructure:"server"`
					DeviceID      string        `mapstructure:"device_id"`
					ProjectID     string        `mapstructure:"project_id"`
					CloudRegion   string        `mapstructure:"cloud_region"`
					RegistryID    string        `mapstructure:"registry_id"`
					JWTExpiration time.Duration `mapstructure:"jwt_expiration"`
					JWTKeyFile    string        `mapstructure:"jwt_key_file"`
				} `mapstructure:"gcp_cloud_iot_core"`

				AzureIoTHub *struct {
					DeviceConnectionString string        `mapstructure:"device_connection_string"`
					DeviceID               string        `mapstructure:"device_id"`
					Hostname               string        `mapstructure:"hostname"`
					DeviceKey              string        `mapstructure:"-"`
					SASTokenExpiration     time.Duration `mapstructure:"sas_token_expiration"`
					TLSCert                string        `mapstructure:"tls_cert"`
					TLSKey                 string        `mapstructure:"tls_key"`
				} `mapstructure:"azure_iot_hub"`
			} `mapstructure:"auth"`
		} `mapstructure:"mqtt"`
	} `mapstructure:"integration"`
}

func (rc RouterConfig) ForwarderListenerAddress() string {
	var (
		host = "0.0.0.0"
		port = uint16(8080)
	)

	if rc.Forwarder.Endpoint.Host != "" {
		host = rc.Forwarder.Endpoint.Host
	}
	if rc.Forwarder.Endpoint.Port != 0 {
		port = rc.Forwarder.Endpoint.Port
	}

	return fmt.Sprintf("%s:%d", host, port)
}

type Config struct {
	Log struct {
		Level     logrus.Level
		Timestamp bool
	}

	Router RouterConfig `mapstructure:"router"`

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
	))); err != nil {
		logrus.WithError(err).Fatal("unable to load configuration")
	}

	logrus.SetLevel(cfg.Log.Level)
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: cfg.Log.Timestamp})

	return &cfg
}
