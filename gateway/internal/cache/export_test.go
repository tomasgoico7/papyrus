package cache

import "time"

// SetClock replaces an LRU's time source so expiry can be exercised without
// sleeping. This file is only compiled for tests, so the knob never reaches the
// package's real surface.
func SetClock(c *LRU, now func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}
