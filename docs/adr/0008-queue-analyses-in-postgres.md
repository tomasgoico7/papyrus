# 0008. Queue analyses in Postgres

- **Status:** accepted
- **Date:** 2026-09-21

## Context

An analysis takes tens of seconds. Measured end to end against the deployed
stack, a cold one took **31.5 seconds** — a browser holding a connection open
that whole time, with nothing to show for it if the tab is closed, the network
blips, or the upstream fails on the last attempt.

That latency also forces the two tiers to scale together. The part that serves
pages is bound by user traffic; the part that calls the model is bound by
provider quota and by how long the provider takes. Collapsed into one
synchronous request they share a process, and a burst of analyses starves
everything else in it.

The work itself is a good fit for a queue: it is slow, it is retryable, and
[0005](0005-keep-deterministic-work-out-of-the-model.md) already made it
reproducible.

A broker — NATS, RabbitMQ, Kafka — is the obvious answer and the wrong one at
this size. It is another service to run, pay for and keep alive on a free tier,
to buy throughput that is not needed and durability that Postgres already
provides.

## Decision

**The queue is a Postgres table**, claimed with `select … for update skip
locked`. A worker takes the oldest due row and marks it running in one
statement; a row another worker already holds is skipped rather than waited on,
so workers never serialise behind each other.

**The store does storage, the worker does policy.** Whether a failure deserves
another attempt, and how long to back off, is not the table's business — it
exposes `Retry(runAfter)` and `Fail`, and the worker decides which.

**Delivery is at-least-once.** A claim older than a staleness window is treated
as due again, on the assumption that the worker holding it is dead. A job can
therefore run twice, which is acceptable precisely because the work is
reproducible and the result is idempotent to write.

**Duplicate submissions join the work in flight.** A unique index over
`dedup_key`, partial to the live states, means the same inputs cannot queue
twice — and the key is the cache key from
[0007](0007-cache-analyses-in-two-tiers.md), so the two mechanisms agree on what
"the same analysis" means. Once a job reaches a terminal state it stops holding
the key and the analysis can be asked for again.

**The upload lives in the row**, as `bytea`. The alternative — a second storage
system, or Redis as a required dependency rather than an optional one — buys
scalability this does not need.

A successful job drops its bytes immediately: the caller has the result and the
cache holds it. A failed one keeps them until the retention sweep takes the whole
row, because a dead letter nobody can re-run is not much of a dead letter queue.
Failures are rare enough that the space is not the constraint.

**Finished jobs are swept on a timer.** Nothing else removes them, and a row per
analysis kept forever is a slow leak on a database measured in hundreds of
megabytes. Successes go after a day, dead letters after a week.

**The table has RLS enabled and no policies.** Unlike `analyses` and `cvs`, it
is not part of the client's data plane ([0002](0002-let-the-browser-read-the-database-directly.md)):
the browser reaches it only through the gateway. No policy means PostgREST
cannot read it as `anon` or `authenticated`, so a stray request cannot reach
another user's CV bytes. The gateway connects with a role that bypasses RLS and
scopes every read by the user id in the verified token — in the `where` clause,
not in Go, because a filter that lives in the caller is one somebody eventually
forgets.

## Consequences

The gateway stops being stateless with respect to the database, which
contradicts the rule stated in [0001](0001-split-the-backend-into-three-services.md).
That is the deliberate part of this change: the gateway becomes the owner of the
job lifecycle rather than a proxy, and it is also what finally justifies the
three-service split on load curves instead of on a secret.

The client gets more complicated. A `202` and a job id mean a state machine,
polling, and a visible "processing" step that did not exist before. The user
experience gets worse in the short term in exchange for never losing an analysis
to a closed tab.

Tests need a real Postgres. `skip locked` has no meaningful fake, so the store
is tested with testcontainers against the actual migration — which pulls part of
the planned data-and-integration work forward, and means the suite needs Docker.

Postgres as a queue has a ceiling in the low thousands of jobs per second, far
above anything this will see. Revisit when a single table is contended enough to
show up in the claim latency, or when a second consumer needs the same events —
a queue has one reader per message, and fanning out is where a broker starts
earning its keep.
