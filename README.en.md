# Papyrus

*Versión en español: [README.md](README.md).*

Papyrus tells you whether your CV actually fits a job before you spend an afternoon tailoring it. You drop in a PDF, paste the posting, and it comes back with a 0–100 compatibility score, the skills you already cover, the ones you're missing, and a short list of concrete edits for *that* specific role.

I built it as a proper full-stack exercise: three independently deployable services, a real Google sign-in flow, Row Level Security doing the authorization, and an LLM doing the actual reasoning. The rule I set for myself was that nothing could be faked — no mock data, no half-wired buttons, no "TODO: handle errors". It either works end to end or it isn't in here.

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
- **History** — every analysis is saved. You can reopen any of them, download the original CV (signed URL, private bucket), or delete it (with a confirmation, because it also wipes the stored file).
- **Bilingual content** — the analysis itself is generated in English *and* Spanish in a single model call, so switching the UI language re-renders the result instantly without re-running anything.
- **Light / dark theme** — follows your OS by default, with a manual toggle.
- **Google sign-in** — via Supabase Auth. No passwords to store.

Everything runs on free tiers. There are no paid API keys anywhere in the stack.

---

## Architecture

Three services, one clear rule: **the gateway never touches the database, and the AI service holds no state.** Persistence lives entirely in Supabase and is reached straight from the browser through Row Level Security, so every read and write is automatically scoped to the signed-in user — no database credentials ever sit on a server I run.

That leaves each service with exactly one job: the frontend renders and owns the user's data, the gateway is the secured edge for the one operation that needs a secret, and the AI service is a pure function from `(CV, posting)` to `analysis`.

```
                            ┌──────────────────────────────────────────────┐
                            │                  Supabase                     │
                            │   Postgres · Auth (Google) · Storage           │
                            │   RLS-scoped: profiles, analyses, cvs          │
                            └──────────────────────────────────────────────┘
                               ▲                                  ▲
                  Google OAuth │                   reads / writes │  publishable key
                   + JWT (ES256)│                  under RLS      │  + user JWT
                               │                                  │
        ┌──────────────────────┴──────────────┐                  │
        │              Frontend                │──────────────────┘
        │      Next.js 14 · Tailwind · i18n    │
        │   Landing · Auth · Dashboard · Hist. │
        └──────────────────┬───────────────────┘
                           │  POST /analyze
                           │  multipart: cv (PDF) + jobOffer
                           │  Authorization: Bearer <supabase JWT>
                           ▼
        ┌──────────────────────────────────────┐
        │             API Gateway               │
        │              Go · Gin                 │
        │  CORS · per-user rate limit           │
        │  JWT verify via JWKS · upload checks  │
        └──────────────────┬───────────────────┘
                           │  POST /analyze  (internal, server-to-server)
                           ▼
        ┌──────────────────────────────────────┐
        │             AI Service                │
        │       Python · FastAPI · LangChain    │
        │  PDF → prompt → Gemini → JSON          │
        │  bilingual, structured, stateless      │
        └──────────────────────────────────────┘
```

The split is honestly a bit much for a CV tool — you could collapse this into one Next.js app with a couple of route handlers. I kept it separate on purpose: the LLM layer, the request edge, and the UI each scale and fail differently, and I wanted the project to look like something you'd actually run in production rather than a weekend toy. See [Decisions and trade-offs](#decisions-and-trade-offs) for the honest version of that.

---

## The request, end to end

1. You sign in with Google. Supabase runs the OAuth dance and drops a session into HTTP-only cookies. Middleware refreshes that session on every request and guards `/dashboard`.
2. You upload a CV and paste a posting. The browser sends both to the gateway as `multipart/form-data` with the Supabase access token attached.
3. The gateway verifies the token's signature against your project's **JWKS** (Supabase signs with ES256 now, not a shared secret), enforces a per-user rate limit, checks the upload is a PDF under the size cap, and forwards the request to the AI service.
4. The AI service extracts the CV text with `pypdf`, builds a structured prompt, and asks Gemini — through LangChain's `with_structured_output` — for a validated, bilingual JSON object. The verdict band is computed from the score in Python, not trusted to the model.
5. The browser renders the result, uploads the CV to a private Storage bucket, and writes the analysis (linked to that file) into the `analyses` table. RLS guarantees you only ever see your own rows and files. The original CV stays downloadable later through a short-lived signed URL.

---

## Tech stack

| Layer       | What's in it                                                                 |
|-------------|------------------------------------------------------------------------------|
| Frontend    | Next.js 14 (App Router), TypeScript (strict), Tailwind CSS, `@supabase/ssr`, `next-themes`, cookie-based i18n, `zod` for env validation |
| Gateway     | Go 1.22, Gin, `golang-jwt/v5` with hand-rolled JWKS verification, `golang.org/x/time/rate` |
| AI service  | Python 3.11, FastAPI, LangChain (`langchain-core` + `langchain-google-genai`), `pypdf`, `pydantic-settings` |
| Database    | Supabase (PostgreSQL) with Row Level Security + private Storage               |
| LLM         | Google Gemini (`gemini-2.5-flash`, free tier)                                |
| Local dev   | Docker + Docker Compose                                                      |
| Deploy      | Vercel (frontend) · Render (gateway + AI service) · Supabase (managed)       |

---

## Project layout

```
papyrus/
├── frontend/                    # Next.js 14 app
│   ├── app/                     # routes: landing, /dashboard, /auth/{callback,signout}, icon.svg
│   ├── components/
│   │   ├── analysis/            # score ring, skill lists, verdict badge, suggestions
│   │   ├── dashboard/           # workspace, form, dropzone, history, result states
│   │   ├── marketing/           # header, footer, hero preview, scroll reveal
│   │   └── ui/                  # button, language/theme toggles, confirm dialog
│   └── lib/
│       ├── analyses/, cvs/      # Supabase data access (the "repositories")
│       ├── api/                 # gateway client
│       ├── i18n/                # dictionaries (en/es) + server & client helpers
│       └── supabase/            # browser / server / middleware clients
├── gateway/                     # Go + Gin edge
│   ├── cmd/server/              # entrypoint + graceful shutdown
│   └── internal/
│       ├── auth/                # JWKS fetch + cache, ES256/RS256 verification
│       ├── config/              # env loading + validation (fail fast)
│       ├── handlers/, router/   # /analyze, /health, engine assembly
│       ├── middleware/          # CORS, auth, rate limiting
│       └── services/, transport/, httpx/
├── ai-service/                  # Python + FastAPI
│   ├── app/{api,core,schemas,services}/
│   └── tests/                   # offline, model chain is faked
├── supabase/migrations/         # 0001 schema+RLS, 0002 storage, 0003 bilingual
├── docker-compose.yml
└── .env.example
```

---

## Running it locally

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) + Compose v2
- A free [Supabase](https://supabase.com) project
- A free [Google AI Studio](https://aistudio.google.com/app/apikey) key

If you want to run a service outside Docker you'll also need Node 20+, Go 1.22+, or Python 3.11+ depending on which one.

### 1. Supabase (this is the only fiddly part)

**Schema.** Open the SQL Editor and run, in order:

- [`0001_init.sql`](supabase/migrations/0001_init.sql) — `profiles`, `analyses`, `cvs`, the RLS policies, and a trigger that creates a profile row on sign-up.
- [`0002_cv_storage.sql`](supabase/migrations/0002_cv_storage.sql) — the private `cvs` Storage bucket and owner-scoped object policies (`<user-id>/<cv-id>.pdf`).
- [`0003_bilingual_analyses.sql`](supabase/migrations/0003_bilingual_analyses.sql) — only needed if your DB predates the bilingual change; it's a guarded no-op on a fresh install.

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
cp .env.example .env     # fill in the four real values; the rest have sane defaults
docker compose up --build
```

| Service     | URL                     |
|-------------|-------------------------|
| Frontend    | http://localhost:3000   |
| Gateway     | http://localhost:8080   |
| AI service  | http://localhost:8000   |

Only four variables actually need real values — `NEXT_PUBLIC_SUPABASE_URL`, `NEXT_PUBLIC_SUPABASE_ANON_KEY`, and `GEMINI_API_KEY`. Compose will warn you by name if any are missing. Stop everything with `docker compose down`.

> One catch: Next inlines `NEXT_PUBLIC_*` at **build** time, so if you change those you have to `docker compose up --build` again — a restart won't pick them up.

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

### `GET /health` — gateway & AI service

Returns `{ "status": "ok" }`. Used by the Docker and Render health checks.

---

## Testing

```bash
cd gateway && go test ./...
cd ai-service && pip install -r requirements-dev.txt && pytest
```

Both suites run **offline and for free** — they never call the live LLM. The gateway tests sign their own ES256 tokens and stub the AI service with `httptest`; the Python tests inject a fake LangChain chain and build real one-page PDFs with `reportlab` to exercise the extraction boundary. The tests target the parts most likely to break quietly: token verification, the verdict bands, and "what happens when the PDF is garbage".

> The Python tests target 3.11 (what the Dockerfile uses). On a much newer interpreter you may not get prebuilt wheels for the pinned deps.

---

## Deploying

- **Frontend → Vercel.** Import `frontend/`, set the `NEXT_PUBLIC_*` and `NEXT_PUBLIC_GATEWAY_URL` vars, deploy. Add the production callback URL to the Supabase redirect allow list and CORS origins.
- **Gateway + AI service → Render.** Two Web Services from this repo, each pointing at its Dockerfile. Set each service's env from its `.env.example`, point `AI_SERVICE_URL` at the deployed AI service, and point the frontend's `NEXT_PUBLIC_GATEWAY_URL` at the deployed gateway.
- **Supabase** is already managed — just keep using the same project.

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

A few choices I'd defend, and the cost of each:

- **Three services for a CV tool is overkill, and that's the point.** A single Next.js app would be less to run. I split it so the LLM work, the secured edge, and the UI deploy and fail independently — and so the repo reads like production, not a demo. If this were a real product on a budget, I'd probably start merged and split later.
- **Direct-to-Supabase CRUD + RLS, instead of routing everything through the gateway.** This is the Supabase-native pattern: less glue code, authorization centralized in the database, lower latency. The trade-off is that the table/column shape is visible to the client and the frontend is coupled to Supabase's API. If I needed to hide the schema, run multi-step transactions, or add heavier server-side rules, I'd move that CRUD behind the gateway. For per-user CRUD guarded by RLS, the direct path is the right call.
- **Deriving the verdict from the score in code, not the model.** Anything I can compute deterministically, I don't ask the LLM for. One less thing to second-guess.
- **Best-effort CV storage.** If the Storage upload fails, the analysis is still saved (without a downloadable file) and the UI says so quietly. A storage hiccup shouldn't cost you the analysis you just waited for.

---

## What I'd add next

- An undo on delete (right now it confirms, then it's gone) instead of, or alongside, the confirmation dialog.
- Caching identical `(CV, posting)` pairs so re-runs are free and instant.
- Generated Supabase types to drop the one `unknown` cast in the data layer.
- A lightweight rate-limit / abuse story for the public deployment beyond the per-user budget.

---

## License

MIT — do what you like with it.
