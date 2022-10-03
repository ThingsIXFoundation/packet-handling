package packetexchange

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/mitchellh/mapstructure"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Run the packet exchange
func Run(cmd *cobra.Command, args []string) {
	var (
		ctx, shutdown = context.WithCancel(context.Background())
		cfg           = mustLoadConfig(args)
		wg            sync.WaitGroup
		sign          = make(chan os.Signal, 1)
		exchange, err = NewExchange(cfg)
	)

	if err != nil {
		logrus.WithError(err).Fatal("unable to instantiate packet exchange")
	}

	// run packet exchange
	wg.Add(1)
	go func() {
		exchange.Run(ctx)
		wg.Done()
	}()

	// enable prometheus endpoint if configured
	if cfg.Metrics != nil {
		wg.Add(1)
		go func() {
			publicPrometheusMetrics(ctx, cfg)
			wg.Done()
		}()
	}

	// wait for shutdown signal
	signal.Notify(sign, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-sign
	logrus.Info("initiate shutdown...")
	shutdown()
	wg.Wait()
	logrus.Info("bye")
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
	for _, r := range cfg.Routes.Default {
		r.Default = true
	}

	return &cfg
}
