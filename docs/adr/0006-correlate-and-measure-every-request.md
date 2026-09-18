# 0006. Correlate and measure every request

- **Status:** accepted
- **Date:** 2026-09-18

## Context

Three services and no way to connect them. A failed analysis produced a line in
the gateway log and, if anything, an unrelated traceback in the AI service, with
nothing linking the two beyond a rough timestamp.

There was also no latency number anywhere. Any later claim that a cache or a
queue improved things would have had nothing to compare against, and "it feels
faster" is not a result.

## Decision

**One identifier per request.** The gateway stamps `X-Request-ID`, forwards it to
the AI service, echoes it to the caller, and carries it on every log line in both
services. An inbound value is reused only if it is short and alphanumeric —
otherwise it is replaced, because a header containing a newline can forge log
entries. The identifier is 128 bits, matching the width of a trace id so the two
can be reconciled when tracing arrives.

**Structured logs.** JSON in production, readable text in development. On the
Python side structlog and the standard library share one renderer, so a traceback
from the model provider arrives correlated rather than as loose text.

**RED metrics on both services**, exposed at `/metrics` from a registry owned by
the service rather than a process global, so tests can assert on an isolated
instance.

Two constraints on the metrics, both enforced by tests:

- The `route` label is always the registered template, never the request path.
  Labelling by path lets anything walking URLs create a time series per request.
  Unregistered paths collapse into `unmatched`.
- Histogram buckets extend to 60 seconds. Library defaults stop at 10, which
  would put every real analysis in the overflow bucket and make the p95
  meaningless.

## Consequences

A failure is followed across all three services with one `grep`. Latency,
throughput and error rate are observable, and there is a recorded baseline for
later work to be measured against.

`/metrics` is served on the public port. It carries no user-identifying labels —
which is what the cardinality rule buys — but a serious deployment would move it
to a separate internal port.

The costs: a Prometheus client dependency in each service, a middleware on every
request, and a discipline that has to be maintained. Every new label is a
cardinality decision, and the cheapest way to lose a monitoring stack is to add
one that holds a user id.
