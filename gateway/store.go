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
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ThingsIXFoundation/frequency-plan/go/frequency_plan"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
)

type GatewayRanger interface {
	Do(*Gateway) bool
}

type GatewayRangerFunc func(*Gateway) bool

func (fn GatewayRangerFunc) Do(gw *Gateway) bool {
	return fn(gw)
}

type GatewayStore interface {
	// Run the background tasks in the store until the given ctx expires.
	Run(ctx context.Context)

	// Count returns the number of gateways in the store.
	Count() int

	// Range calls fn sequentially for each gateway present in the store.
	// If fn returns false, range stops the iteration.
	Range(GatewayRanger)

	// ByLocalID returns the gateway identified by the given local id.
	// If not found ErrNotFound is returned.
	ByLocalID(localID lorawan.EUI64) (*Gateway, error)

	// Returns the gateway identified by the given local id as string.
	// If not found ErrNotFound is returned.
	ByLocalIDString(id string) (*Gateway, error)

	// ContainsByLocalID returns an indication if there is gateway in the store
	// that is identified by the given local id.
	ContainsByLocalID(localID lorawan.EUI64) bool

	// ByNetworkID returns the gateway identified by the given network id.
	// If not found ErrNotFound is returned.
	ByNetworkID(netID lorawan.EUI64) (*Gateway, error)

	// ByNetworkIDString returns the gateway identified by the given network id
	// as string. If not found ErrNotFound is returned.
	ByNetworkIDString(id string) (*Gateway, error)

	// ContainsByNetID returns an indication if there is gateway in the store
	// that is identified by the given network id.
	ContainsByNetID(netID lorawan.EUI64) bool

	// ByThingsIxID returns the gateway identified by the given ThingsIX id.
	// If not found ErrNotFound is returned.
	ByThingsIxID(id ThingsIxID) (*Gateway, error)

	// Add creates a net gateway record based on the given localID and key and
	// adds it to the store.
	Add(ctx context.Context, localID lorawan.EUI64, key *ecdsa.PrivateKey) (*Gateway, error)

	// SyncGatewayByLocalID syncs the gateway identified by the given local id
	// and returns it after sync.
	SyncGatewayByLocalID(ctx context.Context, localID lorawan.EUI64, force bool) (*Gateway, error)

	// UniqueGatewayBands returns the flattened set of frequency plans for all
	// gateways in the store.
	UniqueGatewayBands() UniqueGatewayBands

	// DefaultFrequencyPlan returns the default frequency plan if set through
	// the configuration. If not it returns Invalid.
	DefaultFrequencyPlan() frequency_plan.BandName
}

// UniqueGatewayBands represents the unique set of frequency plans/bands
type UniqueGatewayBands struct {
	bands map[frequency_plan.BandName]struct{}
	plans map[frequency_plan.BlockchainFrequencyPlan]struct{}
}

func (u *UniqueGatewayBands) addBand(band frequency_plan.BandName) {
	u.bands[band] = struct{}{}
	u.plans[band.ToBlockchain()] = struct{}{}
}

func (u UniqueGatewayBands) ContainsBand(band frequency_plan.BandName) bool {
	_, found := u.bands[band]
	return found
}

func (u UniqueGatewayBands) ContainsFrequencyPlan(plan frequency_plan.BlockchainFrequencyPlan) bool {
	_, found := u.plans[plan]
	return found
}

// NewGatewayStore returns a gateway store that was configured in the given cfg.
func NewGatewayStore(ctx context.Context, storeCfg *StoreConfig, registryCfg *RegistrySyncConfig) (GatewayStore, error) {
	registery, err := NewThingsIXGatewayRegistry(registryCfg)
	if err != nil {
		return nil, err
	}

	switch storeCfg.Type() {
	case YamlFileGatewayStore:
		return NewYamlFileStore(ctx, *storeCfg.YamlStorePath, registery, storeCfg.DefaultGatewayFrequencyPlan)
	case PostgresqlGatewayStore:
		return NewPostgresStore(ctx, storeCfg.RefreshInterval, registery, storeCfg.DefaultGatewayFrequencyPlan)
	case NoGatewayStoreType:
		// no gateway store configured, fallback to default yaml gateway store
		// in $HOME/gateway-store.yaml
		home, err := os.UserHomeDir()
		if err != nil {
			logrus.Fatal("no gateway store configured")
		}
		storePath := filepath.Join(home, "gateway-store.yaml")
		return NewYamlFileStore(ctx, storePath, registery, storeCfg.DefaultGatewayFrequencyPlan)
	}

	return nil, ErrInvalidConfig
}

func GatewayNetworkIDFromPrivateKey(priv *ecdsa.PrivateKey) lorawan.EUI64 {
	pub := priv.PublicKey
	thingsIxID := utils.DeriveThingsIxID(&pub)
	h := sha256.Sum256(thingsIxID[:])

	gatewayID, _ := BytesToGatewayID(h[0:8])

	return gatewayID
}

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

func NewGateway(LocalID lorawan.EUI64, priv *ecdsa.PrivateKey) (*Gateway, error) {
	return &Gateway{
		LocalID:        LocalID,
		NetworkID:      GatewayNetworkIDFromPrivateKey(priv),
		PrivateKey:     priv,
		PublicKey:      &priv.PublicKey,
		ThingsIxID:     utils.DeriveThingsIxID(&priv.PublicKey),
		PublicKeyBytes: utils.CalculatePublicKeyBytes(&priv.PublicKey),
	}, nil
}

func NewOnboardedGateway(LocalID lorawan.EUI64, priv *ecdsa.PrivateKey, owner common.Address, version uint8) (*Gateway, error) {
	return &Gateway{
		LocalID:        LocalID,
		NetworkID:      GatewayNetworkIDFromPrivateKey(priv),
		PrivateKey:     priv,
		PublicKey:      &priv.PublicKey,
		ThingsIxID:     utils.DeriveThingsIxID(&priv.PublicKey),
		PublicKeyBytes: utils.CalculatePublicKeyBytes(&priv.PublicKey),
		Owner:          &owner,
		Version:        &version,
	}, nil
}

func printGatewayStoreChanges(old map[lorawan.EUI64]*Gateway, new map[lorawan.EUI64]*Gateway) {
	if len(old) == 0 {
		logrus.WithField("count", len(new)).Info("loaded gateways from gateway store")
		return // not interested to print more details, most cases initial start
	}

	var (
		removed = 0
		added   = 0
	)
	for k, gw := range old {
		if _, found := new[k]; !found {
			logrus.WithFields(logrus.Fields{
				"local_id":    gw.LocalID,
				"network_id":  gw.NetworkID,
				"thingsix_id": gw.ThingsIxID,
			}).Info("gateway removed from store")

			removed++
		}
	}
	for k, gw := range new {
		if _, found := old[k]; !found {
			logrus.WithFields(logrus.Fields{
				"local_id":    gw.LocalID,
				"network_id":  gw.NetworkID,
				"thingsix_id": gw.ThingsIxID,
			}).Info("gateway loaded from store")

			added++
		}
	}
	if added != 0 || removed != 0 {
		logrus.WithFields(logrus.Fields{
			"added":   added,
			"removed": removed,
			"count":   len(new),
		}).Info("loaded gateways from gateway store")
	}
}
