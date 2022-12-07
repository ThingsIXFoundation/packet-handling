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
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/binary"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sirupsen/logrus"

	"github.com/brocaar/lorawan"
)

// Eui64ToUint64 converts a lorawan.EUI64 into an uint64 in BigEndian format.
func Eui64ToUint64(eui64 lorawan.EUI64) uint64 {
	return binary.BigEndian.Uint64(eui64[:])
}

// Eui64FromString tries to read a lorawan.EUI64 from string.
// It returns an error if it doesn't succeed.
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

func DeriveThingsIxID(pub *ecdsa.PublicKey) [32]byte {
	var id [32]byte
	raw := crypto.CompressPubkey(pub)[1:]
	copy(id[:], raw)
	return id
}

func CalculatePublicKeyBytes(pub *ecdsa.PublicKey) []byte {
	return crypto.FromECDSAPub(pub)
}
