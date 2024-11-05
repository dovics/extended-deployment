/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
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
