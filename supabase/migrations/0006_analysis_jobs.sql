-- Analysis jobs: the queue behind the asynchronous analyze endpoint.
--
-- An analysis takes tens of seconds, which is too long to hold an HTTP request
-- open. The gateway enqueues here and answers immediately; a worker claims the
-- row, runs the analysis, and writes the result back for the client to collect.
--
-- The queue is Postgres rather than a broker: `for update skip locked` gives
-- exactly the semantics needed, and no new infrastructure has to be paid for or
-- kept alive.

create table if not exists public.analysis_jobs (
  id            uuid primary key default gen_random_uuid(),
  user_id       uuid not null references auth.users (id) on delete cascade,

  -- Same inputs, same job. This is the cache key from the gateway, reused so a
  -- double submit joins the work already queued instead of duplicating it.
  dedup_key     text not null,

  state         text not null default 'queued'
                  check (state in ('queued', 'running', 'done', 'failed')),
  attempts      integer not null default 0,
  max_attempts  integer not null default 3,

  -- When the job becomes claimable. Moved into the future to back off a retry.
  run_after     timestamptz not null default now(),
  -- When the current attempt was claimed, so an abandoned job can be reclaimed.
  claimed_at    timestamptz,

  -- The upload lives here only while the job needs it, and is dropped the
  -- moment the job reaches a terminal state. Keeping megabytes of PDF in a row
  -- indefinitely would be the wrong trade; keeping them for the seconds a
  -- worker needs avoids a second storage system entirely.
  cv            bytea,
  cv_filename   text not null,
  job_offer     text not null,
  job_title     text,

  result        jsonb,
  error_code    text,
  error_message text,

  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now(),
  finished_at   timestamptz
);

-- The claim query's index: only pending rows, ordered by when they may run.
-- Partial, because the done and failed rows are the ones that accumulate and
-- they are never claimed.
create index if not exists analysis_jobs_claimable_idx
  on public.analysis_jobs (run_after, created_at)
  where state = 'queued';

-- At most one live job per set of inputs. Terminal rows are excluded, so the
-- same analysis can be run again later once the first one has finished.
create unique index if not exists analysis_jobs_live_dedup_idx
  on public.analysis_jobs (dedup_key)
  where state in ('queued', 'running');

create index if not exists analysis_jobs_user_id_created_at_idx
  on public.analysis_jobs (user_id, created_at desc);

-- Row level security with no policy at all, on purpose. Unlike `analyses` and
-- `cvs`, this table is not part of the client's data plane: the browser reaches
-- it only through the gateway, which connects with a role that bypasses RLS and
-- scopes every read by the user id in the verified token. Enabling RLS without
-- policies makes the table unreadable through PostgREST, so a stray anon or
-- authenticated request cannot see another user's CV bytes.
alter table public.analysis_jobs enable row level security;
