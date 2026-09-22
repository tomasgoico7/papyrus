package ratelimit

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// Fallback asks the shared limiter, and falls back to a local one when it cannot
// answer.
//
// The alternative behaviours are both worse. Failing closed turns a Redis blip
// into a total outage — the limiter becomes the thing that takes the service
// down. Failing open drops the limit entirely at exactly the moment something is
// already going wrong, which is when it is most needed.
//
// Falling back to a per-process limiter keeps a real ceiling: N replicas means
// the effective budget is N times the intended one, which is a degraded promise
// rather than an abandoned one.
type Fallback struct {
	shared Limiter
	local  Limiter
	logger *slog.Logger

	// degraded tracks the current mode so the switch is logged once per
	// transition instead of once per request.
	degraded atomic.Bool
	// Failures within the cooldown skip the shared limiter entirely: hammering
	// an unhealthy Redis on every request adds load to a system already in
	// trouble, and each attempt costs a caller the timeout.
	cooldown time.Duration
	failedAt atomic.Int64
	metrics  Recorder
}

// Recorder counts what the limiter decided, so a degraded limiter is visible
// rather than silent.
type Recorder interface {
	RecordRateLimit(outcome string)
}

// Rate limit outcomes.
const (
	OutcomeAllowed  = "allowed"
	OutcomeLimited  = "limited"
	OutcomeDegraded = "degraded"
)

func NewFallback(shared, local Limiter, metrics Recorder, logger *slog.Logger) *Fallback {
	return &Fallback{
		shared:   shared,
		local:    local,
		logger:   logger,
		cooldown: 5 * time.Second,
		metrics:  metrics,
	}
}

func (f *Fallback) Allow(ctx context.Context, key string) (bool, error) {
	if f.shared != nil && !f.cooling() {
		allowed, err := f.shared.Allow(ctx, key)
		if err == nil {
			f.recovered()
			f.record(allowed)
			return allowed, nil
		}
		f.failed(err)
	}

	allowed, _ := f.local.Allow(ctx, key)
	f.metrics.RecordRateLimit(OutcomeDegraded)
	return allowed, nil
}

func (f *Fallback) record(allowed bool) {
	if allowed {
		f.metrics.RecordRateLimit(OutcomeAllowed)
		return
	}
	f.metrics.RecordRateLimit(OutcomeLimited)
}

func (f *Fallback) cooling() bool {
	at := f.failedAt.Load()
	return at != 0 && time.Since(time.Unix(0, at)) < f.cooldown
}

func (f *Fallback) failed(err error) {
	f.failedAt.Store(time.Now().UnixNano())
	if f.degraded.CompareAndSwap(false, true) {
		f.logger.Warn("rate limiting degraded to this process only", slog.Any("error", err))
	}
}

func (f *Fallback) recovered() {
	f.failedAt.Store(0)
	if f.degraded.CompareAndSwap(true, false) {
		f.logger.Info("rate limiting is shared again")
	}
}
