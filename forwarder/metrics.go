package forwarder

import (
	"context"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var (
	uplinksCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "data",
		Name:      "uplinks",
		Help:      "received uplink frames that could not be processed, group by gateway network id",
	}, []string{"gw_network_id", "status"})

	downlinksCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "data",
		Name:      "downlinks",
		Help:      "downlink received to send to gateway",
	}, []string{"gw_network_id", "status"})

	downlinkTxAckCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "data",
		Name:      "downlink_tx_acks",
		Help:      "downlink tx acks that could not be processed",
	}, []string{"gw_network_id", "status"})

	routersConnectedGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "router",
		Name:      "online",
		Help:      "online routers",
	})

	routersDisconnectedGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "router",
		Name:      "offline",
		Help:      "offline routers",
	})

	gatewayRxPacketsPerFrequencyGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "gateway",
		Name:      "rx_packets_per_freq",
		Help:      "gateway received packets group by gateway and frequency",
	}, []string{"gw_network_id", "freq"})

	gatewayRxPacketsPerModulationGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "gateway",
		Name:      "rx_packets_per_modulation",
		Help:      "gateway received packets grouped by gateway and modulation",
	}, []string{"gw_network_id", "bandwidth", "spreading_factor", "code_rate"})

	gatewayRxReceivedGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "gateways",
		Name:      "rx_received",
		Help:      "Received packets by gateway",
	}, []string{"gw_network_id", "status"})

	gatewayTxEmittedGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "gateway",
		Name:      "tx_emitted",
		Help:      "Send packets by gateway",
	}, []string{"gw_network_id"})

	gatewayTxPacketsPerFrequencyGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "gateway",
		Name:      "tx_packets_per_freq",
	}, []string{"gw_network_id", "freq"})

	gatewayTxPacketsPerModulationGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "gateway",
		Name:      "tx_packets_per_modulation",
	}, []string{"gw_network_id", "bandwidth", "spreading_factor", "code_rate"})

	gatewayTxPacketsPerStatusGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "gateway",
		Name:      "tx_packets_status",
	}, []string{"gw_network_id", "status"})

	gatewayTxPacketsReceivedGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "gateway",
		Name:      "tx_received",
	}, []string{"gw_network_id"})
)

func init() {
	prometheus.MustRegister(uplinksCounter, downlinksCounter, downlinkTxAckCounter,
		routersConnectedGauge, routersDisconnectedGauge,
		gatewayRxPacketsPerFrequencyGauge, gatewayRxPacketsPerModulationGauge,
		gatewayRxReceivedGauge,
		gatewayTxEmittedGauge,
		gatewayTxPacketsPerFrequencyGauge, gatewayTxPacketsPerModulationGauge,
		gatewayTxPacketsPerStatusGauge, gatewayTxPacketsReceivedGauge,
	)
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

	httpServerDone.Add(1)
	go func() {
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			logrus.WithError(err).Error("prometheus http server stopped unexpected")
		}
		httpServerDone.Done()
	}()

	<-ctx.Done()
	httpServer.Shutdown(context.Background())
	httpServerDone.Wait()
	logrus.Info("prometheus metrics stopped")
}
