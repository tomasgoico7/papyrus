package handlers

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/breaker"
	"github.com/papyrus/gateway/internal/httpx"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/services"
)

// respondUpstream turns a failure from the AI service into the gateway's error
// envelope. Every branch logs: a 4xx used to pass straight through to the caller
// with nothing written on our side, which left throttling invisible in the
// gateway's own logs.
//
// The operation names which call failed, so one log query covers both endpoints.
func respondUpstream(c *gin.Context, err error, operation string) {
	logger := observability.LoggerFrom(c.Request.Context()).With(slog.String("operation", operation))

	var upstream *services.UpstreamError
	if errors.As(err, &upstream) {
		if upstream.IsThrottled() {
			// Capacity, not a bad request. It gets its own code so the client can
			// tell "wait and retry" apart from "fix your input", and the upstream
			// hint is passed along rather than guessed at.
			if upstream.RetryAfter != "" {
				c.Header("Retry-After", upstream.RetryAfter)
			}
			logger.Warn("upstream throttled the request",
				slog.String("retry_after", upstream.RetryAfter),
			)
			httpx.RespondError(c, http.StatusTooManyRequests, "upstream_rate_limited",
				"The service is busy right now. Please try again in a moment.")
			return
		}

		if upstream.IsClientError() {
			logger.Warn("upstream rejected the request",
				slog.Int("status", upstream.StatusCode),
				slog.String("code", upstream.Code),
			)
			httpx.RespondError(c, upstream.StatusCode, upstream.Code, upstream.Message)
			return
		}
	}

	// Checked before the timeout, because a refused call is not a slow one: it
	// was never made. The breaker already knows the upstream is down, so the
	// honest answer is to say so at once and when to come back, instead of
	// making this caller wait out the same failure the last few did.
	var open *breaker.OpenError
	if errors.As(err, &open) {
		retry := max(int(math.Ceil(open.RetryAfter.Seconds())), 1)
		c.Header("Retry-After", strconv.Itoa(retry))
		logger.Warn("upstream call refused by the breaker", slog.Int("retry_after", retry))
		httpx.RespondError(c, http.StatusServiceUnavailable, "upstream_unavailable",
			"The service is temporarily unavailable. Please try again shortly.")
		return
	}

	if errors.Is(err, context.DeadlineExceeded) {
		logger.Warn("upstream timed out")
		httpx.RespondError(c, http.StatusGatewayTimeout, "upstream_timeout",
			"The request took too long. Please try again.")
		return
	}

	logger.Error("upstream failure", slog.Any("error", err))
	httpx.RespondError(c, http.StatusBadGateway, "upstream_unavailable",
		"The service is temporarily unavailable.")
}
