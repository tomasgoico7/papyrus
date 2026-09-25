# 0013. Hold the queue's query plans in tests

- **Status:** accepted
- **Date:** 2026-09-25

## Context

The queue lives in Postgres ([0008](0008-queue-analyses-in-postgres.md)), and
its efficiency rested on comments. The migration that created it said the claim
was served by a partial index; nothing checked.

Explaining every statement the worker runs, against a hundred thousand rows in
steady state — the shape the table settles into once the retention sweep has
taken what expired — found three things:

| Query | How often | Plan | Time |
|---|---|---|---|
| depth gauge | every 15 s | whole table | 16.6 ms |
| retention sweep | every 2 min | whole table, to delete nothing | 15.5 ms |
| claim | every poll | two thousand live rows, sorted | 1.6 ms |

The first two read everything to act on almost nothing. The third was fast by
accident. The index built for claiming covered queued jobs only, and the claim
had since grown a second branch that takes back running jobs whose worker died,
which that index could not serve. The dedup index happened to cover both
branches, so it served the claim instead, and the index built for the claim was
written on every change and read by nothing. Widening the dedup index for a
reason of its own — measured — sent the claim to reading all hundred thousand
rows, seven times slower.

## Decision

**Index for the queries that actually run.** An index over the finished rows, by
state and then by age, serves the sweep and the dead letter count. An index over
both branches of the claim, in the order the claim reads, lets it stop at the
first row it can take. The unused claim index is dropped. The depth gauge is
rewritten in two parts so each matches an index.

**Test the property, not the index.** A test seeds a hundred thousand rows,
explains each statement, and asserts that none of them scans the table and that
a claim reads the live jobs rather than the history. It names no index, so a
reasonable schema change does not break it — only one that sends a query back to
reading everything does.

## Consequences

| Query | Before | After |
|---|---|---|
| claim | 2,002 rows, 1.6 ms | 3 rows, 0.2 ms |
| depth gauge | 100,000 rows, 16.6 ms | 8,000 rows, 2.3 ms |
| retention sweep | 100,000 rows, 15.5 ms | none, 0.08 ms |

Each statement's cost now follows the work in front of it rather than the
history behind it, and a change that undoes that fails CI with the plan printed.

At current traffic the table holds a few dozen rows, and none of this is
measurable in production. That is not a reason to leave it: the point is that
the queue's cost is set by its design and proven by a test, rather than holding
because nobody has used it much yet.

Plans depend on statistics, so the seed has to look like a real table — mostly
finished work, a thin live layer, everything inside its retention window. A seed
spread over a month made the sweep look three times more expensive than it is,
because it was deleting a month of history in one go.

Each plan test seeds its own hundred thousand rows, a few seconds apiece. That
cost buys a check that would otherwise not exist at all.
