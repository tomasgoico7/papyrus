# 0010. Wait for the upstream instead of retrying blind

- **Status:** accepted, amended 2026-09-24
- **Date:** 2026-09-22

> Two of the factual claims below turned out to be wrong, and the numbers have
> changed. The decision stands; see [the update](#update-2026-09-24) at the end
> before relying on the reasoning.

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
ready before scheduling the retry.** It issues `GET /health` and holds the
request open until the service answers, for up to 100 seconds in total, and when
it does answer the retry is scheduled two seconds later rather than after the
throttle delay.

**Holding the request open is the mechanism, not an implementation detail.** A
scale-to-zero platform starts an instance because a request is waiting for it;
hanging up says nobody is, and the start is abandoned. Short probes on a timer
are therefore not a gentler version of this — they are a version that cannot
work. The first deployment used a 30 second probe timeout against a cold start
measured at 31, and every wait it performed reset a start it had just triggered.
The timeout is 90 seconds now, and the budget bounds the probe rather than only
the gaps between probes.

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

A worker slot is held for up to 100 seconds while waiting. With the default
concurrency of two this is real but acceptable — the alternative was holding it
for 135 seconds across three doomed attempts.

The numbers are tied to a measurement that can drift. A cold start that grows
past 90 seconds silently returns the system to the old behaviour: the wait
expires, the job takes the long backoff, and nothing announces that the
mechanism stopped working. `upstream did not come back` in the log is the signal
to re-measure rather than to assume the upstream is down.

The wait is visible: `upstream came back` carries how long it took, which turns
the cold start from an inference into a measurement.

This encodes a platform-specific fact — that authenticated requests to a parked
free instance are refused without waking it — in the shape of the retry policy.
It is written down here because it is not derivable from the code, and it is the
kind of thing that quietly stops being true.

Revisit if the service stops being parked at all. On a plan without idle
suspension the wait never triggers, but it also stops earning its complexity.

## Update, 2026-09-24

Tracing, once it reached production, showed two things this record states as
fact were not.

**Hanging up a probe does not abandon the start it triggered.** A probe cut off
by the old hundred second budget was followed by another that was answered
fifty three seconds later — far short of a fresh three minute start. Whatever
the platform does when a request is dropped mid-start, it is not resetting it.
Holding the probe open is still worth doing, because one long probe replaces
several short ones and does not report "not ready" for a service seconds away
from answering; it is just not the mechanism it was described as.

**An authenticated request is not refused at the edge.** The version fetch
carries the internal token, and a trace showed it waking a sleeping instance in
twenty three seconds. The token was never visible to the platform in the first
place. Why the analysis call specifically was answered 429 during a start is
still unknown, and is not explained by anything here.

The numbers moved with what was measured. The slowest cold start seen took
three minutes, so the budget went from a hundred seconds to four minutes. And
the gap between probes now doubles from five seconds to a cap of thirty: a
probe that fails at once is being refused, and a fixed short gap turned each
refusal into dozens more from the same address.
