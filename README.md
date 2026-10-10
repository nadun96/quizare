<p align="center"><img src="frontend/static/logo-192.png" width="96" height="96" alt="Classroom Quiz logo"></p>

# QR Classroom Quiz Platform

Timed, proctored in-class quizzes that students join by scanning a QR code, with
answer-key and LLM-assisted marking, predefined and AI feedback, and configurable
results publishing and analytics. Live polls (anonymous or identified, 20 input types,
live word clouds and charts, scoring, groups and a whiteboard) run alongside quizzes.
Optional **tutoring sessions** (live classes with screens, cameras, chat and attendance)
run as a separate service with LiveKit as the media server. Admins can share their work
with managers, see the space the platform uses, and export encrypted backups.

- Choices that fill gaps in the BA document: `DECISIONS.md`
- Requirements, architecture (ADRs) and stack documents are private and are **not in the repository**.
  Keep them in a local `docs/` folder (git-ignored): the Business Analysis document, the Architecture
  document and `tech_stack_recommendations.xlsx`.

## Layout

| Path | What |
|------|------|
| `backend/` | Single Go binary (modular monolith, ADR-01): Chi, coder/websocket, River, pgx, PostgreSQL |
| `tutoring/` | The tutoring service (D-59): its own Go module, program and schema; LiveKit carries the media (D-58) |
| `frontend/` | SvelteKit static SPA (`adapter-static`, ADR-11): student, teacher, admin, tutoring and public pages |
| `deploy/` | Caddyfile, systemd units (platform, tutoring), LiveKit config, PostgreSQL tuning, encrypted backup script |
| `loadtest/` | k6 scenarios ("class of 100 × 3", architecture §2.3, and the whiteboard) and the tutoring media load test |

Backend modules (`backend/internal/`), each owning one PostgreSQL schema:

| Module | Covers |
|--------|--------|
| `auth` | Accounts, Argon2id pool, `__Host-` sessions, verification, reset, profile pictures, admin account management, managers with per-feature permissions (FR-ACC, D-56) |
| `settings` | Hierarchical configuration: platform → teacher → classroom → module → topic → quiz → question → session → student (BA §7) |
| `content` | Classrooms, modules, topics, enrolment with classroom student IDs (FR-CLS) |
| `quiz` | Seven question types, CSV import, resource mapping, Drive link normaliser, SSRF-safe link checks (FR-QZ, BA §10) |
| `live` | Sessions, QR join, waiting room, admission, countdown, server timers, pause/extend, proctoring, WebSocket hub (FR-SS, FR-PR) |
| `eval` | Answer-key marking, predefined feedback, overrides, release, student results (FR-EV) |
| `llm` | Per-teacher key vault (AES-256-GCM envelope), Anthropic/OpenAI/Gemini adapters, marking jobs (FR-EV-02/03/05, ADR-09) |
| `analytics` | Precomputed session/quiz analytics, public share links, CSV export (FR-RS, BA §11) |
| `poll` | Live polls: identity modes, presenter pacing, 20 question types, file/audio/video answers, live results hub, moderation, CSV export (D-40) |
| `admin` | Usage, audit log (filterable by person and role), own-data export |
| `storage` | Storage overview and limits, clean-ups, encrypted console backups, `server restore-backup` and `check-backup` (D-57) |
| `tutorlink` | Single sign-on to the tutoring service and its internal API (D-59) |

Every list that can grow is paginated on the server (D-55).

## Developer documentation

The frontend includes a developer documentation site at **`/docs`** (for example, <http://localhost:5173/docs> in development). It covers the architecture, every backend module, the data model, the configuration hierarchy, live sessions and timing, proctoring, marking, analytics, the frontend, the HTTP and WebSocket interfaces, security, deployment, testing and contributing, with Mermaid diagrams. The pages are Markdown in `frontend/src/lib/docs/pages/`; the decision log is rendered straight from `DECISIONS.md`. Tests fail if:

- a page is missing from the navigation;
- a cross-page link or anchor is broken;
- a diagram doesn't parse;
- a backend module, setting key or migration is undocumented.

## API documentation

The server serves an OpenAPI 3.1 spec with an embedded Swagger UI (no CDN):

| URL | What |
|-----|------|
| `/api/docs` | Swagger UI. "Try it out" works after logging in on the same origin: the UI sends the session cookie and the CSRF header |
| `/api/docs/openapi.yaml` | The spec (source: `backend/internal/apidocs/openapi.yaml`) |
| `/api/docs/openapi.json` | The same spec as JSON, for code generators |

`go test ./internal/app` fails if a route is missing from the spec, the spec lists a route that doesn't exist, a `$ref` doesn't resolve, or an operationId is missing or duplicated. CI also lints the spec with Redocly. Set `QP_API_DOCS=0` to stop serving the docs.

## Branching

- `main`: released code only.
- `test`: release candidates; merged from `dev` once the full test suite passes.
- `dev`: the integration branch.
- `feature/<name>`: one branch per feature, cut from `dev` and merged back with `--no-ff`.

## Development

Requirements: Go 1.26+, Node 20.19+ (22 recommended). No Docker needed: tests use an embedded PostgreSQL 16. The tutoring service is a second Go module: run its tests with `cd tutoring && go test ./...` (it uses `QP_TEST_DATABASE_URL` too).

```sh
# Backend tests (terminal 1 keeps one shared embedded PostgreSQL running)
cd backend
go run ./cmd/testdb
export QP_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:54329/postgres?sslmode=disable'
go test ./...          # without the env var each package starts its own embedded server (slower)

# Frontend
cd frontend
npm install
npm run check && npm test
npm run dev            # http://localhost:5173, proxies /api, /ws and /beacon to :8080
```

Run the server locally. Everything below runs from `backend/`, the Go module. The master key is read from a 32-byte file, never from an environment variable (ADR-09); `QP_KEK_GENERATE=1` creates it on first start and never overwrites it.

```sh
# Terminal 1: a local PostgreSQL on port 54329 with a qp_dev database (or use your own PostgreSQL)
cd backend
go run ./cmd/testdb

# Terminal 2: the server
cd backend
export QP_DATABASE_URL='postgres://postgres:postgres@localhost:54329/qp_dev?sslmode=disable'
export QP_KEK_FILE=.data/kek QP_KEK_GENERATE=1   # git-ignored; keep this file
export QP_BASE_URL=http://localhost:5173          # the Vite dev server; use :8080 with QP_STATIC_DIR
go run ./cmd/server

# Terminal 3: the first admin (same exports as terminal 2; password on stdin)
cd backend
export QP_DATABASE_URL='postgres://postgres:postgres@localhost:54329/qp_dev?sslmode=disable' QP_KEK_FILE=.data/kek
echo 'a-strong-password' | go run ./cmd/server create-admin admin@school.edu "Admin"
```

For a server, create the key once and never overwrite it (losing it makes saved LLM API keys unreadable):

```sh
[ -f kek.key ] || { openssl rand 32 > kek.key && chmod 0400 kek.key; }
```

To serve the built SPA from the Go binary without Caddy, set `QP_STATIC_DIR=../frontend/build`.
Without `QP_SMTP_ADDR`, emails (verification, password reset) are written to the log.

| Variable | Default | Meaning |
|----------|---------|---------|
| `QP_DATABASE_URL` | (required) | PostgreSQL URL |
| `QP_KEK_FILE` | `$CREDENTIALS_DIRECTORY/kek` | 32-byte master key file (raw, hex or base64), mode 0400 |
| `QP_BASE_URL` | `http://localhost:8080` | Public origin: QR links, Origin/CSRF checks |
| `QP_LISTEN` | `127.0.0.1:8080` | Listen address (Caddy proxies to it) |
| `QP_TRUSTED_PROXIES` | | Proxy IPs or CIDRs, besides loopback, whose `X-Forwarded-For` is believed (compose sets Caddy's address) |
| `QP_DB_MAX_CONNS` | 15 | pgx pool size |
| `QP_ARGON2_WORKERS` | 2 | Concurrent password hashes (login-burst memory cap) |
| `QP_SMTP_ADDR`, `QP_SMTP_FROM`, `QP_SMTP_USER`, `QP_SMTP_PASSWORD_FILE` | | Optional SMTP relay |
| `QP_STATIC_DIR` | | Serve the SPA build from Go (dev / single binary) |
| `QP_API_DOCS` | on | `0` stops serving `/api/docs` |
| `QP_KEK_GENERATE` | off | `1` creates the KEK file (0400) on first start if it is missing; never overwrites (containers) |
| `QP_DB_WAIT_SECONDS` | 60 | How long startup keeps retrying an unreachable database |
| `QP_BACKUP_DIR` | `data/backups` | Where backups made from the admin console are written (D-57) |
| `QP_NIGHTLY_BACKUP_DIR` | | `deploy/backup.sh`'s folder, so the console can show the nightly job |
| `QP_TUTORING_URL`, `QP_TUTORING_SECRET_FILE` | | Turn tutoring on: the tutoring service's public address and the secret file shared with it (D-59) |

Tutoring locally: run LiveKit, the server with the two tutoring variables, and `go run ./cmd/tutor` in `tutoring/`. The in-app docs page *Tutoring sessions* (`/docs/tutoring`) has the commands and the service's own variables.

## Containers (Docker or Podman)

`Dockerfile` builds one image (SvelteKit build + static Go binary on distroless, non-root, about 45 MB). `compose.yaml` runs it with PostgreSQL 16, and an optional Caddy for HTTPS. The same files work with `docker compose` and `podman compose` (image names are fully qualified, and the bind mount carries the SELinux `Z` label).

```bash
cp .env.example .env                 # set POSTGRES_PASSWORD
docker compose up -d --build         # or: podman compose up -d --build
echo 'a-strong-password' | docker compose exec -T app /app/server create-admin admin@example.edu "Admin"
# http://localhost:8080
```

- On first start the app writes a random master key into the `appdata` volume (`QP_KEK_GENERATE=1`). Back that volume up together with `pgdata`.
- Backups from the admin console go to `/var/lib/quiz/backups` in that volume; download copies off the host.
- Tutoring: set `QP_TUTORING_URL`, `QP_TUTORING_MEDIA_URL` and `QP_TUTOR_DOMAIN` in `.env` and add `--profile tutoring` (with `--profile tls`). It runs the tutoring service and LiveKit; open UDP 7882 and TCP 7881 for media.
- For phones on the LAN or a domain, use HTTPS: set `QP_DOMAIN` and `QP_BASE_URL=https://…` in `.env`, then run `docker compose --profile tls up -d`. Rootless Podman can't bind ports below 1024 by default, so set `CADDY_HTTP_PORT=8081` and `CADDY_HTTPS_PORT=8443` there.
- To host from your own PC without buying a domain (students reach it through your router's public IP), copy `home.env.example` to `.env` and follow the steps at its top. It uses a free sslip.io name so Caddy can get a real certificate, and it covers port forwarding, CGNAT and a Cloudflare tunnel fallback. The in-app docs page *Deploying without a domain* (`/docs/deploy-without-domain`) covers this and the other setups without a domain: a VPS, a tunnel, and a classroom network only.

## Deployment (Ubuntu, single 4 GB host)

1. Install PostgreSQL 16/17 and copy `deploy/postgresql.conf.d/quiz.conf` into its `conf.d`.
2. Build the backend with `GOOS=linux go build -o server ./cmd/server` and the frontend with `npm run build`, then copy them to `/srv/quiz/server` and `/srv/quiz/frontend`.
3. Put the master key in `/etc/quiz/kek.key` (root-owned, 0400) and keep an offline backup of it. Losing it means teachers must re-enter their API keys.
4. Install `deploy/quiz.service` (it sets `GOMEMLIMIT=700MiB` and `MemoryMax=900M`) and `deploy/Caddyfile` (with your domain).
5. Enable backups: `deploy/backup.sh` with `quiz-backup.timer` (age-encrypted, 14 daily and 8 weekly copies). Test a restore every month. Admins can also export encrypted backups from the console; `server restore-backup` restores them into an empty database.
6. Before real classes, run `loadtest/classroom.js` with k6 against a staging copy.
7. Tutoring (optional): LiveKit with `deploy/livekit.yaml`, the tutoring service with `deploy/quiz-tutoring.service`, the `tutor.` site in `deploy/Caddyfile`, and UDP 7882 and TCP 7881 open. The in-app *Deployment* page has the steps; `node loadtest/tutoring-media.mjs` checks LiveKit's capacity on a machine.

## License

[MIT](LICENSE) © 2026 Nadun Udaraka
