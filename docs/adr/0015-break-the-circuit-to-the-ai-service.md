# 0015. Break the circuit to the AI service

- **Status:** accepted
- **Date:** 2026-09-27

## Context

When the AI service stops answering — parked by the platform, redeploying, or
down with its model provider — every caller finds out separately. A synchronous
request waits out the failure before it is told; the next one waits it out
again. The worker spends a job's attempts, one after another, on calls that the
last few callers have already shown will fail. Each of those calls is also load
on an upstream already in trouble.

The readiness probe from [0010](0010-wait-for-the-upstream.md) helps one job
wait intelligently. It does nothing for the request path, and nothing to stop a
second job repeating what the first just learned.

## Decision

**One circuit breaker per process, in front of every call that does work against
the AI service** — analyses from both the request path and the worker, the
tailor, and the version fetch. It is shared, so what one caller learns every
caller acts on.

**It opens after five consecutive failures.** Throttling, server errors,
connection failures and timeouts count. A client error does not: a rejected
token or an unreadable upload is the caller's problem, and an upstream that says
so clearly is working. A cancelled request counts for nothing, because nobody
waited for the answer. Five, because a single analysis into a sleeping upstream
produces two or three failures on its own, and that is a cold start rather than
an outage.

**Open, it refuses without calling.** The request path answers at once with 503
and a `Retry-After` of the cooldown left, instead of making another person wait
out the same failure. The worker stops claiming jobs, the same way it does while
the schema is behind ([0012](0012-gate-the-queue-on-the-schema-version.md)), so
queued work waits instead of spending attempts on calls that would be refused.
The queue keeps accepting jobs throughout: absorbing work while a dependency
recovers is what a queue is for.

**After a cooldown, one trial.** Thirty seconds — about a cold start — and then
exactly one call goes through. Success closes the breaker; failure reopens it for
twice as long, up to five minutes. An upstream that is properly down is asked
less and less often; one that is back is found within a few minutes at worst.

**The readiness probe does not go through it.** The probe is how the gateway
notices the upstream is back. A probe the breaker refused could never notice.

**It sits inside the tracing.** A refused call still produces a span, short and
marked with the refusal, so an open breaker is visible in a trace rather than
showing up as calls that never happened.

## Consequences

An outage is found out once rather than by every caller in turn. People on the
request path get an immediate, honest answer with a time to come back, and jobs
keep their attempts for calls that have a chance.

Queued work no longer spends its attempts in parallel against an outage that is
already known. While the breaker is open nothing is claimed; during a trial,
only one job is — the oldest due — and the trial is that job's call, so it
spends one of its attempts. A long outage still turns jobs into dead letters,
but one at a time and at the pace of the trials, which slow down as the outage
goes on, rather than all of them together. A job that survives until the
upstream is back may run after its submitter's browser has stopped polling; its
result is still cached, so asking again is instant.

Making the trial a health check instead of a job's call would spare that
attempt, and was not done: a health check answering says the service is up, not
that analyses work, and a trial that cannot tell those apart closes the breaker
over a broken model provider.

A burst of genuine failures from something other than the upstream — a bug in
how requests are built, say — would open the breaker too. The failures would be
server errors from the AI service, which is correct to count, but the fix would
be in the gateway. `upstream_breaker_transitions_total` and the log line on each
transition make that visible rather than silent.

The thresholds are fixed in code, next to their reasoning, not configuration.
They were chosen against measured cold starts, and changing one is a deploy.
