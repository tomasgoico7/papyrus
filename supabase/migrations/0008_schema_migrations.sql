-- Record which migrations the database has had applied.
--
-- Migrations go in by hand, through the SQL editor, while the code that depends
-- on them deploys on its own the moment it is pushed. Nothing tied the two
-- together: 0007 added a column the next release wrote to, and whether it was
-- applied first was down to luck. Had the code gone out first, every analysis
-- would have failed on a column that did not exist, with nothing to say why.
--
-- With this table the gateway can ask how far the schema has got, compare it to
-- what it was built against, and switch the queue off until they agree instead
-- of failing on every request.
--
-- Every migration from here on ends by recording itself, the way this one does
-- at the bottom. A test in the gateway fails any migration that does not.

create table if not exists public.schema_migrations (
  version    text primary key,
  applied_at timestamptz not null default now()
);

comment on table public.schema_migrations is
  'One row per migration applied, so the application can tell whether the schema it needs is in place.';

-- Nobody reads this but the gateway, which connects as the table owner. Row
-- level security with no policies keeps it out of reach of the browser, which
-- talks to this database directly.
alter table public.schema_migrations enable row level security;

-- Everything before this table existed. They are in production already, which
-- is the only reason recording them here, after the fact, is true.
insert into public.schema_migrations (version) values
  ('0001'), ('0002'), ('0003'), ('0004'), ('0005'), ('0006'), ('0007')
on conflict (version) do nothing;

insert into public.schema_migrations (version) values ('0008')
on conflict (version) do nothing;
