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
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

// yamlFileStore is gateway store that uses a yaml file on disk for persistency.
type yamlFileStore struct {
	// refreshInterval indicates how often the store must be loaded from disk
	// if nil the store won't do hot reloads.
	refreshInterval *time.Duration
	// path contains the full path to where the yaml gateway store is on disk
	path string
	// guards byLocalId and byNetId
	gwMapMu sync.RWMutex
	// collection of gateways indexed by their local ID
	byLocalId map[lorawan.EUI64]*Gateway
	// collection of gateways indexed by their network ID
	byNetId map[lorawan.EUI64]*Gateway
	// collectio of gateways indexed by their ThingsIX ID
	byThingsIxID map[ThingsIxID]*Gateway
	// filters gateways retrieved from the backend before adding them to this
	// store. Can be used to filter out not onboarded gateways
	filterer GatewayFiltererFunc
}

func NewYamlFileStore(ctx context.Context, path string, refreshInterval *time.Duration) (*yamlFileStore, error) {
	return NewYamlFileStoreWithFilterer(ctx, path, refreshInterval, NoGatewayFilterer)
}

func NewYamlFileStoreWithFilterer(ctx context.Context, path string, refreshInterval *time.Duration, filterer GatewayFiltererFunc) (*yamlFileStore, error) {
	if path == "" {
		return nil, fmt.Errorf("invalid gateway store file")
	}

	logrus.WithFields(logrus.Fields{
		"file": path,
	}).Info("use file based gateway store")

	store := &yamlFileStore{
		refreshInterval: refreshInterval,
		path:            path,
		byLocalId:       make(map[lorawan.EUI64]*Gateway),
		byNetId:         make(map[lorawan.EUI64]*Gateway),
		byThingsIxID:    make(map[ThingsIxID]*Gateway),
		filterer:        filterer,
	}

	if err := store.loadFromFile(); err != nil {
		logrus.WithError(err).Fatal("unable to load gateways from disk")
	}

	go store.run(ctx)

	return store, nil
}

func (store *yamlFileStore) Count() int {
	store.gwMapMu.RLock()
	defer store.gwMapMu.RUnlock()

	return len(store.byLocalId)
}

func (store *yamlFileStore) Range(r GatewayRanger) {
	store.gwMapMu.RLock()
	defer store.gwMapMu.RUnlock()

	for _, gw := range store.byLocalId {
		if !r.Do(gw) {
			return
		}
	}
}

func (store *yamlFileStore) ByLocalID(localID lorawan.EUI64) (*Gateway, error) {
	store.gwMapMu.RLock()
	defer store.gwMapMu.RUnlock()
	if gw, ok := store.byLocalId[localID]; ok {
		return gw, nil
	}
	return nil, ErrNotFound
}

func (store *yamlFileStore) ByLocalIDString(id string) (*Gateway, error) {
	eui, err := utils.Eui64FromString(id)
	if err != nil {
		return nil, ErrInvalidGatewayID
	}
	return store.ByLocalID(eui)
}

func (store *yamlFileStore) ContainsByLocalID(localID lorawan.EUI64) bool {
	gw, _ := store.ByLocalID(localID)
	return gw != nil
}

func (store *yamlFileStore) ByNetworkID(netID lorawan.EUI64) (*Gateway, error) {
	store.gwMapMu.RLock()
	defer store.gwMapMu.RUnlock()
	if gw, ok := store.byNetId[netID]; ok {
		return gw, nil
	}
	return nil, ErrNotFound
}

func (store *yamlFileStore) ByNetworkIDString(id string) (*Gateway, error) {
	eui, err := utils.Eui64FromString(id)
	if err != nil {
		return nil, ErrInvalidGatewayID
	}
	return store.ByNetworkID(eui)
}

func (store *yamlFileStore) ContainsByNetID(netID lorawan.EUI64) bool {
	gw, _ := store.ByNetworkID(netID)
	return gw != nil
}

func (store *yamlFileStore) ByThingsIxID(id ThingsIxID) (*Gateway, error) {
	store.gwMapMu.RLock()
	defer store.gwMapMu.RUnlock()
	if gw, ok := store.byThingsIxID[id]; ok {
		return gw, nil
	}
	return nil, ErrNotFound
}

// Add gateway to the gateway store file on disk.
func (store *yamlFileStore) Add(ctx context.Context, localID lorawan.EUI64, key *ecdsa.PrivateKey) (*Gateway, error) {
	// ensure that gateway doesn't already exists in store
	if store.ContainsByLocalID(localID) {
		return nil, ErrAlreadyExists
	}

	// generate gateway instance
	gw, err := NewGateway(localID, key)
	if err != nil {
		return nil, err
	}

	// encode gateway entry
	encoded, err := yaml.Marshal([]gatewayYAML{{
		LocalID:    localID,
		PrivateKey: hex.EncodeToString(crypto.FromECDSA(gw.PrivateKey)),
	}})
	if err != nil {
		return nil, fmt.Errorf("unable to encode gateway: %w", err)
	}

	// append gateway to store
	f, err := os.OpenFile(store.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if _, err := f.Write(encoded); err != nil {
		return nil, err
	}

	// add new gateway to in memory cache
	store.gwMapMu.Lock()
	store.byLocalId[gw.LocalID] = gw
	store.byNetId[gw.NetworkID] = gw
	store.byThingsIxID[gw.ThingsIxID] = gw
	store.gwMapMu.Unlock()

	return gw, nil
}

// loadFromFile loads the gateway store from disk into this in-memory store.
func (store *yamlFileStore) loadFromFile() error {
	rawGateways, err := os.ReadFile(store.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return ErrStoreNotExists
		}
		if len(rawGateways) < 10 {
			return nil
		}
	}

	var (
		gws          []gatewayYAML
		byLocalId    = make(map[lorawan.EUI64]*Gateway)
		byNetId      = make(map[lorawan.EUI64]*Gateway)
		byThingsIxID = make(map[ThingsIxID]*Gateway)
	)

	if err := yaml.Unmarshal(rawGateways, &gws); err != nil {
		return fmt.Errorf("gateway store corrupt: %w", err)
	}

	// convert yaml gateways to *Gateway
	for _, ygw := range gws {
		gw, err := ygw.asGateway()
		if err != nil {
			return fmt.Errorf("unable to load gateway (localID=%s) from database", ygw.LocalID)
		}

		if store.filterer(gw) {
			byLocalId[gw.LocalID] = gw
			byNetId[gw.NetworkID] = gw
			byThingsIxID[gw.ThingsIxID] = gw
		} else {
			logrus.WithFields(logrus.Fields{
				"localID":   gw.LocalID,
				"networkID": gw.NetworkID,
			}).Trace("gateway didn't pass filter")
		}
	}

	oldByLocalId := store.byLocalId

	store.gwMapMu.Lock()
	store.byLocalId = byLocalId
	store.byNetId = byNetId
	store.byThingsIxID = byThingsIxID
	store.gwMapMu.Unlock()

	printGatewayStoreChanges(oldByLocalId, byLocalId)

	return nil
}

// run a loop in the background that periodically refreshes the in-memory
// gateway store with the gateway store file on disk.
func (store *yamlFileStore) run(ctx context.Context) {
	if store.refreshInterval == nil || *store.refreshInterval == 0 {
		logrus.Info("don't refresh gateway store from disk periodically")
		return
	}

	// fetch periodically gateways from file until the given ctx expired
	logrus.Infof("refresh gateway store from disk every %s", store.refreshInterval)
	for {
		select {
		case <-time.NewTimer(*store.refreshInterval).C:
			if err := store.loadFromFile(); err != nil {
				logrus.WithError(err).Error("unable to load gateways from disk")
			}
		case <-ctx.Done():
			return
		}
	}
}

// gatewayYAML is a helper type to serialize gateway store entires.
type gatewayYAML struct {
	// LocalID is the gateway id as used between gateway and forwarder
	LocalID lorawan.EUI64 `yaml:"local_id"`
	// PrivateKey is the gateways ECDSA hex encoded key that is registered in
	// ThingsIX and the gateway can use to proof its identity.
	PrivateKey string `yaml:"private_key"`
}

// asGatway converts the gatewayYAML store entry to a gateway entry with all
// gateway data derived from its local id and private key.
func (gw gatewayYAML) asGateway() (*Gateway, error) {
	keyBytes, err := hex.DecodeString(gw.PrivateKey)
	if err != nil {
		return nil, err
	}
	key, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		return nil, err
	}

	return &Gateway{
		LocalID:    gw.LocalID,
		NetworkID:  GatewayNetworkIDFromPrivateKey(key),
		PrivateKey: key,
		PublicKey:  &key.PublicKey,
		ThingsIxID: utils.DeriveThingsIxID(&key.PublicKey),
		Owner:      common.Address{}, // TODO: set once gateway onboard are required
	}, nil
}
