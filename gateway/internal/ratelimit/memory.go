package ratelimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Memory limits per process. It is the whole story on a single instance, and
// the fallback when the shared limiter cannot answer — at which point the budget
// stops being global and becomes per replica, which is a weaker promise but a
// far better one than no limit at all.
type Memory struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    rate.Limit
	burst    int
	ttl      time.Duration
}

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewMemory(requestsPerMinute int) *Memory {
	if requestsPerMinute < 1 {
		requestsPerMinute = 1
	}

	m := &Memory{
		visitors: make(map[string]*visitor),
		limit:    rate.Limit(float64(requestsPerMinute) / 60.0),
		burst:    requestsPerMinute,
		ttl:      10 * time.Minute,
	}
	go m.evictLoop()
	return m
}

func (m *Memory) Allow(_ context.Context, key string) (bool, error) {
	return m.limiterFor(key).Allow(), nil
}

func (m *Memory) limiterFor(key string) *rate.Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()

	v, ok := m.visitors[key]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(m.limit, m.burst)}
		m.visitors[key] = v
	}
	v.lastSeen = time.Now()
	return v.limiter
}

// evictLoop keeps the map from growing with every caller ever seen.
func (m *Memory) evictLoop() {
	ticker := time.NewTicker(m.ttl)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		for key, v := range m.visitors {
			if time.Since(v.lastSeen) > m.ttl {
				delete(m.visitors, key)
			}
		}
		m.mu.Unlock()
	}
}
