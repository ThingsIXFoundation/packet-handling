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
