package packetexchange

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// Run the packet exchange
func Run(cmd *cobra.Command, args []string) {
	var (
		ctx, shutdown = context.WithCancel(context.Background())
		cfg           = mustLoadConfig()
		wg            sync.WaitGroup
		sign          = make(chan os.Signal, 1)
		exchange, err = NewExchange(cfg)
	)

	if err != nil {
		logrus.WithError(err).Fatal("unable to instantiate packet exchange")
	}

	// run packet exchange
	wg.Add(1)
	go func() {
		exchange.Run(ctx)
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
