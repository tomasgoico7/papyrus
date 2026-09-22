package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/papyrus/gateway/internal/ratelimit"
)

func newShared(t *testing.T, server *miniredis.Miniredis, perMinute int) *ratelimit.Shared {
	t.Helper()
	limiter, err := ratelimit.NewShared("redis://"+server.Addr(), perMinute, time.Second)
	if err != nil {
		t.Fatalf("new shared limiter: %v", err)
	}
	t.Cleanup(func() { _ = limiter.Close() })
	return limiter
}

func allowedOf(t *testing.T, limiter ratelimit.Limiter, key string, times int) int {
	t.Helper()
	ctx := context.Background()

	allowed := 0
	for range times {
		ok, err := limiter.Allow(ctx, key)
		if err != nil {
			t.Fatalf("allow: %v", err)
		}
		if ok {
			allowed++
		}
	}
	return allowed
}

func TestSharedAllowsTheBurstAndThenRefuses(t *testing.T) {
	server := miniredis.RunT(t)
	limiter := newShared(t, server, 5)

	if got := allowedOf(t, limiter, "u1", 8); got != 5 {
		t.Errorf("allowed %d of 8, want the budget of 5", got)
	}
}

func TestSharedKeepsCallersApart(t *testing.T) {
	server := miniredis.RunT(t)
	limiter := newShared(t, server, 3)

	allowedOf(t, limiter, "u1", 3)

	// Spending one caller's budget must not touch anyone else's.
	if got := allowedOf(t, limiter, "u2", 3); got != 3 {
		t.Errorf("second caller allowed %d of 3, want a budget of its own", got)
	}
}

func TestSharedRefillsOverTime(t *testing.T) {
	server := miniredis.RunT(t)
	// The script asks Redis for the time, so the server's clock is the one that
	// has to move — advancing TTLs alone would not refill anything.
	start := time.Now()
	server.SetTime(start)

	limiter := newShared(t, server, 60) // one per second

	if got := allowedOf(t, limiter, "u1", 60); got != 60 {
		t.Fatalf("allowed %d of 60 on a full bucket", got)
	}
	if got := allowedOf(t, limiter, "u1", 1); got != 0 {
		t.Fatalf("the bucket should be empty")
	}

	server.SetTime(start.Add(3 * time.Second))

	if got := allowedOf(t, limiter, "u1", 5); got != 3 {
		t.Errorf("allowed %d after three seconds at one per second, want 3", got)
	}
}

// TestTwoReplicasShareOneBudget is the reason this package exists. Two limiters
// are two gateway processes: separately they would each hand out the full
// allowance, and the caller would get twice what the configuration says.
func TestTwoReplicasShareOneBudget(t *testing.T) {
	server := miniredis.RunT(t)

	const budget = 10
	replicaA := newShared(t, server, budget)
	replicaB := newShared(t, server, budget)

	ctx := context.Background()
	allowed := 0
	// Alternating between them, the way a load balancer would.
	for i := range budget * 2 {
		limiter := ratelimit.Limiter(replicaA)
		if i%2 == 1 {
			limiter = replicaB
		}
		ok, err := limiter.Allow(ctx, "u1")
		if err != nil {
			t.Fatalf("allow: %v", err)
		}
		if ok {
			allowed++
		}
	}

	if allowed != budget {
		t.Errorf("two replicas allowed %d requests, want the shared budget of %d", allowed, budget)
	}
}

func TestSharedReportsAnOutageRatherThanGuessing(t *testing.T) {
	server := miniredis.RunT(t)
	limiter := newShared(t, server, 5)
	server.Close()

	// Not "denied": the limiter could not decide, and saying no would make an
	// unreachable Redis into a refused request.
	if _, err := limiter.Allow(context.Background(), "u1"); err == nil {
		t.Error("expected an error when the instance is unreachable")
	}
}

func TestSharedRejectsAMalformedURL(t *testing.T) {
	if _, err := ratelimit.NewShared("not-a-url", 5, time.Second); err == nil {
		t.Error("expected a malformed connection url to be rejected at construction")
	}
}
