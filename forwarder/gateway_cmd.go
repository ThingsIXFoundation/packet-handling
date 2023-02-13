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

package forwarder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	GatewayCmds = &cobra.Command{
		Use:   "gateway",
		Short: "gateway related commands",
	}

	importGatewayCmd = &cobra.Command{
		Use:   "import <owner> <version>",
		Short: "Import recorded unknown gateways in gateway store and generate onboard message",
		Args:  cobra.ExactArgs(2),
		Run:   importGatewayStore,
	}

	listGatewayCmd = &cobra.Command{
		Use:   "list",
		Short: "List gateway in gateway store",
		Args:  cobra.NoArgs,
		Run:   listGatewayStore,
	}

	addGatewayCmd = &cobra.Command{
		Use:   "add <local-id>",
		Short: "Add gateway to gateway store",
		Args:  cobra.ExactArgs(1),
		Run:   addGatewayToStore,
	}

	onboardGatewayCmd = &cobra.Command{
		Use:   "onboard <local-id> <version> <owner>",
		Short: "Generate onboard message",
		Args:  cobra.ExactArgs(3),
		Run:   onboardGateway,
	}

	gatewayDetailsCmd = &cobra.Command{
		Use:   "details <local-id>",
		Short: "Show gateway details",
		Args:  cobra.ExactArgs(1),
		Run:   gatewayDetails,
	}
)

func init() {
	GatewayCmds.AddCommand(importGatewayCmd)
	GatewayCmds.AddCommand(listGatewayCmd)
	GatewayCmds.AddCommand(addGatewayCmd)
	GatewayCmds.AddCommand(onboardGatewayCmd)
	GatewayCmds.AddCommand(gatewayDetailsCmd)
}

func onboardGateway(cmd *cobra.Command, args []string) {
	var (
		cfg                 = mustLoadConfig()
		localID, err        = utils.Eui64FromString(args[0])
		version, versionErr = strconv.ParseUint(args[1], 0, 8)
		owner               = common.HexToAddress(args[2])
	)

	if err != nil {
		logrus.WithError(versionErr).Fatal("invalid local id given")
	}
	if versionErr != nil {
		logrus.WithError(versionErr).Fatal("invalid version given")
	}
	if cfg.Forwarder.Gateways.HttpAPI.Address == "" {
		logrus.Fatal("HTTP API endpoint missing")
	}

	req, _ := json.Marshal(map[string]interface{}{
		"localId": localID,
		"version": version,
		"owner":   owner,
	})

	resp, err := http.Post(
		fmt.Sprintf("http://%s/v1/gateways/onboard", cfg.Forwarder.Gateways.HttpAPI.Address),
		"application/json",
		bytes.NewReader(req))
	if err != nil {
		logrus.WithError(err).Fatal("unable to retrieve recorded unknown gateways")
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var reply OnboardGatewayReply
		if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
			logrus.WithError(err).Fatal("unable to decode response")
		}
		printOnboardsAsTable([]*OnboardGatewayReply{&reply})
	default:
		msg, _ := io.ReadAll(resp.Body)
		logrus.Errorf("unexpected reply from API: %d - %s",
			resp.StatusCode, msg)
	}
}

func gatewayDetails(cmd *cobra.Command, args []string) {
	var (
		cfg          = mustLoadConfig()
		localID, err = utils.Eui64FromString(args[0])
		gw           gateway.Gateway
	)
	if err != nil {
		logrus.WithError(err).Fatal("invalid local id")
	}
	if cfg.Forwarder.Gateways.HttpAPI.Address == "" {
		logrus.Fatal("HTTP API endpoint missing")
	}

	resp, err := http.Get(fmt.Sprintf("http://%s/v1/gateways/%s",
		cfg.Forwarder.Gateways.HttpAPI.Address, localID))
	if err != nil {
		logrus.WithError(err).Fatal("unable to retrieve gateway data")
	}

	if err := json.NewDecoder(resp.Body).Decode(&gw); err != nil {
		logrus.WithError(err).Fatal("unable to decode response")
	}

	printGatewaysAsTable([]*gateway.Gateway{&gw})
}

func importGatewayStore(cmd *cobra.Command, args []string) {
	var (
		cfg                 = mustLoadConfig()
		owner               = common.HexToAddress(args[0])
		version, versionErr = strconv.ParseUint(args[1], 0, 8)
		unknown             []gateway.RecordedUnknownGateway
	)
	if versionErr != nil {
		logrus.WithError(versionErr).Fatal("invalid version given")
	}
	if cfg.Forwarder.Gateways.HttpAPI.Address == "" {
		logrus.Fatal("HTTP API endpoint missing")
	}

	resp, err := http.Get(fmt.Sprintf("http://%s/v1/gateways/unknown", cfg.Forwarder.Gateways.HttpAPI.Address))
	if err != nil {
		logrus.WithError(err).Fatal("unable to retrieve recorded unknown gateways")
	}

	if resp.StatusCode != http.StatusOK {
		logrus.Fatalf("unable to retrieve recorded unknown gateways")
	}

	if err := json.NewDecoder(resp.Body).Decode(&unknown); err != nil {
		logrus.WithError(err).Fatal("unable to decode response")
	}

	var (
		endpoint  = fmt.Sprintf("http://%s/v1/gateways/onboard", cfg.Forwarder.Gateways.HttpAPI.Address)
		onboarded []*OnboardGatewayReply
	)

	for _, rec := range unknown {
		payload, _ := json.Marshal(map[string]interface{}{
			"localId": rec.LocalID,
			"owner":   owner,
			"version": version,
		})

		resp, err := http.Post(endpoint, "application/json", bytes.NewReader(payload))
		if err != nil {
			logrus.WithError(err).Fatal("unable to import gateway")
		}

		switch resp.StatusCode {
		case http.StatusOK, http.StatusCreated:
			var reply OnboardGatewayReply
			if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
				logrus.WithError(err).Fatal("unable to decode response")
			}
			onboarded = append(onboarded, &reply)
		default:
			msg, _ := io.ReadAll(resp.Body)
			logrus.Errorf("unexpected reply from API: %d - %s",
				resp.StatusCode, msg)
		}
	}

	printOnboardsAsTable(onboarded)
}

func listGatewayStore(cmd *cobra.Command, args []string) {
	var (
		cfg      = mustLoadConfig()
		gateways map[string][]*gateway.Gateway
	)

	if cfg.Forwarder.Gateways.HttpAPI.Address == "" {
		logrus.Fatal("HTTP API endpoint missing")
	}

	endpoint := fmt.Sprintf("http://%s/v1/gateways", cfg.Forwarder.Gateways.HttpAPI.Address)
	resp, err := http.Get(endpoint)
	if err != nil {
		logrus.WithError(err).Fatal("unable to retrieve gateways")
	}

	if err := json.NewDecoder(resp.Body).Decode(&gateways); err != nil {
		logrus.WithError(err).Fatal("unable to decode gateways response")
	}

	all := append(gateways["onboarded"], gateways["pending"]...)
	printGatewaysAsTable(all)
}

func addGatewayToStore(cmd *cobra.Command, args []string) {
	var (
		localID    = mustDecodeGatewayID(args[0])
		cfg        = mustLoadConfig()
		reqPayload = map[string]interface{}{
			"localId": localID,
		}
	)

	if cfg.Forwarder.Gateways.HttpAPI.Address == "" {
		logrus.Fatal("HTTP API endpoint missing")
	}

	endpoint := fmt.Sprintf("http://%s/v1/gateways", cfg.Forwarder.Gateways.HttpAPI.Address)
	payload, err := json.Marshal(reqPayload)
	if err != nil {
		logrus.WithError(err).Fatal("unable to prepare request")
	}

	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		logrus.WithError(err).Fatal("unable to add gateway")
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var gw gateway.Gateway
		if err := json.NewDecoder(resp.Body).Decode(&gw); err != nil {
			logrus.WithError(err).Fatal("unable to decode response")
		}
		printGatewaysAsTable([]*gateway.Gateway{&gw})
	default:
		msg, _ := io.ReadAll(resp.Body)
		logrus.Errorf("unexpected reply from API: %d - %s",
			resp.StatusCode, msg)
	}
}
