package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/papyrus/gateway/internal/httpx"
)

type RateLimiter struct {
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

func NewRateLimiter(requestsPerMinute int) *RateLimiter {
	if requestsPerMinute < 1 {
		requestsPerMinute = 1
	}

	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		limit:    rate.Limit(float64(requestsPerMinute) / 60.0),
		burst:    requestsPerMinute,
		ttl:      10 * time.Minute,
	}
	go rl.evictLoop()
	return rl
}

func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetString(ContextUserID)
		if key == "" {
			key = c.ClientIP()
		}

		if !rl.limiterFor(key).Allow() {
			httpx.RespondError(c, http.StatusTooManyRequests, "rate_limited", "Too many requests. Please wait a moment and try again.")
			return
		}

		c.Next()
	}
}

func (rl *RateLimiter) limiterFor(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, ok := rl.visitors[key]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rl.limit, rl.burst)}
		rl.visitors[key] = v
	}
	v.lastSeen = time.Now()
	return v.limiter
}

func (rl *RateLimiter) evictLoop() {
	ticker := time.NewTicker(rl.ttl)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		for key, v := range rl.visitors {
			if time.Since(v.lastSeen) > rl.ttl {
				delete(rl.visitors, key)
			}
		}
		rl.mu.Unlock()
	}
}
