package middleware

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/httpx"
	"github.com/papyrus/gateway/internal/observability"
)

// ShedRecorder counts requests turned away before any work was done on them.
type ShedRecorder interface {
	RecordShed(reason string)
}

// Limit lets at most n requests through at once, across every route it is
// attached to, and turns the rest away immediately.
//
// It exists for the routes that hold a request open while the model works. Each
// one keeps the whole upload in memory for the length of the call, and the
// instance this runs on has half a gigabyte. Without a ceiling, a burst holds
// as many uploads as arrive at once; with one, the worst case is n of them.
//
// Turning away is immediate rather than a queue of its own. A request that
// waited here would be waiting on the same model everyone else is, with a
// browser on the other end that has its own limit on patience — and the
// asynchronous endpoint already is the queue, with room for exactly this.
//
// One Limit is meant to be shared by every route it guards: the routes share an
// upstream and a memory budget, so they share a ceiling.
func Limit(n int, retryAfterSeconds int, shed ShedRecorder) gin.HandlerFunc {
	slots := make(chan struct{}, max(n, 1))
	retryAfter := strconv.Itoa(max(retryAfterSeconds, 1))

	return func(c *gin.Context) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			c.Next()
		default:
			if shed != nil {
				shed.RecordShed(observability.ShedConcurrency)
			}
			c.Header("Retry-After", retryAfter)
			httpx.RespondError(c, http.StatusServiceUnavailable, "overloaded",
				"The service is busy right now. Please try again in a moment.")
		}
	}
}
