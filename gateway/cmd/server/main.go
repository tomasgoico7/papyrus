package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/papyrus/gateway/internal/app"
	"github.com/papyrus/gateway/internal/config"
	"github.com/papyrus/gateway/internal/jobs"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/router"
	"github.com/papyrus/gateway/internal/services"
	"github.com/papyrus/gateway/internal/worker"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 30 * time.Second
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
	metrics := observability.NewMetrics()

	// One registry and one analyzer, shared with the worker when it runs here:
	// two of either would split the cache and halve the metrics.
	upstream := app.UpstreamClient(cfg.RequestTimeout)
	analyzer := app.Analyzer(cfg, upstream, metrics, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var workers sync.WaitGroup
	if cfg.RunWorker {
		shutdown, err := startWorker(ctx, cfg, logger, metrics, analyzer, &workers)
		if err != nil {
			return err
		}
		defer shutdown()
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router.New(cfg, logger, metrics, analyzer),
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

	select {
	case err := <-serverErr:
		return fmt.Errorf("serving: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down gateway")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	// Jobs already claimed finish before the process goes away; abandoning them
	// would only have them reclaimed and redone.
	workers.Wait()

	logger.Info("gateway stopped")
	return nil
}

// startWorker runs the queue worker inside this process. Splitting it into its
// own service is the real shape — see cmd/worker — but a free tier has no room
// for a second always-on service, so the code stays separate and the deployment
// does not.
func startWorker(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	metrics *observability.Metrics,
	analyzer services.Analyzer,
	wg *sync.WaitGroup,
) (func(), error) {
	if !cfg.QueueEnabled() {
		return nil, errors.New("RUN_WORKER is set but DATABASE_URL is empty")
	}

	pool, err := app.Pool(ctx, cfg)
	if err != nil {
		return nil, err
	}

	embedded := worker.New(
		jobs.NewStore(pool),
		analyzer,
		metrics,
		logger.With(slog.String("component", "worker")),
		worker.Config{
			Concurrency: cfg.WorkerConcurrency,
			JobTimeout:  cfg.RequestTimeout,
		},
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		embedded.Run(ctx)
	}()

	return pool.Close, nil
}
