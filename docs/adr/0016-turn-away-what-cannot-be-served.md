# 0016. Turn away what cannot be served

- **Status:** accepted
- **Date:** 2026-09-27

## Context

Nothing bounded how much work the gateway would take on. Two places made that a
real risk on the free tier it runs on, rather than a theoretical one.

The synchronous routes — `/analyze` and both tailor routes — hold a request open
for the length of a model call, and keep the whole upload in memory for all of
it: up to five megabytes each, on an instance with half a gigabyte. A burst held
as many uploads as arrived at once.

Every queued job keeps its upload in Postgres until it is claimed. The free
database is five hundred megabytes. A backlog of large uploads could fill it,
and past a point the work would not start until long after the person who asked
for it had stopped waiting anyway.

## Decision

**A ceiling on synchronous calls in flight: eight, shared across all three
routes.** They share an upstream and a memory budget, so they share a ceiling —
one per route would allow three times what was meant. Past it, a request is
turned away immediately with 503, code `overloaded`, and a `Retry-After` of five
seconds. It is not queued: the asynchronous endpoint already is the queue, and a
request waiting here would be waiting on the same model with a browser that has
its own limit on patience.

**A limit on waiting jobs: twenty.** At the upload cap that is a fifth of the
database, and about eight minutes of work at two analyses at a time — already
twice as long as a browser keeps polling. A new job past it is refused with 503,
code `queue_full`, and a `Retry-After` of a minute.

**Admission is part of the enqueue, not a count taken first.** The check sits in
the same statement as the insert, after the dedup conflict clause has had its
say. A second submit of work already in the queue joins it as it always has,
even when the queue is full — only new work is refused. Jobs a worker is already
on do not count; they are not backlog.

**Both have their own codes and messages.** "Busy, come back shortly" and
"something is broken" call for different words and different alerts. A test on
the gateway side fails if the client has no message for a code the gateway can
send, which is how both of these arrived with one.

## Consequences

Memory held for uploads on the synchronous path is bounded at forty megabytes,
and the database's exposure to a backlog at a hundred. Neither was bounded
before.

The queue limit is soft. The count is not locked, so a burst can overshoot it by
a few. It exists to stop an unbounded backlog, not to be exact, and locking the
table on every enqueue would cost more than the overshoot.

Someone can now be told "try again in a few minutes" where before their job
would have been accepted and run late. That is the trade: a clear answer now
instead of a result that arrives after they have left.

At current traffic neither limit is ever reached. `requests_shed_total`, by
reason, is what would show them starting to matter.
