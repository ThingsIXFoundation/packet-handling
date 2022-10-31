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
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func Run(cmd *cobra.Command, args []string) {
	var (
		ctx, shutdown    = context.WithCancel(context.Background())
		cfg              = mustLoadConfig(args)
		wg               sync.WaitGroup
		sign             = make(chan os.Signal, 1)
		integration, err = buildIntegrations(cfg)
	)

	if err != nil {
		logrus.WithError(err).Fatal("unable to instantiate integration")
	}

	go integration.Start()

	router, err := NewRouter(cfg, integration)
	if err != nil {
		logrus.WithError(err).Fatal("unable to instantiate router")
	}

	// start routing packets
	wg.Add(1)
	go func() {
		go router.Run(ctx)
		wg.Done()
	}()

	// enable prometheus endpoint if configured
	if cfg.PrometheusEnabled() {
		wg.Add(1)
		go func() {
			publicPrometheusMetrics(ctx, cfg)
			wg.Done()
		}()
	}

	// wait for shutdown signal
	signal.Notify(sign, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-sign
	logrus.Info("initiate shutdown...")
	shutdown()
	wg.Wait()
	logrus.Info("bye")
}
