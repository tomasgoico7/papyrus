package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is a shared cache tier: entries written by one replica are visible to
// the others, which an in-process cache cannot do.
type Redis struct {
	client  *redis.Client
	timeout time.Duration
}

// NewRedis builds a client from a connection URL, so the same setting covers a
// local container (redis://…) and a managed instance over TLS (rediss://…).
//
// The timeout applies per operation and deliberately ignores how much budget the
// request still has. A cache that makes a slow answer slower is worse than no
// cache: if the lookup cannot beat the upstream it is not worth waiting for.
func NewRedis(url string, timeout time.Duration) (*Redis, error) {
	options, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("cache: parsing redis url: %w", err)
	}
	options.DialTimeout = timeout
	options.ReadTimeout = timeout
	options.WriteTimeout = timeout

	return &Redis{client: redis.NewClient(options), timeout: timeout}, nil
}

func (r *Redis) Get(ctx context.Context, key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	value, err := r.client.Get(ctx, key).Bytes()
	switch {
	case errors.Is(err, redis.Nil):
		return nil, ErrMiss
	case err != nil:
		return nil, fmt.Errorf("cache: redis get: %w", err)
	default:
		return value, nil
	}
}

func (r *Redis) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if err := r.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("cache: redis set: %w", err)
	}
	return nil
}

// Ping reports whether the instance is reachable. It is used once at startup to
// say so in the logs — never to decide whether the gateway may serve traffic.
func (r *Redis) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.client.Ping(ctx).Err()
}

func (r *Redis) Close() error {
	return r.client.Close()
}
