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

package router

import (
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type Identity struct {
	ID         string            `yaml:"id"`
	PrivateKey *ecdsa.PrivateKey `yaml:"private_key"`
}

type Keyfile struct {
	Router struct {
		ID         string `yaml:"id"`
		PrivateKey string `yaml:"private_key"`
	}
}

func mustGenerateKey(filename string) {
	if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
		outputFile, err := os.Create(filename)
		if err != nil {
			logrus.WithError(err).Fatalf("unable to open output file %s", filename)
		}
		defer outputFile.Close()

		key, err := utils.GeneratePrivateKey()
		if err != nil {
			logrus.WithError(err).Fatal("unable to generate key")
		}

		// serialize gateway details
		var keyfile Keyfile
		keyfile.Router.ID = hex.EncodeToString(utils.CalculateCompressedPublicKeyBytes(&key.PublicKey))
		keyfile.Router.PrivateKey = hex.EncodeToString(crypto.FromECDSA(key))
		if err := yaml.NewEncoder(outputFile).Encode(keyfile); err != nil {
			logrus.WithError(err).Fatalf("unable to write router key to %s", filename)
		}
		fmt.Printf("router key written to: %s\n", filename)
		fmt.Printf("router id: %s\n", keyfile.Router.ID)
	} else {
		logrus.Fatalf("output file %s already exists", filename)
	}
}

func loadRouterIdentity(cfg *Config) (*Identity, error) {
	var keyfile Keyfile
	file, err := os.Open(cfg.Router.Keyfile)
	if err != nil {
		return nil, fmt.Errorf("unable to open router keyfile '%s'", cfg.Router.Keyfile)
	}
	defer file.Close()

	if err := yaml.NewDecoder(file).Decode(&keyfile); err != nil {
		return nil, fmt.Errorf("unable to decode router keyfile")
	}

	key, err := crypto.HexToECDSA(keyfile.Router.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("could not decode router private key from keyfile %s", cfg.Router.Keyfile)
	}

	return &Identity{
		ID:         hex.EncodeToString(utils.CalculateCompressedPublicKeyBytes(&key.PublicKey)),
		PrivateKey: key,
	}, nil
}
