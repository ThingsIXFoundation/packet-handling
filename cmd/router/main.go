package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ThingsIXFoundation/packet-handling/cmd/router/config"
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

	var int integration.Integration
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
