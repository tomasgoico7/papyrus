# 0011. Carry one trace across the queue

- **Status:** accepted
- **Date:** 2026-09-22

## Context

Every request already carries a correlation id ([0006](0006-correlate-and-measure-every-request.md)),
which is enough to find the log lines belonging to one analysis across three
services. It is not enough to say where the time went. An analysis takes tens of
seconds; the logs say it started and finished, and the interval in between is a
single number with no structure.

The week this was written, six separate problems were diagnosed by reading
timestamps out of the job table and inferring what the worker must have been
doing between them. Every one of those inferences was a guess, and two of them
were wrong.

The queue makes it worse rather than better. The request that enqueues an
analysis finishes in under a second; the work happens later, in another process,
possibly after two retries spread over two minutes. Those are four separate
things in the logs with nothing but a shared id to connect them, and no
indication of how long the job spent waiting versus running.

## Decision

**OpenTelemetry, exported over OTLP, in both services.** The instrumentation is
standard and the backend is not part of the decision: Tempo locally, a hosted
one in production, the same protocol either way.

**The job row carries the W3C `traceparent`.** This is the part that matters. A
queue is a gap in a trace — the producer ends before the consumer starts, in a
different process, with nothing to inherit. Storing the header on the row and
resuming from it on claim is what makes one analysis one trace, including the
waiting, which becomes a visible interval rather than an absence.

**The worker's span is a child of the enqueueing span, not a link.** The
messaging conventions suggest a link when a consumer may run far later than its
producer, and that argument applies here. A child was chosen anyway because the
question being asked is "where did the fifty-five seconds go", and a parent-child
waterfall answers it directly while a link asks the reader to open two traces and
compare them. The cost is a trace whose span is minutes wide, which every backend
handles and which is a fair description of what happened. Retries land in the
same trace for the same reason: three attempts over two minutes are one story.

**The trace id becomes the correlation id when there is no inbound one.** The
two were always the same width — `requestid.New` was written to match a trace id
against the day this would be done. One identifier that finds both the logs and
the trace is worth more than two that each find half. An id supplied by the
caller still wins, because they are correlating on it from their side.

**Sampling is parent-based everywhere.** Once a trace is being recorded, every
service on it records. Sampling independently produces traces with holes, which
are worse than no trace because they look complete. The sampling decision
crosses the queue with the header.

**Tracing off is the same code path with a sampler that records nothing.** Not a
branch around the instrumentation. A second configuration that only runs when
nobody is looking is a configuration nobody tests.

## Consequences

An analysis is one waterfall: the request, the queue wait, each attempt, the
model call. The thing that was inferred from timestamps all week is now read
off directly.

The queue table grows a column, and jobs written before it existed have none.
That case is not an error — the worker starts a trace of its own — because a
missing trace must never stop an analysis from running.

`tracestate` is dropped. Only `traceparent` is stored, so a vendor-specific
sampling hint from upstream does not survive the queue. Nothing in this system
sets one, and storing two columns to preserve something unused is not worth it.

A worker slot now holds a span open for the length of an attempt, which on this
service is up to two minutes. That is what a long operation looks like; it is
not a leak, but it does mean the batch exporter can hold a trace for a while
before flushing it.

Both services depend on the OTel SDK, which is a substantial tree. It earns the
weight only if the traces are actually read — if they are not, this is overhead
with a dashboard attached.

Revisit the child-span choice if traces start being produced at a rate where
long-open traces cost real money at the backend, or if batching ever means one
consumer handles many jobs — at which point a link is the only honest shape.
