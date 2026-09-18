package router

import (
	"context"
	"log/slog"
	"net/http"
	"time"

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
func buildAnalyzer(
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

	store, localTTL := buildStore(cfg, logger)
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

func buildStore(cfg *config.Config, logger *slog.Logger) (cache.Store, time.Duration) {
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
