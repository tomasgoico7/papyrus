-- CV storage. Run this after 0001_init.sql.
-- Creates a private bucket for uploaded CVs and scopes every object to the
-- owner's folder, so the same `auth.uid()` isolation the tables enjoy also
-- applies to the files themselves.

insert into storage.buckets (id, name, public)
values ('cvs', 'cvs', false)
on conflict (id) do nothing;

-- Objects are stored under `<user-id>/<cv-id>.pdf`; the first path segment is
-- the owner, which every policy below checks against the caller.

drop policy if exists "cv objects are owner-readable" on storage.objects;
create policy "cv objects are owner-readable"
  on storage.objects for select
  using (
    bucket_id = 'cvs'
    and (storage.foldername(name))[1] = auth.uid()::text
  );

drop policy if exists "cv objects are owner-insertable" on storage.objects;
create policy "cv objects are owner-insertable"
  on storage.objects for insert
  with check (
    bucket_id = 'cvs'
    and (storage.foldername(name))[1] = auth.uid()::text
  );

drop policy if exists "cv objects are owner-deletable" on storage.objects;
create policy "cv objects are owner-deletable"
  on storage.objects for delete
  using (
    bucket_id = 'cvs'
    and (storage.foldername(name))[1] = auth.uid()::text
  );
