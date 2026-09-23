package observability

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
)

// traceParentKey is the W3C header name, which is also what the queue column
// stores. Using the standard name rather than one of our own means the value
// can be handed to any propagator, ours or somebody else's.
const traceParentKey = "traceparent"

// queueCarrier is the W3C trace context propagator, named explicitly rather
// than taken from the global.
//
// The global is a composite that also carries baggage, and which service
// startup happens to have installed. Neither belongs here: the column holds one
// specific thing, defined by one specific specification, and reading it should
// not depend on what some other part of the process configured. A worker that
// silently stops continuing traces because startup changed is the kind of thing
// nobody notices for months.
var queueCarrier = propagation.TraceContext{}

// TraceParentFrom serialises ctx's trace so it can be stored and picked up by a
// different process later.
//
// A queue is a gap in a trace: the request that enqueues work ends in under a
// second, and whatever runs the work starts fresh with nothing to inherit.
// Carrying the header across the gap is what makes the two halves one trace.
//
// Returns empty when there is no trace to carry, which is the ordinary state
// with tracing switched off.
func TraceParentFrom(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	queueCarrier.Inject(ctx, carrier)
	return carrier.Get(traceParentKey)
}

// ContextWithTraceParent returns ctx carrying the trace named by a stored
// header, for use as the parent of whatever happens next.
//
// An unparseable or empty value yields ctx unchanged rather than an error: a
// job enqueued before the column existed, or by a process with tracing off, is
// a job like any other and must still run.
func ContextWithTraceParent(ctx context.Context, traceParent string) context.Context {
	if traceParent == "" {
		return ctx
	}
	return queueCarrier.Extract(ctx, propagation.MapCarrier{traceParentKey: traceParent})
}
