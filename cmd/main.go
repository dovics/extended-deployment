package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/component-base/logs"
	_ "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/dovics/extendeddeployment/cmd/app"
)

var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
var onlyOneSignalHandler = make(chan struct{})
var shutdownHandler chan os.Signal

func main() {
	if err := runControllerManagerCmd(); err != nil {
		os.Exit(1)
	}
}

func runControllerManagerCmd() error {
	logs.InitLogs()
	defer logs.FlushLogs()

	ctx := SetupSignalContext()
	if err := app.NewControllerManagerCommand(ctx).Execute(); err != nil {
		return err
	}

	return nil
}

func SetupSignalContext() context.Context {
	close(onlyOneSignalHandler) // panics when called twice

	shutdownHandler = make(chan os.Signal, 2)

	ctx, cancel := context.WithCancel(context.Background())
	signal.Notify(shutdownHandler, shutdownSignals...)
	go func() {
		<-shutdownHandler
		cancel()
		<-shutdownHandler
		os.Exit(1) // second signal. Exit directly.
	}()
	return ctx
}
