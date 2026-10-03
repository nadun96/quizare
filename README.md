# QR Classroom Quiz Platform

Timed, proctored in-class quizzes that students join by scanning a QR code, with
answer-key and LLM-assisted marking, predefined and AI feedback, and configurable
results publishing and analytics.

- Requirements: `docs/QR Classroom Quiz Platform — Business Analysis Document.pdf`
- Architecture & ADRs: `docs/QR Classroom Quiz Platform — Architecture Document.pdf`
- Stack: `docs/tech_stack_recommendations.xlsx`
- Choices that fill gaps in the BA document: `DECISIONS.md`

## Layout

| Path | What |
|------|------|
| `backend/` | Single Go binary (modular monolith, ADR-01): Chi, coder/websocket, River, pgx, PostgreSQL |
| `frontend/` | SvelteKit static SPA (`adapter-static`, ADR-11): student, teacher, admin and public pages |
| `deploy/` | Caddyfile, systemd units, PostgreSQL tuning, encrypted backup script |
| `loadtest/` | k6 "class of 100 × 3" scenario (architecture §2.3) |

Backend modules (`backend/internal/`), each owning one PostgreSQL schema:

| Module | Covers |
|--------|--------|
| `auth` | Accounts, Argon2id pool, `__Host-` sessions, verification, reset, admin account management (FR-ACC) |
| `settings` | Hierarchical configuration: platform → teacher → classroom → module → topic → quiz → question → session → student (BA §7) |
| `content` | Classrooms, modules, topics, enrolment with classroom student IDs (FR-CLS) |
| `quiz` | Seven question types, CSV import, resource mapping, Drive link normaliser, SSRF-safe link checks (FR-QZ, BA §10) |
| `live` | Sessions, QR join, waiting room, admission, countdown, server timers, pause/extend, proctoring, WebSocket hub (FR-SS, FR-PR) |
| `eval` | Answer-key marking, predefined feedback, overrides, release, student results (FR-EV) |
| `llm` | Per-teacher key vault (AES-256-GCM envelope), Anthropic/OpenAI/Gemini adapters, marking jobs (FR-EV-02/03/05, ADR-09) |
| `analytics` | Precomputed session/quiz analytics, public share links, CSV export (FR-RS, BA §11) |
| `admin` | Usage, audit log, own-data export |

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

Requirements: Go 1.26+, Node 20.19+ (22 recommended). No Docker needed: tests use an embedded PostgreSQL 16.

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

Run the server. The master key is read from a 32-byte file, never from an environment variable (ADR-09):

```sh
openssl rand 32 > kek.key && chmod 0400 kek.key
export QP_DATABASE_URL=postgres://... QP_KEK_FILE=kek.key QP_BASE_URL=http://localhost:5173
go run ./cmd/server
echo 'a-strong-password' | go run ./cmd/server create-admin admin@school.edu "Admin"
```

To serve the built SPA from the Go binary without Caddy, set `QP_STATIC_DIR=../frontend/build`.
Without `QP_SMTP_ADDR`, emails (verification, password reset) are written to the log.

| Variable | Default | Meaning |
|----------|---------|---------|
| `QP_DATABASE_URL` | (required) | PostgreSQL URL |
| `QP_KEK_FILE` | `$CREDENTIALS_DIRECTORY/kek` | 32-byte master key file (raw, hex or base64), mode 0400 |
| `QP_BASE_URL` | `http://localhost:8080` | Public origin: QR links, Origin/CSRF checks |
| `QP_LISTEN` | `127.0.0.1:8080` | Listen address (Caddy proxies to it) |
| `QP_DB_MAX_CONNS` | 15 | pgx pool size |
| `QP_ARGON2_WORKERS` | 2 | Concurrent password hashes (login-burst memory cap) |
| `QP_SMTP_ADDR`, `QP_SMTP_FROM`, `QP_SMTP_USER`, `QP_SMTP_PASSWORD_FILE` | | Optional SMTP relay |
| `QP_STATIC_DIR` | | Serve the SPA build from Go (dev / single binary) |
| `QP_API_DOCS` | on | `0` stops serving `/api/docs` |

## Deployment (Ubuntu, single 4 GB host)

1. Install PostgreSQL 16/17 and copy `deploy/postgresql.conf.d/quiz.conf` into its `conf.d`.
2. Build the backend with `GOOS=linux go build -o server ./cmd/server` and the frontend with `npm run build`, then copy them to `/srv/quiz/server` and `/srv/quiz/frontend`.
3. Put the master key in `/etc/quiz/kek.key` (root-owned, 0400) and keep an offline backup of it. Losing it means teachers must re-enter their API keys.
4. Install `deploy/quiz.service` (it sets `GOMEMLIMIT=700MiB` and `MemoryMax=900M`) and `deploy/Caddyfile` (with your domain).
5. Enable backups: `deploy/backup.sh` with `quiz-backup.timer` (age-encrypted, 14 daily and 8 weekly copies). Test a restore every month.
6. Before real classes, run `loadtest/classroom.js` with k6 against a staging copy.
