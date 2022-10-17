package main

import (
	"github.com/ThingsIXFoundation/packet-handling/router"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "router",
	Short: "run the router service",
	Args:  cobra.RangeArgs(0, 1),
	Run:   router.Run,
}
