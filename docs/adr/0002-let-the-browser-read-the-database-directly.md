# 0002. Let the browser read the database directly

- **Status:** accepted
- **Date:** 2026-09-18 (recorded retroactively; decided during the initial build)

## Context

Every analysis, CV record and profile row belongs to exactly one user. The
obvious shape is a REST API in front of the database that checks ownership on
each call — which means writing, testing and maintaining a CRUD layer whose only
job is to re-implement `where user_id = me`.

Supabase enforces that check in Postgres instead, through row level security, and
publishes a REST interface the browser can call with the user's own JWT. The
publishable key is public by design; authorisation does not depend on it.

## Decision

The frontend talks to Supabase directly for all per-user CRUD and file storage.
The gateway handles only the operations that genuinely need a server: the model
call, with its secret, rate limiting and upload validation.

A hand-crafted request cannot reach another user's rows, because the policy lives
in the database rather than in client code.

## Consequences

No glue layer to write or keep in sync, one place where authorisation is defined,
and one fewer network hop on every read.

The costs: the table and column names are visible to the client, and the frontend
is coupled to the Supabase client API. Multi-step transactions and any rule that
must not be skippable have nowhere to live — there is no server in that path.

Revisit when the schema needs hiding, when a write spans several tables and must
be atomic, or when server-side rules stop fitting in a policy expression. Moving
CRUD behind the gateway is a contained change; the RLS policies stay as a second
layer either way.
