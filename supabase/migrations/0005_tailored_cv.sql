-- Persist the tailored CV alongside its analysis, so it can be reopened and
-- re-downloaded without regenerating. Stored as the structured CV the app
-- renders; re-tailoring overwrites it. Owner-scoped by the existing RLS policy.

alter table public.analyses
  add column if not exists tailored_cv jsonb;
