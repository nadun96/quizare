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
