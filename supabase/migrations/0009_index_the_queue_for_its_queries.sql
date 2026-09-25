-- Index the queue for the queries it actually runs.
--
-- Measured on a hundred thousand rows in steady state — everything inside its
-- retention window, the shape the table settles into — three of the queue's
-- queries did far more reading than their work called for.
--
-- The depth gauge, every fifteen seconds, scanned all hundred thousand rows to
-- count the two thousand still live. The retention sweep, every two minutes,
-- scanned them all to delete nothing. Both grew with history rather than with
-- the work in front of them.
--
-- The claim was fast, but by accident. The index built for it covered only
-- queued jobs, and the claim also takes back running jobs whose worker died;
-- with that second branch the index could not serve it at all. What served it
-- instead was the dedup index, whose predicate happened to cover both branches,
-- so every claim read the whole live layer and sorted it. Widening the dedup
-- index for a reason of its own would have sent the claim back to reading the
-- entire table — measured, a hundred thousand rows and seven times slower —
-- with nothing on the claim's side to show why.

-- The finished rows, by state and then by when they finished. The sweep reads a
-- range per state; the dead letter count reads the failed prefix alone. The
-- cost is one index entry when a job reaches a terminal state, once per job.
create index if not exists analysis_jobs_finished_idx
  on public.analysis_jobs (state, finished_at)
  where state in ('done', 'failed');

-- Both branches of the claim, in the order the claim wants them. Walking it,
-- the claim stops at the first row it can take instead of reading every live
-- job and sorting them: three rows read out of a hundred thousand, where the
-- dedup index had it reading two thousand.
create index if not exists analysis_jobs_due_idx
  on public.analysis_jobs (run_after, created_at)
  where state in ('queued', 'running');

-- Replaced by the one above, which covers everything this did and the branch
-- it could not. Kept, it would be written on every change to a queued job and
-- read by nothing.
drop index if exists public.analysis_jobs_claimable_idx;

insert into public.schema_migrations (version) values ('0009')
on conflict (version) do nothing;
