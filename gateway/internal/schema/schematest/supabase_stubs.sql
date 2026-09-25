-- Stand-ins for what Supabase provides before any migration runs.
--
-- The migrations reach into schemas Supabase owns — auth for users, storage for
-- uploaded files — and grant to roles it creates. None of that exists in a bare
-- Postgres, so it is sketched here: only the columns, functions and roles the
-- migrations actually touch, shaped the way Supabase shapes them.
--
-- Keep this minimal. Anything added here that Supabase does not have makes a
-- migration pass in CI that would fail in production, which is the opposite of
-- what this is for.

create extension if not exists pgcrypto;

create schema if not exists auth;

-- The trigger in 0001 reads email and raw_user_meta_data from a new user, so
-- both have to be here for it to run at all.
create table if not exists auth.users (
  id                 uuid primary key default gen_random_uuid(),
  email              text,
  raw_user_meta_data jsonb not null default '{}'::jsonb
);

-- Supabase resolves this from the request's JWT. A test sets the claim to act
-- as a user; without it the function returns null, as it does for anon.
create or replace function auth.uid() returns uuid
language sql stable
as $$
  select nullif(current_setting('request.jwt.claim.sub', true), '')::uuid
$$;

create schema if not exists storage;

create table if not exists storage.buckets (
  id     text primary key,
  name   text not null,
  public boolean not null default false
);

create table if not exists storage.objects (
  id        uuid primary key default gen_random_uuid(),
  bucket_id text references storage.buckets (id),
  name      text,
  owner     uuid
);

-- The folder segments of an object path, as the storage policies expect them.
create or replace function storage.foldername(name text) returns text[]
language sql immutable
as $$
  select string_to_array(name, '/')
$$;

do $$
begin
  create role anon nologin;
exception when duplicate_object then null;
end
$$;

do $$
begin
  create role authenticated nologin;
exception when duplicate_object then null;
end
$$;
