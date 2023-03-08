// Copyright 2022 Stichting ThingsIX Foundation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/ThingsIXFoundation/packet-handling/forwarder"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/sirupsen/logrus"
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
	Args:    cobra.NoArgs,
	Run:     forwarder.Run,
	Version: utils.Version(),
}

func init() {
	rootCmd.PersistentFlags().String("config", "", "configuration file")
	rootCmd.PersistentFlags().String("net", "main", "the network to load the default parameters for (\"dev\", \"test\", \"main\" or \"\")")
	rootCmd.PersistentFlags().String("default_frequency_plan", "", "default gateway frequency plan")

	if err := viper.BindPFlags(rootCmd.PersistentFlags()); err != nil {
		logrus.WithError(err).Fatal("could not bind command line flags")
	}

	rootCmd.AddCommand(forwarder.GatewayCmds)
}
