# Papyrus

*Versión en español: [README.md](README.md).*

Papyrus measures how well your CV fits a job before you spend an afternoon tailoring it by hand. You upload your CV as a PDF and paste the job description; in seconds you get a 0–100 compatibility score, a verdict (strong / moderate / weak), the skills the role asks for that you already cover, the ones you're missing, and a short list of concrete edits to bring your CV closer to *that* specific role. The point is to stop guessing whether it's worth applying and know, with data, what's worth adjusting.

I built it as a complete, real full-stack project: three independently deployable services, Google sign-in, Row Level Security doing the authorization at the database level, and an LLM doing the actual reasoning. The rule I set for myself was that nothing would be simulated — no mock data, no half-wired buttons, no "TODO: handle errors". It works end to end or it isn't in here.

The interface is bilingual (English / Spanish), has light and dark themes, and tries hard to stay out of the way so the analysis is the only thing on screen worth looking at.

---

## Table of contents

- [Features](#features)
- [Architecture](#architecture)
- [The request, end to end](#the-request-end-to-end)
- [Tech stack](#tech-stack)
- [Project layout](#project-layout)
- [Running it locally](#running-it-locally)
- [How a few things actually work](#how-a-few-things-actually-work)
- [API reference](#api-reference)
- [Observability and measurement](#observability-and-measurement)
- [Testing](#testing)
- [Deploying](#deploying)
- [Gotchas I hit](#gotchas-i-hit-so-you-dont)
- [Decisions and trade-offs](#decisions-and-trade-offs)
- [What I'd add next](#what-id-add-next)

---

## Features

- **Compatibility scoring** — upload a CV (PDF) and a job posting, get a 0–100 fit score plus a `strong` / `moderate` / `weak` verdict.
- **Matched vs. missing skills** — the model separates what the posting asks for into what your CV already proves and what it doesn't.
- **Actionable suggestions** — two to five specific edits for that role, ordered by impact, that never invent experience you don't have.
- **A CV tailored to the role** — before writing anything, the model asks about what the posting wants and your CV doesn't show. From your answers it writes a version of the CV for that role, which you download as PDF or Word. Anything you mark as "I don't have this" stays out: it never invents experience. It's saved with the analysis, so reopening it doesn't regenerate it.
- **History with search and filters** — every analysis is saved. You can search them by role and filter by verdict, reopen any of them, reuse an already-uploaded CV against a new posting, download the original CV (signed URL, private bucket), or delete it (with a confirmation, because it also wipes the stored file).
- **PDF export** — download any analysis as a clean, bilingual PDF (selectable text), generated in the browser.
- **Share by link** — create an expiring public link so anyone can view the analysis (never your CV), revocable whenever you want.
- **Bilingual content** — the analysis itself is generated in English *and* Spanish in a single model call, so switching the UI language re-renders the result instantly without re-running anything.
- **Light / dark theme** — follows your OS by default, with a manual toggle.
- **Google sign-in** — via Supabase Auth. No passwords to store.

Everything runs on free tiers. There are no paid API keys anywhere in the stack.

---

## Architecture

Three services and a queue. User data lives in Supabase, and the browser reads and writes it directly under Row Level Security: every row is scoped to the signed-in user with no server in between. The gateway is the secured edge for the one thing that needs a secret — talking to the model — and the AI service is a stateless function from `(CV, posting)` to `analysis`.

The original rule was that the gateway never touched the database. Since analyses became asynchronous it touches exactly two tables — the job queue and the migration record — with RLS enabled and no policies, so the browser cannot see them. That means the gateway holds a Postgres credential, and today it is the database owner's: it could read more than it does. Giving it a role of its own, with grants on those two tables only, is noted as outstanding.

```
                  ┌───────────────────────────────────────────────────┐
                  │                      Supabase                     │
                  │   Postgres · Auth (Google) · Storage              │
                  │   profiles · analyses · cvs      ← browser, RLS   │
                  │   analysis_jobs · schema_migrations  ← gateway    │
                  └───────────────────────────────────────────────────┘
                     ▲                                    ▲
         OAuth + JWT │ reads / writes under RLS           │ queue: SKIP LOCKED
                     │                                    │
    ┌────────────────┴───────────────┐                    │
    │            Frontend            │··········································┐
    │   Next.js 14 · Vercel          │                    │   GET /health       :
    └───────────────┬────────────────┘                    │   (wakes it)        :
                    │ POST /analyses → 202                │                     :
                    │ GET /analyses/{id}  (polling)       │                     :
                    ▼                                     │                     :
    ┌──────────────────────────────────────────────┐      │                     :
    │           API Gateway  ·  Go · Gin           ├──────┘                     :
    │   JWKS · rate limit · load shedding          │                            :
    │   cache · queue + worker · circuit breaker   ├── Redis: cache, rate limit :
    └───────────────────────┬──────────────────────┘                            :
                            │ analysis · version  (breaker)                     :
                            │ GET /health  (readiness)                          :
                            ▼                                                   :
    ┌──────────────────────────────────────────────┐                            :
    │       AI Service  ·  Python · FastAPI        │◀···························┘
    │   PDF → prompt → Gemini → bilingual JSON     │
    └──────────────────────────────────────────────┘

      Every hop carries one OpenTelemetry trace, the queue included.
```

The split is honestly a lot for a CV tool — you could collapse this into one Next.js app. I kept it separate on purpose: the LLM layer, the request edge and the UI scale and fail differently, and the project exists to work through those problems for real. The honest version is in [Decisions and trade-offs](#decisions-and-trade-offs).

---

## The request, end to end

1. **Sign in.** Supabase runs the Google OAuth and leaves the session in HTTP-only cookies. Middleware refreshes it on every request and guards `/dashboard`.
2. **Open the workspace.** The browser sends a `GET /health` straight to the AI service to wake it: on the free tier it sleeps, and a request from the gateway does not wake it while one from outside the platform does.
3. **Submit a CV and a posting** to `POST /analyses`. The gateway verifies the JWT against Supabase's JWKS, applies the per-user rate limit (shared across replicas through Redis) and checks the cache. An analysis already done comes back at once. Otherwise it is queued in Postgres and the gateway answers `202` with the job id — in under a second, even with the AI service asleep.
4. **The worker** claims the job with `SELECT … FOR UPDATE SKIP LOCKED` and calls the AI service behind a circuit breaker. If a cold start answers 429, it probes `/health` until the service responds and retries two seconds later. The result is stored on the job and in the cache.
5. **The AI service** extracts the text with `pypdf` and asks Gemini, through LangChain's `with_structured_output`, for a validated bilingual JSON object. The verdict is computed from the score in Python, not trusted to the model.
6. **The browser** polls `GET /analyses/{id}` at a growing interval, gets the result, uploads the CV to a private Storage bucket and writes the analysis to `analyses`, under RLS.

If the gateway has no queue — or the database does not yet have the schema the code needs — `POST /analyses` answers 404 and the browser falls back to `POST /analyze`, the original synchronous path. The analysis still happens.

---

## Tech stack

| Layer       | What's in it                                                                 |
|-------------|------------------------------------------------------------------------------|
| Frontend    | Next.js 14 (App Router), strict TypeScript, Tailwind CSS, `@supabase/ssr`, `next-themes`, cookie-based i18n, `zod`, Vitest |
| Gateway     | Go 1.26, Gin, `pgx/v5`, `go-redis/v9`, `golang-jwt/v5` with hand-rolled JWKS, OpenTelemetry, Prometheus, testcontainers |
| AI service  | Python 3.11, FastAPI, LangChain (`langchain-core` + `langchain-google-genai`), `pypdf`, `pydantic-settings`, `structlog`, OpenTelemetry |
| Data        | Supabase (PostgreSQL with RLS + private Storage) · Redis (shared cache and rate limit) |
| LLM         | Google Gemini (`gemini-2.5-flash`, free tier)                                |
| Observability | OTLP traces to Grafana Cloud (Tempo) · Prometheus metrics · JSON logs carrying `trace_id` |
| Local dev   | Docker Compose, with profiles for observability (Prometheus, Grafana, Tempo), two replicas and load (k6) |
| Deploy      | Vercel (frontend) · Render (gateway + AI service) · Supabase · Redis Cloud · Grafana Cloud — all free tier |

---

## Project layout

```
papyrus/
├── frontend/                    # Next.js 14 app, Node 22
│   ├── app/                     # routes: landing, /dashboard, /share/[token], /auth/{callback,signout}
│   ├── components/
│   │   ├── analysis/            # score ring, skills, verdict, suggestions, tailored CV
│   │   ├── auth/, share/        # sign in and out, the public view of a shared analysis
│   │   ├── dashboard/           # workspace, form, dropzone, history, result states
│   │   ├── marketing/           # header, footer, hero preview, scroll reveal
│   │   └── ui/                  # button, language/theme toggles, confirm dialog
│   └── lib/
│       ├── analyses/, cvs/      # Supabase data access (the "repositories")
│       ├── api/                 # gateway client, job polling, waking the AI service
│       ├── i18n/                # dictionaries (en/es) + server & client helpers
│       └── supabase/            # browser / server / middleware clients
├── gateway/                     # Go + Gin edge
│   ├── cmd/server/              # API: entrypoint + graceful shutdown
│   ├── cmd/worker/              # queue worker as its own service
│   └── internal/
│       ├── app/                 # composition root: wiring shared by API and worker
│       ├── auth/                # JWKS fetch + cache, ES256/RS256 verification
│       ├── breaker/             # circuit breaker in front of the AI service
│       ├── buildinfo/           # which revision is running, reported on /health
│       ├── cache/               # byte store: in-process LRU, Redis, and the tier
│       ├── config/              # env loading + validation (fail fast)
│       ├── handlers/, router/   # /analyze, /analyses, /tailor, /health, /metrics, engine assembly
│       ├── jobs/                # Postgres queue, claimed with SKIP LOCKED
│       ├── middleware/          # CORS, auth, rate limiting, load shedding, request id
│       ├── observability/       # structured logger, RED metrics and traces
│       ├── ratelimit/           # per-caller budget: local, Redis, and the fallback
│       ├── requestid/           # correlation id and its context plumbing
│       ├── schema/              # schema version and the gate that turns the queue off
│       ├── worker/              # queue loop, jittered backoff, dead letters
│       └── services/, transport/, httpx/
├── ai-service/                  # Python + FastAPI
│   ├── app/{api,core,schemas,services}/
│   └── tests/                   # offline, model chain is faked
├── supabase/migrations/         # 0001–0005 user data · 0006–0009 the queue and the migration record
├── docs/                        # adr/ for architecture decisions, runbook.md for running it
├── ops/                         # Prometheus config and Grafana dashboards
├── load/                        # k6 scenario + AI-service stub
├── docker-compose.yml
└── .env.example
```

---

## Running it locally

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) + Compose v2
- A free [Supabase](https://supabase.com) project
- A free [Google AI Studio](https://aistudio.google.com/app/apikey) key

If you want to run a service outside Docker you'll also need Node 22, Go 1.26+, or Python 3.11+ depending on which one.

### 1. Supabase (this is the only fiddly part)

**Schema.** Open the SQL Editor and run, in order:

- [`0001_init.sql`](supabase/migrations/0001_init.sql) — `profiles`, `analyses`, `cvs`, the RLS policies, and a trigger that creates a profile row on sign-up.
- [`0002_cv_storage.sql`](supabase/migrations/0002_cv_storage.sql) — the private `cvs` Storage bucket and owner-scoped object policies (`<user-id>/<cv-id>.pdf`).
- [`0003_bilingual_analyses.sql`](supabase/migrations/0003_bilingual_analyses.sql) — only needed if your DB predates the bilingual change; it's a guarded no-op on a fresh install.
- [`0004_analysis_sharing.sql`](supabase/migrations/0004_analysis_sharing.sql) — the sharing columns (`share_token`, `share_expires_at`) and the `get_shared_analysis` (`security definer`) function that serves an analysis over a public link without bypassing RLS.
- [`0005_tailored_cv.sql`](supabase/migrations/0005_tailored_cv.sql) — the `tailored_cv` column on `analyses`, where the tailored CV is kept so it reopens without being regenerated.
- [`0006_analysis_jobs.sql`](supabase/migrations/0006_analysis_jobs.sql) — the analysis queue, `analysis_jobs`, with RLS enabled and no policies: the browser cannot see it.
- [`0007_analysis_jobs_traceparent.sql`](supabase/migrations/0007_analysis_jobs_traceparent.sql) — the `traceparent` that carries an analysis's trace across the queue.
- [`0008_schema_migrations.sql`](supabase/migrations/0008_schema_migrations.sql) — the record of applied migrations, which the gateway compares against the version it needs.
- [`0009_index_the_queue_for_its_queries.sql`](supabase/migrations/0009_index_the_queue_for_its_queries.sql) — the indexes for the queue's queries.

Only [the queue](#the-queue-optional) uses the last four, but running them anyway costs nothing. Any of them can be pasted twice without breaking anything; a test holds them to it.

**Google auth.** This is the part that takes a few minutes. In the [Google Cloud Console](https://console.cloud.google.com/apis/credentials), configure the OAuth consent screen (External), then create an *OAuth client ID → Web application* with this authorized redirect URI:

```
https://<your-project-ref>.supabase.co/auth/v1/callback
```

Copy the Client ID and Client Secret into **Supabase → Authentication → Providers → Google** and enable it. Then, under **Authentication → URL Configuration**, set the Site URL to `http://localhost:3000` and add `http://localhost:3000/auth/callback` to the redirect allow list.

**Values you'll need** (Project Settings → API):

- `Project URL` → `NEXT_PUBLIC_SUPABASE_URL`
- the `anon` / `publishable` key → `NEXT_PUBLIC_SUPABASE_ANON_KEY`

Note there's no JWT secret to copy. The gateway verifies tokens against the project's public JWKS (`<Project URL>/auth/v1/.well-known/jwks.json`), which it derives from `NEXT_PUBLIC_SUPABASE_URL` — so that one value does double duty.

### 2. Gemini key

Create one at [AI Studio](https://aistudio.google.com/app/apikey) → `GEMINI_API_KEY`. Make sure it's an actual **API key** (starts with `AIza…`), not an OAuth token. The default model is `gemini-2.5-flash` — free, fast, and it supports the structured output this relies on.

### 3. Environment + run

```bash
cp .env.example .env     # fill in the three real values; the rest have sane defaults
docker compose up --build
```

| Service     | URL                     |
|-------------|-------------------------|
| Frontend    | http://localhost:3000   |
| Gateway     | http://localhost:8080   |
| AI service  | http://localhost:8000   |

Only three variables actually need real values — `NEXT_PUBLIC_SUPABASE_URL`, `NEXT_PUBLIC_SUPABASE_ANON_KEY`, and `GEMINI_API_KEY`. Compose will warn you by name if any are missing. Stop everything with `docker compose down`.

> One catch: Next inlines `NEXT_PUBLIC_*` at **build** time, so if you change those you have to `docker compose up --build` again — a restart won't pick them up.

### The queue (optional)

Everything above already works: without `DATABASE_URL` the gateway does not register `POST /analyses`, and the frontend takes the synchronous path. To try the queue:

1. Run migrations 0006 to 0009, if you haven't.
2. In `.env`, set `DATABASE_URL` to the *transaction pooler* URL (Supabase → Project Settings → Database → Connection pooling) and `RUN_WORKER=true`, so the worker runs inside the gateway.
3. Bring everything up again with `docker compose up`.

On start, the gateway compares the schema's version with the one it needs. If a migration is missing it says so in the log and keeps the queue off; the frontend stays on the synchronous path, and the queue switches itself on within a minute of the migration going in.

### Running a single service

Each service reads its own `.env` and runs on its own. This is the fast loop when you're working on one of them:

```bash
# frontend
cd frontend && cp .env.example .env.local && npm install && npm run dev

# gateway
cd gateway && cp .env.example .env && go mod tidy && go run ./cmd/server

# ai-service
cd ai-service && cp .env.example .env && python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt && uvicorn app.main:app --reload --port 8000
```

---

## How a few things actually work

### Auth — JWKS, not a shared secret

Supabase used to sign access tokens with HS256 and a shared secret you'd copy into your backend. New projects sign with **asymmetric keys (ES256)** and publish the public half at a JWKS endpoint. The gateway fetches that key set, caches it, and verifies each token's signature against the key matching its `kid` — refreshing the cache if it sees an unknown one, so key rotation doesn't need a redeploy. Accepted algorithms are pinned to `ES256`/`RS256` to dodge algorithm-confusion attacks. It's all standard-library crypto; no extra dependency.

### Bilingual analyses — one call, two languages

The model is asked for every natural-language field (`summary`, each suggestion's `title`/`detail`, and the skill lists) in both English and Spanish at once, as `{ "en": …, "es": … }`. Both versions are stored, and the UI just reads the half that matches the current language. So toggling EN/ES re-renders instantly — no second request, no re-analysis. The cost is roughly double the output tokens per analysis, which is a non-issue on the free tier. Score and verdict are language-neutral and stay flat.

### Why the browser talks to Supabase directly

The frontend hits Supabase's REST and Storage APIs straight from the client — there's no proxy in front of the CRUD. That's deliberate, and it's safe because **authorization is enforced in Postgres, not the client**: every table has RLS (`auth.uid() = user_id`), Storage objects are scoped to the owner's folder, and the publishable key is public by design. A crafted request still can't reach another user's data. The one operation that genuinely needs a server — calling Gemini, which requires a real secret plus rate limiting and validation — is the only thing that goes through the gateway. More on the trade-off below.

---

## API reference

### `POST /analyze` — gateway

- **Auth:** `Authorization: Bearer <supabase access token>`
- **Body:** `multipart/form-data`

| Field      | Type   | Required | Notes                          |
|------------|--------|----------|--------------------------------|
| `cv`       | file   | yes      | PDF, 5 MB cap (configurable)   |
| `jobOffer` | string | yes      | Raw job description text       |
| `jobTitle` | string | no       | Labels the saved record        |

**`200`** — natural-language fields come back in both languages; `score` and `verdict` are language-neutral:

```json
{
  "score": 78,
  "verdict": "strong",
  "summary": {
    "en": "Strong overlap on backend and cloud, but the role leans heavily on Kubernetes.",
    "es": "Buen solapamiento en backend y cloud, pero el rol depende mucho de Kubernetes."
  },
  "matchedSkills": {
    "en": ["Go", "PostgreSQL", "Docker", "CI/CD"],
    "es": ["Go", "PostgreSQL", "Docker", "CI/CD"]
  },
  "missingSkills": { "en": ["Kubernetes", "gRPC"], "es": ["Kubernetes", "gRPC"] },
  "suggestions": [
    {
      "title": {
        "en": "Surface container orchestration experience",
        "es": "Resaltá tu experiencia en orquestación de contenedores"
      },
      "detail": {
        "en": "The posting names Kubernetes three times. Add a bullet quantifying cluster size.",
        "es": "La oferta menciona Kubernetes tres veces. Agregá un bullet con el tamaño del clúster."
      },
      "priority": "high"
    }
  ],
  "cvFilename": "jane-doe-resume.pdf"
}
```

Errors share one envelope across all three services, so the frontend only has to understand one shape:

```json
{ "error": { "code": "payload_too_large", "message": "CV exceeds the 5 MB limit." } }
```

The gateway passes a 4xx from the AI service straight through (e.g. `422 unreadable_cv` for a scanned PDF with no text layer) and collapses anything else — timeouts, 5xx, a dead upstream — into a `502`/`504`.

There are two cases where the gateway does not even try, and answers `503` with `Retry-After`: `upstream_unavailable` while the circuit breaker is open, and `overloaded` when too many requests to the AI service are already in flight.

### `POST /analyses` — gateway *(asynchronous)*

The same body as `/analyze`, but it does not wait for the model. It exists only
where the gateway has a `DATABASE_URL`; without one the route is not registered.
If the database does not yet have the schema the code needs, it answers
`404 queue_unavailable`: the same as a gateway with no queue, so the browser falls
back to the synchronous path without telling the two apart.

- **`200`** — the result was already cached, in the same shape as `/analyze`.
  There is no job to create for work that is already done.
- **`202`** — queued. `Location` points at where to check.
- **`503 queue_full`** — twenty analyses are already waiting their turn. It comes
  with `Retry-After: 60`.

```json
{ "jobId": "9f2c1ab3-…", "status": "queued" }
```

Resubmitting the same analysis while the first is still running returns **the
same `jobId`**: the dedup key is the cache key, so the queue and the cache agree
on what counts as the same analysis.

### `GET /analyses/{id}` — gateway

Answers **`200`** in every case except a job that does not exist or belongs to
someone else, which are indistinguishable `404`s. An analysis that failed is not
a failed lookup, so the reason travels in the body:

```json
{ "jobId": "9f2c1ab3-…", "status": "running", "attempt": 1 }
{ "jobId": "9f2c1ab3-…", "status": "done",    "attempt": 1, "result": { … } }
{ "jobId": "9f2c1ab3-…", "status": "failed",  "attempt": 3,
  "error": { "code": "unreadable_cv", "message": "…" } }
```

### `POST /tailor/questions` — gateway

The first step of the tailored CV. The same body as `/analyze`, plus an optional
`locale` field (`en` or `es`) for the language of the questions. It returns the
questions and the text already extracted from the PDF, which the second step reuses
instead of parsing it again:

```json
{
  "questions": [
    { "topic": "Kubernetes", "question": "Have you run Kubernetes clusters? How large?" }
  ],
  "cvText": "Jane Doe · Backend engineer…"
}
```

### `POST /tailor/generate` — gateway

The second step. It takes JSON with the `cvText` from the first step, the posting and
the answers, and returns the tailored CV:

```json
{
  "cvText": "…",
  "jobOffer": "…",
  "jobTitle": "Backend Engineer",
  "answers": [{ "topic": "Kubernetes", "answer": "Two years running a 40-node cluster." }],
  "extra": "CKA certified in 2024."
}
```

The response is structured — `fullName`, `contact`, `headline`, `summary`,
`experience`, `skills`, `education`, `additional` — so the frontend can render it and
export it to PDF or Word. Neither step goes through the queue or the cache: both are
synchronous, behind the same circuit breaker and concurrency limit as `/analyze`.

### `GET /health` — gateway & AI service

Returns `{ "status": "ok" }`; the gateway's also carries `"revision"`, the commit it
was built from, so you can tell whether a deploy is live. Used by the Docker and
Render health checks.

---

## Observability and measurement

### One id across all three services

The gateway stamps every request with an `X-Request-ID`. An inbound one is reused
only if it survives sanitising — short and alphanumeric, since a header carrying a
newline could forge log lines — and otherwise a fresh 128-bit id is generated. That
id travels to the AI service in the same header, comes back to the browser on the
response, and appears on **every log line in both services**. A failure is followed
end to end with a single `grep`.

Logs are JSON in production and human-readable text in development. On the Python
side `structlog` and the standard library share one renderer, so even an unexpected
traceback from the model provider arrives with its `request_id` attached.

### Metrics

Both services expose `/metrics` in the Prometheus text format with the three RED
signals, plus the runtime collectors:

| Metric | What it measures |
|---|---|
| `http_requests_total{method,route,status}` | Rate and errors |
| `http_request_duration_seconds{method,route}` | Duration |
| `http_requests_in_flight` | Current concurrency |

Two details that matter:

- **The `route` label is always the registered template**, never the raw path.
  Labelling by path lets anything walking URLs mint a time series per request and
  eventually take the scrape down. Unregistered paths collapse into `unmatched`.
- **The histogram buckets reach 60s.** Library defaults stop at 10, and since a real
  analysis takes tens of seconds, every observation would land in the overflow
  bucket and the p95 would mean nothing.

`/metrics` carries no user data — that is exactly what the cardinality discipline
buys. Even so, a serious deployment would put it on a separate internal port rather
than the public one.

### Distributed tracing

An id lets you *find* the logs for an analysis. It does not tell you where the 55
seconds went. Both services emit OpenTelemetry traces over OTLP for that.

**The queue is the interesting part.** The request that enqueues finishes in
under a second; the work happens later, in another process, possibly after two
retries spread over two minutes. With nothing crossing that gap they are four
unrelated things. The job row stores the `traceparent` of whoever enqueued it,
and the worker resumes that trace instead of starting its own.

The first real trace in production found a bug at a glance:

```
POST /analyses              23.92 s   ← was meant to return in under a second
  └─ HTTP GET               23.08 s   ← /version, waiting on a sleeping AI service
analysis job  (attempt 1)    2 m      ← cut off by the job timeout
  └─ HTTP POST               2 m
analysis job  (attempt 2)   36.46 s
  └─ HTTP POST              36.29 s
analysis job  (attempt 3)    3.54 s   ← the AI service mid-redeploy
  └─ HTTP POST               3.37 s
```

The asynchronous endpoint existed so as not to depend on the AI service, and
the enqueue path depended on it synchronously: before writing the row it
fetched the prompt version to build the cache key, and with the service asleep
that call waited out the entire cold start. The 628 ms I had measured was with
the service awake.

The fix separates how long the caller waits from how long the work runs. The
request waits at most two seconds for the version and, failing that, enqueues
without a cache key; the fetch carries on in the background and wakes the
service for the worker, instead of being cancelled halfway through the start.
The worker does wait for the fresh version, because it is the one writing to
the cache: storing under a stale version would file the new prompt's answer
under the old prompt's key.

No amount of inference from timestamps had got there in a week of diagnosis.
The trace showed it at first sight.

Four decisions that make it useful:

- **The worker's span is a child of the enqueueing one, not a link.** The
  messaging conventions suggest a link when the consumer may run far later. The
  question here is "where did the 55 seconds go", and a parent-child waterfall
  answers it directly while a link asks you to open two traces and compare them.
- **Sampling is parent-based everywhere.** Once a trace is being recorded, every
  service records. Deciding separately produces traces with holes, which are
  worse than none because they look complete. The decision crosses the queue
  with the header.
- **The trace id is the correlation id** when the caller supplies none. The two
  were always the same width — `requestid.New()` was written with this day in
  mind. One number that finds the logs *and* the trace beats two that each find
  half.
- **Tracing off is the same code path with a sampler that records nothing**, not
  a branch around it. A second configuration that only runs when nobody is
  looking is one nobody tests. A job enqueued with no trace runs the same.

### Dashboards

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://tempo:4318   docker compose --profile observability up --build
```

Grafana lands on http://localhost:3001 (no login, it is local) with the RED dashboard
and the Tempo datasource already provisioned; Prometheus on http://localhost:9090.
In production the same two variables (`OTEL_EXPORTER_OTLP_ENDPOINT` and
`OTEL_EXPORTER_OTLP_HEADERS`) point at a hosted backend — free tier, like
everything else.

### Caching

A repeated analysis — the same CV against the same posting — does not pay for the
model again. The gateway caches in two tiers: an in-process LRU that absorbs the
repeats, and Redis for what a single process cannot have, entries shared between
replicas and entries that survive a restart. `singleflight` in front collapses
concurrent requests for the same key into **one** model call.

The key covers everything that can change the answer, including a fingerprint the
AI service derives from its own prompts and schema. Editing a prompt changes the
fingerprint and the old entries stop being addressed on their own: there is no
version number anyone has to remember to bump.

Redis is optional — without it the gateway still caches in process — and any cache
failure degrades to calling the upstream. The full reasoning is in
[ADR 0007](docs/adr/0007-cache-analyses-in-two-tiers.md).

```bash
docker compose up          # Redis is already part of the stack
```

### Baseline

**Gateway overhead**, measured against a stub upstream so the number is the
gateway's own work rather than model latency:

```bash
cd gateway && go test ./internal/handlers/ -run '^$' -bench BenchmarkAnalyzeHandler -benchtime 3s -count 3
```

```bash
cd gateway && go test ./internal/services/ -run '^$' -bench BenchmarkAnalysisCache -benchtime 2s -count 3
```

| Path | Latency | Memory | Allocations |
|---|---|---|---|
| Analysis (gateway overhead) | **~0.58 ms** | ~683 KB | ~305 |
| Cache hit (repeated analysis) | **~0.12 ms** | ~209 KB | ~53 |

<sub>Measured on a Ryzen 7 5825U. The gateway overhead covers multipart parsing,
validation, the upstream round trip and serialisation; it excludes JWT verification,
which needs a live JWKS endpoint. The cache hit covers reading the file, hashing it
and decoding the stored result.</sub>

What a cache hit replaces is not that 0.58 ms: it is the call to the model. Measured
end to end against the full stack, with a real Gemini call and the same analysis run
twice:

| Path | Observed latency |
|---|---|
| First analysis (miss → Gemini) | **~31.5 s** |
| Repeat (hit) | **< 10 ms** |

<sub>Read off the gateway histogram: of the two observations one landed in the
`le=0.01` bucket and the other between 30 and 45 s, summing to 31.54 s.</sub>

This is also what justifies the buckets reaching 60 s: with the library defaults,
which stop at 10, those 31.5 s would have landed in the overflow bucket and the p95
would have meant nothing.

The 683 KB per request is the interesting find: the gateway **buffers the whole CV in
memory** to forward it. At the 5 MB limit that is tens of megabytes under
concurrency. Noted here because only now is it visible.

**Sustained load** with k6, against a gateway pointed at the stub:

```bash
docker compose --profile loadtest up          # gateway on :8081 + stub
k6 run -e TOKEN="<access token>" load/k6/analyze.js
```

The token is a real Supabase one — the gateway verifies the signature against the
project JWKS and there is no way to mint one offline. Take it from a browser session
with `(await supabase.auth.getSession()).data.session.access_token`.


### The queue, on a full table

The queue's efficiency rested on comments. Explaining every statement the worker
runs, against 100,000 rows in steady state — mostly finished work, a thin layer
of live jobs, everything inside its retention window — found two periodic
queries reading the whole table to act on almost nothing, and a claim that was
fast **by accident**: an index built for something else served it, and widening
that index sent the claim to reading all 100,000 rows.

```bash
cd gateway && go test ./internal/jobs/ -run 'TestTheQueueQueries|TestAClaimReads' -v
```

| Query | How often | Before | After |
|---|---|---|---|
| claim | every poll | 2,002 rows, 1.6 ms | **3 rows, 0.2 ms** |
| depth gauge | every 15 s | 100,000 rows, 16.6 ms | **8,000 rows, 2.3 ms** |
| retention sweep | every 2 min | 100,000 rows, 15.5 ms | **0 rows, 0.08 ms** |

The claim now has an index of its own, ordered the way it reads, and stops at
the first row it can take. The tests assert the property — that no query scans
the table, and that a claim reads the live layer rather than the history —
without naming indexes, so a reasonable schema change does not break them; only
one that sends a query back to reading everything does.

At current traffic the table holds a few dozen rows and none of this shows in
production. That is the point: the cost is set by the design and proven by a
test, rather than holding because nobody has used it much yet. See
[ADR 0013](docs/adr/0013-hold-the-queue-plans-in-tests.md).


### Resilience: stop insisting, and say no in time

When the AI service stops answering — parked by the platform, redeploying, or
down with its model provider — every caller used to find out separately: a
synchronous request waited out the failure, the next one waited it out again,
and the worker spent a job's attempts on calls the previous ones had already
shown would fail.

**A shared circuit breaker** in front of everything that talks to the service —
analyses, tailoring, the version fetch — opens after five consecutive failures.
Open, the request path answers at once with 503 and `Retry-After`, and the
worker stops claiming jobs: they wait in the queue instead of spending attempts
on an outage already known. After 30 seconds a single trial call goes through —
a real job's call, which does spend an attempt — and if it succeeds, the breaker
closes. The readiness probe does not go through the breaker, because it is the
very thing that notices the service is back.

**Load shedding** where the free tier makes it necessary:

| Limit | Why | Past it |
|---|---|---|
| 8 synchronous calls in flight | each holds the CV in memory; the instance has 512 MB | 503 `overloaded` |
| 20 waiting jobs | each keeps its PDF in Postgres; the database has 500 MB | 503 `queue_full` |

Queue admission is part of the same `INSERT` that enqueues, so resubmitting work
already queued joins the existing job even when the queue is full: only new work
is refused. And a test on the Go side fails if the gateway can send an error
code the frontend has no message for — which is how two errors had already
ended up showing the generic message.
See [ADR 0015](docs/adr/0015-break-the-circuit-to-the-ai-service.md) and
[ADR 0016](docs/adr/0016-turn-away-what-cannot-be-served.md).

---

## Testing

```bash
cd frontend && npm test
cd gateway && go test ./...
cd ai-service && pip install -r requirements-dev.txt && pytest
```

All three suites run **offline and for free** — they never call the live LLM. The gateway tests sign their own ES256 tokens and stub the AI service with `httptest`; the Python tests inject a fake LangChain chain and build real one-page PDFs with `reportlab` to exercise the extraction boundary. The tests target the parts most likely to break quietly: token verification, the verdict bands, and "what happens when the PDF is garbage".

The frontend tests cover the gateway client: polling, the fallback to the synchronous path, waking the AI service and the error map. The queue and schema tests start a real Postgres with testcontainers and apply every migration to it; without Docker they skip on your machine and fail on CI.

> The Python tests target 3.11 (what the Dockerfile uses). On a much newer interpreter you may not get prebuilt wheels for the pinned deps.

---

## Deploying

- **Frontend → Vercel.** Import `frontend/`, set the `NEXT_PUBLIC_*` vars and deploy; Vercel picks Node 22 up from `engines` in `package.json`. `NEXT_PUBLIC_AI_SERVICE_URL` is the AI service's URL, so the browser can wake it before an analysis needs it. Add the production callback URL to the Supabase redirect allow list and CORS origins.
- **Gateway + AI service → Render.** Two Web Services from this repo, each pointing at its Dockerfile. Set each service's env from its `.env.example`, point `AI_SERVICE_URL` at the deployed AI service, and point the frontend's `NEXT_PUBLIC_GATEWAY_URL` at the deployed gateway. On the gateway, `DATABASE_URL` (the pooler) and `RUN_WORKER=true` turn the queue on with the worker in the same process; `cmd/worker` is there to run it separately once there is room for another service. `REDIS_URL` (Redis Cloud, `rediss://`) and the `OTEL_EXPORTER_OTLP_*` variables (Grafana Cloud) are optional. If you use `INTERNAL_API_KEY`, it has to match on both services.
- **Supabase** is already managed — just keep using the same project. Migrations go in by hand through the SQL Editor, **before** pushing the code that needs them; the steps are in the [runbook](docs/runbook.md#adding-a-migration).

---

## Gotchas I hit (so you don't)

These all cost me real time while building it, so they're worth writing down:

- **Redirects landing on `http://0.0.0.0:3000`.** The Next standalone server binds to `0.0.0.0` inside the container, and `request.url` reflects that — so any redirect built from it (OAuth callback, sign-out, the auth guard) sent the browser to an address it can't open. The fix is to rebuild redirect URLs from the `Host` / `x-forwarded-host` header instead of `request.url`.
- **A 401 on every analysis.** Classic HS256-with-a-shared-secret verification rejects modern Supabase tokens, which are ES256. The signature isn't "wrong" — you're checking it the wrong way. Verify against the JWKS.
- **A 404 from Gemini that looks like a bad key.** `gemini-1.5-flash` got retired; calls fail with *"model not found"*, which reads like an auth problem but isn't. Point at a current model (`gemini-2.5-flash`) and use the `ListModels` endpoint to see what your key can actually call.
- **The gateway container stuck on "unhealthy".** The health check used `wget --spider`, which sends a `HEAD` request. Gin only registers `GET /health`, so `HEAD` 404'd and the container never went healthy — which blocked everything that depended on it. The fix is a `GET`-based health check.
- **A CV showing "0.0 MB".** Formatting every file size in MB rounds a normal few-hundred-KB PDF to zero. Show KB under 1 MB.

---

## Decisions and trade-offs

The summary is here; the full log — one decision per file, with its context and
consequences — lives in [`docs/adr/`](docs/adr/). The [runbook](docs/runbook.md) has the operational half: what to look at when something breaks.

A few choices I'd defend, and the cost of each:

- **Three services for a CV tool is overkill, and that's the point.** A single Next.js app would be less to run. I split it so the LLM work, the secured edge, and the UI deploy and fail independently — and so the repo reads like production, not a demo. If this were a real product on a budget, I'd probably start merged and split later.
- **Direct-to-Supabase CRUD + RLS, instead of routing everything through the gateway.** This is the Supabase-native pattern: less glue code, authorization centralized in the database, lower latency. The trade-off is that the table/column shape is visible to the client and the frontend is coupled to Supabase's API. If I needed to hide the schema, run multi-step transactions, or add heavier server-side rules, I'd move that CRUD behind the gateway. For per-user CRUD guarded by RLS, the direct path is the right call.
- **The queue lives in Postgres, not a broker.** `SELECT … FOR UPDATE SKIP LOCKED` gives exactly the semantics needed — each job taken by one worker, with no worker waiting on another — and adds no infrastructure to pay for or keep alive on a free tier. The cost is polling instead of push, which does not show at this volume, and that the gateway now holds a database credential.
- **The properties that matter are held by a test, not a comment.** That no queue query reads the whole table, that every migration can be pasted twice, that every error code has a message in the client: each of those fails CI if it breaks. Several of them were written down as comments and turned out to be false when somebody measured.
- **Wake the AI service from the browser.** On the free tier the platform parks it, and a request from the gateway does not wake it while one from outside does. It is a workaround for observed, undocumented behaviour that could stop working without notice — which is why ADR 0014 says so.
- **Deriving the verdict from the score in code, not the model.** Anything I can compute deterministically, I don't ask the LLM for. One less thing to second-guess.
- **Best-effort CV storage.** If the Storage upload fails, the analysis is still saved (without a downloadable file) and the UI says so quietly. A storage hiccup shouldn't cost you the analysis you just waited for.

---

## What I'd add next

- **A Postgres role of the gateway's own**, with grants on the queue and the migration record only. Today it uses the database owner's.
- **Save the result to the history from the server.** Today the browser writes it when polling finishes; if the person closed the tab first, the analysis still runs but never shows up in their history — it sits on the job and in the cache, and asking again is instant, but it is not visible.
- **Wake the AI service through a route of the frontend's own**, so ad blockers cannot cut the ping. First, measure whether Vercel's servers wake it the way a browser does or get the same 429 the gateway does.
- An undo on delete, instead of — or as well as — the confirmation dialog.
- Generated Supabase types, to remove the one `unknown` cast in the data layer.

---

## License

MIT — do what you like with it.
