# Runbook

What to do when something is wrong. Written for whoever is looking at it at the
time, which is usually one person with one terminal.

The dashboard is *Papyrus — requests, cache, queue, limits*, on Grafana. Locally:
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

A job stuck in `running` for longer than `REQUEST_TIMEOUT_SECONDS` means the
worker holding it died — a deploy, usually. It is reclaimed automatically once
the claim goes stale, five minutes by default, and the attempt counter goes up.
Nothing needs doing; the user just waits longer than they should.

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

A cluster of `upstream_rate_limited`, `ai_service_error`, `upstream_timeout` or
`upstream_unavailable` at the same timestamp is an incident: the upstream was
down long enough to burn every attempt. `worker_lost` after a deploy is the same
kind of thing from the other side. All of them are worth requeuing once it is
healthy. A dead letter keeps its upload for exactly this reason, so there is
something to re-run:

```sql
update analysis_jobs
set state = 'queued', attempts = 0, run_after = now(),
    claimed_at = null, error_code = null, error_message = null
where state = 'failed'
  and cv is not null
  and error_code in ('upstream_rate_limited', 'ai_service_error', 'upstream_timeout',
                     'upstream_unavailable', 'worker_lost')
  and finished_at > now() - interval '2 hours';
```

`upstream_unavailable` means the connection never produced an answer — refused,
cut, or never made. Before 2026-09-24 the worker recorded that as
`analysis_failed`, so older rows carrying that code with the message *The
analysis could not be completed.* are the same thing and requeue the same way.

Check what it would touch with a `select` first. Rows past `DLQ_RETENTION_HOURS`
are gone entirely, so this only reaches recent ones — which is the intent.

Do **not** requeue `unreadable_cv`. It will fail again, identically.

`upstream_rate_limited` with a body of `Too Many Requests` is the platform, not
the model. A free instance that is asleep answers 429 while it wakes, and the
worker's own retries are enough concurrency to trigger it. The analysis is fine;
the service was not there yet.

The durable fix is not more retries — it is keeping the service awake. The
scheduled `keep-warm` workflow does not manage it: GitHub throttles cron on free
accounts hard, and in practice it runs every two to six hours rather than the
ten minutes it asks for. Check its history before trusting it:

```
gh run list --workflow keep-warm
```

An external pinger would hold the instance open, and is the wrong answer here:
Render's free allowance is instance-hours shared across services, and keeping two
of them awake around the clock spends roughly twice what the month provides. The
services get suspended instead.

So the cold start is tolerated rather than prevented, and the worker waits it out
deliberately instead of retrying blind. A 429 is not treated as an attempt to
repeat later on a timer: the worker issues `GET /health` on the AI service and
holds it open until the service answers, for up to four minutes in all, then
schedules the retry two seconds later. The slowest cold start seen in production
took three.

The probe waits rather than samples: each one is allowed ninety seconds, so a
start is usually covered by one or two. A probe that fails *at once* is being
refused rather than kept waiting, and the gap before the next one doubles, from
five seconds up to thirty — asking again on a fixed short timer turned a two
minute wait into dozens of requests to a service already refusing them.

Two things once written here turned out not to be true, and are worth knowing so
they are not rediscovered as fact. Hanging up a probe does not abandon the start
it triggered: a probe cut off at a hundred seconds was followed by one answered
fifty three seconds later, well short of a fresh start. And a request carrying
the internal token is not refused at the platform edge: the version fetch is
authenticated, and it woke a sleeping instance in twenty three seconds. Why the
analysis call was answered 429 during a start while other requests were not is
still not known.

If the budget passes with no answer, the attempt falls back to the long throttle
delay — three attempts spanning 90 to 135 seconds — which still outlasts a slow
wake, and stops the retries from being the concurrency that provokes the 429 in
the first place.

In the worker log, `upstream came back` with a `waited` duration means the wait
worked and the next attempt is imminent. `upstream did not come back` means the
budget expired and the job is on the slow path.

`upstream did not come back` on every attempt, with the AI service reachable by
hand, means the cold start has outgrown the probe timeout. Measure it before
changing anything:

```
curl -s -o /dev/null -w '%{http_code} %{time_total}s
' --max-time 120   https://papyrus-94mv.onrender.com/health
```

Run that against an instance that has been idle for twenty minutes or more; a
warm one answers in under a second and tells you nothing.

Two things sit between a worker and the AI service, and each has a timeout of
its own that applies whatever the worker's deadline says: the shared HTTP
client, and the cache's shared call, which runs detached from whoever started it
so that others waiting on the same analysis keep their answer. Both are sized
from `app.UpstreamBudget`, the largest deadline any caller sets. Sizing either
for the request path makes `JOB_TIMEOUT_SECONDS` configuration that does
nothing — which happened twice, the second time capping every attempt at
`REQUEST_TIMEOUT_SECONDS` with the job timeout set to twice that.

The tell is an attempt that dies at a round number matching the *request*
timeout rather than the job timeout. The trace shows it directly: the attempt
span and the `POST /analyze` under it end together, at that number.

The numbers are not environment variables. They are the defaults in
`Config.withDefaults` in `gateway/internal/worker/worker.go` and
`readinessTimeout` in `gateway/internal/app/deps.go`, each with the reasoning
next to it. Changing one is a deploy, on purpose — they are set against a
measured cold start, not guessed at during an incident.

This is only bearable because the analysis is queued. Waiting two minutes was not
an option while a browser held the request open; polling a job makes it one.

---

## Where did the time go?

Find the trace. Every log line of a traced request carries `trace_id`, and for a
request that did not arrive with an `X-Request-ID` of its own the correlation id
*is* the trace id — so the id in a log line, in the response header, or in the
browser's network tab is what to search for.

One analysis is one trace from the click to the model call, the queue included:
the job row carries the `traceparent`, so the worker continues the request's
trace rather than starting its own. Retries are in there too. What to read off
it:

- **`job.age_seconds`** on the attempt span — how long the person had been
  waiting when this attempt started. On a retry it covers the earlier ones.
- **The gap before the attempt span** — queue wait. A large one with an empty
  queue means no worker was claiming.
- **The span for the call to the AI service** — nearly all of a healthy
  analysis. If the total is long and this is short, the time went somewhere
  else and the trace says where.

**`POST /analyses` itself is slow.** It should answer in well under a second
whatever state the AI service is in, because all it does is write a row. If the
trace shows it taking seconds, look for a `GET /version` span under it: the
request path fetches the prompt version for the cache key, and is allowed to
wait for it for two seconds at most before queueing without one. A request that
waits longer than that means the budget is not being applied — which is how the
first production trace caught it taking twenty four seconds on a cold start.

A job with no trace shows up as a trace containing only the attempt. That is a
job enqueued before the column existed, or while tracing was off. It is not a
fault.

Locally:

```
OTEL_EXPORTER_OTLP_ENDPOINT=http://tempo:4318 docker compose --profile observability up --build
```

Grafana is on :3001, and the Tempo datasource is already provisioned. There is
no span-to-logs link because the local stack has no log backend — read those
with `docker compose logs` and grep the `trace_id`.

In production the same two variables point at a hosted backend instead:
`OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_HEADERS` for its
credentials. Both services read them; neither needs anything else.

**Tracing off is the normal state of a deployment nobody has configured.** No
endpoint means no export, and everything else behaves identically — including
reading an inbound `traceparent` and passing it on, so a service with tracing
off does not put a hole in anybody else's trace.

---

## The queue is off: the schema is behind

`database schema is behind; the queue is off until it is migrated` in the log,
with `have` and `need` versions, means a release went out ahead of its
migration. The gateway checks every thirty seconds, and `schema_ready` on
`/metrics` is `0` for as long as it lasts.

Nothing is broken. The queued routes answer 404, which is what a gateway with no
queue says, and the client falls back to the synchronous endpoint on exactly
that. People still get their analyses; they wait for them in the browser instead
of polling. The worker claims nothing, because a claim would read columns that
are not there yet.

Apply the migration named by `need`. Within thirty seconds the log says
`database schema is in place; the queue is on`, `schema_ready` goes to `1`, and
jobs start moving again. No restart. A browser tab that already fell back keeps
using the synchronous endpoint until it is reloaded.

## Adding a migration

Take the next number, and end the file by recording itself:

```sql
insert into public.schema_migrations (version) values ('0009')
on conflict (version) do nothing;
```

Then raise `schema.RequiredVersion` in `gateway/internal/schema/schema.go` to
match.

If it touches an index on `analysis_jobs`, the plan tests in
`gateway/internal/jobs/claim_plan_test.go` explain every statement the worker runs
against a hundred thousand rows. One that goes back to reading the whole table
fails there, with its plan printed. They check what is read, not which index
reads it, so moving work from one index to another is fine as long as nothing
ends up scanning. Tests hold both: a migration that does not record itself, or a
`RequiredVersion` behind the newest file, fails CI. They also apply every
migration to a real Postgres, twice, so one that breaks or cannot be pasted a
second time is caught before it reaches the SQL editor.

**Apply it before pushing the code.** Pushing first is safe — the gate keeps the
queue out of the way — but it costs the queue for however long the migration
takes to arrive. The order the gate makes safe is not the order it makes free.

---

## Which build is running

Before diagnosing anything else, check that the fix you are reasoning about is
actually deployed:

```
curl -s https://papyrus-gateway.onrender.com/health
```

The `revision` is the short commit. Compare it against `git log --oneline -1` on
`main`. A platform that rebuilds on every push takes minutes to do so, and a
redeploy also kills whatever the worker had in flight — so a job that spans a
deploy runs its first attempt on one build and its next on another, which makes
its timings mean nothing.

`unknown` means nothing stamped the build. That is expected locally; in a
deployment it means `RENDER_GIT_COMMIT` is not reaching the process.

This is the first check because the alternative is inferring the deployed
version from how long retries took, which is guesswork performed at the worst
possible moment.

---

## A job says `worker_lost`

The process holding it died between claiming the work and recording what
happened — a deploy, an eviction, a crash. It had no attempts left, so nothing
would ever pick it up again, and a sweep closed it rather than leaving it
running forever.

One or two after a deploy is the mechanism working. A steady trickle without
deploys means something is killing the process: check memory first.

The job keeps its upload, so it can be requeued like any other dead letter.

Two rules keep these bounded, and they have to agree with each other:

- A stale `running` job is only reclaimed while `attempts < max_attempts`.
  Without that ceiling a job whose worker keeps dying is handed out forever;
  one reached seven attempts against a limit of three that way.
- A stale `running` job with no attempts left is closed by the sweep, at the
  same threshold. If the two thresholds ever differ, a job can be both too old
  to retry and too young to close, which is how rows go missing from both paths.

`ClaimStaleAfter` is derived rather than configured, for the same reason:
reclaiming a job whose worker is only slow runs the analysis twice, so it has to
clear the longest an attempt can legitimately take — `JOB_TIMEOUT_SECONDS`, plus
the wait for a sleeping upstream, plus the bookkeeping budget — with room to
spare. Raising any one of those raises it automatically.

This does mean recovery is slower than a person's patience: the browser stops
polling after four minutes, and a job abandoned by a dead worker is not closed
for closer to eight. That ordering is deliberate. Running an analysis twice
costs a model call and can return two different answers; making somebody wait
and check their history costs neither. Re-submitting meanwhile joins the job
that is already live rather than starting a second one.

---

## Is the rate limit actually shared?

```
curl -s https://papyrus-gateway.onrender.com/metrics | grep rate_limit_shared
```

`1` means a shared backend is configured, `0` means this process is counting on
its own and the budget is per replica.

The degraded counter does not answer this. It only moves when a backend that
*was* configured fails, so a deployment that was never given `REDIS_URL` looks
exactly like a healthy shared one: no errors, no degraded line, and a limit that
silently means N times what it says.

---

## The rate limit stopped being shared

A rising `degraded` line on *Rate limit decisions*, or
`rate limiting degraded to this process only` in the log, means the limiter
cannot reach Redis. Requests keep flowing — each replica falls back to counting
on its own — but the budget is now the configured one *per replica* rather than
in total.

Nothing is broken and nothing needs to be restarted. Check Redis; the limiter
rejoins on its own and logs `rate limiting is shared again`.

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

Exec mode has a consequence worth remembering when writing queries: without a
prepared statement there is no type information for the parameters, so anything
going into a non-text column needs an explicit cast (`$2::jsonb`). A statement
that works under pgx's default mode can fail under this one, which is why the
test pool is configured the same way.

---

## Rolling back

Every service deploys from `main`. Render redeploys a previous commit from its
dashboard; Vercel promotes a previous deployment.

Migrations are forward-only and additive so far, so a rollback of the code does
not need a rollback of the schema. `0006` adds a table nothing else references —
an older build simply ignores it.
