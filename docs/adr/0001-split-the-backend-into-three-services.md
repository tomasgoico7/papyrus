# 0001. Split the backend into three services

- **Status:** accepted
- **Date:** 2026-09-18 (recorded retroactively; decided during the initial build)

## Context

Papyrus does three things that have almost nothing in common: it renders a UI and
owns the user's data, it calls a language model, and it guards the one operation
that needs a secret. Calling Gemini requires an API key that cannot ship to a
browser, so at least one server-side component was unavoidable.

The smallest thing that works is a single Next.js app with two route handlers.
That is genuinely less to run, less to deploy and less to reason about, and for
the traffic this project sees it would be enough.

The counter-argument is that the three jobs fail and scale differently. The UI is
bound by page loads, the model call by provider quota and tens of seconds of
latency, the edge by request volume. Collapsing them means a slow model call
occupies a process that is also serving pages.

## Decision

Three services, deployed separately: a Next.js frontend, a Go gateway, and a
Python AI service. The gateway never touches the database; the AI service holds
no state.

## Consequences

Each service does one thing, and each can be reasoned about, tested and replaced
on its own. The AI service is a pure function from `(CV, posting)` to an
analysis, which makes it trivial to fake in tests.

The cost is real and worth stating plainly: three deployments, three sets of
environment variables, three cold starts on a free tier, and network latency
between components that used to be function calls. For the current traffic this
is overhead bought on purpose.

The split is also justified by the weakest of the three reasons — a secret. The
stronger justification arrives with [0006](0006-correlate-and-measure-every-request.md)
and the work that follows it: once analysis moves to a queue, the API tier scales
with user traffic and the worker tier with model throughput, which is a real
difference in load curves rather than a packaging preference.

Revisit if the deployment overhead starts costing more than the isolation buys —
for a product with a budget, starting as one service and splitting later is the
better order.
