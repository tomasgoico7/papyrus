package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/papyrus/gateway/internal/cache"
)

// newRedis starts an in-process Redis so the suite stays offline and free, the
// same rule the rest of the project follows.
func newRedis(t *testing.T) (*cache.Redis, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)
	store, err := cache.NewRedis("redis://"+server.Addr(), time.Second)
	if err != nil {
		t.Fatalf("new redis: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, server
}

func TestRedisStoresAndReturnsAValue(t *testing.T) {
	store, _ := newRedis(t)
	ctx := context.Background()

	if err := store.Set(ctx, "k", []byte("value"), time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}

	got, err := store.Get(ctx, "k")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "value" {
		t.Errorf("got %q, want value", got)
	}
}

func TestRedisReportsAMissForAnAbsentKey(t *testing.T) {
	store, _ := newRedis(t)

	if _, err := store.Get(context.Background(), "nope"); !errors.Is(err, cache.ErrMiss) {
		t.Errorf("error = %v, want ErrMiss", err)
	}
}

func TestRedisHonoursTheTTL(t *testing.T) {
	store, server := newRedis(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k", []byte("value"), 30*time.Second)
	server.FastForward(31 * time.Second)

	if _, err := store.Get(ctx, "k"); !errors.Is(err, cache.ErrMiss) {
		t.Errorf("error = %v, want ErrMiss once the ttl has passed", err)
	}
}

func TestRedisIgnoresANonPositiveTTL(t *testing.T) {
	store, _ := newRedis(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k", []byte("value"), 0)

	if _, err := store.Get(ctx, "k"); !errors.Is(err, cache.ErrMiss) {
		t.Error("an entry with no lifetime should never be stored")
	}
}

func TestRedisSurfacesAnOutageAsAnError(t *testing.T) {
	store, server := newRedis(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k", []byte("value"), time.Minute)
	server.Close()

	// Not ErrMiss: an outage is not the same as an absent key, and the caller
	// decides what to do with the difference.
	_, err := store.Get(ctx, "k")
	if err == nil {
		t.Fatal("expected an error once the instance is unreachable")
	}
	if errors.Is(err, cache.ErrMiss) {
		t.Error("an outage must not be reported as a miss")
	}
}

func TestRedisRejectsAMalformedURL(t *testing.T) {
	if _, err := cache.NewRedis("not-a-url", time.Second); err == nil {
		t.Fatal("expected a malformed connection url to be rejected at construction")
	}
}
