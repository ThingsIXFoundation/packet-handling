package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend/semtechudp"
	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/config"
	"github.com/ThingsIXFoundation/packet-handling/forwarder"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "forwarder",
	Short: "run the forwarder service",
	Run:   run,
}

func init() {
	cobra.OnInitialize(initConfig)

	viper.BindPFlags(rootCmd.Flags())
	viper.BindPFlags(rootCmd.PersistentFlags())

}

func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("forwarder.config")
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		logrus.WithField("file", viper.ConfigFileUsed()).Debug("load config file")
	}
}

func run(cmd *cobra.Command, args []string) {
	_, shutdown := context.WithCancel(context.Background())
	logrus.SetLevel(logrus.InfoLevel)
	logrus.Info("starting forwarder")

	gb_conf := config.Config{}
	gb_conf.Backend.Type = "semtech_udp"
	gb_conf.Backend.SemtechUDP.UDPBind = "0.0.0.0:1680"

	backend, err := semtechudp.NewBackend(gb_conf)
	if err != nil {
		logrus.WithError(err).Error("error while creating UDP backend")
	}
	fwd, err := forwarder.NewForwarder(backend)
	if err != nil {
		logrus.WithError(err).Fatal("unable to instantie forwarder")
	}
	fwd.Run()

	backend.Start()

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
