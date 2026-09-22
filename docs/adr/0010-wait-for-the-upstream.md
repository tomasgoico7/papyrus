# 0010. Wait for the upstream instead of retrying blind

- **Status:** accepted
- **Date:** 2026-09-22

## Context

Analyses were failing in production with `upstream_rate_limited` after three
attempts spanning roughly 72 seconds, while the same analysis run against an
awake AI service succeeded on the first attempt in 52 seconds. The failures
correlated exactly with the service being parked by the platform for idleness.

The retry policy was already tuned for this: a throttled upstream got a much
longer delay than an ordinary failure, on the theory that three attempts over
90 to 135 seconds would outlast a 20 to 50 second wake. It did not work, and the
reason is that the retries were not waking anything. Probing the service directly
while it was asleep returned 429 from the edge, repeatedly and instantly, with no
sign the request had reached the platform at all — while a plain `GET /health`
answered in 0.26 seconds and brought the instance back.

The distinguishing feature appears to be the token. An analysis call carries one;
a health check does not. Whatever the mechanism, more attempts on the request path
could not fix it: the worker was asking a question that never arrived.

## Decision

**On a throttled failure, the worker waits for the upstream to report itself
ready before scheduling the retry.** It polls `GET /health` every five seconds
for up to a minute, and when the service answers it retries two seconds later
rather than after the throttle delay.

**The probe is unauthenticated on purpose.** A readiness check that can be refused
on credentials tells you nothing about readiness — and here it would also fail to
do the one thing the probe is for.

**Only throttling triggers the wait.** An ordinary upstream fault does not mean
the far side is coming back, and polling one that is genuinely broken spends a
minute per attempt achieving nothing. `IsThrottled` already distinguishes the two
for the error code; this reuses that judgement rather than inventing a second one.

**A budget that runs out falls back to the old behaviour.** If the minute passes
with no answer, the job takes the long throttle delay. The wait is an
optimisation over the previous policy, not a replacement for it, so an upstream
that stays down degrades to what happened before rather than to something new.

**Waiting is bounded and cancellable.** The probe loop honours the worker's
context, so a shutdown during a wait is immediate rather than up to a minute late.

## Consequences

A job whose upstream is merely asleep now completes on the second attempt, a few
seconds after the service is actually able to serve it, instead of burning three
attempts and landing in the dead letter queue. The queue depth matters more than
the latency here: a job that fails is a person who has to ask again.

A worker slot is held for up to a minute while waiting. With the default
concurrency of two this is real but acceptable — the alternative was holding it
for 135 seconds across three doomed attempts.

The wait is visible: `upstream came back` carries how long it took, which turns
the cold start from an inference into a measurement.

This encodes a platform-specific fact — that authenticated requests to a parked
free instance are refused without waking it — in the shape of the retry policy.
It is written down here because it is not derivable from the code, and it is the
kind of thing that quietly stops being true.

Revisit if the service stops being parked at all. On a plan without idle
suspension the wait never triggers, but it also stops earning its complexity.
