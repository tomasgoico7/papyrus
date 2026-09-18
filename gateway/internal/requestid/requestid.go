// Package requestid carries one identifier across every service that handles a
// single request, so their logs can be joined after the fact.
package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// Header is the wire name shared by the browser, the gateway and the AI service.
const Header = "X-Request-ID"

// maxLength bounds an inbound identifier. It ends up in log lines, so an
// unbounded caller-supplied value is a log-bloat vector.
const maxLength = 64

type contextKey struct{}

// New returns a random 128-bit identifier, hex encoded. The width matches an
// OpenTelemetry trace id so the two can be reconciled later.
func New() string {
	var buf [16]byte
	// crypto/rand.Read is documented never to return an error.
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// Sanitize returns raw when it is safe to propagate, and the empty string
// otherwise. An identifier arrives from the caller, so it is only accepted when
// it is short and alphanumeric: anything else could inject newlines into a log
// line or smuggle control characters downstream.
func Sanitize(raw string) string {
	if raw == "" || len(raw) > maxLength {
		return ""
	}
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
		default:
			return ""
		}
	}
	return raw
}

// NewContext returns a copy of ctx carrying id.
func NewContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext returns the identifier carried by ctx, or the empty string when
// the request did not pass through the middleware that sets it.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}
