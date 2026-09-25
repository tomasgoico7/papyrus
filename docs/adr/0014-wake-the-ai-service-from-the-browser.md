# 0014. Wake the AI service from the browser

- **Status:** accepted
- **Date:** 2026-09-25

## Context

The AI service runs on a free instance that the platform parks after about
fifteen idle minutes. For a week, analyses that arrived while it was parked
failed with 429, and every explanation offered for why turned out wrong when
the next measurement came in ([0010](0010-wait-for-the-upstream.md) has the
record of two of them).

With tracing in production ([0011](0011-carry-one-trace-across-the-queue.md)),
the failure became visible directly: the gateway's requests to a parked AI
service were refused by the platform's edge in under a hundred milliseconds,
and never produced a span on the AI service's side. They did not reach it, and
they did not wake it. The worker's readiness probes spent four minutes asking
without the service ever starting.

Then a single request from a laptop woke it in thirty one seconds. Repeated
from the same laptop with the gateway's own HTTP client — same library, same
transport, same HTTP/2 — it woke it again in thirty one seconds. The protocol
and the client were ruled out. What differed was where the request came from.

"Where from" bundles several things that cannot be told apart from outside the
platform: a shared outbound address, the edge location a request lands on, the
platform's internal routing. None of that changes the conclusion. A request
from outside the platform wakes the service; one from the gateway does not.

## Decision

**The browser wakes the service**, with a request straight to its health
endpoint, when someone opens the workspace and again when they submit.
Uploading a CV and pasting an offer takes about as long as the service takes to
start, so by the time the analysis is queued it is usually up.

**The worker's probes stay, with a different job.** From the gateway they wake
nothing, but they still see the moment the service answers, so a job queued
slightly ahead of the wake is retried seconds after it lands rather than after
a backoff sized for the worst case.

**The request is opaque and cannot fail the page.** It goes out as `no-cors`,
since nothing reads the response and the service need not allow the app's
origin, and its promise resolves whether it lands or not. A wake that fails
costs a slower analysis, not a failed one.

**One wake per ten minutes.** The service stays up for fifteen after a request,
so asking again inside that window changes nothing.

Rejected: **a scheduled pinger.** It would hold the service up around the clock,
and the free allowance is instance hours shared between services — two of them
awake all month spend about twice what it provides. The services get suspended
instead. A wake tied to someone using the app spends hours only when there is a
reason to.

Rejected: **having the gateway wake it.** That is precisely what does not work.

## Consequences

An analysis submitted into a cold service now has the start-up underway before
it arrives, instead of failing three attempts against an edge that will not let
the gateway through.

The AI service's address is now known to the browser. It was already public —
the platform serves it on a public hostname — and the one endpoint the browser
calls needs no token and returns nothing of interest.

Someone who opens the workspace and leaves wakes the service for fifteen minutes
for nothing. At this traffic that is a handful of instance hours a month, far
under what a pinger would spend.

This is a workaround for how one platform treats one tier. It encodes something
observed, not documented, and could stop being true without notice. If analyses
into a cold service start failing again, the first thing to check is whether a
browser request still wakes it — the runbook has the command.

Revisit if the AI service moves to an instance that is not parked. The wake then
does nothing, and can go.
