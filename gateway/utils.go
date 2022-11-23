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

package gateway

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	gateway_registry "github.com/ThingsIXFoundation/gateway-registry-go"
	h3light "github.com/ThingsIXFoundation/h3-light"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/olekukonko/tablewriter"
	"github.com/sirupsen/logrus"
)

type Config struct {
	BlockChain struct {
		Endpoint      string
		ChainID       uint64
		Confirmations uint64
	} `mapstructure:"blockchain"`
}

func GeneratePrivateKey() (*ecdsa.PrivateKey, error) {
	for {
		priv, err := crypto.GenerateKey()
		if err != nil {
			return nil, err
		}

		compressedPub := crypto.CompressPubkey(&priv.PublicKey)
		if compressedPub[0] == 0x02 {
			return priv, nil
		}
	}
}

func mustDecodeGatewayID(input string) lorawan.EUI64 {
	bytes, err := hex.DecodeString(input)
	if err != nil {
		logrus.WithError(err).Fatal("invalid gateway local id")
	}

	id, err := utils.BytesToGatewayID(bytes)
	if err != nil {
		logrus.WithError(err).Fatal("invalid gateway local id")
	}
	return id
}

func mustDecodeThingsIXID(input string) [32]byte {
	if strings.HasPrefix(input, "0x") || strings.HasPrefix(input, "0X") {
		input = input[2:]
	}
	thingsIXID, err := hex.DecodeString(input)
	if err != nil {
		logrus.WithError(err).Fatal("invalid gateway ThingsIX id")
	}
	if len(thingsIXID) != 32 {
		logrus.Fatal("invalid ThingsIX id")
	}
	var ret [32]byte
	copy(ret[:], thingsIXID)
	return ret
}

func printGatewaysAsTable(gateways []*Gateway, registry *gateway_registry.GatewayRegistry) {
	var (
		table  = tablewriter.NewWriter(os.Stdout)
		header = []string{"", "thingsix_id", "local_id", "network_id"}
	)
	if registry != nil {
		header = append(header, "owner", "antenna_gain", "location", "altitude")
	}
	table.SetHeader(header)

	for i, gw := range gateways {
		var thingsIXID [32]byte
		copy(thingsIXID[:], gw.CompressedPublicKeyBytes)

		row := []string{
			fmt.Sprintf("%d", i+1),
			hex.EncodeToString(thingsIXID[:]),
			gw.LocalGatewayID.String(),
			gw.NetworkGatewayID.String(),
		}

		if registry != nil {
			if gateway, err := registry.Gateways(nil, thingsIXID); err == nil {
				if gateway.Owner != (common.Address{}) {
					row = append(row, gateway.Owner.Hex()) // guaranteed to be set
					if gateway.AntennaGain != 0 {
						location := h3light.Cell(gateway.Location)
						row = append(row, fmt.Sprintf("%.1f", float32(gateway.AntennaGain)/10.0))
						row = append(row, location.String())
						row = append(row, fmt.Sprintf("%d", uint64(gateway.Altitude)*3))
					}
				}
			}
		}
		table.Append(row)
	}
	table.Render()
}
