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
