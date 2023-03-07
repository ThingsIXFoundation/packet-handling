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
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/ThingsIXFoundation/frequency-plan/go/frequency_plan"
	"github.com/ThingsIXFoundation/packet-handling/database"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/brocaar/lorawan"
	"github.com/cockroachdb/cockroach-go/v2/crdb/crdbgorm"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// pgStore is gateway store that uses a Postgres database as backend.
type pgStore struct {
	// refreshInterval indicates how often the store must be loaded from the
	// database
	refreshInterval time.Duration
	// guards byLocalId and byNetId
	gwMapMu sync.RWMutex
	// collection of gateways indexed by their local ID
	byLocalId map[lorawan.EUI64]*Gateway
	// collection of gateways indexed by their network ID
	byNetId map[lorawan.EUI64]*Gateway
	// collectio of gateways indexed by their ThingsIX ID
	byThingsIxID map[ThingsIxID]*Gateway
	// thingsix gateway registry
	registery ThingsIXRegistry
}

// NewPostgresStore returns a gateway store that uses a postgresql backend.
func NewPostgresStore(ctx context.Context, refreshInterval *time.Duration, registry ThingsIXRegistry) (*pgStore, error) {
	// create gateway table if not already available
	db := database.DBWithContext(ctx)
	if err := db.AutoMigrate(&pgGateway{}, &RecordedUnknownGateway{}); err != nil {
		return nil, err
	}

	refresh := refreshInterval
	if refresh == nil {
		*refresh = time.Duration(30 * time.Minute)
	}

	logrus.WithFields(logrus.Fields{
		"table":   pgGateway{}.TableName(),
		"refresh": refresh,
	}).Info("use database based gateway store")

	// instantiate gateway store
	store := &pgStore{
		refreshInterval: *refresh,
		byLocalId:       make(map[lorawan.EUI64]*Gateway),
		byNetId:         make(map[lorawan.EUI64]*Gateway),
		byThingsIxID:    make(map[ThingsIxID]*Gateway),
		registery:       registry,
	}

	// load initial set of gateways from backend
	if err := store.loadFromPostgres(ctx); err != nil {
		logrus.WithError(err).Error("unable to load gateways from database, retry later")
	}

	return store, nil
}

func (store *pgStore) Run(ctx context.Context) {
	for {
		select {
		case <-time.NewTimer(store.refreshInterval).C:
			// reload and sync with gateway registry
			store.syncAll(ctx)
		case <-ctx.Done():
			logrus.Info("stop gateway store")
			return
		}
	}
}

func (store *pgStore) Count() int {
	store.gwMapMu.RLock()
	defer store.gwMapMu.RUnlock()

	return len(store.byLocalId)
}

func (store *pgStore) Range(r GatewayRanger) {
	store.gwMapMu.RLock()
	defer store.gwMapMu.RUnlock()

	for _, gw := range store.byLocalId {
		if !r.Do(gw) {
			return
		}
	}
}

func (store *pgStore) ByLocalID(localID lorawan.EUI64) (*Gateway, error) {
	store.gwMapMu.RLock()
	defer store.gwMapMu.RUnlock()
	if gw, ok := store.byLocalId[localID]; ok {
		return gw, nil
	}
	return nil, ErrNotFound
}

func (store *pgStore) ByLocalIDString(id string) (*Gateway, error) {
	eui, err := utils.Eui64FromString(id)
	if err != nil {
		return nil, ErrInvalidGatewayID
	}
	return store.ByLocalID(eui)
}

func (store *pgStore) ContainsByLocalID(localID lorawan.EUI64) bool {
	gw, _ := store.ByLocalID(localID)
	return gw != nil
}

func (store *pgStore) ByNetworkID(netID lorawan.EUI64) (*Gateway, error) {
	store.gwMapMu.RLock()
	defer store.gwMapMu.RUnlock()
	if gw, ok := store.byNetId[netID]; ok {
		return gw, nil
	}
	return nil, ErrNotFound
}

func (store *pgStore) ByNetworkIDString(id string) (*Gateway, error) {
	eui, err := utils.Eui64FromString(id)
	if err != nil {
		return nil, ErrInvalidGatewayID
	}
	return store.ByNetworkID(eui)
}

func (store *pgStore) ContainsByNetID(netID lorawan.EUI64) bool {
	gw, _ := store.ByNetworkID(netID)
	return gw != nil
}

func (store *pgStore) ByThingsIxID(id ThingsIxID) (*Gateway, error) {
	store.gwMapMu.RLock()
	defer store.gwMapMu.RUnlock()
	if gw, ok := store.byThingsIxID[id]; ok {
		return gw, nil
	}
	return nil, ErrNotFound
}

func (store *pgStore) Add(ctx context.Context, localID lorawan.EUI64, key *ecdsa.PrivateKey) (*Gateway, error) {
	gw, err := NewGateway(localID, key)
	if err != nil {
		return nil, err
	}

	// store gateway record in db and remove it from the unknown recorded
	// gateways if its there
	var (
		unknown = RecordedUnknownGateway{LocalID: gw.LocalID}
		pggw    = pgGateway{
			LocalID:    gw.LocalID,
			PrivateKey: crypto.FromECDSA(gw.PrivateKey),
			CreatedAt:  time.Now(),
		}
	)

	db := database.DBWithContext(ctx)
	if err = crdbgorm.ExecuteTx(ctx, db, nil, func(tx *gorm.DB) error {
		if err := tx.Unscoped().Delete(&unknown).Error; err != nil {
			return err
		}
		err := tx.Create(&pggw).Error
		if err != nil {
			if database.IsErrUniqueViolation(err) {
				return ErrAlreadyExists
			}
		}
		return err
	}); err != nil {
		return nil, err
	}

	store.gwMapMu.Lock()
	store.byLocalId[gw.LocalID] = gw
	store.byNetId[gw.NetworkID] = gw
	store.gwMapMu.Unlock()

	logrus.WithFields(logrus.Fields{
		"localID":   gw.LocalID,
		"networkID": gw.NetworkID,
	}).Debug("loaded new gateway")

	return gw, nil
}

func (store *pgStore) SyncGatewayByLocalID(ctx context.Context, localID lorawan.EUI64, force bool) (*Gateway, error) {
	return store.syncGatewayByLocalID(ctx, localID, force)
}

func (store *pgStore) syncGatewayByLocalID(ctx context.Context, localID lorawan.EUI64, force bool) (*Gateway, error) {
	gw, err := store.ByLocalID(localID)
	if err != nil {
		return nil, err
	}

	owner, version, details, err := store.registery.GatewayDetails(ctx, gw.ThingsIxID, force)
	if err != nil {
		logrus.WithError(err).Debug("unable to retrieve gateway details from gateway registry")
		return gw, nil
	}

	synced, err := NewOnboardedGateway(gw.LocalID, gw.PrivateKey, owner, version)
	if err == nil {
		synced.Details = details
		var ( // update gateway in db
			pggw = pgGateway{
				LocalID:    synced.LocalID,
				PrivateKey: crypto.FromECDSA(gw.PrivateKey),
				Owner:      synced.Owner,
				Version:    synced.Version,
				Details:    synced.Details,
				LastSynced: sql.NullTime{
					Time:  time.Now(),
					Valid: true,
				},
			}
			db = database.DBWithContext(ctx)
		)
		if err = crdbgorm.ExecuteTx(ctx, db, nil, func(tx *gorm.DB) error {
			err := tx.Save(&pggw).Error
			if err != nil {
				if database.IsErrUniqueViolation(err) {
					return ErrAlreadyExists
				}
			}
			return err
		}); err != nil {
			return nil, err
		}
	}

	store.gwMapMu.Lock()
	store.byLocalId[synced.LocalID] = synced
	store.byNetId[synced.NetworkID] = synced
	store.byThingsIxID[synced.ThingsIxID] = synced
	store.gwMapMu.Unlock()

	return synced, nil
}

func (store *pgStore) UniqueGatewayBands() UniqueGatewayBands {
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

func (store *pgStore) syncAll(ctx context.Context) {
	var collector Collector
	store.Range(&collector)

	for _, gw := range collector.Gateways {
		_, _ = store.syncGatewayByLocalID(ctx, gw.LocalID, false)
	}
}

func (store *pgStore) loadFromPostgres(ctx context.Context) error {
	var (
		gws              []*pgGateway
		lctx, cancel     = context.WithTimeout(ctx, 10*time.Minute)
		db               = database.DBWithContext(lctx)
		byLocalId        = make(map[lorawan.EUI64]*Gateway)
		byNetId          = make(map[lorawan.EUI64]*Gateway)
		byThingsIxID     = make(map[ThingsIxID]*Gateway)
		mustSyncLocalIDs []lorawan.EUI64
		syncCutoff       = time.Now().Add(-10 * time.Minute)
	)
	defer cancel()

	if err := db.Find(&gws).Error; err != nil {
		return err
	}

	// convert postgres gateways to *Gateway
	for _, pggw := range gws {
		gw, err := pggw.asGateway()
		if err != nil {
			return fmt.Errorf("unable to load gateway (localID=%s) from database / %s", pggw.LocalID, err)
		}

		byLocalId[gw.LocalID] = gw
		byNetId[gw.NetworkID] = gw
		byThingsIxID[gw.ThingsIxID] = gw
		if pggw.LastSynced.Valid && pggw.LastSynced.Time.Before(syncCutoff) {
			mustSyncLocalIDs = append(mustSyncLocalIDs, gw.LocalID)
		}
	}

	oldByLocalId := store.byLocalId

	store.gwMapMu.Lock()
	store.byLocalId = byLocalId
	store.byNetId = byNetId
	store.byThingsIxID = byThingsIxID
	store.gwMapMu.Unlock()

	for _, localID := range mustSyncLocalIDs {
		_, _ = store.syncGatewayByLocalID(lctx, localID, false)
	}

	printGatewayStoreChanges(oldByLocalId, byLocalId)

	return nil
}

// pgGateway is a helper type to store gateway information in a PGSQL DB
type pgGateway struct {
	// LocalID holds the gateway ID as used between gateway and forwarder
	LocalID lorawan.EUI64 `gorm:"type:bytea;primaryKey"`
	// PrivateKey holds the gateway ECDSA private key DER encoded
	PrivateKey []byte `gorm:"uniqueIndex;not null"`
	// Owner of the gateway
	Owner *common.Address `gorm:"type:bytea"`
	// Version as used during onboarding
	Version *uint8
	// Details holds details retrieved from the ThingsIX registry
	Details *GatewayDetails `gorm:"embedded"`
	// LastSynced keeps track when the last time the data was retrieved from the
	// ThingsIX gateway registry
	LastSynced sql.NullTime
	// Set by gorm
	CreatedAt time.Time
}

func (pgGateway) TableName() string {
	return "gateway_store"
}

func (gw pgGateway) asGateway() (*Gateway, error) {
	key, err := crypto.ToECDSA(gw.PrivateKey)
	if err != nil {
		return nil, err
	}

	var details *GatewayDetails
	if gw.Details != nil && gw.Details.Band != nil {
		details = gw.Details
	}

	return &Gateway{
		LocalID:    gw.LocalID,
		NetworkID:  GatewayNetworkIDFromPrivateKey(key),
		PrivateKey: key,
		PublicKey:  &key.PublicKey,
		ThingsIxID: utils.DeriveThingsIxID(&key.PublicKey),
		Owner:      gw.Owner,
		Version:    gw.Version,
		Details:    details,
	}, nil
}
