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
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var (
	connectedForwardersGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "forwarders",
		Name:      "connected",
		Help:      "number of connected forwarders",
	})

	downlinksCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "data",
		Name:      "downlinks",
		Help:      "processed downlinks count",
	}, []string{"gw_network_id", "status"})

	uplinksCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "data",
		Name:      "uplinks",
		Help:      "processed uplinks count",
	}, []string{"gw_network_id", "status"})
)

func init() {
	prometheus.MustRegister(connectedForwardersGauge, downlinksCounter, uplinksCounter)
}

func publicPrometheusMetrics(ctx context.Context, cfg *Config) {
	var (
		addr = cfg.MetricsPrometheusAddress()
		path = cfg.MetricsPrometheusPath()
	)

	logrus.WithFields(logrus.Fields{
		"addr": addr,
		"path": path,
	}).Info("serve prometheus metrics")

	var (
		mux        = http.NewServeMux()
		httpServer = http.Server{
			Addr:    addr,
			Handler: mux,
		}
		httpServerDone sync.WaitGroup
	)

	mux.Handle(path, promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	httpServerDone.Add(1)
	go func() {
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			logrus.WithError(err).Error("prometheus http server stopped unexpected")
		}
		httpServerDone.Done()
	}()

	<-ctx.Done()
	err := httpServer.Shutdown(context.Background())
	if err != nil {
		logrus.WithError(err).Error("could not stop prometheus metrics cleanly, stopping anyway")
	}
	httpServerDone.Wait()
	logrus.Info("prometheus metrics stopped")
}
