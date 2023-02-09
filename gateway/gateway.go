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

	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ThingsIxID is the gateways public key without the leading `0x02`.
type ThingsIxID [32]byte

func (id ThingsIxID) String() string {
	return fmt.Sprintf("0x%x", id[:])
}

func (tid ThingsIxID) MarshalText() ([]byte, error) {
	return []byte(tid.String()), nil
}

func (tid *ThingsIxID) UnmarshalText(raw []byte) error {
	input := string(raw)
	if len(input) < 2 {
		return fmt.Errorf("invalid gateway id")
	}
	if input[0] == '0' && (input[1] == 'x' || input[1] == 'X') {
		input = input[2:]
	}
	id, err := hex.DecodeString(input)
	if err != nil {
		return fmt.Errorf("invalid gateway id")
	}
	if len(id) != len(*tid) {
		return fmt.Errorf("invalid gateway id")
	}
	var r [32]byte
	copy(r[:], id)

	*tid = r

	return nil
}

// Details are available after the owner set gateway details
type GatewayDetails struct {
	AntennaGain *string `json:"antennaGain,omitempty"`
	Band        *string `json:"band,omitempty"`
	Location    *string `json:"location,omitempty"`
	Altitude    *uint16 `json:"altitude,omitempty"`
}

// Gateway represents a ThingsIX gateway
type Gateway struct {
	// LocalID is the gateway ID as used in the communication between gateway
	// and ThingsIX forwarder and is usually derived from the gateways hardware.
	LocalID lorawan.EUI64 `json:"localId"`
	// NetId is the gateway id as used in the communication between the
	// forwarder and the ThingsIX network.
	NetworkID lorawan.EUI64 `json:"networkId"`
	// PrivateKey is the gateways private key
	PrivateKey *ecdsa.PrivateKey `json:"-"`
	// PublicKey is the gateways public key from which the ThingsIX is derived.
	PublicKey *ecdsa.PublicKey `json:"-"`
	// PublicKeyBytes
	PublicKeyBytes []byte `json:"-"`
	// ThingsIxID is the gateway id as used when onboarding the gateway in
	// ThingsIX. It is the gateways public key without the `0x02` prefix.
	ThingsIxID ThingsIxID `json:"gatewayId"`
	// Owner is the gateways owner if the gateway is onboarded in ThingsIX. If
	// nil the gateway is in the store but it is not onboarded in the ThingsIX
	// gateway registry.
	Owner *common.Address `json:"owner,omitempty"`
	// Version can be freely set when importing the gateway in the store. If nil
	// it means the gateway is in the store but it is not onboarded in the
	// ThingsIX gateway registry. ThingsIX doesn't use this value.
	Version *uint8 `json:"version,omitempty"`
	// Details set by the gateway owner. Can be empty when the details are not
	// set or the details are not yet retrieved.
	Details *GatewayDetails `json:"details,omitempty"`
}

// ID is the identifier as which the gateway is registered in the gateway
// registry.
func (gw Gateway) ID() ThingsIxID {
	return gw.ThingsIxID
}

// Onboarded returns an indication if the gateway is onboarded.
func (gw Gateway) Onboarded() bool {
	return gw.Owner != nil
}

// OwnerBytes returns the owner bytes or if the gateway isn't onboarded the
// zero address bytes.
func (gw Gateway) OwnerBytes() []byte {
	if gw.Owner == nil {
		return (common.Address{}).Bytes()
	}
	return gw.Owner.Bytes()
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
		NetworkID:  GatewayNetworkIDFromPrivateKey(priv),
		PrivateKey: priv,
		PublicKey:  &priv.PublicKey,
		ThingsIxID: utils.DeriveThingsIxID(&priv.PublicKey),
	}, nil
}

func (gw *Gateway) Address() common.Address {
	return crypto.PubkeyToAddress(*gw.PublicKey)
}

type Collector struct {
	Gateways []*Gateway
}

func (c *Collector) Do(gw *Gateway) bool {
	c.Gateways = append(c.Gateways, gw)
	return true
}
