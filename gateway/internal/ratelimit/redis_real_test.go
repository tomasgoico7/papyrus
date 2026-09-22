package ratelimit_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/papyrus/gateway/internal/ratelimit"
)

// miniredis reimplements Lua rather than embedding it, so a script that behaves
// there is not proof it behaves on the real thing — and this script leans on
// redis.call('TIME'), which is exactly the sort of detail a reimplementation
// gets approximately right. These run against actual Redis.
func realRedisURL(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	container, err := tcredis.Run(ctx, "redis:7.4-alpine")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("starting redis: %v", err)
		}
		t.Skipf("skipping, no docker: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	url, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	return url
}

func TestAgainstRealRedisTheBudgetIsSharedAndAtomic(t *testing.T) {
	url := realRedisURL(t)

	const budget = 20
	replicaA, err := ratelimit.NewShared(url, budget, 2*time.Second)
	if err != nil {
		t.Fatalf("replica a: %v", err)
	}
	defer replicaA.Close()

	replicaB, err := ratelimit.NewShared(url, budget, 2*time.Second)
	if err != nil {
		t.Fatalf("replica b: %v", err)
	}
	defer replicaB.Close()

	// Both hammering the same caller at once. Without the read, the refill and
	// the spend happening inside one script, two replicas can each see the same
	// remaining tokens and both spend them.
	type result struct {
		allowed bool
		err     error
	}
	results := make(chan result, budget*4)
	for i := range budget * 4 {
		limiter := ratelimit.Limiter(replicaA)
		if i%2 == 1 {
			limiter = replicaB
		}
		go func() {
			ok, err := limiter.Allow(context.Background(), "burst")
			results <- result{ok, err}
		}()
	}

	allowed := 0
	for range budget * 4 {
		r := <-results
		if r.err != nil {
			t.Fatalf("allow: %v", r.err)
		}
		if r.allowed {
			allowed++
		}
	}

	if allowed != budget {
		t.Errorf("allowed %d of %d concurrent requests, want exactly the shared budget of %d",
			allowed, budget*4, budget)
	}
}

func TestAgainstRealRedisTheClockComesFromTheServer(t *testing.T) {
	url := realRedisURL(t)

	limiter, err := ratelimit.NewShared(url, 120, 2*time.Second) // two per second
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer limiter.Close()

	ctx := context.Background()
	for range 120 {
		if _, err := limiter.Allow(ctx, "u1"); err != nil {
			t.Fatalf("draining: %v", err)
		}
	}
	if ok, _ := limiter.Allow(ctx, "u1"); ok {
		t.Fatal("the bucket should be empty")
	}

	// Real time, not a stubbed clock: the script reads Redis's own, so this is
	// the only way to see the refill actually work there.
	time.Sleep(1100 * time.Millisecond)

	refilled := 0
	for range 4 {
		if ok, _ := limiter.Allow(ctx, "u1"); ok {
			refilled++
		}
	}
	if refilled < 1 || refilled > 3 {
		t.Errorf("refilled %d tokens in ~1s at two per second; want 1 to 3", refilled)
	}
	fmt.Fprintf(os.Stderr, "refilled %d tokens after one second\n", refilled)
}
