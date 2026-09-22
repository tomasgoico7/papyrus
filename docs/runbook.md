# Runbook

What to do when something is wrong. Written for whoever is looking at it at the
time, which is usually one person with one terminal.

The dashboard is *Papyrus — requests, cache, queue*, on Grafana. Locally:
`docker compose --profile observability up`.

---

## Following one request

Every request carries an `X-Request-ID`, generated at the gateway and forwarded
to the AI service. It comes back on the response, so a user who reports a
problem can be asked for it.

```
request_id=3cdcb3d60fd4824350e0c8ff3bcd3387
```

Search that string in both services' logs. A failed analysis usually has the
gateway line saying what it returned and the AI service line saying why.

Logs are JSON in production. On Render, the log viewer's search is enough; there
is no aggregator, and its retention is short — see [ADR 0006](adr/0006-correlate-and-measure-every-request.md).

---

## The analysis is slow

Check, in order:

1. **Is the AI service awake?** On a free tier it spins down after ~15 minutes
   idle and takes 20–50s to come back. `process_start_time_seconds` on
   `/metrics` says when it last started. The scheduled `keep-warm` workflow is
   what prevents this; if it has been failing, that is the cause.
2. **Is the cache working?** *Analysis cache hit rate* near zero when it should
   not be, or a rising `error` line in *Cache lookups*, means the shared tier is
   unhealthy. The service keeps working without it — just slower and at full
   cost.
3. **Is the model slow, or are we?** *Job duration p95* is the model. *Latency
   p95* on the request panel is the gateway. They move independently.

---

## The queue is backing up

*Queue depth* climbing while *Job outcomes* stays mostly `done` means the
workers are keeping up with nothing — there are too few of them. Raise
`WORKER_CONCURRENCY`, or run the worker as its own service
(`cmd/worker`) so it scales without the API.

Climbing while `retried` climbs too means the upstream is unhealthy. The workers
are fine; they are being told no. Look at the AI service.

Depth flat at zero with jobs visibly not running means no worker is consuming.
Check that `RUN_WORKER=true` and `DATABASE_URL` are both set — the startup log
says `worker starting` when they are.

---

## Dead letters

A job that exhausts its attempts stops in `failed` and stays on the table.
`analysis_jobs_dead` is the standing count; the red line on *Queue depth*.

See what died and why:

```sql
select id, error_code, count(*) over () as total, attempts, finished_at
from analysis_jobs
where state = 'failed'
order by finished_at desc
limit 20;
```

Most dead letters are a single input that will never work — a scanned PDF with
no text layer, usually `unreadable_cv`. Those are correct outcomes, not
incidents; the user was told.

A cluster of `upstream_rate_limited`, `ai_service_error` or `upstream_timeout`
at the same timestamp is an incident: the upstream was down long enough to burn
every attempt. Those are worth requeuing once it is healthy. A dead letter keeps
its upload for exactly this reason, so there is something to re-run:

```sql
update analysis_jobs
set state = 'queued', attempts = 0, run_after = now(),
    claimed_at = null, error_code = null, error_message = null
where state = 'failed'
  and cv is not null
  and error_code in ('upstream_rate_limited', 'ai_service_error', 'upstream_timeout')
  and finished_at > now() - interval '2 hours';
```

Check what it would touch with a `select` first. Rows past `DLQ_RETENTION_HOURS`
are gone entirely, so this only reaches recent ones — which is the intent.

Do **not** requeue `unreadable_cv`. It will fail again, identically.

---

## The table is growing

Finished jobs are swept by the worker, hourly: successes after
`JOB_RETENTION_HOURS` (24 by default), dead letters after `DLQ_RETENTION_HOURS`
(168). The sweep logs `purged finished jobs` when it removes anything.

If the table is large anyway, the sweep is not running — which means no worker
is running. Same check as above.

To see where the space went:

```sql
select state, count(*), pg_size_pretty(sum(pg_column_size(result))) as results
from analysis_jobs
group by state;
```

---

## Everything 504s, including the landing page

The Next.js middleware calls Supabase on every request. If the project is
paused, its hostname stops resolving and the middleware times out — taking the
public pages down with it, not just the dashboard.

The middleware has a 2.5s ceiling and lets public routes through when auth is
unreachable, so this should degrade rather than fail. If it does not:

1. Check the Supabase project is not paused. A paused project returns
   `NXDOMAIN`, not an error page.
2. The `keep-warm` workflow pings Postgres every 10 minutes to prevent it. It
   needs `SUPABASE_URL` and `SUPABASE_ANON_KEY` as repository secrets; without
   them that step skips silently.

---

## Connecting to the database

`DATABASE_URL` should be the **transaction-mode pooler** (port 6543), not the
direct connection. A worker that polls holds a connection open; direct ones are
a small, shared budget for the whole project.

The pool runs in exec mode because of that: a transaction pooler gives each
statement a different backend, so pgx's prepared-statement cache would miss and
report `prepared statement already exists`. Nothing needs to be configured for
this — it is set in code — but it is worth knowing if the connection string is
ever changed to a direct one.

---

## Rolling back

Every service deploys from `main`. Render redeploys a previous commit from its
dashboard; Vercel promotes a previous deployment.

Migrations are forward-only and additive so far, so a rollback of the code does
not need a rollback of the schema. `0006` adds a table nothing else references —
an older build simply ignores it.
