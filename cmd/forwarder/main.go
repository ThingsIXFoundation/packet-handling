package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ThingsIXFoundation/packet-handling/forwarder"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var cmd = &cobra.Command{
	Use:   "forwarder",
	Short: "run the forwarder service",
	Run:   run,
}

func run(cmd *cobra.Command, args []string) {
	_, shutdown := context.WithCancel(context.Background())
	logrus.Info("starting forwarder")

	forwarder.NewForwarder(nil)

	// Wait for signal to shutdown
	sign := make(chan os.Signal, 1)
	signal.Notify(sign, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-sign

	logrus.Info("shutdown initiated...")
	shutdown()
	logrus.Info("bye!")
}

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
