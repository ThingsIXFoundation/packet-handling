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
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"time"

	"github.com/ThingsIXFoundation/frequency-plan/go/frequency_plan"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	router_registry "github.com/ThingsIXFoundation/router-registry-go"
	"github.com/brocaar/lorawan"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/olekukonko/tablewriter"
	"github.com/sirupsen/logrus"
)

// localUplinkFrameToNetwork converts the given frame that was received from a gateway
// into a frame that can be send onto the network on behalf of the given gw.
func localUplinkFrameToNetwork(gw *gateway.Gateway, frame *gw.UplinkFrame) (*gw.UplinkFrame, error) {
	frame.RxInfo.GatewayId = gw.NetworkID.String()
	return frame, nil
}

// localDownlinkTxAckToNetwork converts the given txack that was received from a gateway
// into a txack that can be send onto the network on behalf of the given gw.
func localDownlinkTxAckToNetwork(gw *gateway.Gateway, txack *gw.DownlinkTxAck) (*gw.DownlinkTxAck, error) {
	txack.GatewayId = gw.NetworkID.String()
	return txack, nil
}

// networkDownlinkFrameToLocal converts the given frame received from gw into
// a frame that can be forwarded onto the network.
func networkDownlinkFrameToLocal(gw *gateway.Gateway, frame *gw.DownlinkFrame) *gw.DownlinkFrame {
	frame.GatewayId = gw.LocalID.String()
	return frame
}

// GatewayIDBytesToLoraEUID decodes the given id  bytes into a gateway id.
func GatewayIDBytesToLoraEUID(id []byte) lorawan.EUI64 {
	var lid lorawan.EUI64
	copy(lid[:], id)
	return lid
}

func fetchRoutersFromChain(cfg *Config, accounter Accounter) (RoutesUpdaterFunc, time.Duration, error) {
	interval := 30 * time.Minute // default refresh interval
	if cfg.Forwarder.Routers.OnChain.UpdateInterval != nil {
		if *cfg.Forwarder.Routers.OnChain.UpdateInterval < time.Minute {
			logrus.Warn("router on chain update interval too small, fall back to 30m")
		} else {
			interval = *cfg.Forwarder.Routers.OnChain.UpdateInterval
		}
	}

	logrus.WithFields(logrus.Fields{
		"interval": interval,
		"contract": cfg.Forwarder.Routers.OnChain.RegistryContract,
	}).Info("retrieve routes from on-chain router registry")

	return func() ([]*Router, error) {
		client, err := dialRPCNode(cfg)
		if err != nil {
			return nil, err
		}
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// determine latest confirmed block
		head, err := client.HeaderByNumber(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("unable to determine chain head: %w", err)
		}

		if head.Number.Uint64() < cfg.BlockChain.Polygon.Confirmations {
			return nil, nil // no confirmed blocks yet
		}

		var (
			confirmedBlock = head.Number.Uint64() - cfg.BlockChain.Polygon.Confirmations
			callOpts       = &bind.CallOpts{
				BlockNumber: new(big.Int).SetUint64(confirmedBlock),
			}
		)

		registry, err := router_registry.NewRouterRegistryCaller(cfg.Forwarder.Routers.OnChain.RegistryContract, client)
		if err != nil {
			return nil, fmt.Errorf("unable to instantiate router registry bindings")
		}

		routerCount, err := registry.RouterCount(callOpts)
		if err != nil {
			return nil, fmt.Errorf("unable to determine router count: %w", err)
		}

		var (
			routers  []*Router
			pageSize = int64(50)
		)
		for i := int64(0); i*pageSize < routerCount.Int64(); i += pageSize {
			fetchedRouters, err := registry.RoutersPaged(callOpts, big.NewInt(i), big.NewInt(i+pageSize))
			if err != nil {
				return nil, fmt.Errorf("unable to retrieve routers from registry: %w", err)
			}

			for _, r := range fetchedRouters {
				var netidb [4]byte
				binary.BigEndian.PutUint32(netidb[:], uint32(r.Netid.Uint64()))
				netid := lorawan.NetID{netidb[1], netidb[2], netidb[3]}
				freqPlan := frequency_plan.BlockchainFrequencyPlan(r.FrequencyPlan)
				routers = append(routers, NewRouter(r.Id, r.Endpoint, false, netid, r.Prefix, r.Mask, freqPlan, r.Owner, accounter))
			}
		}

		return routers, nil
	}, interval, nil
}

func dialRPCNode(cfg *Config) (*ethclient.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := ethclient.DialContext(ctx, cfg.BlockChain.Polygon.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("unable to dial RPC node: %w", err)
	}

	// ensure connected to the expected chain
	chainID, err := client.ChainID(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("unable to determine if dial RPC node on the correct network")
	}
	if chainID.Uint64() != cfg.BlockChain.Polygon.ChainID {
		return nil, fmt.Errorf("RPC node connected to wrong chain, want %d, got %d", cfg.BlockChain.Polygon.ChainID, chainID)
	}

	return client, nil
}

func fetchRoutersFromThingsIXAPI(cfg *Config, accounter Accounter) (RoutesUpdaterFunc, time.Duration, error) {
	interval := 30 * time.Minute // default refresh interval
	if cfg.Forwarder.Routers.ThingsIXApi.UpdateInterval != nil {
		if *cfg.Forwarder.Routers.ThingsIXApi.UpdateInterval < (15 * time.Minute) {
			logrus.Warnf("router ThingsIX update interval too small %s, fall back to 30m", *cfg.Forwarder.Routers.ThingsIXApi.UpdateInterval)
		} else {
			interval = *cfg.Forwarder.Routers.ThingsIXApi.UpdateInterval
		}
	}

	logrus.WithField("interval", interval).WithField("api", *cfg.Forwarder.Routers.ThingsIXApi.Endpoint).Info("retrieve routers from ThingsIX API")

	return func() ([]*Router, error) {
		resp, err := http.Get(*cfg.Forwarder.Routers.ThingsIXApi.Endpoint)
		if err != nil {
			return nil, err
		}

		snapshot := struct {
			BlockNumber uint64
			ChainID     uint64 `json:"chainId"`
			Routers     []struct {
				Endpoint      string
				ID            string
				Owner         common.Address
				NetId         uint32
				Prefix        uint32
				FrequencyPlan frequency_plan.BandName
				Mask          uint8
			}
		}{}

		if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()

		if snapshot.ChainID != cfg.BlockChain.Polygon.ChainID {
			return nil, fmt.Errorf("router snapshot from wrong chain, got %d, want %d", snapshot.ChainID, cfg.BlockChain.Polygon.ChainID)
		}

		// convert from snapshot to internal format
		routers := make([]*Router, len(snapshot.Routers))
		for i, r := range snapshot.Routers {
			var (
				id [32]byte
			)
			rID := common.FromHex(r.ID)
			if len(rID) != 32 {
				logrus.WithError(err).Error("invalid router id")
				continue
			}

			copy(id[:], rID)
			var netidb [4]byte
			binary.BigEndian.PutUint32(netidb[:], r.NetId)
			netid := lorawan.NetID{netidb[1], netidb[2], netidb[3]}
			routers[i] = NewRouter(id, r.Endpoint, false, netid, r.Prefix, r.Mask, r.FrequencyPlan.ToBlockchain(), r.Owner, accounter)
		}
		logrus.WithField("#routers", len(routers)).Info("fetched routing table from ThingsIX API")
		return routers, nil
	}, interval, nil
}

func SetDevAddrPrefix(devAddr lorawan.DevAddr, prefix uint32, maskLength uint8) lorawan.DevAddr {
	// convert DevAddr to uint32
	devAddrU := binary.BigEndian.Uint32(devAddr[:])

	// clear the bits for the prefix using the mask
	var mask uint32
	mask-- // sets all uint32 bits to 1
	devAddrU &^= mask << uint32(32-maskLength)

	// set the prefix
	devAddrU |= prefix

	ret := lorawan.DevAddr{}
	binary.BigEndian.PutUint32(ret[:], devAddrU)

	return ret
}

func DevAddrHasPrefix(devAddr lorawan.DevAddr, prefix uint32, mask uint8) bool {
	// The mask of 0 is a special case where no mask is applied, it doesn't match any prefix
	// It's impossible to have a prefix longer than 32.
	// 32 is also very unlikely as there are no variable bits left but in theory possible for testing
	if mask == 0 || mask > 32 {
		return false
	}

	tempAddr := SetDevAddrPrefix(devAddr, prefix, mask)
	return tempAddr == devAddr
}

func mustDecodeGatewayID(input string) lorawan.EUI64 {
	bytes, err := hex.DecodeString(input)
	if err != nil {
		logrus.WithError(err).Fatal("invalid gateway local id")
	}

	id, err := gateway.BytesToGatewayID(bytes)
	if err != nil {
		logrus.WithError(err).Fatal("invalid gateway local id")
	}
	return id
}

type GatewayCollector struct {
	Gateways []*gateway.Gateway
}

func (c *GatewayCollector) Do(gw *gateway.Gateway) bool {
	c.Gateways = append(c.Gateways, gw)
	return true
}

func printOnboards(json bool, onboards []*OnboardGatewayReply) {
	if json {
		printOnboardsAsJSON(onboards)
	} else {
		printOnboardsAsTable(onboards)
	}
}

func printOnboardsAsJSON(onboards []*OnboardGatewayReply) {
	var res []map[string]interface{}
	for _, onb := range onboards {
		res = append(res, map[string]interface{}{
			"owner":     onb.Owner,
			"gatewayId": onb.GatewayID,
			"version":   onb.Version,
			"localId":   onb.LocalID,
			"networkId": onb.NetworkID,
			"address":   onb.Address,
			"chainId":   onb.ChainID,
			"signature": onb.GatewayOnboardSignature,
		})
	}
	_ = json.NewEncoder(os.Stdout).Encode(res)
}

func printOnboardsAsTable(onboards []*OnboardGatewayReply) {
	var (
		table  = tablewriter.NewWriter(os.Stdout)
		header = []string{"", "gateway_id", "local_id",
			"owner", "version", "gateway_onboard_signature"}
	)

	table.SetHeader(header)

	for i, onb := range onboards {
		table.Append([]string{
			fmt.Sprintf("%d", i+1),
			onb.GatewayID.String(),
			onb.LocalID.String(),
			onb.Owner.String(),
			fmt.Sprintf("%d", onb.Version),
			onb.GatewayOnboardSignature,
		})
	}

	table.Render()
}

func printGatewaysAsTable(gateways []*gateway.Gateway) {
	var (
		table  = tablewriter.NewWriter(os.Stdout)
		header = []string{"", "thingsix_id", "local_id", "network_id", "owner", "band", "version", "antenna_gain", "location", "altitude"}
	)

	table.SetHeader(header)

	for i, gw := range gateways {
		var (
			owner       = ""
			band        = ""
			version     = ""
			antennaGain = ""
			location    = ""
			altitude    = ""
		)
		if gw.Owner != nil {
			owner = gw.Owner.String()
			version = fmt.Sprintf("%d", *gw.Version)
		}
		if gw.Details != nil {
			band = *gw.Details.Band
			antennaGain = *gw.Details.AntennaGain
			location = *gw.Details.Location
			altitude = fmt.Sprintf("%d", *gw.Details.Altitude)
		}
		row := []string{
			fmt.Sprintf("%d", i+1),
			hex.EncodeToString(gw.ThingsIxID[:]),
			gw.LocalID.String(),
			gw.NetworkID.String(),
			owner,
			band,
			version,
			antennaGain,
			location,
			altitude,
		}
		table.Append(row)
	}
	table.Render()
}

func mustParseAddress(addr string) common.Address {
	if !common.IsHexAddress(addr) {
		logrus.Fatalf("invalid address %s", addr)
	}
	return common.HexToAddress(addr)
}
