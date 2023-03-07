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
	"path/filepath"
	"sync"
	"time"

	"github.com/ThingsIXFoundation/frequency-plan/go/frequency_plan"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

// yamlFileStore is gateway store that uses a yaml file on disk for persistency.
type yamlFileStore struct {
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
	// thingsix gateway registry
	registry ThingsIXRegistry
}

func NewYamlFileStore(ctx context.Context, path string, registry ThingsIXRegistry) (*yamlFileStore, error) {
	if path == "" {
		return nil, fmt.Errorf("invalid gateway store file")
	}

	logrus.WithField("file", path).Info("use file based gateway store")

	store := &yamlFileStore{
		path:         path,
		byLocalId:    make(map[lorawan.EUI64]*Gateway),
		byNetId:      make(map[lorawan.EUI64]*Gateway),
		byThingsIxID: make(map[ThingsIxID]*Gateway),
		registry:     registry,
	}

	if err := store.loadFromFile(); err != nil {
		logrus.WithError(err).Fatal("unable to load gateways from disk")
	}

	// sync immediatly with registry
	_ = store.syncAllGatewaysWithRegistry(ctx)

	return store, nil
}

// run a loop in the background that periodically refreshes the in-memory
// gateway store with the gateway store file on disk and executes commando's
// that mutate the store. This ensures that mutations are synchronized.
func (store *yamlFileStore) Run(ctx context.Context) {
	for {
		select {
		case <-time.NewTimer(30 * time.Minute).C:
			_ = store.syncAllGatewaysWithRegistry(ctx)
		case <-ctx.Done(): // forwarder issues to stop
			logrus.Info("stop gateway store")
			return
		}
	}
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

func (store *yamlFileStore) SyncGatewayByLocalID(ctx context.Context, localID lorawan.EUI64, force bool) (*Gateway, error) {
	gw, err := store.ByLocalID(localID)
	if err != nil {
		return nil, err
	}

	owner, version, details, err := store.registry.GatewayDetails(ctx, gw.ThingsIxID, force)
	if err != nil {
		logrus.WithError(err).Debug("unable to sync gateway with gateway registry")
		return gw, nil
	}

	synced, err := NewOnboardedGateway(gw.LocalID, gw.PrivateKey, owner, version)
	if err == nil {
		synced.Details = details
	} else {
		synced = gw
	}

	store.gwMapMu.Lock()
	store.byLocalId[synced.LocalID] = synced
	store.byNetId[synced.NetworkID] = synced
	store.byThingsIxID[synced.ThingsIxID] = synced
	store.gwMapMu.Unlock()

	return synced, nil
}

func (store *yamlFileStore) Add(ctx context.Context, localID lorawan.EUI64, key *ecdsa.PrivateKey) (*Gateway, error) {
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

	store.gwMapMu.Lock()
	defer store.gwMapMu.Unlock()

	if _, ok := store.byLocalId[gw.LocalID]; ok {
		return nil, ErrAlreadyExists
	}

	// append gateway to store
	f, err := os.OpenFile(store.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}

	if _, err := f.Write(encoded); err != nil {
		_ = f.Close()
		return nil, err
	}

	_ = f.Sync()
	_ = f.Close()

	// add new gateway to memory cache
	store.byLocalId[gw.LocalID] = gw
	store.byNetId[gw.NetworkID] = gw
	store.byThingsIxID[gw.ThingsIxID] = gw

	return gw, nil
}

func (store *yamlFileStore) syncAllGatewaysWithRegistry(ctx context.Context) error {
	var collector Collector
	store.Range(&collector)

	var gateways = collector.Gateways

	lctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// fetch latest gateway details for gateway
	for _, gw := range gateways {
		if _, err := store.SyncGatewayByLocalID(lctx, gw.LocalID, false); err != nil {
			logrus.WithError(err).
				WithField("gw_local_id", gw.LocalID).
				Warn("unable to sync gateway")
		}
	}

	return nil
}

func (store *yamlFileStore) UniqueGatewayBands() UniqueGatewayBands {
	var (
		collector Collector
		result    = UniqueGatewayBands{
			bands: make(map[frequency_plan.BandName]struct{}),
			plans: make(map[frequency_plan.BlockchainFrequencyPlan]struct{}),
		}
	)

	store.Range(&collector)

	for _, gw := range collector.Gateways {
		if gw.Details != nil && gw.Details.Band != nil {
			result.addBand(frequency_plan.BandName(*gw.Details.Band))
		}
	}
	return result
}

// loadFromFile loads the gateway store from disk into this in-memory store.
func (store *yamlFileStore) loadFromFile() error {
	rawGateways, err := os.ReadFile(store.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// try to create the directory if it doesn't yet exist
			dir := filepath.Dir(store.path)
			if err := os.MkdirAll(dir, os.ModePerm); err != nil {
				return ErrStoreNotExists
			}

			// try to create it, on success return empty store
			f, err := os.OpenFile(store.path, os.O_CREATE, 0600)
			if err != nil {
				return ErrStoreNotExists
			}
			_ = f.Close()
			printGatewayStoreChanges(nil, nil)
			return nil
		}
		return err
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

		byLocalId[gw.LocalID] = gw
		byNetId[gw.NetworkID] = gw
		byThingsIxID[gw.ThingsIxID] = gw
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
	}, nil
}
