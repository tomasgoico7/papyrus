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
- **Historial con búsqueda y filtros** — cada análisis queda guardado. Podés buscarlos por puesto y filtrarlos por compatibilidad, reabrir cualquiera, reusar un CV ya subido para medirlo contra otra oferta, descargar el CV original (URL firmada, bucket privado), o eliminarlo (con confirmación, porque también borra el archivo guardado).
- **Exportar a PDF** — descargás cualquier análisis como un PDF prolijo y bilingüe (texto seleccionable), generado en el navegador.
- **Compartir por link** — generás un link público con vencimiento para que cualquiera vea el análisis (nunca tu CV), revocable cuando quieras.
- **Contenido bilingüe** — el análisis se genera en inglés *y* español en una sola llamada al modelo, así que cambiar el idioma de la UI re-renderiza el resultado al instante sin volver a correr nada.
- **Tema claro / oscuro** — sigue tu sistema operativo por defecto, con un toggle manual.
- **Login con Google** — vía Supabase Auth. Sin contraseñas que guardar.

Todo corre en free tiers. No hay ninguna API key paga en todo el stack.

---

## Arquitectura

Tres servicios, una regla clara: **el gateway nunca toca la base de datos, y el servicio de IA no guarda estado.** La persistencia vive entera en Supabase y se accede directo desde el browser a través de Row Level Security, así que cada lectura y escritura queda automáticamente scopeada al usuario logueado — ninguna credencial de base vive en un servidor que yo corra.

Eso deja a cada servicio con exactamente una tarea: el frontend renderiza y es dueño de los datos del usuario, el gateway es el borde seguro para la única operación que necesita un secreto, y el servicio de IA es una función pura de `(CV, oferta)` a `análisis`.

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

El split, siendo honesto, es un poco demasiado para una herramienta de CVs — podrías colapsar todo en una sola app de Next.js con un par de route handlers. Lo dejé separado a propósito: la capa del LLM, el borde de requests y la UI escalan y fallan distinto, y quería que el proyecto se viera como algo que de verdad correrías en producción y no como un juguete de fin de semana. La versión honesta de eso está en [Decisiones y trade-offs](#decisiones-y-trade-offs).

---

## El request, de punta a punta

1. Te logueás con Google. Supabase hace el baile de OAuth y deja la sesión en cookies HTTP-only. El middleware refresca esa sesión en cada request y protege `/dashboard`.
2. Subís un CV y pegás una oferta. El browser manda los dos al gateway como `multipart/form-data` con el access token de Supabase adjunto.
3. El gateway verifica la firma del token contra el **JWKS** de tu proyecto (Supabase ahora firma con ES256, no con un secreto compartido), aplica un rate limit por usuario, chequea que el upload sea un PDF dentro del límite de tamaño, y reenvía el request al servicio de IA.
4. El servicio de IA extrae el texto del CV con `pypdf`, arma un prompt estructurado, y le pide a Gemini — a través del `with_structured_output` de LangChain — un objeto JSON validado y bilingüe. El veredicto se calcula a partir del score en Python; no se le confía al modelo.
5. El browser renderiza el resultado, sube el CV a un bucket de Storage privado, y escribe el análisis (linkeado a ese archivo) en la tabla `analyses`. RLS garantiza que solo veas tus propias filas y archivos. El CV original queda descargable después con una URL firmada de corta duración.

---

## Stack

| Capa        | Qué tiene                                                                    |
|-------------|------------------------------------------------------------------------------|
| Frontend    | Next.js 14 (App Router), TypeScript (strict), Tailwind CSS, `@supabase/ssr`, `next-themes`, i18n por cookie, `zod` para validar el entorno |
| Gateway     | Go 1.26, Gin, `golang-jwt/v5` con verificación JWKS hecha a mano, `golang.org/x/time/rate` |
| Servicio IA | Python 3.11, FastAPI, LangChain (`langchain-core` + `langchain-google-genai`), `pypdf`, `pydantic-settings` |
| Base        | Supabase (PostgreSQL) con Row Level Security + Storage privado               |
| LLM         | Google Gemini (`gemini-2.5-flash`, free tier)                               |
| Dev local   | Docker + Docker Compose                                                     |
| Deploy      | Vercel (frontend) · Render (gateway + servicio IA) · Supabase (managed)     |

---

## Estructura del proyecto

```
papyrus/
├── frontend/                    # app Next.js 14
│   ├── app/                     # rutas: landing, /dashboard, /share/[token], /auth/{callback,signout}
│   ├── components/
│   │   ├── analysis/            # anillo de score, listas de skills, badge de veredicto, sugerencias
│   │   ├── dashboard/           # workspace, formulario, dropzone, historial, estados de resultado
│   │   ├── marketing/           # header, footer, preview del hero, reveal al scrollear
│   │   └── ui/                  # botón, toggles de idioma/tema, diálogo de confirmación
│   └── lib/
│       ├── analyses/, cvs/      # acceso a datos de Supabase (los "repositorios")
│       ├── api/                 # cliente del gateway
│       ├── i18n/                # diccionarios (en/es) + helpers de server y cliente
│       └── supabase/            # clientes browser / server / middleware
├── gateway/                     # borde Go + Gin
│   ├── cmd/server/              # API: entrypoint + graceful shutdown
│   ├── cmd/worker/              # worker de la cola como servicio propio
│   └── internal/
│       ├── app/                 # composition root: el wiring que comparten API y worker
│       ├── auth/                # fetch + caché de JWKS, verificación ES256/RS256
│       ├── cache/               # store de bytes: LRU en proceso, Redis, y el tier
│       ├── config/              # carga + validación del entorno (fail fast)
│       ├── handlers/, router/   # /analyze, /health, /metrics, armado del engine
│       ├── jobs/                # cola en Postgres, reclamada con SKIP LOCKED
│       ├── middleware/          # CORS, auth, rate limiting, request id
│       ├── observability/       # logger estructurado y métricas RED
│       ├── ratelimit/           # presupuesto por usuario: local, en Redis, y el fallback
│       ├── requestid/           # id de correlación y su transporte por contexto
│       ├── worker/              # loop de la cola, backoff con jitter, dead letters
│       └── services/, transport/, httpx/
├── ai-service/                  # Python + FastAPI
│   ├── app/{api,core,schemas,services}/
│   └── tests/                   # offline, la cadena del modelo está fakeada
├── supabase/migrations/         # 0001 esquema+RLS, 0002 storage, 0003 bilingüe, 0004 compartir
├── docs/adr/                    # registro de decisiones de arquitectura
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

Si querés correr un servicio fuera de Docker vas a necesitar además Node 20+, Go 1.26+ o Python 3.11+, según cuál.

### 1. Supabase (esta es la única parte tediosa)

**Esquema.** Abrí el SQL Editor y corré, en orden:

- [`0001_init.sql`](supabase/migrations/0001_init.sql) — `profiles`, `analyses`, `cvs`, las políticas RLS, y un trigger que crea la fila de perfil al registrarse.
- [`0002_cv_storage.sql`](supabase/migrations/0002_cv_storage.sql) — el bucket privado `cvs` de Storage y las políticas de objetos scopeadas al dueño (`<user-id>/<cv-id>.pdf`).
- [`0003_bilingual_analyses.sql`](supabase/migrations/0003_bilingual_analyses.sql) — solo hace falta si tu base es anterior al cambio bilingüe; en una instalación nueva es un no-op protegido.
- [`0004_analysis_sharing.sql`](supabase/migrations/0004_analysis_sharing.sql) — las columnas de compartir (`share_token`, `share_expires_at`) y la función `get_shared_analysis` (`security definer`) que sirve un análisis por link público sin saltarse la RLS.

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
cp .env.example .env     # completá los cuatro valores reales; el resto tiene defaults razonables
docker compose up --build
```

| Servicio    | URL                     |
|-------------|-------------------------|
| Frontend    | http://localhost:3000   |
| Gateway     | http://localhost:8080   |
| Servicio IA | http://localhost:8000   |

Solo cuatro variables necesitan valores reales — `NEXT_PUBLIC_SUPABASE_URL`, `NEXT_PUBLIC_SUPABASE_ANON_KEY` y `GEMINI_API_KEY`. Compose te avisa por nombre si falta alguna. Frenás todo con `docker compose down`.

> Un detalle: Next "hornea" las `NEXT_PUBLIC_*` en tiempo de **build**, así que si las cambiás tenés que hacer `docker compose up --build` de nuevo — un restart no las toma.

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

### `POST /analyses` — gateway *(asíncrono)*

Mismo cuerpo que `/analyze`, pero no espera al modelo. Existe sólo cuando el
gateway tiene `DATABASE_URL`; sin eso, la ruta no está registrada.

- **`200`** — el resultado ya estaba en caché, con la misma forma que `/analyze`.
  No se crea ningún job para trabajo que ya está hecho.
- **`202`** — encolado. `Location` apunta a dónde consultarlo.

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

### `GET /health` — gateway y servicio IA

Devuelve `{ "status": "ok" }`. Lo usan los health checks de Docker y Render.

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
worker retoma ese trace en vez de empezar uno propio:

```
POST /analyses                    628 ms
  └─ espera en cola                 27 s        ← el hueco, ahora visible
     └─ analysis job (intento 1)    31 s
        └─ POST /analyze → ai-service
           └─ llamada al modelo
```

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

---

## Tests

```bash
cd gateway && go test ./...
cd ai-service && pip install -r requirements-dev.txt && pytest
```

Las dos suites corren **offline y gratis** — nunca llaman al LLM real. Los tests del gateway firman sus propios tokens ES256 y mockean el servicio de IA con `httptest`; los de Python inyectan una cadena de LangChain falsa y arman PDFs reales de una página con `reportlab` para ejercitar la extracción. Apuntan a lo que más probablemente se rompa en silencio: la verificación de tokens, las bandas del veredicto, y "qué pasa cuando el PDF es basura".

> Los tests de Python apuntan a 3.11 (lo que usa el Dockerfile). En un intérprete mucho más nuevo puede que no haya wheels precompiladas para las dependencias fijadas.

---

## Deploy

- **Frontend → Vercel.** Importá `frontend/`, seteá las vars `NEXT_PUBLIC_*` y `NEXT_PUBLIC_GATEWAY_URL`, deployá. Agregá la callback URL de producción a la lista de redirects de Supabase y a los orígenes de CORS.
- **Gateway + servicio IA → Render.** Dos Web Services desde este repo, cada uno apuntando a su Dockerfile. Seteá el entorno de cada uno desde su `.env.example`, apuntá `AI_SERVICE_URL` al servicio de IA deployado, y apuntá el `NEXT_PUBLIC_GATEWAY_URL` del frontend al gateway deployado.
- **Supabase** ya es managed — seguís usando el mismo proyecto.

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
- **Derivar el veredicto del score en código, no en el modelo.** Todo lo que puedo calcular de forma determinista, no se lo pido al LLM. Una cosa menos que dudar.
- **Guardado del CV best-effort.** Si la subida a Storage falla, el análisis igual se guarda (sin archivo descargable) y la UI lo avisa sin drama. Un hipo de Storage no debería costarte el análisis que recién esperaste.

---

## Qué le agregaría

- Un undo en el borrado (hoy confirma, y después chau) en vez de — o además de — el diálogo de confirmación.
- Cachear pares `(CV, oferta)` idénticos para que re-correr sea gratis e instantáneo.
- Tipos generados de Supabase para sacar el único cast `unknown` de la capa de datos.
- Un esquema liviano de rate-limit / anti-abuso para el deploy público, más allá del presupuesto por usuario.

---

## Licencia

MIT — hacé lo que quieras con esto.
