package router

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func Run(cmd *cobra.Command, args []string) {
	var (
		ctx, shutdown    = context.WithCancel(context.Background())
		cfg              = mustLoadConfig(args)
		wg               sync.WaitGroup
		sign             = make(chan os.Signal, 1)
		integration, err = buildIntegrations(cfg)
	)

	if err != nil {
		logrus.WithError(err).Fatal("unable to instantiate integration")
	}

	go integration.Start()

	router, err := NewRouter(cfg, integration)
	if err != nil {
		logrus.WithError(err).Fatal("unable to instantiate router")
	}

	// start routing packets
	wg.Add(1)
	go func() {
		go router.Run(ctx)
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

/*
func run(cmd *cobra.Command, args []string) {
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	ctx, shutdown := context.WithCancel(context.Background())
	logrus.SetLevel(logrus.InfoLevel)
	logrus.Info("starting router")

	// TODO: Improve
	chirpconf := chirpconfig.Config{}
	chirpconf.Integration.MQTT.StateRetained = true
	chirpconf.Integration.MQTT.KeepAlive = 30 * time.Second
	chirpconf.Integration.MQTT.MaxReconnectInterval = time.Minute
	chirpconf.Integration.MQTT.MaxTokenWait = time.Second
	chirpconf.Integration.MQTT.Auth.Type = "generic"
	chirpconf.Integration.MQTT.Auth.Generic.Servers = []string{"tcp://localhost:1883"}
	chirpconf.Integration.MQTT.Auth.Generic.Username = ""
	chirpconf.Integration.MQTT.Auth.Generic.Password = ""
	chirpconf.Integration.MQTT.EventTopicTemplate = "eu868/gateway/{{ .GatewayID }}/event/{{ .EventType }}"
	chirpconf.Integration.MQTT.StateTopicTemplate = "eu868/gateway/{{ .GatewayID }}/state/{{ .StateType }}"
	chirpconf.Integration.MQTT.CommandTopicTemplate = "eu868/gateway/{{ .GatewayID }}/command/#"
	chirpconf.Integration.MQTT.Auth.Generic.CleanSession = true
	//chirpconf.Integration.MQTT.Auth.Generic.ClientID = "auto-81DE6B71-E671-5889-19E2-319916050B16"
	chirpconf.Integration.Marshaler = "protobuf"
	err := integration.Setup(chirpconf)
	if err != nil {
		logrus.WithError(err).Fatal("could not setup MQTT")
	}
	int := integration.GetIntegration()
	go int.Start()
	r, err := router.NewRouter(int)
	if err != nil {
		logrus.WithError(err).Fatal("unable to setup router")
	}

	if err := r.Run(ctx, viper.GetString(config.GrpcListenAddress)); err != nil {
		logrus.WithError(err).Fatal("unable to run router")
	}

	// Wait for signal to shutdown
	sign := make(chan os.Signal, 1)
	signal.Notify(sign, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-sign

	logrus.Info("shutdown initiated...")
	shutdown()
	logrus.Info("bye!")
}
*/
