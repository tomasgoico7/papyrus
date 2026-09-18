package middleware

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/requestid"
)

// RequestID gives every request an identifier, echoes it back to the caller and
// puts it — together with a logger already carrying it — on the request context,
// so the gateway's own logs and the AI service's can be joined on one key.
//
// An inbound identifier is reused when it survives sanitising, which lets a
// caller correlate from their side; anything suspicious is replaced rather than
// trusted.
func RequestID(base *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := requestid.Sanitize(c.GetHeader(requestid.Header))
		if id == "" {
			id = requestid.New()
		}

		ctx := requestid.NewContext(c.Request.Context(), id)
		ctx = observability.ContextWithLogger(ctx, base.With(slog.String("request_id", id)))
		c.Request = c.Request.WithContext(ctx)

		c.Header(requestid.Header, id)
		c.Next()
	}
}
