package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/papyrus/gateway/internal/config"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/router"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("gateway exited with an error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	logger := observability.NewLogger(cfg.IsProduction(), cfg.LogLevel)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router.New(cfg, logger),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	// Buffered: a failed listen must not block on a channel nobody reads once
	// shutdown has already been triggered by a signal.
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("gateway listening",
			slog.String("port", cfg.Port),
			slog.String("environment", cfg.Environment),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("serving: %w", err)
	case <-quit:
	}

	logger.Info("shutting down gateway")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("gateway stopped")
	return nil
}
