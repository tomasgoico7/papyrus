package middleware

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"

	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/requestid"
)

// RequestID gives every request an identifier, echoes it back to the caller and
// puts it — together with a logger already carrying it — on the request context,
// so the gateway's own logs and the AI service's can be joined on one key.
//
// An inbound identifier is reused when it survives sanitising, which lets a
// caller correlate from their side; anything suspicious is replaced rather than
// trusted. Failing that the trace is the identifier: the two are the same width
// for exactly this reason, and one number that finds both the logs and the trace
// is worth more than two that each find half the story.
//
// This has to run after whatever starts the server span, or there is no trace to
// borrow from yet and every request falls back to a random identifier.
func RequestID(base *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		span := trace.SpanContextFromContext(ctx)

		id := requestid.Sanitize(c.GetHeader(requestid.Header))
		switch {
		case id != "":
		case span.IsValid():
			id = span.TraceID().String()
		default:
			id = requestid.New()
		}

		logger := base.With(slog.String("request_id", id))
		if span.IsValid() {
			// Logged even when it is the same string as the request id: a log
			// aggregator's link to the tracing backend keys off the field name,
			// not off what the value happens to look like.
			logger = logger.With(
				slog.String("trace_id", span.TraceID().String()),
				slog.String("span_id", span.SpanID().String()),
			)
		}

		ctx = requestid.NewContext(ctx, id)
		ctx = observability.ContextWithLogger(ctx, logger)
		c.Request = c.Request.WithContext(ctx)

		c.Header(requestid.Header, id)
		c.Next()
	}
}
