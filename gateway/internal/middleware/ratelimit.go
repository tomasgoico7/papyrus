package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/httpx"
	"github.com/papyrus/gateway/internal/ratelimit"
)

// RateLimit spends one unit of the caller's budget per request.
//
// The budget is per user where there is one, and per address otherwise: an
// unauthenticated caller has no identity to charge, and charging them all
// together would let one of them exhaust everybody's.
func RateLimit(limiter ratelimit.Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := httpx.UserID(c)
		if key == "" {
			key = c.ClientIP()
		}

		// The limiter never returns an error it expects the caller to act on: a
		// decision it could not make has already been answered by a fallback.
		allowed, _ := limiter.Allow(c.Request.Context(), key)
		if !allowed {
			httpx.RespondError(c, http.StatusTooManyRequests, "rate_limited",
				"Too many requests. Please wait a moment and try again.")
			return
		}

		c.Next()
	}
}
