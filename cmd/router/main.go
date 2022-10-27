package main

import (
	"github.com/ThingsIXFoundation/packet-handling/router"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "router",
	Short: "run the router service",
	Args:  cobra.RangeArgs(0, 1),
	Run:   router.Run,
}

func init() {
	rootCmd.PersistentFlags().String("config", "", "configuration file")
	viper.BindPFlag("config", rootCmd.PersistentFlags().Lookup("config"))
}
