# Papyrus

*English version: [README.en.md](README.en.md).*

Papyrus te dice si tu CV realmente encaja con un puesto antes de que te pases una tarde adaptándolo. Subís un PDF, pegás la oferta, y te devuelve un score de compatibilidad del 0 al 100, las skills que ya cubrís, las que te faltan, y una lista corta de cambios concretos para *ese* puesto puntual.

Lo construí como un ejercicio full-stack en serio: tres servicios que se deployan por separado, un login real con Google, Row Level Security haciendo la autorización, y un LLM haciendo el razonamiento de verdad. La regla que me puse fue que nada podía estar truchado — sin datos de mentira, sin botones a medio cablear, sin "TODO: manejar errores". O funciona de punta a punta, o no está acá.

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
- **Historial** — cada análisis queda guardado. Podés reabrir cualquiera, descargar el CV original (URL firmada, bucket privado), o eliminarlo (con confirmación, porque también borra el archivo guardado).
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
| Gateway     | Go 1.22, Gin, `golang-jwt/v5` con verificación JWKS hecha a mano, `golang.org/x/time/rate` |
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
│   ├── app/                     # rutas: landing, /dashboard, /auth/{callback,signout}, icon.svg
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
│   ├── cmd/server/              # entrypoint + graceful shutdown
│   └── internal/
│       ├── auth/                # fetch + caché de JWKS, verificación ES256/RS256
│       ├── config/              # carga + validación del entorno (fail fast)
│       ├── handlers/, router/   # /analyze, /health, armado del engine
│       ├── middleware/          # CORS, auth, rate limiting
│       └── services/, transport/, httpx/
├── ai-service/                  # Python + FastAPI
│   ├── app/{api,core,schemas,services}/
│   └── tests/                   # offline, la cadena del modelo está fakeada
├── supabase/migrations/         # 0001 esquema+RLS, 0002 storage, 0003 bilingüe
├── docker-compose.yml
└── .env.example
```

---

## Correrlo localmente

### Prerequisitos

- [Docker](https://docs.docker.com/get-docker/) + Compose v2
- Un proyecto gratis de [Supabase](https://supabase.com)
- Una key gratis de [Google AI Studio](https://aistudio.google.com/app/apikey)

Si querés correr un servicio fuera de Docker vas a necesitar además Node 20+, Go 1.22+ o Python 3.11+, según cuál.

### 1. Supabase (esta es la única parte tediosa)

**Esquema.** Abrí el SQL Editor y corré, en orden:

- [`0001_init.sql`](supabase/migrations/0001_init.sql) — `profiles`, `analyses`, `cvs`, las políticas RLS, y un trigger que crea la fila de perfil al registrarse.
- [`0002_cv_storage.sql`](supabase/migrations/0002_cv_storage.sql) — el bucket privado `cvs` de Storage y las políticas de objetos scopeadas al dueño (`<user-id>/<cv-id>.pdf`).
- [`0003_bilingual_analyses.sql`](supabase/migrations/0003_bilingual_analyses.sql) — solo hace falta si tu base es anterior al cambio bilingüe; en una instalación nueva es un no-op protegido.

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

### `GET /health` — gateway y servicio IA

Devuelve `{ "status": "ok" }`. Lo usan los health checks de Docker y Render.

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
