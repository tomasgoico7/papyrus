// Package app is the composition root: the wiring that both the API and the
// worker need, in one place so the two cannot drift into analysing requests
// differently.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/papyrus/gateway/internal/cache"
	"github.com/papyrus/gateway/internal/config"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/services"
)

const (
	// How long a local copy may disagree with the shared one. Short, because the
	// point of the shared tier is that replicas converge.
	localTTLWithRedis = 5 * time.Minute
	// The version changes on deploy, so it is read on a timer, not per request.
	versionTTL = 5 * time.Minute
	// A cache lookup that cannot beat the upstream is not worth waiting for.
	redisTimeout = 250 * time.Millisecond
)

// buildAnalyzer returns the analyzer the handler will use: the bare client when
// caching is off, and the cache in front of it otherwise.
//
// Caching needs no configuration to be useful. Without REDIS_URL the gateway
// still keeps an in-process cache, which is enough for a single instance; Redis
// adds what one process cannot have — entries shared between replicas and
// entries that survive a restart.
func Analyzer(
	cfg *config.Config,
	upstream *http.Client,
	metrics *observability.Metrics,
	logger *slog.Logger,
) services.Analyzer {
	client := services.NewAnalyzerClient(cfg.AIServiceURL, cfg.AIServiceToken, upstream)
	if !cfg.CacheEnabled() {
		logger.Info("analysis cache disabled")
		return client
	}

	store, localTTL := cacheStore(cfg, logger)
	logger.Info("analysis cache enabled",
		slog.Duration("ttl", cfg.CacheTTL),
		slog.Duration("local_ttl", localTTL),
		slog.Int("local_entries", cfg.CacheLocalEntries),
	)

	return services.NewCachedAnalyzer(
		client,
		store,
		services.NewVersionClient(cfg.AIServiceURL, cfg.AIServiceToken, upstream, versionTTL),
		metrics,
		cfg.CacheTTL,
		cfg.RequestTimeout,
	)
}

func cacheStore(cfg *config.Config, logger *slog.Logger) (cache.Store, time.Duration) {
	local := cache.NewLRU(cfg.CacheLocalEntries)

	if cfg.RedisURL == "" {
		// Nothing to converge with, so the local copy may live as long as the
		// entry itself.
		return local, cfg.CacheTTL
	}

	cache.SetRedisLogger(logger)

	shared, err := cache.NewRedis(cfg.RedisURL, redisTimeout)
	if err != nil {
		// A misconfigured URL costs the shared tier, not the service.
		logger.Warn("redis unavailable, caching in process only", slog.Any("error", err))
		return local, cfg.CacheTTL
	}

	// Reported, not enforced: Redis may well come up after the gateway does, and
	// the tier reads as a miss until it does.
	ctx, cancel := context.WithTimeout(context.Background(), redisTimeout)
	defer cancel()
	if err := shared.Ping(ctx); err != nil {
		logger.Warn("redis did not answer at startup", slog.Any("error", err))
	}

	localTTL := min(cfg.CacheTTL, localTTLWithRedis)
	return cache.NewTiered(local, shared, localTTL), localTTL
}

// UpstreamClient is shared by every client of the AI service so they pool
// connections to the single upstream instead of keeping a pool each.
//
// The default transport caps idle connections per host at 2, which under any
// concurrency forces a fresh dial — and a fresh TLS handshake — per request.
// The client timeout sits just above the per-request context deadline so the
// context still wins the race and callers get a 504 rather than a bare
// transport error.
func UpstreamClient(requestTimeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 100
	transport.IdleConnTimeout = 90 * time.Second

	return &http.Client{
		Transport: transport,
		Timeout:   requestTimeout + 5*time.Second,
	}
}

// Pool opens the connection pool for the job queue.
//
// The queue is polled, so the pool is kept small and the connections short
// lived: a managed Postgres caps how many a project may hold, and a worker that
// hoards them starves the rest of the system. Point DATABASE_URL at the
// transaction pooler rather than the direct port for the same reason.
func Pool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	options, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing database url: %w", err)
	}
	// A transaction-mode pooler hands each statement a different backend, so a
	// prepared statement cached against one connection is not there on the next
	// — which surfaces as "prepared statement already exists" long after the
	// cause. Exec mode skips the cache. The queue runs a handful of statements
	// per second, so what it costs is not measurable, and it is the difference
	// between working and not behind pgbouncer.
	options.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec

	options.MaxConns = int32(max(4, cfg.WorkerConcurrency+2))
	options.MinConns = 1
	options.MaxConnIdleTime = time.Minute
	options.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("connecting to the database: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging the database: %w", err)
	}
	return pool, nil
}
