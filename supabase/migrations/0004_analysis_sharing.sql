-- Shareable analyses: an opt-in, expiring public link per analysis.
--
-- The owner sets share_token + share_expires_at through the existing
-- owner-scoped policy (a normal authenticated update). Anonymous viewers never
-- touch the table directly — they read through get_shared_analysis(), which is
-- security definer and only returns a row while its link is still live.

alter table public.analyses
  add column if not exists share_token text,
  add column if not exists share_expires_at timestamptz;

-- One live link per token; nulls (the unshared majority) are not constrained.
create unique index if not exists analyses_share_token_idx
  on public.analyses (share_token)
  where share_token is not null;

create or replace function public.get_shared_analysis(token text)
returns table (
  id             uuid,
  job_title      text,
  score          integer,
  verdict        text,
  summary        jsonb,
  matched_skills jsonb,
  missing_skills jsonb,
  suggestions    jsonb,
  created_at     timestamptz
)
language sql
security definer
set search_path = public
as $$
  select a.id, a.job_title, a.score, a.verdict,
         a.summary, a.matched_skills, a.missing_skills, a.suggestions, a.created_at
  from public.analyses a
  where a.share_token = token
    and a.share_expires_at is not null
    and a.share_expires_at > now();
$$;

grant execute on function public.get_shared_analysis(text) to anon, authenticated;
