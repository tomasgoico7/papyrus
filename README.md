# Papyrus

*English version: [README.en.md](README.en.md).*

Papyrus mide qué tan bien encaja tu CV con un puesto antes de que te pases una tarde adaptándolo a mano. Subís tu CV en PDF y pegás la descripción de la oferta; en segundos obtenés un score de compatibilidad del 0 al 100, un veredicto (alta / media / baja), las skills que el puesto pide y vos ya cubrís, las que te faltan, y una lista corta de cambios concretos para acercar tu CV a *ese* puesto en particular. La idea es dejar de adivinar si vale la pena postularte y saber con datos qué conviene ajustar.

Lo construí como un proyecto full-stack completo y real: tres servicios que se deployan por separado, login con Google, Row Level Security haciendo la autorización a nivel de base de datos, y un LLM haciendo el razonamiento de verdad. La regla que me puse fue que nada estuviera simulado — sin datos de mentira, sin botones a medio cablear, sin "TODO: manejar errores". Funciona de punta a punta o no está acá.

La interfaz es bilingüe (español / inglés), tiene tema claro y oscuro, y trata de correrse del medio para que lo único que importe en pantalla sea el análisis.

---

## Índice

- [Características](#características)
- [Arquitectura](#arquitectura)
- [El request, de punta a punta](#el-request-de-punta-a-punta)
- [Stack](#stack)
- [Estructura del proyecto](#estructura-del-proyecto)
- [Correrlo localmente](#correrlo-localmente)
- [Cómo funcionan algunas cosas por dentro](#cómo-funcionan-algunas-cosas-por-dentro)
- [Referencia de la API](#referencia-de-la-api)
- [Observabilidad y medición](#observabilidad-y-medición)
- [Tests](#tests)
- [Deploy](#deploy)
- [Bugs que aparecieron](#bugs-que-aparecieron-para-que-a-vos-no)
- [Decisiones y trade-offs](#decisiones-y-trade-offs)
- [Qué le agregaría](#qué-le-agregaría)

---

## Características

- **Score de compatibilidad** — subís un CV (PDF) y una oferta, y obtenés un score de ajuste del 0 al 100 más un veredicto `strong` / `moderate` / `weak`.
- **Skills que coinciden vs. las que faltan** — el modelo separa lo que pide la oferta entre lo que tu CV ya demuestra y lo que no.
- **Sugerencias accionables** — de dos a cinco cambios concretos para ese puesto, ordenados por impacto, que nunca inventan experiencia que no tenés.
- **CV adaptado al puesto** — antes de escribir nada, el modelo te pregunta por lo que la oferta pide y tu CV no muestra. Con tus respuestas arma una versión del CV para ese puesto, que descargás en PDF o en Word. Lo que marcás como "No tengo esto" no se agrega: no inventa experiencia. Queda guardado con el análisis, así que reabrirlo no lo regenera.
- **Historial con búsqueda y filtros** — cada análisis queda guardado. Podés buscarlos por puesto y filtrarlos por compatibilidad, reabrir cualquiera, reusar un CV ya subido para medirlo contra otra oferta, descargar el CV original (URL firmada, bucket privado), o eliminarlo (con confirmación, porque también borra el archivo guardado).
- **Exportar a PDF** — descargás cualquier análisis como un PDF prolijo y bilingüe (texto seleccionable), generado en el navegador.
- **Compartir por link** — generás un link público con vencimiento para que cualquiera vea el análisis (nunca tu CV), revocable cuando quieras.
- **Contenido bilingüe** — el análisis se genera en inglés *y* español en una sola llamada al modelo, así que cambiar el idioma de la UI re-renderiza el resultado al instante sin volver a correr nada.
- **Tema claro / oscuro** — sigue tu sistema operativo por defecto, con un toggle manual.
- **Login con Google** — vía Supabase Auth. Sin contraseñas que guardar.

Todo corre en free tiers. No hay ninguna API key paga en todo el stack.

---

## Arquitectura

Tres servicios y una cola. Los datos del usuario viven en Supabase y el browser los lee y escribe directo, bajo Row Level Security: cada fila queda scopeada al usuario logueado sin que ningún servidor intermedie. El gateway es el borde seguro para lo que necesita un secreto —hablar con el modelo—, y el servicio de IA es una función de `(CV, oferta)` a `análisis`, sin estado.

Al principio la regla era que el gateway no tocara la base. Desde que los análisis son asíncronos toca exactamente dos tablas —la cola de trabajos y el registro de migraciones—, con RLS activado y sin políticas, así que el browser no puede verlas. Eso significa que el gateway tiene una credencial de Postgres, y hoy es la del rol dueño de la base: podría leer más de lo que lee. Darle un rol propio con permisos solo sobre esas dos tablas está anotado como pendiente.

```
                  ┌───────────────────────────────────────────────────┐
                  │                      Supabase                     │
                  │   Postgres · Auth (Google) · Storage              │
                  │   profiles · analyses · cvs   ← el browser, RLS   │
                  │   analysis_jobs · schema_migrations  ← el gateway │
                  └───────────────────────────────────────────────────┘
                     ▲                                    ▲
         OAuth + JWT │ lecturas / escrituras bajo RLS     │ cola: SKIP LOCKED
                     │                                    │
    ┌────────────────┴───────────────┐                    │
    │            Frontend            │··········································┐
    │   Next.js 14 · Vercel          │                    │   GET /health       :
    └───────────────┬────────────────┘                    │   (lo despierta)    :
                    │ POST /analyses → 202                │                     :
                    │ GET /analyses/{id}  (sondeo)        │                     :
                    ▼                                     │                     :
    ┌──────────────────────────────────────────────┐      │                     :
    │           API Gateway  ·  Go · Gin           ├──────┘                     :
    │   JWKS · rate limit · load shedding          │                            :
    │   caché · cola + worker · circuit breaker    ├── Redis: caché, rate limit :
    └───────────────────────┬──────────────────────┘                            :
                            │ análisis · versión  (breaker)                     :
                            │ GET /health  (readiness)                          :
                            ▼                                                   :
    ┌──────────────────────────────────────────────┐                            :
    │       AI Service  ·  Python · FastAPI        │◀···························┘
    │   PDF → prompt → Gemini → JSON bilingüe      │
    └──────────────────────────────────────────────┘

      Cada salto lleva la misma traza OpenTelemetry, la cola incluida.
```

El split, siendo honesto, es mucho para una herramienta de CVs — podrías colapsar todo en una sola app de Next.js. Lo dejé separado a propósito: la capa del LLM, el borde de requests y la UI escalan y fallan distinto, y el proyecto existe para trabajar esos problemas de verdad. La versión honesta está en [Decisiones y trade-offs](#decisiones-y-trade-offs).

---

## El request, de punta a punta

1. **Login.** Supabase hace el OAuth con Google y deja la sesión en cookies HTTP-only. El middleware la refresca en cada request y protege `/dashboard`.
2. **Abrís el workspace.** El browser le hace un `GET /health` directo al servicio de IA para despertarlo: en el free tier se duerme, y un request desde el gateway no lo despierta, pero uno desde afuera de la plataforma sí.
3. **Enviás un CV y una oferta** a `POST /analyses`. El gateway verifica el JWT contra el JWKS de Supabase, aplica el rate limit por usuario (compartido entre réplicas vía Redis) y busca el resultado en el caché. Si el mismo análisis ya se hizo, vuelve al instante. Si no, lo encola en Postgres y responde `202` con el id del job — en menos de un segundo, aunque el servicio de IA esté dormido.
4. **El worker** reclama el job con `SELECT … FOR UPDATE SKIP LOCKED` y llama al servicio de IA detrás de un circuit breaker. Si recibe un 429 de un arranque en frío, sondea `/health` hasta que el servicio contesta y reintenta a los dos segundos. El resultado queda en el job y en el caché.
5. **El servicio de IA** extrae el texto con `pypdf` y le pide a Gemini, con el `with_structured_output` de LangChain, un JSON validado y bilingüe. El veredicto se calcula del score en Python; no se le confía al modelo.
6. **El browser** sondea `GET /analyses/{id}` con un intervalo creciente, recibe el resultado, sube el CV a un bucket privado de Storage y escribe el análisis en `analyses`, bajo RLS.

Si el gateway no tiene cola —o la base todavía no tiene el esquema que el código necesita—, `POST /analyses` responde 404 y el browser cae a `POST /analyze`, el camino síncrono original. El usuario igual obtiene su análisis.

---

## Stack

| Capa        | Qué tiene                                                                    |
|-------------|------------------------------------------------------------------------------|
| Frontend    | Next.js 14 (App Router), TypeScript strict, Tailwind CSS, `@supabase/ssr`, `next-themes`, i18n por cookie, `zod`, Vitest |
| Gateway     | Go 1.26, Gin, `pgx/v5`, `go-redis/v9`, `golang-jwt/v5` con JWKS hecho a mano, OpenTelemetry, Prometheus, testcontainers |
| Servicio IA | Python 3.11, FastAPI, LangChain (`langchain-core` + `langchain-google-genai`), `pypdf`, `pydantic-settings`, `structlog`, OpenTelemetry |
| Datos       | Supabase (PostgreSQL con RLS + Storage privado) · Redis (caché y rate limit compartidos) |
| LLM         | Google Gemini (`gemini-2.5-flash`, free tier)                               |
| Observabilidad | Trazas OTLP a Grafana Cloud (Tempo) · métricas Prometheus · logs JSON con `trace_id` |
| Dev local   | Docker Compose, con perfiles para observabilidad (Prometheus, Grafana, Tempo), dos réplicas y carga (k6) |
| Deploy      | Vercel (frontend) · Render (gateway + servicio IA) · Supabase · Redis Cloud · Grafana Cloud — todo free tier |

---

## Estructura del proyecto

```
papyrus/
├── frontend/                    # app Next.js 14, Node 22
│   ├── app/                     # rutas: landing, /dashboard, /share/[token], /auth/{callback,signout}
│   ├── components/
│   │   ├── analysis/            # anillo de score, skills, veredicto, sugerencias, CV adaptado
│   │   ├── auth/, share/        # login y logout, la vista pública de un análisis compartido
│   │   ├── dashboard/           # workspace, formulario, dropzone, historial, estados de resultado
│   │   ├── marketing/           # header, footer, preview del hero, reveal al scrollear
│   │   └── ui/                  # botón, toggles de idioma/tema, diálogo de confirmación
│   └── lib/
│       ├── analyses/, cvs/      # acceso a datos de Supabase (los "repositorios")
│       ├── api/                 # cliente del gateway, sondeo de jobs, despertar el servicio de IA
│       ├── i18n/                # diccionarios (en/es) + helpers de server y cliente
│       └── supabase/            # clientes browser / server / middleware
├── gateway/                     # borde Go + Gin
│   ├── cmd/server/              # API: entrypoint + graceful shutdown
│   ├── cmd/worker/              # worker de la cola como servicio propio
│   └── internal/
│       ├── app/                 # composition root: el wiring que comparten API y worker
│       ├── auth/                # fetch + caché de JWKS, verificación ES256/RS256
│       ├── breaker/             # circuit breaker delante del servicio de IA
│       ├── buildinfo/           # qué revisión está corriendo, expuesta en /health
│       ├── cache/               # store de bytes: LRU en proceso, Redis, y el tier
│       ├── config/              # carga + validación del entorno (fail fast)
│       ├── handlers/, router/   # /analyze, /analyses, /tailor, /health, /metrics, armado del engine
│       ├── jobs/                # cola en Postgres, reclamada con SKIP LOCKED
│       ├── middleware/          # CORS, auth, rate limiting, load shedding, request id
│       ├── observability/       # logger estructurado, métricas RED y trazas
│       ├── ratelimit/           # presupuesto por usuario: local, en Redis, y el fallback
│       ├── requestid/           # id de correlación y su transporte por contexto
│       ├── schema/              # versión del esquema y el gate que apaga la cola
│       ├── worker/              # loop de la cola, backoff con jitter, dead letters
│       └── services/, transport/, httpx/
├── ai-service/                  # Python + FastAPI
│   ├── app/{api,core,schemas,services}/
│   └── tests/                   # offline, la cadena del modelo está fakeada
├── supabase/migrations/         # 0001–0005 datos del usuario · 0006–0009 la cola y el registro de migraciones
├── docs/                        # adr/ con las decisiones de arquitectura, runbook.md para operarlo
├── ops/                         # config de Prometheus y dashboards de Grafana
├── load/                        # escenario de k6 + stub del servicio de IA
├── docker-compose.yml
└── .env.example
```

---

## Correrlo localmente

### Prerequisitos

- [Docker](https://docs.docker.com/get-docker/) + Compose v2
- Un proyecto gratis de [Supabase](https://supabase.com)
- Una key gratis de [Google AI Studio](https://aistudio.google.com/app/apikey)

Si querés correr un servicio fuera de Docker vas a necesitar además Node 22, Go 1.26+ o Python 3.11+, según cuál.

### 1. Supabase (esta es la única parte tediosa)

**Esquema.** Abrí el SQL Editor y corré, en orden:

- [`0001_init.sql`](supabase/migrations/0001_init.sql) — `profiles`, `analyses`, `cvs`, las políticas RLS, y un trigger que crea la fila de perfil al registrarse.
- [`0002_cv_storage.sql`](supabase/migrations/0002_cv_storage.sql) — el bucket privado `cvs` de Storage y las políticas de objetos scopeadas al dueño (`<user-id>/<cv-id>.pdf`).
- [`0003_bilingual_analyses.sql`](supabase/migrations/0003_bilingual_analyses.sql) — solo hace falta si tu base es anterior al cambio bilingüe; en una instalación nueva es un no-op protegido.
- [`0004_analysis_sharing.sql`](supabase/migrations/0004_analysis_sharing.sql) — las columnas de compartir (`share_token`, `share_expires_at`) y la función `get_shared_analysis` (`security definer`) que sirve un análisis por link público sin saltarse la RLS.
- [`0005_tailored_cv.sql`](supabase/migrations/0005_tailored_cv.sql) — la columna `tailored_cv` en `analyses`, donde queda el CV adaptado para reabrirlo sin regenerarlo.
- [`0006_analysis_jobs.sql`](supabase/migrations/0006_analysis_jobs.sql) — la cola de análisis, `analysis_jobs`, con RLS activado y sin políticas: el browser no la ve.
- [`0007_analysis_jobs_traceparent.sql`](supabase/migrations/0007_analysis_jobs_traceparent.sql) — el `traceparent` que lleva la traza de un análisis a través de la cola.
- [`0008_schema_migrations.sql`](supabase/migrations/0008_schema_migrations.sql) — el registro de migraciones aplicadas, que el gateway compara con la versión que necesita.
- [`0009_index_the_queue_for_its_queries.sql`](supabase/migrations/0009_index_the_queue_for_its_queries.sql) — los índices de las consultas de la cola.

Las cuatro últimas solo las usa [la cola](#la-cola-opcional), pero correrlas igual no cuesta nada. Cualquiera se puede pegar dos veces sin romper nada; hay un test que lo exige.

**Auth de Google.** Esta es la parte que lleva unos minutos. En la [Google Cloud Console](https://console.cloud.google.com/apis/credentials), configurá la pantalla de consentimiento de OAuth (External), después creá un *ID de cliente de OAuth → Aplicación web* con este URI de redireccionamiento autorizado:

```
https://<tu-project-ref>.supabase.co/auth/v1/callback
```

Copiá el Client ID y el Client Secret en **Supabase → Authentication → Providers → Google** y activalo. Después, en **Authentication → URL Configuration**, poné el Site URL en `http://localhost:3000` y agregá `http://localhost:3000/auth/callback` a la lista de redirects permitidos.

**Valores que vas a necesitar** (Project Settings → API):

- `Project URL` → `NEXT_PUBLIC_SUPABASE_URL`
- la key `anon` / `publishable` → `NEXT_PUBLIC_SUPABASE_ANON_KEY`

Ojo que no hay ningún JWT secret para copiar. El gateway verifica los tokens contra el JWKS público del proyecto (`<Project URL>/auth/v1/.well-known/jwks.json`), que deriva de `NEXT_PUBLIC_SUPABASE_URL` — así que ese único valor cumple doble función.

### 2. Key de Gemini

Creá una en [AI Studio](https://aistudio.google.com/app/apikey) → `GEMINI_API_KEY`. Asegurate de que sea una **API key** de verdad (arranca con `AIza…`), no un token OAuth. El modelo por defecto es `gemini-2.5-flash` — gratis, rápido, y soporta el structured output del que depende esto.

### 3. Entorno + correrlo

```bash
cp .env.example .env     # completá los tres valores reales; el resto tiene defaults razonables
docker compose up --build
```

| Servicio    | URL                     |
|-------------|-------------------------|
| Frontend    | http://localhost:3000   |
| Gateway     | http://localhost:8080   |
| Servicio IA | http://localhost:8000   |

Solo tres variables necesitan valores reales — `NEXT_PUBLIC_SUPABASE_URL`, `NEXT_PUBLIC_SUPABASE_ANON_KEY` y `GEMINI_API_KEY`. Compose te avisa por nombre si falta alguna. Frenás todo con `docker compose down`.

> Un detalle: Next "hornea" las `NEXT_PUBLIC_*` en tiempo de **build**, así que si las cambiás tenés que hacer `docker compose up --build` de nuevo — un restart no las toma.

### La cola (opcional)

Con lo de arriba ya anda todo: sin `DATABASE_URL`, el gateway no registra `POST /analyses` y el frontend usa el camino síncrono. Para probar la cola:

1. Corré las migraciones 0006 a 0009, si no lo hiciste.
2. En `.env`, poné en `DATABASE_URL` la URL del *transaction pooler* (Supabase → Project Settings → Database → Connection pooling) y `RUN_WORKER=true`, para que el worker corra dentro del gateway.
3. Levantá todo de nuevo con `docker compose up`.

Al arrancar, el gateway compara la versión del esquema con la que necesita. Si falta una migración lo dice en el log y deja la cola apagada; el frontend sigue por el camino síncrono, y la cola se prende sola en menos de un minuto cuando la aplicás.

### Correr un solo servicio

Cada servicio lee su propio `.env` y corre solo. Este es el loop rápido cuando estás trabajando en uno:

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

## Cómo funcionan algunas cosas por dentro

### Auth — JWKS, no un secreto compartido

Supabase antes firmaba los access tokens con HS256 y un secreto compartido que copiabas en tu backend. Los proyectos nuevos firman con **claves asimétricas (ES256)** y publican la mitad pública en un endpoint JWKS. El gateway baja ese set de claves, lo cachea, y verifica la firma de cada token contra la clave que coincide con su `kid` — refrescando el caché si ve un `kid` desconocido, para que rotar las claves no requiera un redeploy. Los algoritmos aceptados están fijados a `ES256`/`RS256` para esquivar ataques de confusión de algoritmo. Todo con la librería estándar de Go; sin dependencias extra.

### Análisis bilingüe — una llamada, dos idiomas

Al modelo se le pide cada campo de texto (`summary`, el `title`/`detail` de cada sugerencia, y las listas de skills) en inglés y español a la vez, como `{ "en": …, "es": … }`. Se guardan las dos versiones, y la UI lee la mitad que coincide con el idioma actual. Por eso cambiar EN/ES re-renderiza al instante — sin un segundo request, sin re-analizar. El costo es más o menos el doble de tokens de salida por análisis, que en el free tier no es un problema. El score y el veredicto son neutrales al idioma y no se mueven.

### Por qué el browser habla directo con Supabase

El frontend le pega a las APIs REST y de Storage de Supabase directo desde el cliente — no hay un proxy adelante del CRUD. Es a propósito, y es seguro porque **la autorización se enforcea en Postgres, no en el cliente**: cada tabla tiene RLS (`auth.uid() = user_id`), los objetos de Storage están scopeados a la carpeta del dueño, y la publishable key es pública por diseño. Un request armado a mano igual no puede llegar a los datos de otro usuario. La única operación que de verdad necesita un servidor — llamar a Gemini, que requiere un secreto real más rate limiting y validación — es lo único que pasa por el gateway. Más sobre el trade-off abajo.

---

## Referencia de la API

### `POST /analyze` — gateway

- **Auth:** `Authorization: Bearer <access token de supabase>`
- **Body:** `multipart/form-data`

| Campo      | Tipo   | Requerido | Notas                            |
|------------|--------|-----------|----------------------------------|
| `cv`       | file   | sí        | PDF, límite de 5 MB (configurable) |
| `jobOffer` | string | sí        | Texto crudo de la oferta         |
| `jobTitle` | string | no        | Etiqueta el registro guardado    |

**`200`** — los campos de texto vuelven en los dos idiomas; `score` y `verdict` son neutrales al idioma:

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

Los errores comparten un único envelope en los tres servicios, así el frontend solo tiene que entender una forma:

```json
{ "error": { "code": "payload_too_large", "message": "CV exceeds the 5 MB limit." } }
```

El gateway pasa un 4xx del servicio de IA tal cual (por ejemplo `422 unreadable_cv` para un PDF escaneado sin capa de texto) y colapsa cualquier otra cosa — timeouts, 5xx, un upstream caído — en un `502`/`504`.

Hay dos casos en los que el gateway ni lo intenta y contesta `503` con `Retry-After`: `upstream_unavailable` cuando el circuit breaker está abierto, y `overloaded` cuando ya hay demasiados pedidos en vuelo hacia el servicio de IA.

### `POST /analyses` — gateway *(asíncrono)*

Mismo cuerpo que `/analyze`, pero no espera al modelo. Existe sólo cuando el
gateway tiene `DATABASE_URL`; sin eso, la ruta no está registrada. Si la base
todavía no tiene el esquema que el código necesita, contesta `404 queue_unavailable`:
lo mismo que un gateway sin cola, así el browser cae al camino síncrono sin tener
que distinguir los dos casos.

- **`200`** — el resultado ya estaba en caché, con la misma forma que `/analyze`.
  No se crea ningún job para trabajo que ya está hecho.
- **`202`** — encolado. `Location` apunta a dónde consultarlo.
- **`503 queue_full`** — ya hay 20 análisis esperando turno. Viene con
  `Retry-After: 60`.

```json
{ "jobId": "9f2c1ab3-…", "status": "queued" }
```

Reenviar el mismo análisis mientras el primero corre devuelve **el mismo `jobId`**:
la clave de deduplicación es la misma clave de caché, así que la cola y el caché
coinciden en qué es "el mismo análisis".

### `GET /analyses/{id}` — gateway

Devuelve **`200`** en todos los casos salvo que el job no exista o sea de otro
usuario, que dan `404` indistinguibles. Que el análisis haya fallado no es un
fallo de la consulta, así que el motivo viaja en el cuerpo:

```json
{ "jobId": "9f2c1ab3-…", "status": "running", "attempt": 1 }
{ "jobId": "9f2c1ab3-…", "status": "done",    "attempt": 1, "result": { … } }
{ "jobId": "9f2c1ab3-…", "status": "failed",  "attempt": 3,
  "error": { "code": "unreadable_cv", "message": "…" } }
```

### `POST /tailor/questions` — gateway

El primer paso del CV adaptado. Mismo cuerpo que `/analyze`, más un campo `locale`
opcional (`en` o `es`) para el idioma de las preguntas. Devuelve las preguntas y el
texto ya extraído del PDF, que el segundo paso reusa para no volver a parsearlo:

```json
{
  "questions": [
    { "topic": "Kubernetes", "question": "¿Operaste clústeres de Kubernetes? ¿De qué tamaño?" }
  ],
  "cvText": "Jane Doe · Backend engineer…"
}
```

### `POST /tailor/generate` — gateway

El segundo paso. Recibe JSON con el `cvText` del paso anterior, la oferta y las
respuestas, y devuelve el CV adaptado:

```json
{
  "cvText": "…",
  "jobOffer": "…",
  "jobTitle": "Backend Engineer",
  "answers": [{ "topic": "Kubernetes", "answer": "Dos años operando un clúster de 40 nodos." }],
  "extra": "Certificación CKA en 2024."
}
```

La respuesta viene estructurada —`fullName`, `contact`, `headline`, `summary`,
`experience`, `skills`, `education`, `additional`— para que el frontend la renderice
y la exporte a PDF o Word. Ninguno de los dos pasos usa la cola ni el caché: son
síncronos, detrás del mismo circuit breaker y el mismo límite de concurrencia que
`/analyze`.

### `GET /health` — gateway y servicio IA

Devuelve `{ "status": "ok" }`; el del gateway suma `"revision"`, el commit del build,
para saber si un deploy ya salió. Lo usan los health checks de Docker y Render.

---

## Observabilidad y medición

### Un id que cruza los tres servicios

El gateway le pone un `X-Request-ID` a cada request — reusa el que venga del cliente
sólo si sobrevive un saneo (corto y alfanumérico; un header con un salto de línea
podría fabricar líneas de log falsas), y si no genera uno de 128 bits. Ese id viaja
al servicio de IA en el mismo header, vuelve al browser en la respuesta, y aparece
en **cada línea de log de los dos servicios**. Un fallo se sigue de punta a punta
con un solo `grep`.

Los logs son JSON en producción y texto legible en desarrollo. Del lado de Python,
`structlog` y la librería estándar salen por el mismo renderer, así que hasta un
traceback inesperado del proveedor del modelo llega con su `request_id` puesto.

### Métricas

Los dos servicios exponen `/metrics` en formato Prometheus con las tres señales RED,
más los colectores de runtime:

| Métrica | Qué mide |
|---|---|
| `http_requests_total{method,route,status}` | Rate y errores |
| `http_request_duration_seconds{method,route}` | Duración |
| `http_requests_in_flight` | Concurrencia en curso |

Dos detalles que importan:

- **La etiqueta `route` es siempre la plantilla registrada**, nunca el path crudo.
  Etiquetar por path deja que cualquier escáner de URLs cree una serie temporal por
  request y termine tirando abajo el scrape. Lo que no está registrado cae en
  `unmatched`.
- **Los buckets del histograma llegan a 60s.** Los que traen por defecto las
  librerías cortan en 10, y como un análisis real tarda decenas de segundos, todas
  las observaciones caerían en el bucket de overflow y el p95 no significaría nada.

`/metrics` no lleva ningún dato de usuario — esa es justamente la razón de la
disciplina de cardinalidad. Aun así, en un deploy serio iría en un puerto interno
separado y no en el público.

### Trazas distribuidas

Un id te deja *encontrar* los logs de un análisis. No te dice dónde se fueron los
55 segundos. Para eso los dos servicios emiten trazas OpenTelemetry por OTLP.

Lo interesante es **la cola**. Un request que encola termina en menos de un
segundo; el trabajo pasa después, en otro proceso, quizás tras dos reintentos
repartidos en dos minutos. Sin nada que cruce ese hueco son cuatro cosas
inconexas. La fila del job guarda el `traceparent` de quien la encoló, y el
worker retoma ese trace en vez de empezar uno propio.

La primera traza real en producción encontró un bug de un vistazo:

```
POST /analyses              23.92 s   ← tenía que devolver en menos de un segundo
  └─ HTTP GET               23.08 s   ← /version, esperando al AI service dormido
analysis job  (intento 1)    2 m      ← cortado por el timeout del job
  └─ HTTP POST               2 m
analysis job  (intento 2)   36.46 s
  └─ HTTP POST              36.29 s
analysis job  (intento 3)    3.54 s   ← el AI service redeployando
  └─ HTTP POST               3.37 s
```

El endpoint asíncrono existía para no depender del AI service, y el camino de
encolado dependía de él de forma sincrónica: antes de escribir la fila traía la
versión del prompt para armar la clave del cache, y con el servicio dormido esa
llamada esperaba el arranque en frío entero. Los 628 ms que había medido eran
con el servicio despierto.

El arreglo separa cuánto espera el que llama de cuánto dura el trabajo. El
request espera la versión como mucho dos segundos y, si no llega, encola sin
clave de cache; el fetch sigue en segundo plano y despierta al servicio para el
worker, en vez de cancelarse a mitad del arranque. El worker sí espera la versión
fresca, porque es el que escribe en el cache: guardar con una versión vieja
archivaría la respuesta del prompt nuevo bajo la clave del viejo.

Ninguna deducción a partir de timestamps había llegado a esto en una semana de
diagnóstico. La traza lo mostró en la primera mirada.

Cuatro decisiones que hacen que esto sirva:

- **El span del worker es hijo del que encoló, no un link.** Las convenciones de
  messaging sugieren un link cuando el consumidor corre mucho después. Acá la
  pregunta es "dónde se fueron los 55 segundos", y una cascada padre-hijo la
  contesta directo mientras que un link te hace abrir dos trazas y compararlas.
- **El sampling es parent-based en todos lados.** Si una traza se está
  registrando, todos los servicios la registran. Decidir por separado produce
  trazas con agujeros, que son peores que ninguna porque parecen completas. La
  decisión de sampleo cruza la cola con el header.
- **El trace id es el correlation id** cuando el cliente no manda uno propio. Los
  dos siempre midieron lo mismo — `requestid.New()` se escribió con este día en
  mente. Un número que encuentra los logs *y* la traza vale más que dos que
  encuentran la mitad cada uno.
- **Tracing apagado es el mismo código con un sampler que no registra nada**, no
  una rama que lo esquiva. Una segunda configuración que sólo corre cuando nadie
  mira es una configuración que nadie prueba. Un job encolado sin traza corre
  igual.

### Dashboards

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://tempo:4318   docker compose --profile observability up --build
```

Grafana queda en http://localhost:3001 (sin login, es local) con el dashboard RED
y el datasource de Tempo ya aprovisionados; Prometheus en http://localhost:9090.
En producción las mismas dos variables (`OTEL_EXPORTER_OTLP_ENDPOINT` y
`OTEL_EXPORTER_OTLP_HEADERS`) apuntan a un backend hosteado — free tier, como
todo el resto.

### Caché

Un análisis repetido — el mismo CV contra la misma oferta — no vuelve a pagar el
modelo. El gateway lo cachea en dos niveles: un LRU en proceso que absorbe las
repeticiones, y Redis que aporta lo que un proceso solo no puede tener, entradas
compartidas entre réplicas y entradas que sobreviven a un reinicio. `singleflight`
adelante colapsa los pedidos concurrentes de la misma clave en **una sola** llamada
al modelo.

La clave incluye todo lo que puede cambiar la respuesta, incluido un *fingerprint*
del prompt que el servicio de IA deriva de sus propios prompts y del esquema. Editar
un prompt cambia el fingerprint y las entradas viejas dejan de direccionarse solas:
no hay ningún número que haya que acordarse de subir.

Redis es opcional — sin él el gateway igual cachea en proceso — y cualquier falla
del caché degrada a llamar al upstream. El detalle completo está en el
[ADR 0007](docs/adr/0007-cache-analyses-in-two-tiers.md).

```bash
docker compose up          # Redis ya viene incluido
```

### Baseline

**Overhead del gateway**, medido con un stub como upstream, así el número es el
trabajo del gateway y no la latencia del modelo:

```bash
cd gateway && go test ./internal/handlers/ -run '^$' -bench BenchmarkAnalyzeHandler -benchtime 3s -count 3
```

```bash
cd gateway && go test ./internal/services/ -run '^$' -bench BenchmarkAnalysisCache -benchtime 2s -count 3
```

| Camino | Latencia | Memoria | Allocs |
|---|---|---|---|
| Análisis (overhead del gateway) | **~0,58 ms** | ~683 KB | ~305 |
| Cache hit (análisis repetido) | **~0,12 ms** | ~209 KB | ~53 |

<sub>Medido en un Ryzen 7 5825U. El overhead del gateway cubre parseo del multipart,
validación, el ida y vuelta al upstream y la serialización; no incluye la
verificación del JWT, que necesita un JWKS vivo. El cache hit cubre leer el archivo,
hashearlo y decodificar el resultado guardado.</sub>

Lo que reemplaza el cache hit no son esos 0,58 ms: es la llamada al modelo. Medido
de punta a punta contra el stack completo, con una llamada real a Gemini y el mismo
análisis corrido dos veces:

| Camino | Latencia observada |
|---|---|
| Primer análisis (miss → Gemini) | **~31,5 s** |
| Repetición (hit) | **< 10 ms** |

<sub>Tomado del histograma del gateway: de las dos observaciones, una cayó en el
bucket `le=0.01` y la otra entre 30 y 45 s, con una suma de 31,54 s.</sub>

Esto es también lo que justifica los buckets hasta 60 s: con los que traen por
defecto las librerías, que cortan en 10, esos 31,5 s habrían caído en el bucket de
overflow y el p95 no habría significado nada.

Los 683 KB por request son el hallazgo interesante: el gateway **bufferea el CV
entero en memoria** para reenviarlo. Con el límite de 5 MB, eso es varias decenas de
megabytes bajo concurrencia. Lo dejo anotado acá porque recién ahora es visible.

**Carga sostenida** con k6, contra un gateway apuntado al stub:

```bash
docker compose --profile loadtest up          # gateway en :8081 + stub
k6 run -e TOKEN="<access token>" load/k6/analyze.js
```

El token es uno real de Supabase — el gateway verifica la firma contra el JWKS del
proyecto y no hay forma de fabricar uno offline. Se saca de una sesión del browser
con `(await supabase.auth.getSession()).data.session.access_token`.


### La cola, sobre una tabla llena

La eficiencia de la cola descansaba en comentarios. Explicar cada consulta que
corre el worker, contra 100.000 filas en régimen —casi todo trabajo terminado,
una capa fina de jobs vivos, todo dentro de su ventana de retención—, encontró
dos consultas periódicas que leían la tabla entera para actuar sobre casi nada, y
un claim que era rápido **por accidente**: lo servía un índice creado para otra
cosa, y ensancharlo lo mandaba a leer las 100.000 filas.

```bash
cd gateway && go test ./internal/jobs/ -run 'TestTheQueueQueries|TestAClaimReads' -v
```

| Consulta | Cada cuánto | Antes | Después |
|---|---|---|---|
| claim | cada poll | 2.002 filas, 1,6 ms | **3 filas, 0,2 ms** |
| gauge de profundidad | cada 15 s | 100.000 filas, 16,6 ms | **8.000 filas, 2,3 ms** |
| purga por retención | cada 2 min | 100.000 filas, 15,5 ms | **0 filas, 0,08 ms** |

El claim ahora tiene un índice propio, ordenado como lo lee, y para en la primera
fila que puede tomar. Los tests afirman la propiedad —que ninguna consulta
recorra la tabla y que el claim lea la capa viva y no la historia— sin nombrar
índices, así que un cambio razonable del esquema no los rompe; solo uno que
devuelva alguna consulta a leer todo.

Con el tráfico actual la tabla tiene unas decenas de filas y nada de esto se nota
en producción. Ese es el punto: que el costo lo fije el diseño y lo pruebe un
test, en vez de sostenerse porque todavía nadie la usó mucho. Ver
[ADR 0013](docs/adr/0013-hold-the-queue-plans-in-tests.md).


### Resiliencia: dejar de insistir, y decir que no a tiempo

Cuando el servicio de IA deja de responder —dormido por la plataforma,
redeployando, o caído con su proveedor de modelos—, antes cada llamador se
enteraba por separado: un request síncrono esperaba la falla entera, el
siguiente la esperaba de nuevo, y el worker gastaba los intentos de un job en
llamadas que los anteriores ya habían mostrado que iban a fallar.

**Un circuit breaker compartido** por todo lo que le habla al servicio —
análisis, tailor, fetch de versión— se abre tras cinco fallas consecutivas.
Abierto, el camino del request responde al instante con 503 y `Retry-After`, y
el worker deja de reclamar jobs: esperan en la cola sin gastar intentos contra
una caída ya conocida. Tras 30 segundos pasa una sola llamada de prueba —la de
un job real, que sí gasta un intento—; si sale bien, cierra. La sonda
de readiness no pasa por el breaker, porque es justamente lo que detecta que el
servicio volvió.

**Load shedding** donde el free tier lo hace necesario:

| Límite | Por qué | Al pasarlo |
|---|---|---|
| 8 llamadas síncronas en vuelo | cada una retiene el CV en memoria; la instancia tiene 512 MB | 503 `overloaded` |
| 20 jobs en espera | cada uno guarda el PDF en Postgres; la base tiene 500 MB | 503 `queue_full` |

La admisión a la cola va dentro del mismo `INSERT` que encola, así que un doble
submit de algo ya encolado se une al job existente aunque la cola esté llena:
solo se rechaza trabajo nuevo. Y un test del lado Go falla si el gateway puede
mandar un código de error que el frontend no sabe mostrar — la forma en que dos
errores ya habían caído en el mensaje genérico antes.
Ver [ADR 0015](docs/adr/0015-break-the-circuit-to-the-ai-service.md) y
[ADR 0016](docs/adr/0016-turn-away-what-cannot-be-served.md).

---

## Tests

```bash
cd frontend && npm test
cd gateway && go test ./...
cd ai-service && pip install -r requirements-dev.txt && pytest
```

Las tres suites corren **offline y gratis** — nunca llaman al LLM real. Los tests del gateway firman sus propios tokens ES256 y mockean el servicio de IA con `httptest`; los de Python inyectan una cadena de LangChain falsa y arman PDFs reales de una página con `reportlab` para ejercitar la extracción. Apuntan a lo que más probablemente se rompa en silencio: la verificación de tokens, las bandas del veredicto, y "qué pasa cuando el PDF es basura".

Los del frontend cubren el cliente del gateway: el sondeo, la caída al camino síncrono, el despertar del servicio de IA y el mapa de errores. Los de la cola y del esquema levantan un Postgres real con testcontainers y le aplican todas las migraciones; sin Docker se saltean en tu máquina, y en CI fallan.

> Los tests de Python apuntan a 3.11 (lo que usa el Dockerfile). En un intérprete mucho más nuevo puede que no haya wheels precompiladas para las dependencias fijadas.

---

## Deploy

- **Frontend → Vercel.** Importá `frontend/`, seteá las vars `NEXT_PUBLIC_*` y deployá; Vercel toma Node 22 del `engines` del `package.json`. `NEXT_PUBLIC_AI_SERVICE_URL` es la URL del servicio de IA, para que el browser lo despierte antes de que un análisis lo necesite. Agregá la callback URL de producción a la lista de redirects de Supabase y a los orígenes de CORS.
- **Gateway + servicio IA → Render.** Dos Web Services desde este repo, cada uno apuntando a su Dockerfile. Seteá el entorno de cada uno desde su `.env.example`, apuntá `AI_SERVICE_URL` al servicio de IA deployado, y apuntá el `NEXT_PUBLIC_GATEWAY_URL` del frontend al gateway deployado. En el gateway, `DATABASE_URL` (el pooler) y `RUN_WORKER=true` prenden la cola con el worker dentro del mismo proceso; `cmd/worker` está para correrlo aparte cuando haya lugar para un servicio más. `REDIS_URL` (Redis Cloud, `rediss://`) y las `OTEL_EXPORTER_OTLP_*` (Grafana Cloud) son opcionales. Si usás `INTERNAL_API_KEY`, tiene que ser la misma en los dos servicios.
- **Supabase** ya es managed — seguís usando el mismo proyecto. Las migraciones se aplican a mano en el SQL Editor y **antes** de pushear el código que las necesita; el paso a paso está en el [runbook](docs/runbook.md#adding-a-migration).

---

## Bugs que aparecieron (para que a vos no)

Todos estos me costaron tiempo real mientras lo construía, así que vale la pena anotarlos:

- **Redirects cayendo en `http://0.0.0.0:3000`.** El server standalone de Next escucha en `0.0.0.0` dentro del contenedor, y `request.url` refleja eso — así que cualquier redirect armado a partir de ahí (el callback de OAuth, el sign-out, el guard de auth) mandaba al browser a una dirección que no puede abrir. El fix es reconstruir las URLs de redirect desde el header `Host` / `x-forwarded-host` en vez de `request.url`.
- **Un 401 en cada análisis.** La verificación clásica de HS256 con secreto compartido rechaza los tokens modernos de Supabase, que son ES256. La firma no está "mal" — la estás chequeando de la forma equivocada. Verificá contra el JWKS.
- **Un 404 de Gemini que parece una key mala.** `gemini-1.5-flash` quedó discontinuado; las llamadas fallan con *"model not found"*, que se lee como un problema de auth pero no lo es. Apuntá a un modelo vigente (`gemini-2.5-flash`) y usá el endpoint `ListModels` para ver qué puede llamar tu key realmente.
- **El contenedor del gateway clavado en "unhealthy".** El health check usaba `wget --spider`, que manda un request `HEAD`. Gin solo registra `GET /health`, así que el `HEAD` daba 404 y el contenedor nunca pasaba a healthy — lo que frenaba todo lo que dependía de él. El fix es un health check por `GET`.
- **Un CV mostrando "0.0 MB".** Formatear todo tamaño de archivo en MB redondea a cero un PDF normal de unos cientos de KB. Mostrá KB por debajo de 1 MB.

---

## Decisiones y trade-offs

El resumen está acá; el registro completo, una decisión por archivo con su contexto
y sus consecuencias, vive en [`docs/adr/`](docs/adr/). El [runbook](docs/runbook.md) tiene lo operativo: qué mirar cuando algo se rompe.

Algunas elecciones que defiendo, y el costo de cada una:

- **Tres servicios para una herramienta de CVs es exagerado, y ese es el punto.** Una sola app de Next.js sería menos para correr. Lo separé para que el trabajo del LLM, el borde seguro y la UI deployen y fallen por separado — y para que el repo se lea como producción, no como un demo. Si fuera un producto real con presupuesto, probablemente arrancaría junto y separaría después.
- **CRUD directo a Supabase + RLS, en vez de enrutar todo por el gateway.** Es el patrón nativo de Supabase: menos código de pegamento, autorización centralizada en la base, menos latencia. El trade-off es que la forma de las tablas/columnas queda visible para el cliente y el frontend queda acoplado a la API de Supabase. Si necesitara ocultar el schema, correr transacciones multi-paso, o sumar reglas pesadas del lado del servidor, movería ese CRUD detrás del gateway. Para CRUD por usuario protegido con RLS, el camino directo es la decisión correcta.
- **La cola vive en Postgres, no en un broker.** `SELECT … FOR UPDATE SKIP LOCKED` da exactamente la semántica que hace falta —cada job lo toma un solo worker, sin que los workers se esperen entre sí— y no suma infraestructura que pagar ni mantener viva en un free tier. El costo es sondear en vez de recibir push, que a este volumen no se nota, y que el gateway ahora tiene una credencial de base.
- **Las propiedades que importan las sostiene un test, no un comentario.** Que ninguna consulta de la cola recorra la tabla entera, que cada migración se pueda pegar dos veces, que cada código de error tenga un mensaje en el frontend: todo eso falla CI si se rompe. Varias de esas propiedades estaban escritas en comentarios y resultaron falsas cuando alguien las midió.
- **Despertar el servicio de IA desde el browser.** En el free tier la plataforma lo duerme, y un request desde el gateway no lo despierta mientras que uno desde afuera sí. Es un parche para una conducta observada, no documentada, y puede dejar de funcionar sin aviso — por eso está escrito como tal en el ADR 0014.
- **Derivar el veredicto del score en código, no en el modelo.** Todo lo que puedo calcular de forma determinista, no se lo pido al LLM. Una cosa menos que dudar.
- **Guardado del CV best-effort.** Si la subida a Storage falla, el análisis igual se guarda (sin archivo descargable) y la UI lo avisa sin drama. Un hipo de Storage no debería costarte el análisis que recién esperaste.

---

## Qué le agregaría

- **Un rol de Postgres propio para el gateway**, con permisos solo sobre la cola y el registro de migraciones. Hoy usa el rol dueño de la base.
- **Guardar el resultado en el historial desde el servidor.** Hoy lo escribe el browser cuando termina de sondear; si la persona cerró la pestaña antes, el análisis se hace igual pero no aparece en su historial — queda en el job y en el caché, y pedirlo de nuevo es instantáneo, pero no se ve.
- **Despertar el servicio de IA a través de una ruta propia del frontend**, para que los bloqueadores de anuncios no corten el ping. Antes hay que medir si los servidores de Vercel lo despiertan como un browser o reciben el mismo 429 que el gateway.
- Un undo en el borrado, en vez de —o además de— el diálogo de confirmación.
- Tipos generados de Supabase, para sacar el único cast `unknown` de la capa de datos.

---

## Licencia

MIT — hacé lo que quieras con esto.
