package ratelimit_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/papyrus/gateway/internal/ratelimit"
)

type stubLimiter struct {
	allow bool
	err   error
	calls atomic.Int64
}

func (s *stubLimiter) Allow(context.Context, string) (bool, error) {
	s.calls.Add(1)
	return s.allow, s.err
}

type recorder struct {
	mu       sync.Mutex
	outcomes []string
}

func (r *recorder) RecordRateLimit(outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outcomes = append(r.outcomes, outcome)
}

func (r *recorder) count(outcome string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, o := range r.outcomes {
		if o == outcome {
			n++
		}
	}
	return n
}

func newFallback(shared, local ratelimit.Limiter) (*ratelimit.Fallback, *recorder) {
	rec := &recorder{}
	return ratelimit.NewFallback(shared, local, rec, slog.New(slog.NewTextHandler(io.Discard, nil))), rec
}

func TestFallbackPrefersTheSharedLimiter(t *testing.T) {
	shared := &stubLimiter{allow: false}
	local := &stubLimiter{allow: true}
	limiter, rec := newFallback(shared, local)

	allowed, err := limiter.Allow(context.Background(), "u1")
	if err != nil {
		t.Fatalf("allow: %v", err)
	}

	if allowed {
		t.Error("the shared limiter said no; the local one does not get a vote")
	}
	if local.calls.Load() != 0 {
		t.Error("the local limiter should not have been consulted")
	}
	if rec.count(ratelimit.OutcomeLimited) != 1 {
		t.Errorf("outcomes = %v, want one limited", rec.outcomes)
	}
}

func TestFallbackKeepsServingWhenTheSharedLimiterIsDown(t *testing.T) {
	shared := &stubLimiter{err: errors.New("redis is down")}
	local := &stubLimiter{allow: true}
	limiter, rec := newFallback(shared, local)

	allowed, err := limiter.Allow(context.Background(), "u1")

	// Failing closed would turn a Redis blip into a total outage: the limiter
	// becomes the thing that takes the service down.
	if err != nil {
		t.Fatalf("a limiter that cannot decide must not fail the request: %v", err)
	}
	if !allowed {
		t.Error("the local limiter allowed it; the answer should stand")
	}
	if rec.count(ratelimit.OutcomeDegraded) != 1 {
		t.Errorf("outcomes = %v, want the degraded mode recorded", rec.outcomes)
	}
}

func TestFallbackStillEnforcesALimitWhileDegraded(t *testing.T) {
	// Degraded is not "no limit". The budget stops being global and becomes per
	// replica, which is a weaker promise — not an abandoned one.
	shared := &stubLimiter{err: errors.New("redis is down")}
	limiter, _ := newFallback(shared, ratelimit.NewMemory(3))

	allowed := 0
	for range 6 {
		ok, _ := limiter.Allow(context.Background(), "u1")
		if ok {
			allowed++
		}
	}

	if allowed != 3 {
		t.Errorf("allowed %d of 6 while degraded, want the local budget of 3", allowed)
	}
}

func TestFallbackStopsHammeringAnUnhealthySharedLimiter(t *testing.T) {
	shared := &stubLimiter{err: errors.New("redis is down")}
	local := &stubLimiter{allow: true}
	limiter, _ := newFallback(shared, local)

	for range 20 {
		_, _ = limiter.Allow(context.Background(), "u1")
	}

	// Retrying an unhealthy Redis on every request adds load to a system already
	// in trouble, and each attempt costs the caller a timeout.
	if got := shared.calls.Load(); got != 1 {
		t.Errorf("the shared limiter was called %d times during the cooldown, want 1", got)
	}
	if local.calls.Load() != 20 {
		t.Errorf("the local limiter answered %d of 20", local.calls.Load())
	}
}

func TestFallbackWorksWithNoSharedLimiterAtAll(t *testing.T) {
	// A deployment with no REDIS_URL is not degraded; it is a single instance
	// doing exactly what it can.
	local := &stubLimiter{allow: true}
	limiter, _ := newFallback(nil, local)

	if _, err := limiter.Allow(context.Background(), "u1"); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if local.calls.Load() != 1 {
		t.Error("the local limiter should have answered")
	}
}
