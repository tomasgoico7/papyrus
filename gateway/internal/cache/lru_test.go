package cache_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/papyrus/gateway/internal/cache"
)

const minute = time.Minute

func TestLRUStoresAndReturnsAValue(t *testing.T) {
	c := cache.NewLRU(4)
	ctx := context.Background()

	if err := c.Set(ctx, "k", []byte("value"), minute); err != nil {
		t.Fatalf("set: %v", err)
	}

	got, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "value" {
		t.Errorf("got %q, want value", got)
	}
}

func TestLRUReportsAMissForAnAbsentKey(t *testing.T) {
	c := cache.NewLRU(4)

	if _, err := c.Get(context.Background(), "nope"); !errors.Is(err, cache.ErrMiss) {
		t.Errorf("error = %v, want ErrMiss", err)
	}
}

func TestLRUEvictsTheLeastRecentlyUsed(t *testing.T) {
	c := cache.NewLRU(2)
	ctx := context.Background()

	_ = c.Set(ctx, "a", []byte("1"), minute)
	_ = c.Set(ctx, "b", []byte("2"), minute)

	// Touching "a" must make "b" the eviction candidate, not "a".
	if _, err := c.Get(ctx, "a"); err != nil {
		t.Fatalf("get a: %v", err)
	}
	_ = c.Set(ctx, "c", []byte("3"), minute)

	if _, err := c.Get(ctx, "b"); !errors.Is(err, cache.ErrMiss) {
		t.Error("b was the least recently used and should have been evicted")
	}
	if _, err := c.Get(ctx, "a"); err != nil {
		t.Error("a was used most recently and should have survived")
	}
	if c.Len() != 2 {
		t.Errorf("len = %d, want the capacity to hold", c.Len())
	}
}

func TestLRUExpiresAnEntry(t *testing.T) {
	c := cache.NewLRU(4)
	ctx := context.Background()

	clock := time.Now()
	cache.SetClock(c, func() time.Time { return clock })

	_ = c.Set(ctx, "k", []byte("value"), 30*time.Second)
	if _, err := c.Get(ctx, "k"); err != nil {
		t.Fatalf("the entry should be live: %v", err)
	}

	clock = clock.Add(31 * time.Second)

	if _, err := c.Get(ctx, "k"); !errors.Is(err, cache.ErrMiss) {
		t.Errorf("error = %v, want ErrMiss once the ttl has passed", err)
	}
	if c.Len() != 0 {
		t.Errorf("len = %d, want the expired entry dropped on lookup", c.Len())
	}
}

func TestLRUOverwritesWithoutGrowing(t *testing.T) {
	c := cache.NewLRU(4)
	ctx := context.Background()

	_ = c.Set(ctx, "k", []byte("first"), minute)
	_ = c.Set(ctx, "k", []byte("second"), minute)

	got, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "second" {
		t.Errorf("got %q, want the later value", got)
	}
	if c.Len() != 1 {
		t.Errorf("len = %d, want 1", c.Len())
	}
}

func TestLRUIgnoresANonPositiveTTL(t *testing.T) {
	c := cache.NewLRU(4)
	ctx := context.Background()

	_ = c.Set(ctx, "k", []byte("value"), 0)

	if _, err := c.Get(ctx, "k"); !errors.Is(err, cache.ErrMiss) {
		t.Error("an entry with no lifetime should never be stored")
	}
}

func TestLRUReturnsACopyTheCallerCannotCorrupt(t *testing.T) {
	c := cache.NewLRU(4)
	ctx := context.Background()

	original := []byte("value")
	_ = c.Set(ctx, "k", original, minute)
	original[0] = 'V' // the caller reuses its buffer

	got, _ := c.Get(ctx, "k")
	if string(got) != "value" {
		t.Errorf("got %q; the stored value must not alias the caller's slice", got)
	}

	got[0] = 'X' // and a returned value must not alias the stored one
	again, _ := c.Get(ctx, "k")
	if string(again) != "value" {
		t.Errorf("got %q; a returned value must not alias the stored one", again)
	}
}

func TestLRUIsSafeUnderConcurrency(t *testing.T) {
	c := cache.NewLRU(64)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = c.Set(ctx, "shared", []byte("value"), minute)
		}()
		go func() {
			defer wg.Done()
			_, _ = c.Get(ctx, "shared")
			_ = c.Set(ctx, string(rune('a'+i%26)), []byte("v"), minute)
		}()
	}
	wg.Wait()

	if c.Len() > 64 {
		t.Errorf("len = %d, want the capacity respected under concurrency", c.Len())
	}
}
