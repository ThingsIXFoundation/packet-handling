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

package forwarder

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/ThingsIXFoundation/packet-handling/database"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/brocaar/lorawan"
	"github.com/cockroachdb/cockroach-go/v2/crdb/crdbgorm"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"gorm.io/gorm"
)

type UnknownGatewayLoggerFunc func(localGatewayID lorawan.EUI64)

// NewUnknownGatewayLogger returns a callback that can be used to record gateways their
// local id to a source defined in the given cfg. This is used to record unknown gateways
// that connected to the backend. These can be verified later and if required imported
// into the gateway store and registered on ThingsIX.
func NewUnknownGatewayLogger(cfg *Config) UnknownGatewayLoggerFunc {
	if cfg.Forwarder.Gateways.RecordUnknown.Postgresql != nil && *cfg.Forwarder.Gateways.RecordUnknown.Postgresql {
		return recordUnkownGatewaysToPostgresql(cfg)
	}
	if cfg.Forwarder.Gateways.RecordUnknown != nil && cfg.Forwarder.Gateways.RecordUnknown.File != "" {
		return recordUnkownGatewaysToFile(cfg)
	}

	logrus.Info("don't record unknown gateways")
	return func(lorawan.EUI64) {}
}

type pgUnknownGateway struct {
	LocalID lorawan.EUI64 `gorm:"primaryKey"`
}

func (pgUnknownGateway) TableName() string {
	return "unknown_gateways"
}

func recordUnkownGatewaysToPostgresql(cfg *Config) UnknownGatewayLoggerFunc {
	var (
		db               = database.DBWithContext(context.Background())
		ctx              = context.Background()
		gatewaysMu       sync.RWMutex
		recordedGateways = make(map[lorawan.EUI64]struct{})
	)

	if err := db.AutoMigrate(&pgUnknownGateway{}); err != nil {
		logrus.WithError(err).Fatal("unable to create table to record unknown gateways")
	}

	logrus.WithField("table", pgUnknownGateway{}.TableName()).
		Info("record unknown gateways to database")

	// return callback to record connected gateways that are not included in the store
	return func(localID lorawan.EUI64) {
		gatewaysMu.RLock()
		_, alreadyRecored := recordedGateways[localID]
		gatewaysMu.RUnlock()

		if alreadyRecored {
			return
		}

		gw := pgUnknownGateway{
			LocalID: localID,
		}
		err := crdbgorm.ExecuteTx(ctx, db, nil, func(tx *gorm.DB) error {
			err := tx.Create(&gw).Error
			if err != nil && database.IsErrUniqueViolation(err) {
				return nil
			}
			return err
		})

		if err != nil {
			logrus.WithField("local_id", localID).Error("unable to record unknown gateway in db")
		} else {
			gatewaysMu.Lock()
			recordedGateways[localID] = struct{}{}
			gatewaysMu.Unlock()
			logrus.WithField("gw_local_id", localID).Info("unknown gateway recorded")
		}
	}
}

// recordUnkownGatewaysToFile uses a yaml file to record gateway local id's for
// gateways that are connected but not in the forwarders gateway store. This can
// be used to easily add new gateways.
func recordUnkownGatewaysToFile(cfg *Config) UnknownGatewayLoggerFunc {
	var (
		outputfile       = cfg.Forwarder.Gateways.RecordUnknown.File
		log              = logrus.WithField("file", outputfile)
		gatewaysMu       sync.Mutex
		recordedGateways = make(map[lorawan.EUI64]struct{})
		recordID         = make(chan lorawan.EUI64, 16)
	)

	log.WithField("file", outputfile).Info("record unknown gateways to file")

	// read already recorded gateways from file
	file, err := os.Open(outputfile)
	if err == nil {
		var alreadyRecorded []lorawan.EUI64
		if err := yaml.NewDecoder(file).Decode(&alreadyRecorded); err != nil {
			log.WithError(err).Fatal("unable to load existint set of recorded gateways")
		}
		file.Close()
		for _, id := range alreadyRecorded {
			recordedGateways[id] = struct{}{}
		}
	} else if !os.IsNotExist(err) {
		log.WithError(err).
			Fatal("unable to read recorded unknown gateways from file")
	}

	// write new gateway id in the background
	go func() {
		for {
			id, ok := <-recordID
			if !ok {
				return
			}
			bytes, _ := yaml.Marshal([]lorawan.EUI64{id})
			file, err := os.OpenFile(outputfile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0666)
			if err != nil {
				log.WithError(err).Error("unable to open unknown gateway record file")
				continue
			}
			_, err = file.Write(bytes)
			if err != nil {
				log.WithError(err).Error("unable to write unknown gateway record file")
				continue
			}
			file.Close()

			log.WithField("gw_local_id", id).Info("unknown gateway recorded")
		}
	}()

	// return callback to record connected gateways that are not included in the store
	return func(localGatewayID lorawan.EUI64) {
		gatewaysMu.Lock()
		defer gatewaysMu.Unlock()

		if _, ok := recordedGateways[localGatewayID]; !ok {
			recordedGateways[localGatewayID] = struct{}{}
			go func() {
				recordID <- localGatewayID
			}()
		}
	}
}

var (
	ErrInvalidConfig = errors.New("config doesn't contain recorded gateways configuration")
)

func GetAllRecordedUnknownGateways(cfg *gateway.ForwarderGatewayRecordUnknownConfig) ([]lorawan.EUI64, error) {
	if cfg == nil {
		return nil, ErrInvalidConfig
	}
	if cfg.Postgresql != nil && *cfg.Postgresql {
		return recordedUnknownGatewaysFromPostgres(cfg.File)
	}
	if cfg.File != "" {
		return recordedUnknownGatewaysFromFile(cfg.File)
	}
	return nil, ErrInvalidConfig
}

func recordedUnknownGatewaysFromFile(file string) ([]lorawan.EUI64, error) {
	// load list with unknown gateway local ids and generate a new gateway
	// record for each of them
	in, err := os.Open(file)
	if err != nil {
		logrus.WithError(err).Fatal("unable to open recorded gateways file")
	}
	defer in.Close()

	var ids []lorawan.EUI64
	if err := yaml.NewDecoder(in).Decode(&ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func recordedUnknownGatewaysFromPostgres(file string) ([]lorawan.EUI64, error) {
	var (
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		db          = database.DBWithContext(ctx)
		gateways    []pgUnknownGateway
		ids         []lorawan.EUI64
	)
	defer cancel()

	if err := db.Find(&gateways).Error; err != nil {
		return nil, err
	}

	for _, gw := range gateways {
		ids = append(ids, gw.LocalID)
	}

	return ids, nil
}
