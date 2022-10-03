package packetexchange

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var (
	uplinksCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "packet_exchange",
		Name:      "packet_exchange_uplink_frames_recv",
		Help:      "received uplink frames from all gateways",
	})

	uplinksFailedCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "packet_exchange",
		Name:      "uplink_frames_failed",
		Help:      "received uplink frames that could not be processed, group by gateway network id",
	}, []string{"gw_network_id"})

	downlinksCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "packet_exchange",
		Name:      "downlink_frames_recv",
		Help:      "received downlink frames from all routers",
	})

	downlinksFailedCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "packet_exchange",
		Name:      "downlink_frames_failed",
		Help:      "downlink frames from all routers that could not be processed",
	}, []string{"gw_network_id"})

	downlinkTxAckCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "packet_exchange",
		Name:      "downlink_tx_ack_recv",
		Help:      "received downlink tx ack from all gateways",
	})

	downlinkTxAckFailedCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "packet_exchange",
		Name:      "downlink_tx_ack_failed",
		Help:      "downlink tx acks that could not be processed",
	}, []string{"gw_network_id"})

	routersConnectedGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "packet_exchange",
		Name:      "routers_online",
		Help:      "online routers",
	})

	routersDisconnectedGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "packet_exchange",
		Name:      "routers_offline",
		Help:      "offline routers",
	})

	gatewayRxPacketsPerFrequencyGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "packet_exchange",
		Name:      "gateway_rx_packets_per_freq",
		Help:      "gateway received packets group by gateway and frequency",
	}, []string{"gw_network_id", "freq"})

	gatewayRxPacketsPerModulationGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "packet_exchange",
		Name:      "gateway_rx_packets_per_modulation",
		Help:      "gateway received packets grouped by gateway and modulation",
	}, []string{"gw_network_id", "bandwidth", "spreading_factor", "code_rate"})

	gatewayRxReceivedGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "packet_exchange",
		Name:      "gateway_rx_received",
		Help:      "Received packets by gateway",
	}, []string{"gw_network_id"})

	gatewayRxReceivedOKGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "packet_exchange",
		Name:      "gateway_rx_received_ok",
		Help:      "Received good packets by gateway",
	}, []string{"gw_network_id"})

	gatewayTxEmittedGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "packet_exchange",
		Name:      "gateway_tx_emitted",
		Help:      "Send packets by gateway",
	}, []string{"gw_network_id"})

	gatewayTxPacketsPerFrequencyGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "packet_exchange",
		Name:      "gateway_tx_packets_per_freq",
	}, []string{"gw_network_id", "freq"})

	gatewayTxPacketsPerModulationGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "packet_exchange",
		Name:      "gateway_tx_packets_per_modulation",
	}, []string{"gw_network_id", "bandwidth", "spreading_factor", "code_rate"})

	gatewayTxPacketsPerStatusGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "packet_exchange",
		Name:      "gateway_tx_packets_status",
	}, []string{"gw_network_id", "status"})

	gatewayTxPacketsReceivedGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "packet_exchange",
		Name:      "gateway_tx_received",
	}, []string{"gw_network_id"})
)

func init() {
	prometheus.MustRegister(uplinksCounter, uplinksFailedCounter,
		downlinksCounter, downlinksFailedCounter,
		downlinkTxAckCounter, downlinkTxAckFailedCounter,
		routersConnectedGauge, routersDisconnectedGauge,
		gatewayRxPacketsPerFrequencyGauge, gatewayRxPacketsPerModulationGauge,
		gatewayRxReceivedGauge, gatewayRxReceivedOKGauge,
		gatewayTxEmittedGauge,
		gatewayTxPacketsPerFrequencyGauge, gatewayTxPacketsPerModulationGauge,
		gatewayTxPacketsPerStatusGauge, gatewayTxPacketsReceivedGauge,
	)
}

func publicPrometheusMetrics(ctx context.Context, cfg *Config) {
	var (
		host        = "0.0.0.0"
		port uint16 = 8080
		path        = "/metrics"
	)

	if cfg.Metrics.Host != "" {
		host = cfg.Metrics.Host
	}
	if cfg.Metrics.Port != 0 {
		port = cfg.Metrics.Port
	}
	if cfg.Metrics.Path != "" {
		path = cfg.Metrics.Path
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	logrus.WithFields(logrus.Fields{
		"addr": addr,
		"path": cfg.Metrics.Path,
	}).Info("expose prometheus metrics")

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
