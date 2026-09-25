# Architecture decision records

One file per decision that shaped the system, in the order they were taken.

A record explains the situation that forced a choice, the choice itself, and what
it cost — so a reader six months from now can tell a deliberate trade-off from an
accident, and knows what would have to change for the answer to be different.

Records are append-only. A decision that no longer holds is not edited or
deleted: a new record supersedes it, and the old one is marked accordingly. The
reasoning that turned out to be wrong is the part worth keeping.

These are in English only. The README is bilingual because it is the front door;
duplicating the depth here would double the maintenance for no reader.

## The log

| # | Decision | Status |
|---|---|---|
| [0001](0001-split-the-backend-into-three-services.md) | Split the backend into three services | Accepted |
| [0002](0002-let-the-browser-read-the-database-directly.md) | Let the browser read the database directly | Accepted |
| [0003](0003-verify-access-tokens-against-jwks.md) | Verify access tokens against JWKS | Accepted |
| [0004](0004-produce-both-languages-in-one-model-call.md) | Produce both languages in one model call | Accepted |
| [0005](0005-keep-deterministic-work-out-of-the-model.md) | Keep deterministic work out of the model | Accepted |
| [0006](0006-correlate-and-measure-every-request.md) | Correlate and measure every request | Accepted |
| [0007](0007-cache-analyses-in-two-tiers.md) | Cache analyses in two tiers | Accepted |
| [0008](0008-queue-analyses-in-postgres.md) | Queue analyses in Postgres | Accepted |
| [0009](0009-share-the-rate-limit-budget.md) | Share the rate limit budget | Accepted |
| [0010](0010-wait-for-the-upstream.md) | Wait for the upstream instead of retrying blind | Accepted |
| [0011](0011-carry-one-trace-across-the-queue.md) | Carry one trace across the queue | Accepted |
| [0012](0012-gate-the-queue-on-the-schema-version.md) | Gate the queue on the schema version | Accepted |
| [0013](0013-hold-the-queue-plans-in-tests.md) | Hold the queue's query plans in tests | Accepted |

Records 0001 to 0005 were written after the fact, from decisions already visible
in the code. Everything from 0006 on is recorded as it is taken.

## Adding one

Copy [`0000-template.md`](0000-template.md), take the next number, and add a row
above. Keep it short: if a record needs more than a page, the decision probably
hides more than one.
