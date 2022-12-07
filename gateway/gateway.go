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
	"fmt"

	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ThingsIxID is the gateways public key without the leading `0x02`.
type ThingsIxID [32]byte

func (id ThingsIxID) String() string {
	return fmt.Sprintf("%x", id[:])
}

// Gateway represents a ThingsIX gateway
type Gateway struct {
	// LocalID is the gateway ID as used in the communication between gateway
	// and ThingsIX forwarder and is usually derived from the gateways hardware.
	LocalID lorawan.EUI64
	// NetId is the gateway id as used in the communication between the
	// forwarder and the ThingsIX network.
	NetID lorawan.EUI64
	// PrivateKey is the gateways private key
	PrivateKey *ecdsa.PrivateKey
	// PublicKey is the gateways public key from which the ThingsIX is derived.
	PublicKey *ecdsa.PublicKey
	// PublicKeyBytes
	PublicKeyBytes []byte
	// ThingsIxID is the gateway id as used when onboarding the gateway in
	// ThingsIX. It is the gateways public key without the `0x02` prefix.
	ThingsIxID ThingsIxID
	// Owner is the gateways owner if the gateway is onboarded in ThingsIX. If
	// nil it means the gateway is in the store but it is not onboarded in
	// ThingsIX (yet).
	Owner common.Address
}

// ID is the identifier as which the gateway is registered in the gateway
// registry.
func (gw Gateway) ID() ThingsIxID {
	return gw.ThingsIxID
}

// CompressedPubKeyBytes returns the compressed public key with 0x02 prefix
func (gw Gateway) CompressedPubKeyBytes() []byte {
	return append([]byte{0x2}, gw.ThingsIxID[:]...)
}

func GenerateNewGateway(localID lorawan.EUI64) (*Gateway, error) {
	priv, err := utils.GeneratePrivateKey()
	if err != nil {
		return nil, err
	}

	return &Gateway{
		LocalID:    localID,
		NetID:      GatewayIDFromPrivateKey(priv),
		PrivateKey: priv,
		PublicKey:  &priv.PublicKey,
		ThingsIxID: utils.DeriveThingsIxID(&priv.PublicKey),
		Owner:      common.Address{}, // TODO: set once gateway onboard are required
	}, nil
}

func (gw *Gateway) Address() common.Address {
	return crypto.PubkeyToAddress(*gw.PublicKey)
}
