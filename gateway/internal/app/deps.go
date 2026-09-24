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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/papyrus/gateway/internal/cache"
	"github.com/papyrus/gateway/internal/config"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/ratelimit"
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
	// Longer than a cold start, measured at 31 seconds, and not by a little: the
	// probe is supposed to wait out the start rather than sample it. A timeout
	// under the start time reports "not ready" forever — and worse, hanging up
	// tells the platform nobody is waiting, so the start it triggered is
	// abandoned and the next probe begins again from nothing.
	readinessTimeout = 90 * time.Second
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

// UpstreamBudget is the longest any caller of the AI service may legitimately
// need, which is what the shared client has to be sized for.
//
// One client serves three callers with three different deadlines: a browser
// waiting on a synchronous request, a queued job with nobody waiting on a
// socket, and a readiness probe waiting out a cold start. The client's own
// timeout applies to all of them, so sizing it for the shortest silently caps
// the other two — which is what happened: the job timeout was raised to two
// minutes and the probe to ninety seconds while the client kept cutting every
// call at sixty-five, making both numbers configuration that did nothing.
//
// Sizing it for the longest is safe because each caller still bounds itself
// with a context, and the earlier deadline is the one that fires. The client
// timeout is a backstop against a connection that hangs forever, not a policy.
func UpstreamBudget(cfg *config.Config) time.Duration {
	return max(cfg.RequestTimeout, cfg.JobTimeout, readinessTimeout)
}

// UpstreamClient is shared by every client of the AI service so they pool
// connections to the single upstream instead of keeping a pool each.
//
// The default transport caps idle connections per host at 2, which under any
// concurrency forces a fresh dial — and a fresh TLS handshake — per request.
// The timeout should come from UpstreamBudget: it has to clear the longest
// deadline any caller sets, or it becomes the real limit and theirs are
// decoration.
func UpstreamClient(requestTimeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 100
	transport.IdleConnTimeout = 90 * time.Second

	return &http.Client{
		// Wrapped at the transport rather than at each call site, so a request
		// cannot be made through this client without carrying the trace. This
		// is also what puts the traceparent header on the wire: the AI service
		// is a separate process, and without it the model call shows up as time
		// the gateway spent doing nothing.
		Transport: otelhttp.NewTransport(transport, otelhttp.WithSpanNameFormatter(upstreamSpanName)),
		Timeout:   requestTimeout + 5*time.Second,
	}
}

// upstreamSpanName names an outbound span by the path it calls. The default is
// just the method, so a trace showed "HTTP GET" taking twenty three seconds and
// left the reader to guess which of the AI service's endpoints it was. Naming by
// path is safe here in a way it would not be for inbound traffic: the gateway
// only ever calls a handful of fixed routes, so there is no id in the path to
// turn every call into its own operation.
func upstreamSpanName(_ string, r *http.Request) string {
	return r.Method + " " + r.URL.Path
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

// RateLimiter builds the request budget.
//
// With a Redis URL the budget is shared by every replica; without one it is per
// process, which is the whole truth on a single instance. Either way a Redis
// that stops answering degrades to the local limiter rather than failing
// requests — see docs/adr/0009.
func RateLimiter(
	cfg *config.Config,
	metrics *observability.Metrics,
	logger *slog.Logger,
) ratelimit.Limiter {
	local := ratelimit.NewMemory(cfg.RateLimitRPM)

	if cfg.RedisURL == "" {
		logger.Info("rate limiting is per process", slog.Int("rpm", cfg.RateLimitRPM))
		metrics.SetRateLimitShared(false)
		return ratelimit.NewFallback(nil, local, metrics, logger)
	}

	shared, err := ratelimit.NewShared(cfg.RedisURL, cfg.RateLimitRPM, redisTimeout)
	if err != nil {
		logger.Warn("rate limiting is per process: redis unavailable", slog.Any("error", err))
		metrics.SetRateLimitShared(false)
		return ratelimit.NewFallback(nil, local, metrics, logger)
	}

	ctx, cancel := context.WithTimeout(context.Background(), redisTimeout)
	defer cancel()
	if err := shared.Ping(ctx); err != nil {
		// Reported, not enforced: Redis may well come up after the gateway does,
		// and the limiter falls back until it does.
		logger.Warn("rate limiting redis did not answer at startup", slog.Any("error", err))
	}

	logger.Info("rate limiting is shared across replicas", slog.Int("rpm", cfg.RateLimitRPM))
	metrics.SetRateLimitShared(true)
	return ratelimit.NewFallback(shared, local, metrics, logger)
}

// Readiness probes the AI service, for the worker to tell "asleep" from "broken".
//
// The client must be one built for UpstreamBudget. A client sized for the
// request path cuts the probe short of its own timeout, and the probe then
// reports "not ready" for a service that was merely still starting.
func Readiness(cfg *config.Config, upstream *http.Client) *services.HealthClient {
	return services.NewHealthClient(cfg.AIServiceURL, upstream, readinessTimeout)
}
