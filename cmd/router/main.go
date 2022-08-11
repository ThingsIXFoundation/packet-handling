package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ThingsIXFoundation/packet-handling/cmd/router/config"
	chirpconfig "github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/config"
	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/integration"

	phrouter "github.com/ThingsIXFoundation/packet-handling/router"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "router",
	Short: "run the router service",
	Run:   run,
}

func init() {
	rootCmd.PersistentFlags().String(config.GrpcListenAddress, config.GrpcListenAddressDefault, "gRPC address to open forwder listener on")

	if err := viper.BindPFlags(rootCmd.Flags()); err != nil {
		logrus.WithError(err).Fatal("could not bind command line flags")
	}
	if err := viper.BindPFlags(rootCmd.PersistentFlags()); err != nil {
		logrus.WithError(err).Fatal("could not bind command line flags")
	}
}

func run(cmd *cobra.Command, args []string) {
	ctx, shutdown := context.WithCancel(context.Background())
	logrus.SetLevel(logrus.InfoLevel)
	logrus.Info("starting router")

	// TODO: Improve
	chirpconf := chirpconfig.Config{}
	chirpconf.Integration.MQTT.Auth.Type = "generic"
	chirpconf.Integration.MQTT.Auth.Generic.Servers = []string{"tcp://localhost:1883"}
	chirpconf.Integration.MQTT.Auth.Generic.Username = ""
	chirpconf.Integration.MQTT.Auth.Generic.Password = ""
	chirpconf.Integration.MQTT.EventTopicTemplate = "eu868/gateway/{{ .GatewayID }}/event/{{ .EventType }}"
	chirpconf.Integration.MQTT.StateTopicTemplate = "eu868/gateway/{{ .GatewayID }}/state/{{ .StateType }}"
	chirpconf.Integration.MQTT.CommandTopicTemplate = "eu868/gateway/{{ .GatewayID }}/command/#"
	chirpconf.Integration.Marshaler = "protobuf"
	err := integration.Setup(chirpconf)
	if err != nil {
		logrus.WithError(err).Fatal("could not setup MQTT")
	}
	int := integration.GetIntegration()
	r, err := phrouter.NewRouter(int)
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

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
