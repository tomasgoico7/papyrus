# 0009. Share the rate limit budget

- **Status:** accepted
- **Date:** 2026-09-22

## Context

The rate limiter kept its buckets in a map in the gateway's memory. That works
for exactly one process, and it was the one thing stopping the gateway from
being replicated: two instances would each hand out the full allowance, so a
caller configured for twenty requests a minute would get forty. The limit would
still exist, but it would no longer mean what it says.

It also reset on every deploy. A restart handed everyone a fresh budget, which
on a platform that redeploys on every push is often enough to matter.

The state is small, short-lived and already shared with something: Redis is
there for the analysis cache ([0007](0007-cache-analyses-in-two-tiers.md)).

## Decision

**A token bucket, in Redis, as one Lua script.** The shape matches what was
there before — steady refill with room for a burst, which is what a person
clicking a button looks like. Running it as a script is what makes it correct:
the read, the refill and the spend have to be a single step, or two replicas
both see four tokens left, both spend one, and both write three.

**The clock is Redis's own**, read with `TIME` inside the script rather than
sent by the caller. Replicas do not agree on what time it is, and a bucket
refilled against a fast clock hands out tokens that were never earned.

**A Redis that cannot answer degrades to the in-process limiter.** Both
alternatives are worse. Failing closed turns a Redis blip into a total outage —
the limiter becomes the thing that takes the service down. Failing open drops
the limit at exactly the moment something is already wrong, which is when it
matters most. Falling back keeps a real ceiling: with N replicas the effective
budget becomes N times the intended one, which is a weaker promise rather than
an abandoned one.

**A failure starts a short cooldown** during which the shared limiter is not
consulted at all. Retrying an unhealthy Redis on every request adds load to a
system already in trouble, and each attempt costs a caller the timeout.

**The mode is a metric, not just a log line.** `rate_limit_decisions_total`
counts allowed, limited and degraded separately, because a limiter that has
quietly stopped being shared is otherwise invisible.

## Consequences

The gateway can run more than one instance without the limit losing its
meaning, which was the last thing in the way. The budget also survives a
deploy now, rather than resetting.

Every limited request costs a Redis round trip. On a managed instance that is
tens of milliseconds, which is why the timeout is short and the fallback
exists — a limiter that makes every request wait on a slow Redis has become the
outage it was meant to prevent.

Redis moves from optional to load-bearing in a new way: the cache degrades to a
local one silently and cheaply, and now so does the limiter, but with a
correctness cost that is worth watching rather than ignoring.

The script is tested against real Redis rather than an in-process fake. A
reimplementation of Lua will get `TIME` approximately right, and approximately
is not a useful standard for the thing that decides whether a request proceeds.

Revisit if the bucket stops matching the traffic — a sliding window costs more
memory but refuses more evenly, and matters once a burst allowance is being
gamed rather than used.
