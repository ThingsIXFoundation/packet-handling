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

package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/sirupsen/logrus"

	"github.com/brocaar/lorawan"
)

func GatewayPublicKeyToID(pubKey []byte) (lorawan.EUI64, error) {
	// pubkey is the compressed 33-byte long public key,
	// the gateway ID is the pub key without the 0x02 prefix
	if len(pubKey) == 33 {
		// compressed ThingsIX public keys always start with 0x02.
		// therefore don't use it and use bytes [1:] to derive the id
		h := sha256.Sum256(pubKey[1:])
		return BytesToGatewayID(h[:8])
	}
	return lorawan.EUI64{}, fmt.Errorf("invalid gateway public key (len=%d)", len(pubKey))
}

func BytesToGatewayID(id []byte) (lorawan.EUI64, error) {
	var gid lorawan.EUI64
	if len(id) != len(gid) {
		return lorawan.EUI64{}, fmt.Errorf("invalid gateway id length (len=%d)", len(id))
	}
	copy(gid[:], id)
	return gid, nil
}

// Eui64ToUint64 converts a lorawan.EUI64 into an uint64 in BigEndian format.
func Eui64ToUint64(eui64 lorawan.EUI64) uint64 {
	return binary.BigEndian.Uint64(eui64[:])
}

// Eui64FromString tries to read a lorawan.EUI64 from string. It returns an error if it doesn't succeed.
func Eui64FromString(str string) (lorawan.EUI64, error) {
	eui := lorawan.EUI64{}
	err := eui.UnmarshalText([]byte(str))
	if err != nil {
		return lorawan.EUI64{}, err
	}

	return eui, nil
}

func RandUint32() uint32 {
	b := make([]byte, 4)
	_, err := rand.Read(b)
	if err != nil {
		logrus.WithError(err).Fatal("could not generate random number")
	}
	return binary.BigEndian.Uint32(b)
}
