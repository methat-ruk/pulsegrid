package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/httpserver"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/logging"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %s\n", err)
		os.Exit(1)
	}

	logger := logging.New(cfg.Environment, cfg.LogLevel, os.Stdout)
	server := httpserver.New(cfg, logger)
	listenErr := server.Listen(ctx, cfg.ShutdownTimeout)
	if ctx.Err() != nil {
		waitContext, cancelWait := context.WithTimeout(context.Background(), cfg.ShutdownTimeout+time.Second)
		defer cancelWait()
		select {
		case <-server.StoppedSignal():
		case <-waitContext.Done():
			logger.Error("http server shutdown did not complete", "reason_code", "shutdown_incomplete")
			os.Exit(1)
		}
	}

	if listenErr != nil && !errors.Is(listenErr, context.Canceled) {
		logger.Error("http server exited", "reason_code", "listen_failed", "error", listenErr.Error())
		os.Exit(1)
	}
}
