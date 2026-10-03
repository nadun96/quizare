# QR Classroom Quiz Platform

Timed, proctored in-class quizzes that students join by scanning a QR code, with
answer-key, rule-based and LLM-assisted marking and configurable results publishing.

- Requirements: `docs/QR Classroom Quiz Platform — Business Analysis Document.pdf`
- Architecture & ADRs: `docs/QR Classroom Quiz Platform — Architecture Document.pdf`
- Stack: `docs/tech_stack_recommendations.xlsx`

## Layout

| Path | What |
|------|------|
| `backend/` | Single Go binary (modular monolith): Chi, coder/websocket, River, pgx |
| `frontend/` | SvelteKit static SPA (`adapter-static`) — student and teacher route groups |
| `deploy/` | Caddyfile, systemd units, PostgreSQL tuning |

## Branching

- `main` — released code only.
- `test` — release candidates; merged from `dev` once the full test suite passes.
- `dev` — integration branch.
- `feature/<name>` — one branch per feature, cut from `dev`, merged back with `--no-ff`.

## Development

```sh
cd backend
go run ./cmd/testdb    # terminal 1: shared embedded PostgreSQL 16 on :54329
export QP_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:54329/postgres?sslmode=disable'
go test ./...          # without the env var each package starts its own embedded server (slower)
```

Run the server (needs a 32-byte key file, never an environment variable — ADR-09):

```sh
openssl rand 32 > kek.key           # chmod 0400 in production
QP_DATABASE_URL=postgres://... QP_KEK_FILE=kek.key QP_DEV=1 go run ./cmd/server
echo 'password' | go run ./cmd/server create-admin admin@school.edu "Admin"
```

Design choices that fill gaps in the BA document are logged in `DECISIONS.md`.
