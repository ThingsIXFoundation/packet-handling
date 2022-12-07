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

// GatewayFiltererFunc is used by gateway stores to filter out gateways that
// are not (yet) suitable. For instance if these gateways have not yet been
// onboarded. If the gateway must be added to the in-memory gateway store and
// traffic must be exchanged between gateways and the ThingsIX network the
// filterer must return true for the given gw.
type GatewayFiltererFunc func(gw *Gateway) bool

// NoFilterer doesn't filter.
func NoGatewayFilterer(gw *Gateway) bool {
	return true
}

// func acceptOnlyOnboardedAndRegistryGateways(cfg *Config, store gateway.GatewayStore) (map[lorawan.EUI64]*gateway.Gateway, map[lorawan.EUI64]*gateway.Gateway, error) {
// 	client, err := ethclient.Dial(cfg.BlockChain.Polygon.Endpoint)
// 	if err != nil {
// 		logrus.WithError(err).Error("unable to dial blockchain RPC node")
// 	}
// 	defer client.Close()

// 	registry, err := gateway_registry.NewGatewayRegistry(*cfg.Forwarder.Gateways.RegistryAddress, client)
// 	if err != nil {
// 		return nil, nil, fmt.Errorf("unable to instantiate gateway registry bindings")
// 	}

// 	var (
// 		trustedGatewaysByLocalID   = make(map[lorawan.EUI64]*gateway.Gateway)
// 		trustedGatewaysByNetworkID = make(map[lorawan.EUI64]*gateway.Gateway)
// 	)

// 	for _, gateway := range store.Gateways() {
// 		// forwarder only forwards data for gateways that are onboarded and
// 		// their details such as location are set in the registry. If not print
// 		// a warning and ignore the gateway.
// 		rgw, err := registry.Gateways(nil, gateway.ID())
// 		if err != nil {
// 			logrus.WithError(err).Error("unable to retrieve gateway details from registry")
// 			continue
// 		}

// 		if rgw.AntennaGain != 0 {
// 			gateway.Owner = rgw.Owner
// 			trustedGatewaysByLocalID[gateway.LocalGatewayID] = gateway
// 			trustedGatewaysByNetworkID[gateway.NetworkGatewayID] = gateway
// 			logrus.WithFields(logrus.Fields{
// 				"local-id":     gateway.LocalGatewayID,
// 				"network-id":   gateway.NetworkGatewayID,
// 				"location":     fmt.Sprintf("%x", rgw.Location),
// 				"altitude":     rgw.Altitude * 3,
// 				"antenna-gain": fmt.Sprintf("%.1f", (float32(rgw.AntennaGain) / 10.0)),
// 				"owner":        gateway.Owner,
// 				"freq-plan":    frequency_plan.FromBlockchain(frequency_plan.BlockchainFrequencyPlan(rgw.FrequencyPlan)),
// 			}).Debug("loaded gateway from store")
// 		} else {
// 			l := logrus.WithFields(logrus.Fields{
// 				"id":         fmt.Sprintf("%x", gateway.ID()),
// 				"local_id":   gateway.LocalGatewayID,
// 				"network_id": gateway.NetworkGatewayID,
// 			})
// 			if rgw.Owner != (common.Address{}) {
// 				l.Warn("ingore gateway, details not set in gateway registry")
// 			} else {
// 				l.Warn("ignore gateway, gateway not onboarded and details not set in gateway registry")
// 			}
// 		}
// 	}

// 	return trustedGatewaysByLocalID, trustedGatewaysByNetworkID, err
// }

// func onboardedAndRegisteredGateways(cfg *Config, store gateway.GatewayStore) (map[lorawan.EUI64]*gateway.Gateway, map[lorawan.EUI64]*gateway.Gateway, error) {
// 	// If gateway registry is not configured accept data from all gateways from the store.
// 	// This is temporary until gateway onboards are made possible and ThingsIX moves from
// 	// data-only to a network with rewards.
// 	acceptOnlyRegisteredGateways := cfg.Forwarder.Gateways.RegistryAddress != nil

// 	if !acceptOnlyRegisteredGateways {
// 		logrus.Warn("accept all gateways in gateway store, including non-registered gateways")
// 		var (
// 			trustedGatewaysByLocalID   = make(map[lorawan.EUI64]*gateway.Gateway)
// 			trustedGatewaysByNetworkID = make(map[lorawan.EUI64]*gateway.Gateway)
// 		)

// 		for _, gateway := range store.Gateways() {
// 			trustedGatewaysByLocalID[gateway.LocalGatewayID] = gateway
// 			trustedGatewaysByNetworkID[gateway.NetworkGatewayID] = gateway
// 		}

// 		return trustedGatewaysByLocalID, trustedGatewaysByNetworkID, nil
// 	}

// 	return acceptOnlyOnboardedAndRegistryGateways(cfg, store)
// }
