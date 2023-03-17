// Copyright 2023 Stichting ThingsIX Foundation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
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
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/ThingsIXFoundation/packet-handling/database"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/brocaar/lorawan"
	"github.com/cockroachdb/cockroach-go/v2/crdb/crdbgorm"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"gorm.io/gorm"
)

var (
	// ErrRecordingUnknownGatewaysDisabled is thrown when recorded unknown
	// gateways are retrieved when this functionality is disabled.
	ErrRecordingUnknownGatewaysDisabled = fmt.Errorf("record unknown gateways disabled")
)

type RecordedUnknownGateway struct {
	LocalID lorawan.EUI64 `gorm:"primaryKey;column:local_id" yaml:"local_id" json:"localId"`
	// FirstSeen holds the unix time stamp when the gateway was first seen.
	// Can be nill for old recorded gateways.
	FirstSeen *int64 `yaml:"first_seen" json:"firstSeen,omitempty"`
}

func (unkn *RecordedUnknownGateway) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var val map[string]interface{}
	if err := unmarshal(&val); err == nil {
		localID, err := utils.Eui64FromString(val["local_id"].(string))
		if err != nil {
			return fmt.Errorf("invalid local_id")
		}
		var firstSeen int64
		if val, ok := val["first_seen"]; ok {
			switch v := val.(type) {
			case int:
				firstSeen = int64(v)
			case int32:
				firstSeen = int64(v)
			case int64:
				firstSeen = v
			default:
				return fmt.Errorf("invalid last seen")
			}
		}

		*unkn = RecordedUnknownGateway{
			LocalID:   localID,
			FirstSeen: &firstSeen,
		}
		return nil
	}

	// support legacy
	var s string
	if err := unmarshal(&s); err == nil {
		localID, err := utils.Eui64FromString(s)
		if err != nil {
			return fmt.Errorf("invalid local_id")
		}
		*unkn = RecordedUnknownGateway{
			LocalID: localID,
		}
		return nil
	}
	return fmt.Errorf("invalid unknown gateway")
}

type UnknownGatewayLogger interface {
	Record(localID lorawan.EUI64) error
	Recorded() ([]*RecordedUnknownGateway, error)
}

type noUnknownGatewayLogger struct{}

func (noUnknownGatewayLogger) Record(localID lorawan.EUI64) error {
	return nil
}

func (noUnknownGatewayLogger) Recorded() ([]*RecordedUnknownGateway, error) {
	return nil, ErrRecordingUnknownGatewaysDisabled
}

type yamlUnknownGatewayLogger struct {
	outputFile string
	muRecorded sync.RWMutex
	recorded   []*RecordedUnknownGateway
}

func (logger *yamlUnknownGatewayLogger) Record(localID lorawan.EUI64) error {
	// ensure it is not already logged
	logger.muRecorded.RLock()
	for _, recg := range logger.recorded {
		if recg.LocalID == localID {
			logger.muRecorded.RUnlock()
			return nil
		}
	}
	logger.muRecorded.RUnlock()

	firstSeen := time.Now().Unix()
	recg := RecordedUnknownGateway{
		LocalID:   localID,
		FirstSeen: &firstSeen,
	}

	enc, err := yaml.Marshal([]RecordedUnknownGateway{recg})
	if err != nil {
		return err
	}

	logger.muRecorded.Lock()
	defer logger.muRecorded.Unlock()

	// try to append it
	file, err := os.OpenFile(logger.outputFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}

	_, err = file.Write(enc)
	_ = file.Close()
	if err != nil {
		return err
	}
	logger.recorded = append(logger.recorded, &recg)

	logrus.WithField("gw_local_id", localID).Info("unknown gateway recorded")

	return nil
}

func (logger *yamlUnknownGatewayLogger) Recorded() ([]*RecordedUnknownGateway, error) {
	logger.muRecorded.RLock()
	defer logger.muRecorded.RUnlock()
	result := make([]*RecordedUnknownGateway, len(logger.recorded))
	copy(result, logger.recorded)
	return result, nil
}

type pgUnknownGatewayLogger struct{}

func (logger *pgUnknownGatewayLogger) Record(localID lorawan.EUI64) error {
	var (
		db        = database.DBWithContext(context.Background())
		ctx       = context.Background()
		firstSeen = time.Now().Unix()
		recg      = RecordedUnknownGateway{
			LocalID:   localID,
			FirstSeen: &firstSeen,
		}
	)

	return crdbgorm.ExecuteTx(ctx, db, nil, func(tx *gorm.DB) error {
		err := tx.Create(&recg).Error
		if err == nil {
			logrus.WithField("gw_local_id", localID).
				Info("unknown gateway recorded")
			return nil
		}
		if err != nil && database.IsErrUniqueViolation(err) {
			return nil // already recorded
		}
		return err
	})
}

func (logger *pgUnknownGatewayLogger) Recorded() ([]*RecordedUnknownGateway, error) {
	var (
		db   = database.DBWithContext(context.Background())
		recg []*RecordedUnknownGateway
	)

	if err := db.Order("local_id").Find(&recg).Error; err != nil {
		return nil, err
	}
	return recg, nil
}

// NewUnknownGatewayLogger returns a callback that can be used to record gateways their
// local id to a source defined in the given cfg. This is used to record unknown gateways
// that connected to the backend. These can be verified later and if required imported
// into the gateway store and registered on ThingsIX.
func NewUnknownGatewayLogger(cfg *ForwarderGatewayRecordUnknownConfig) UnknownGatewayLogger {
	if cfg != nil && cfg.Postgresql != nil && *cfg.Postgresql {
		return recordUnkownGatewaysToPostgresql()
	}
	if cfg != nil && cfg.File != "" {
		return recordUnkownGatewaysToFile(cfg.File)
	}

	logrus.Info("don't record unknown gateways")
	return &noUnknownGatewayLogger{}
}

func recordUnkownGatewaysToPostgresql() *pgUnknownGatewayLogger {
	db := database.DBWithContext(context.Background())
	if err := db.AutoMigrate(&RecordedUnknownGateway{}); err != nil {
		logrus.WithError(err).Fatal("unable to create table to record unknown gateways")
	}
	logrus.Info("record unknown gateways in postgresql")

	return &pgUnknownGatewayLogger{}
}

func recordUnkownGatewaysToFile(unknownFile string) *yamlUnknownGatewayLogger {
	// load already logged gateways
	var (
		file, err       = os.Open(unknownFile)
		alreadyRecorded []*RecordedUnknownGateway
		l               = logrus.WithField("file", unknownFile)
	)

	if err == nil {
		// Althought an io.EOF error should not happen here, we have seen it being thrown on some
		// systems. Catching it does no harm so why not.
		if err = yaml.NewDecoder(file).Decode(&alreadyRecorded); err != nil && err != io.EOF {
			l.WithError(err).
				Fatal("unable to load existing set of recorded gateways from file")
		}
		_ = file.Close()
	} else if !os.IsNotExist(err) {
		l.WithError(err).
			Fatal("unable to read recorded unknown gateways from file")
	}

	l.Info("record unknown gateways to file")

	return &yamlUnknownGatewayLogger{
		outputFile: unknownFile,
		recorded:   alreadyRecorded,
	}
}
