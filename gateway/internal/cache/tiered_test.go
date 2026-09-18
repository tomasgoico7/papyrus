package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/papyrus/gateway/internal/cache"
)

// failingStore stands in for a tier that is down.
type failingStore struct {
	gets int
	sets int
}

func (s *failingStore) Get(context.Context, string) ([]byte, error) {
	s.gets++
	return nil, errors.New("tier unavailable")
}

func (s *failingStore) Set(context.Context, string, []byte, time.Duration) error {
	s.sets++
	return errors.New("tier unavailable")
}

func TestTieredPromotesASharedHitToTheLocalTier(t *testing.T) {
	fast := cache.NewLRU(4)
	shared := cache.NewLRU(4)
	tiered := cache.NewTiered(fast, shared, time.Minute)
	ctx := context.Background()

	// Only the shared tier knows the entry, as if another replica wrote it.
	_ = shared.Set(ctx, "k", []byte("value"), time.Hour)

	got, err := tiered.Get(ctx, "k")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "value" {
		t.Errorf("got %q, want value", got)
	}

	// The next hit must not need the shared tier again.
	if _, err := fast.Get(ctx, "k"); err != nil {
		t.Errorf("the shared hit was not promoted locally: %v", err)
	}
}

func TestTieredServesFromTheLocalTierWhenTheSharedOneIsDown(t *testing.T) {
	fast := cache.NewLRU(4)
	shared := &failingStore{}
	tiered := cache.NewTiered(fast, shared, time.Minute)
	ctx := context.Background()

	_ = fast.Set(ctx, "k", []byte("value"), time.Minute)

	got, err := tiered.Get(ctx, "k")
	if err != nil {
		t.Fatalf("a local hit should not depend on the shared tier: %v", err)
	}
	if string(got) != "value" {
		t.Errorf("got %q, want value", got)
	}
	if shared.gets != 0 {
		t.Error("the shared tier should not be consulted after a local hit")
	}
}

func TestTieredReportsAMissWhenBothTiersAreEmpty(t *testing.T) {
	tiered := cache.NewTiered(cache.NewLRU(4), cache.NewLRU(4), time.Minute)

	if _, err := tiered.Get(context.Background(), "nope"); !errors.Is(err, cache.ErrMiss) {
		t.Errorf("error = %v, want ErrMiss", err)
	}
}

func TestTieredStillWritesLocallyWhenTheSharedTierFails(t *testing.T) {
	fast := cache.NewLRU(4)
	shared := &failingStore{}
	tiered := cache.NewTiered(fast, shared, time.Minute)
	ctx := context.Background()

	if err := tiered.Set(ctx, "k", []byte("value"), time.Hour); err == nil {
		t.Error("a failed shared write should be reported to the caller")
	}

	// Reported, but not wasted: this process can still answer from its own copy.
	if _, err := fast.Get(ctx, "k"); err != nil {
		t.Errorf("the local tier should have been written anyway: %v", err)
	}
}

func TestTieredCapsTheLocalLifetime(t *testing.T) {
	fast := cache.NewLRU(4)
	shared := cache.NewLRU(4)
	tiered := cache.NewTiered(fast, shared, 30*time.Second)
	ctx := context.Background()

	clock := time.Now()
	cache.SetClock(fast, func() time.Time { return clock })

	// A long shared lifetime must not give the local copy the same licence to
	// go stale.
	_ = tiered.Set(ctx, "k", []byte("value"), time.Hour)
	clock = clock.Add(31 * time.Second)

	if _, err := fast.Get(ctx, "k"); !errors.Is(err, cache.ErrMiss) {
		t.Error("the local copy should have expired on the shorter ttl")
	}
	if _, err := tiered.Get(ctx, "k"); err != nil {
		t.Errorf("the shared entry should still serve it: %v", err)
	}
}
