# HTTP API

The full reference, with schemas for every request and response, is the OpenAPI 3.1 document. Open the **[Swagger UI](/api/docs)** on a running server, or fetch `/api/docs/openapi.yaml` or `/api/docs/openapi.json` (for code generators; every operation has an `operationId`). Source: `backend/internal/apidocs/openapi.yaml`.

## Conventions

- **JSON in, JSON out.** Request bodies are capped at 1 MiB (CSV uploads at 2 MB) and **unknown fields are rejected** (400).
- **Auth** is the HttpOnly `__Host-sid` cookie set by `POST /api/auth/login` or `/register`. Tokens never appear in response bodies.
- **CSRF**: every POST, PUT, PATCH or DELETE under `/api` must send an `X-Requested-With` header (any value) and, if present, an `Origin` equal to `QP_BASE_URL`. Cross-site forms can't set custom headers, and cross-site `fetch` can't without a preflight the server never grants. Otherwise the response is 403 with code `csrf`.
- **Ownership**: another teacher's or student's resource returns **404**, not 403, so ids can't be probed.
- **IDs** are UUIDs. Join codes are uppercase alphanumerics without `0/O/1/I`.
- **Times** are RFC 3339 in JSON, except live-session deadlines, which are epoch **milliseconds** of server time.
- **Lists are paginated** (D-55, PL-FR-01). Every list whose data can grow takes `page` (from 1), `size` (1–100, default 25), `q` (search across all pages), `sort` (a key the endpoint lists in the spec) and `dir` (`asc` or `desc`; each sort has its own default). The response keeps the list's own key and adds `total` (rows matching, on all pages), `page` and `size`: `{"classrooms": [...], "total": 340, "page": 2, "size": 25}`. Bad values fall back to the defaults instead of failing, so old links still open; an unknown `sort` is ignored. Lists that can't grow past a small fixed size (a quiz's questions) and the live dashboards are not paginated.

## Errors

```json
{ "code": "validation_failed", "message": "validation failed",
  "fields": { "email": "enter a valid email address", "settings.admission_mode": "cannot be set at quiz level (allowed: classroom,session)" } }
```

| Status | Typical `code` |
|--------|----------------|
| 400 | `bad_request`, `invalid_token` |
| 401 | `unauthorized`, `invalid_credentials` |
| 403 | `forbidden`, `csrf`, `account_suspended`, `pending_approval`, `enrolment_pending`, `results_not_released` |
| 404 | `not_found` (also for malformed UUIDs) |
| 409 | `conflict`, `wrong_state`, `quiz_not_ready`, `quiz_has_warnings`, `has_results`, `classroom_archived` |
| 410 | `session_ended`, `link_unavailable` |
| 422 | `validation_failed`, with `fields` |
| 429 | `rate_limited` |
| 503 | `busy` (password-hashing queue full; honour `Retry-After`) |
| 500 | `internal` (details are logged server-side, never returned) |

## Route map

| Prefix | Role | Covers |
|--------|------|--------|
| `/api/auth/*` | public / any | register, login, logout, me, verify, reset, delete own account |
| `/api/admin/*` | admin, or a manager with the route's feature (D-56) | users, auth policy, platform settings, usage, audit; managers (admins only) |
| `/api/teacher/settings` | teacher | teacher defaults |
| `/api/teacher/classrooms…`, `/modules…`, `/topics…`, `/enrolments…` | teacher | content tree, enrolments |
| `/api/teacher/quizzes…`, `/questions…`, `/resources…`, `/quiz-template.csv` | teacher | authoring, CSV, resources, readiness, preview |
| `/api/teacher/quizzes/{id}/sessions`, `/api/teacher/sessions/{id}/…`, `/attempts/{id}/reinstate` | teacher | live control, release, events |
| `/api/teacher/sessions/{id}/results`, `/marks/…` | teacher | review, overrides, re-marking, LLM estimate |
| `/api/teacher/llm-keys…` | teacher | API keys (never readable) |
| `/api/teacher/…/analytics`, `/share-links…`, `/export.csv` | teacher | analytics, sharing, export |
| `/api/join/…`, `/api/enrolments`, `/api/my/…`, `/api/attempts/…` | student (any role for classroom join and own data) | joining, answering, results |
| `/api/teacher/polls…`, `/api/teacher/poll-questions/{id}` | teacher | polls: questions, settings, status, presenter control, results, moderation, files, CSV |
| `/api/polls/{code}…` | anyone (login if the poll identifies people; `X-Poll-Token` for anonymous participants) | view, join, answer, upload; see [Live polls](polls.md) |
| `/api/public/results/{token}` | public | shared results |
| `/ws/attempts/{id}`, `/ws/sessions/{id}` | student / teacher | see [WebSocket protocol](realtime.md) |
| `/ws/polls/{code}`, `/ws/teacher/polls/{id}` | anyone / poll owner | live poll updates and results |
| `/beacon/attempts/{id}/violations` | student | `sendBeacon` fallback |
| `/healthz` | public | liveness and DB ping |

## Example: run a session from the command line

```sh
B=https://quiz.example.edu
H=(-H 'Content-Type: application/json' -H 'X-Requested-With: curl' -H "Origin: $B" -b jar -c jar)
curl "${H[@]}" -d '{"email":"t@school.edu","password":"…"}' $B/api/auth/login
curl "${H[@]}" -X POST $B/api/teacher/quizzes/$QUIZ/sessions -d '{"settings":{"countdown_seconds":30}}'
curl "${H[@]}" -X POST $B/api/teacher/sessions/$SESSION/admit -d '{"all":true}'
curl "${H[@]}" -X POST $B/api/teacher/sessions/$SESSION/extend -d '{"attempt_ids":["…"],"seconds":300}'
curl "${H[@]}" -X POST $B/api/teacher/sessions/$SESSION/end
```

## Keeping the spec honest

`internal/app/openapi_test.go` walks the Chi router and fails when:

- a route is missing from the spec;
- the spec documents a route that doesn't exist;
- a `$ref` doesn't resolve;
- an operation lacks a unique `operationId`.

CI also runs `redocly lint`. When you add or change a route, update `openapi.yaml` in the same commit.
