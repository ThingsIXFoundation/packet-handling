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
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var (
	rxPacketsCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "thingsix_forwarder",
		Name:      "rx_packets",
		Help:      "packets received, grouped by gateway local id and gateway network id",
	}, []string{"gw_network_id", "gw_local_id"})

	rxPacketPerFreqCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "thingsix_forwarder",
		Name:      "rx_packets_per_freq",
		Help:      "packets received, grouped by gateway local id, gateway network id and frequency",
	}, []string{"gw_network_id", "gw_local_id", "frequency"})

	rxPacketPerModulationCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "thingsix_forwarder",
		Name:      "rx_packets_per_modulation",
		Help:      "packets received, grouped by gateway local id, gateway network id and frequency",
	}, []string{"gw_network_id", "gw_local_id", "frequency", "bandwidth", "spreading_factor"})

	rxPacketPerNwkIdCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "thingsix_forwarder",
		Name:      "rx_packets_per_nwkid",
		Help:      "packets received, grouped by gateway local id, gateway network id, nwkid",
	}, []string{"gw_network_id", "gw_local_id", "nwkid"})

	rxPacketsPerRouterCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "thingsix_forwarder",
		Name:      "rx_packets_per_router",
		Help:      "packets received, grouped by gateway local id, gateway network id and router",
	}, []string{"gw_network_id", "gw_local_id", "router"})

	rxPacketsMapperCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "thingsix_forwarder",
		Name:      "rx_packets_mapper",
		Help:      "packets received, grouped by gateway local id, gateway network id and mapper packet type",
	}, []string{"gw_network_id", "gw_local_id", "type"})

	txPacketsCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "thingsix_forwarder",
		Name:      "tx_packets",
		Help:      "packets transmitted, grouped by gateway local id and gateway network id",
	}, []string{"gw_network_id", "gw_local_id"})

	txPacketPerFreqCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "thingsix_forwarder",
		Name:      "tx_packets_per_freq",
		Help:      "packets transmitted, grouped by gateway local id, gateway network id and frequency",
	}, []string{"gw_network_id", "gw_local_id", "frequency"})

	txPacketPerModulationCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "thingsix_forwarder",
		Name:      "tx_packets_per_modulation",
		Help:      "packets transmitted, grouped by gateway local id, gateway network id and frequency",
	}, []string{"gw_network_id", "gw_local_id", "frequency", "bandwidth", "spreading_factor"})

	txPacketPerNwkIdCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "thingsix_forwarder",
		Name:      "tx_packets_per_nwkid",
		Help:      "packets transmitted, grouped by gateway local id, gateway network id, nwkid and if it was forwarded",
	}, []string{"gw_network_id", "gw_local_id", "nwkid", "forwarded"})

	txPacketsPerRouterCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "thingsix_forwarder",
		Name:      "tx_packets_per_router",
		Help:      "packets transmitted, grouped by gateway local id, gateway network id and router",
	}, []string{"gw_network_id", "gw_local_id", "router"})

	txPacketsMapperCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "thingsix_forwarder",
		Name:      "tx_packets_mapper",
		Help:      "packets received, grouped by gateway local id, gateway network id",
	}, []string{"gw_network_id", "gw_local_id"})

	routersOnlineGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "thingsix_forwarder",
		Name:      "router_online",
	}, []string{"router"})

	gatewaysOnlineGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "thingsix_forwarder",
		Name:      "gateways_online",
	}, []string{"gw_network_id", "gw_local_id"})
)

// init registers Prometheus couters/gauges
func init() {
	prometheus.MustRegister(
		rxPacketsCounter, rxPacketPerFreqCounter, rxPacketPerModulationCounter, rxPacketPerNwkIdCounter, rxPacketsPerRouterCounter, rxPacketsMapperCounter,
		txPacketsCounter, txPacketPerFreqCounter, txPacketPerModulationCounter, txPacketPerNwkIdCounter, txPacketsPerRouterCounter, txPacketsMapperCounter,
		routersOnlineGauge,
		gatewaysOnlineGauge)

}

// runPrometheusHTTPEndpoint opens a Prometheus metrics endpoint on the
// configured location until the given ctx expires.
func runPrometheusHTTPEndpoint(ctx context.Context, cfg *Config) {
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
		done = make(chan struct{})
	)

	mux.Handle(path, promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	go func() {
		defer close(done)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			logrus.WithError(err).Error("prometheus http server stopped unexpected")
		} else {
			logrus.Info("prometheus metrics stopped")
		}
	}()

	<-ctx.Done()
	err := httpServer.Shutdown(context.Background())
	if err != nil {
		logrus.WithError(err).Error("could not stop prometheus metrics cleanly, stopping anyway")
	}
	<-done
}
