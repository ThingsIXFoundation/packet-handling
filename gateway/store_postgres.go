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
	"fmt"
	"sync"
	"time"

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
	refreshInterval *time.Duration
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

// NewPostgresStore returns a gateway store that uses a postgresql backend.
func NewPostgresStore(ctx context.Context, refreshInterval *time.Duration) (*pgStore, error) {
	return NewPostgresStoreWithFilterer(ctx, refreshInterval, NoGatewayFilterer)
}

// NewPostgresStore returns a gateway store that uses a postgresql backend and
// only imports gateways into the store for which the given filterer passes.
func NewPostgresStoreWithFilterer(ctx context.Context, refreshInterval *time.Duration, filterer GatewayFiltererFunc) (*pgStore, error) {
	logrus.WithField("table", pgGateway{}.TableName()).
		Info("use database based gateway store")

	// create gateway table if not already available
	db := database.DBWithContext(ctx)
	if err := db.AutoMigrate(&pgGateway{}); err != nil {
		return nil, err
	}

	// instantiate gateway store
	store := &pgStore{
		refreshInterval: refreshInterval,
		byLocalId:       make(map[lorawan.EUI64]*Gateway),
		byNetId:         make(map[lorawan.EUI64]*Gateway),
		byThingsIxID:    make(map[ThingsIxID]*Gateway),
		filterer:        filterer,
	}

	// load initial set of gateways from backend
	if err := store.loadFromPostgres(ctx); err != nil {
		logrus.WithError(err).Error("unable to load gateways from database, retry later")
	}

	// refresh periodically gateway store with backend
	go store.run(ctx)

	return store, nil
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

	encPrivateKey := crypto.FromECDSA(gw.PrivateKey)

	// add to postgresql
	pggw := pgGateway{
		LocalID:    gw.LocalID,
		PrivateKey: encPrivateKey,
	}

	db := database.DBWithContext(ctx)
	if err = crdbgorm.ExecuteTx(ctx, db, nil, func(tx *gorm.DB) error {
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
	store.byNetId[gw.NetID] = gw
	store.gwMapMu.Unlock()

	logrus.WithFields(logrus.Fields{
		"localID": gw.LocalID,
		"netID":   gw.NetID,
	}).Debug("loaded new gateway")

	return gw, nil
}

func (store *pgStore) run(ctx context.Context) {
	// fetch periodically gateways from postgres until the given ctx expired
	logrus.Infof("refresh gateway store from database every %s", store.refreshInterval)

	for {
		select {
		case <-time.NewTimer(*store.refreshInterval).C:
			loadCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			if err := store.loadFromPostgres(loadCtx); err != nil {
				logrus.WithError(err).Error("unable to load gateways from database")
			}
			cancel()
		case <-ctx.Done():
			return
		}
	}
}

func (store *pgStore) loadFromPostgres(ctx context.Context) error {
	var (
		gws          []pgGateway
		db           = database.DBWithContext(ctx)
		byLocalId    = make(map[lorawan.EUI64]*Gateway)
		byNetId      = make(map[lorawan.EUI64]*Gateway)
		byThingsIxID = make(map[ThingsIxID]*Gateway)
	)

	if err := db.Find(&gws).Error; err != nil {
		return err
	}

	// convert postgres gateways to *Gateway
	for _, pggw := range gws {
		gw, err := pggw.asGateway()
		if err != nil {
			return fmt.Errorf("unable to load gateway (localID=%s) from database", pggw.LocalID)
		}

		if store.filterer(gw) {
			byLocalId[gw.LocalID] = gw
			byNetId[gw.NetID] = gw
			byThingsIxID[gw.ThingsIxID] = gw
		} else {
			logrus.WithFields(logrus.Fields{
				"localID": gw.LocalID,
				"netID":   gw.NetID,
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

// pgGateway is a helper type to store gateway information in a PGSQL DB
type pgGateway struct {
	// LocalID holds the gateway ID as used between gateway and forwarder
	LocalID lorawan.EUI64 `gorm:"type:bytea;primaryKey"`
	// PrivateKey holds the gateway ECDSA private key DER encoded
	PrivateKey []byte `gorm:"uniqueIndex;not null"`
}

func (pgGateway) TableName() string {
	return "gateway_store"
}

func (gw pgGateway) asGateway() (*Gateway, error) {
	key, err := crypto.ToECDSA(gw.PrivateKey)
	if err != nil {
		return nil, err
	}

	return &Gateway{
		LocalID:    gw.LocalID,
		NetID:      GatewayIDFromPrivateKey(key),
		PrivateKey: key,
		PublicKey:  &key.PublicKey,
		ThingsIxID: utils.DeriveThingsIxID(&key.PublicKey),
		Owner:      common.Address{}, // TODO set once gateways must be onboarded
	}, nil
}
