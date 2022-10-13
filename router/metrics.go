package router

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var (
// uplinksCounter = prometheus.NewCounter(prometheus.CounterOpts{
// 	Namespace: "packet_exchange",
// 	Name:      "packet_exchange_uplink_frames_recv",
// 	Help:      "received uplink frames from all gateways",
// })

//	uplinksFailedCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
//		Namespace: "packet_exchange",
//		Name:      "uplink_frames_failed",
//		Help:      "received uplink frames that could not be processed, group by gateway network id",
//	}, []string{"gw_network_id"})
)

func init() {
	// prometheus.MustRegister(uplinksCounter, uplinksFailedCounter)
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
