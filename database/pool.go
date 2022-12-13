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

package database

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/dialers/postgres"
	"github.com/dlmiddlecote/sqlstats"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	pool *gorm.DB
)

// MustInit must be called before this package is used.
// It initializes the database connection pool.
func MustInit(dbCfg Config) {
	cfg := &gorm.Config{
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags),
			logger.Config{
				SlowThreshold: time.Second,
				LogLevel:      logger.Info,
				Colorful:      false,
			},
		),
	}

	if !dbCfg.EnableLogging {
		cfg.Logger = logger.Discard
	}

	db, err := gorm.Open(postgres.New(postgres.Config{
		DriverName: dbCfg.DriverName,
		DSN:        dsn(dbCfg),
	}), cfg)
	if err != nil {
		logrus.WithError(err).Fatal("unable to connect to postgres database")
	}

	// expose sql.DBStats in prometheus
	if sqlDB, err := db.DB(); err == nil {
		collector := sqlstats.NewStatsCollector(dbCfg.Database, sqlDB)
		prometheus.MustRegister(collector)
	} else {
		logrus.WithError(err).Warn("unable to obtain sql connection for prometheus DB stats")
	}

	pool = db
}

func DBWithContext(ctx context.Context) *gorm.DB {
	return pool.WithContext(ctx)
}

func dsn(cfg Config) string {
	dsnParts := make(map[string]string)

	if cfg.URI != "" {
		return cfg.URI
	}

	if cfg.Host != "" {
		dsnParts["host"] = cfg.Host
	}

	if cfg.User != "" {
		dsnParts["user"] = cfg.User
	}

	if cfg.Password != "" {
		dsnParts["password"] = cfg.Password
	}

	if cfg.Database != "" {
		dsnParts["database"] = cfg.Database
	}

	if cfg.Port != "" {
		dsnParts["port"] = cfg.Port
	}

	if cfg.SSLMode != "" {
		dsnParts["sslmode"] = cfg.SSLMode
	}

	dsn := ""
	for key, value := range dsnParts {
		dsn += fmt.Sprintf("%s=%s ", key, value)
	}

	return dsn
}
