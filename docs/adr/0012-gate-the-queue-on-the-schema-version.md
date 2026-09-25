# 0012. Gate the queue on the schema version

- **Status:** accepted
- **Date:** 2026-09-25

## Context

Migrations are applied by hand, pasted into the Supabase SQL editor. The code
that depends on them deploys on its own, the moment it is pushed. Nothing tied
the two together.

Migration 0007 added a column that the next release wrote to on every enqueue.
It was applied before that release went out only because the commits happened to
be sitting on an unpushed branch. Pushed first, every queued analysis would have
failed on `column "traceparent" does not exist` until somebody noticed, with
nothing in the gateway to say that a migration was the cause.

## Decision

**The database records which migrations it has had.** A `schema_migrations`
table, created by 0008, with one row per migration. Every migration from 0008 on
ends by inserting its own version.

**The code declares the version it was written against.** `schema.RequiredVersion`
is held equal to the newest migration file by a test, and another test fails any
migration that does not record itself. The convention is only as good as its
enforcement, and both halves of it fail CI when forgotten, with a message that
says exactly what to add.

**Behind means the queue stands aside, not that the service fails.** Every thirty
seconds the gateway reads the applied version. While it is short of the required
one, the queued routes answer 404 and the worker claims nothing. The 404 is
deliberate: it is what a gateway with no queue says, and the client already
falls back to the synchronous endpoint on exactly that. A release ahead of its
migration costs people the queue, not their analysis.

**An outage is not evidence about the schema.** A check that cannot reach the
database leaves the gate as it was. Closing it on a blip would lose queued work
for nothing.

**The first check happens before serving.** A gate that has not looked yet is
closed, a closed gate answers 404, and a client that gets one stops trying the
queue for the rest of its session.

Rejected: **running the migrations from the gateway on startup.** The migrations
live outside the gateway's module and reach into schemas Supabase owns. Several
replicas starting together would need to agree on who migrates, and the obvious
tool for that, a session advisory lock, does not survive the transaction pooler
the gateway connects through. And a migration that fails would then take the
service down at boot, instead of failing in an editor where a person is watching.

Rejected: **refusing to start when behind.** On a platform that restarts crashed
processes, that is a crash loop, and it takes the synchronous path down with the
queue.

## Consequences

Pushing ahead of a migration is safe, and still not free: the queue is off until
the migration lands, and the synchronous fallback makes a person wait on a model
that may be asleep. The runbook says to migrate first.

The gate notices a migration within thirty seconds and opens without a restart.
A browser tab that already fell back keeps using the synchronous endpoint until
it is reloaded.

It keeps checking after it opens. A database restored from an older backup goes
backwards, and the gate closes again.

Every migration now carries one line of bookkeeping at the end. That is the whole
cost of the convention, and the test that enforces it prints the line to paste.
