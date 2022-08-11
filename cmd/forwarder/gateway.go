package main

import (
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/packet-handling/gateway/file"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// hotspotCmd represents the hotspot command
var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "gateways operations",
}

// rootCmd represents the base command when called without any subcommands
var addGatewayCmd = &cobra.Command{
	Use:   "add",
	Short: "add a gateway to the store",
	Args:  cobra.ExactArgs(1),
	Run:   addGateway,
}

func addGateway(cmd *cobra.Command, args []string) {
	gws, err := file.NewFileGatewayStore("gateways.keys")
	if err != nil {
		logrus.WithError(err).Fatal("could not open gateway store")
	}

	gatewayID, err := gateway.NewGatewayIDFromString(args[0])
	if err != nil {
		logrus.WithError(err).Fatal("invalid gatewayID")
	}

	gw, err := gws.GatewayByLocalID(gatewayID[:])
	if err != nil {
		logrus.WithError(err).Fatal("could not lookup gateway")
	}

	if gw != nil {
		logrus.WithError(err).Fatal("gateway already exists in store")
	}

	gw, err = gateway.GenerateNewGateway(gatewayID.Bytes())
	if err != nil {
		logrus.WithError(err).Fatal("could not generate new gateway")
	}

	err = gws.AddGateway(gw)
	if err != nil {
		logrus.WithError(err).Fatal("could not store new gateway")
	}

	logrus.Infof("generated new gateway keys for gateway:\nlocal gatewayID: %s\n network gatewayID: %s\n gateway address: %s", gatewayID, gw.NetworkGatewayID, gw.Address())
}

func init() {
	gatewayCmd.AddCommand(addGatewayCmd)
	rootCmd.AddCommand(gatewayCmd)
}
