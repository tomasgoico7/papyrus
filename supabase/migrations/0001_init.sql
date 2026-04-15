-- Papyrus schema.
-- Run this once in the Supabase SQL editor. It is idempotent enough to re-run
-- during development, but treat it as the source of truth for the data model.

-- ─────────────────────────────────────────────────────────────────────────────
-- profiles: one row per authenticated user, mirrored from auth.users.
-- ─────────────────────────────────────────────────────────────────────────────
create table if not exists public.profiles (
  id         uuid primary key references auth.users (id) on delete cascade,
  email      text not null,
  full_name  text,
  avatar_url text,
  created_at timestamptz not null default now()
);

alter table public.profiles enable row level security;

drop policy if exists "profiles are self-readable" on public.profiles;
create policy "profiles are self-readable"
  on public.profiles for select
  using (auth.uid() = id);

drop policy if exists "profiles are self-writable" on public.profiles;
create policy "profiles are self-writable"
  on public.profiles for update
  using (auth.uid() = id);

-- Provision a profile row automatically when a new auth user is created.
create or replace function public.handle_new_user()
returns trigger
language plpgsql
security definer
set search_path = public
as $$
begin
  insert into public.profiles (id, email, full_name, avatar_url)
  values (
    new.id,
    new.email,
    new.raw_user_meta_data ->> 'full_name',
    new.raw_user_meta_data ->> 'avatar_url'
  )
  on conflict (id) do nothing;
  return new;
end;
$$;

drop trigger if exists on_auth_user_created on auth.users;
create trigger on_auth_user_created
  after insert on auth.users
  for each row execute function public.handle_new_user();

-- ─────────────────────────────────────────────────────────────────────────────
-- cvs: metadata for CVs a user has analyzed. The file itself lives in Storage;
-- here we keep only what the dashboard needs to list and reference them.
-- ─────────────────────────────────────────────────────────────────────────────
create table if not exists public.cvs (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references auth.users (id) on delete cascade,
  filename    text not null,
  byte_size   integer not null,
  storage_path text,
  created_at  timestamptz not null default now()
);

create index if not exists cvs_user_id_created_at_idx
  on public.cvs (user_id, created_at desc);

alter table public.cvs enable row level security;

drop policy if exists "cvs are owner-scoped" on public.cvs;
create policy "cvs are owner-scoped"
  on public.cvs for all
  using (auth.uid() = user_id)
  with check (auth.uid() = user_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- analyses: the historical record of every CV-vs-offer analysis.
-- ─────────────────────────────────────────────────────────────────────────────
create table if not exists public.analyses (
  id             uuid primary key default gen_random_uuid(),
  user_id        uuid not null references auth.users (id) on delete cascade,
  cv_id          uuid references public.cvs (id) on delete set null,
  job_title      text,
  job_offer      text not null,
  cv_filename    text,
  score          integer not null check (score between 0 and 100),
  verdict        text not null check (verdict in ('strong', 'moderate', 'weak')),
  -- Bilingual content: { "en": ..., "es": ... }. Score and verdict stay neutral.
  summary        jsonb not null,
  matched_skills jsonb not null default '{"en": [], "es": []}'::jsonb,
  missing_skills jsonb not null default '{"en": [], "es": []}'::jsonb,
  suggestions    jsonb not null default '[]'::jsonb,
  created_at     timestamptz not null default now()
);

create index if not exists analyses_user_id_created_at_idx
  on public.analyses (user_id, created_at desc);

alter table public.analyses enable row level security;

drop policy if exists "analyses are owner-scoped" on public.analyses;
create policy "analyses are owner-scoped"
  on public.analyses for all
  using (auth.uid() = user_id)
  with check (auth.uid() = user_id);
