// Command worker drains the analysis queue as its own service.
//
// This is the shape the design is built for: the API scales with how many
// people are using the site, the worker with how fast the model answers. Where
// a second always-on service cannot be paid for, the same worker runs inside
// the API process instead — RUN_WORKER=true in cmd/server. The code is the
// same; only the deployment differs.
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

	"github.com/papyrus/gateway/internal/app"
	"github.com/papyrus/gateway/internal/config"
	"github.com/papyrus/gateway/internal/jobs"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/worker"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited with an error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	if !cfg.QueueEnabled() {
		return errors.New("DATABASE_URL is required to run the worker")
	}

	logger := observability.NewLogger(cfg.IsProduction(), cfg.LogLevel).
		With(slog.String("component", "worker"))
	metrics := observability.NewMetrics()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := app.Pool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	upstream := app.UpstreamClient(cfg.RequestTimeout)
	drain := worker.New(
		jobs.NewStore(pool),
		app.Analyzer(cfg, upstream, metrics, logger),
		metrics,
		logger,
		worker.Config{
			Concurrency:   cfg.WorkerConcurrency,
			JobTimeout:    cfg.RequestTimeout,
			DoneRetention: cfg.JobRetention,
			DeadRetention: cfg.DLQRetention,
		},
	)

	// A worker still serves HTTP, for two reasons: a platform health check needs
	// somewhere to knock, and metrics nobody can scrape are metrics nobody has.
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           adminHandler(metrics),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("worker admin listening", slog.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	done := make(chan struct{})
	go func() {
		drain.Run(ctx)
		close(done)
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("serving: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	// Jobs already claimed are allowed to finish. Dropping them would only have
	// them reclaimed and redone by whoever comes next.
	select {
	case <-done:
	case <-shutdownCtx.Done():
		logger.Warn("gave up waiting for in-flight jobs")
	}

	logger.Info("worker stopped")
	return nil
}

func adminHandler(metrics *observability.Metrics) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("GET "+metrics.Path(), metrics.Handler())
	return mux
}
