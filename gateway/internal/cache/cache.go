// Package cache holds the gateway's caching layers: an in-process LRU, a Redis
// store, and a tier that puts one in front of the other.
//
// Every operation is best-effort by contract. A cache that is slow, full or down
// must never turn into a failed analysis — callers treat any error as a miss and
// go to the upstream, which is the answer they would have given anyway.
package cache

import (
	"context"
	"errors"
	"time"
)

// ErrMiss reports that a key is absent or expired. It is returned instead of a
// nil value so a caller cannot mistake "nothing cached" for "cached emptiness".
var ErrMiss = errors.New("cache: miss")

// Store is a byte-level key/value cache with per-entry expiry.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}
