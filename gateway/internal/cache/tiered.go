package cache

import (
	"context"
	"time"
)

// Tiered puts a fast local cache in front of a shared remote one.
//
// The split is not decoration. A managed Redis on a free tier sits tens of
// milliseconds away, which is most of the latency caching was meant to remove;
// the local tier absorbs the repeats, and the remote one carries what a single
// process cannot know — entries written by another replica, and entries that
// have to survive a restart.
type Tiered struct {
	fast   Store
	shared Store
	// fastTTL bounds how long a local copy may disagree with the shared one.
	// It is normally shorter than the entry's own lifetime.
	fastTTL time.Duration
}

func NewTiered(fast, shared Store, fastTTL time.Duration) *Tiered {
	return &Tiered{fast: fast, shared: shared, fastTTL: fastTTL}
}

// Get tries the local tier, then the shared one, promoting whatever it finds so
// the next hit stays local. A failure in either tier reads as a miss: the caller
// asks the upstream, which is the answer it would have produced anyway.
func (t *Tiered) Get(ctx context.Context, key string) ([]byte, error) {
	if value, err := t.fast.Get(ctx, key); err == nil {
		return value, nil
	}

	value, err := t.shared.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	_ = t.fast.Set(ctx, key, value, t.fastTTL)
	return value, nil
}

// Set writes both tiers. The shared error is the one returned, because that is
// the write another replica depends on; a local failure only costs this process
// a future hit.
func (t *Tiered) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	_ = t.fast.Set(ctx, key, value, min(ttl, t.fastTTL))
	return t.shared.Set(ctx, key, value, ttl)
}
