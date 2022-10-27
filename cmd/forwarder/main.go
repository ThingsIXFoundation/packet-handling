package main

import (
	"github.com/ThingsIXFoundation/packet-handling/forwarder"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "forwarder",
	Short: "run the ThingsIX gateway packet forwarder",
	Long: `The ThingsIX packet forwarder allows gateways to exchange LoRa packets with
registered ThingsIX routers. It accepts packets from trusted gateways and
forwards these to routers and delivers packets from these routers back to
the gateway.`,
	Args: cobra.NoArgs,
	Run:  forwarder.Run,
}

func init() {
	rootCmd.PersistentFlags().String("config", "", "configuration file")
	viper.BindPFlag("config", rootCmd.PersistentFlags().Lookup("config"))

	rootCmd.AddCommand(gateway.Cmd)
}
