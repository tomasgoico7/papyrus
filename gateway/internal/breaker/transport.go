package breaker

import (
	"context"
	"errors"
	"net/http"
)

// Transport refuses requests while its breaker is open, and tells the breaker
// what each response said about the upstream.
//
// It sits at the transport rather than at each call site for the same reason
// the tracing does: nothing that talks to the upstream through this client can
// forget to consult it, and a request that is refused never leaves the process.
type Transport struct {
	Base    http.RoundTripper
	Breaker *Breaker
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	record, err := t.Breaker.Allow()
	if err != nil {
		return nil, err
	}

	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	record(Classify(resp, err))
	return resp, err
}

// Classify reads a round trip as evidence about the upstream.
//
// Throttling and server errors count against it: it is not serving. A client
// error does not — a rejected token or an unreadable upload is the caller's
// problem, and an upstream that says so clearly is working. A request the
// caller cancelled counts for nothing, because no answer was waited for. One
// that timed out counts against the upstream: it did not answer in time.
func Classify(resp *http.Response, err error) Outcome {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return Ignored
		}
		return Failure
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return Failure
	}
	return Success
}
