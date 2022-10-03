package main

import (
	packetexchange "github.com/ThingsIXFoundation/packet-handling/packet_exchange"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "packetexchange <config>",
	Short: "run the ThingsIX gateway packet exhange",
	Long: `The ThingsIX packet exchange allows gateways to exchange LoRa packets with
registered ThingsIX routers. It accepts packets from trusted gateways and
forwards these to routers and delivers packets from these routers back to
the gateway.`,
	Run: packetexchange.Run,
}
