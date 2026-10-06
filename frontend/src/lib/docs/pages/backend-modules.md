# Backend modules

Every module lives in `backend/internal/<name>`, owns one PostgreSQL schema, exposes a `Service` (or `Store`), and mounts its HTTP routes through `TeacherRoutes`, `StudentRoutes`, `AdminRoutes` and similar functions called from `internal/app/app.go`.

## platform (shared plumbing)

| Package | Purpose |
|---------|---------|
| `platform/config` | Reads `QP_*` environment variables. The KEK path falls back to `$CREDENTIALS_DIRECTORY/kek` (systemd `LoadCredential`). |
| `platform/db` | Opens the pgx pool; `Migrate` applies `migrations/*.sql` in order, each in its own transaction, recorded in `public.schema_migrations`. |
| `platform/httpx` | `Handler`, `Error` and its helpers (`Invalid`, `Conflict`, ...), `Decode` (1 MiB, unknown fields rejected), `Recover`, `SecurityHeaders`, `SameOrigin`, `ClientIP`, `SPA`. |
| `platform/jobs` | River client, queue names and caps, the `Inserter` interface (`InsertTx`), River migrations. |
| `platform/audit` | `audit.Log(ctx, tx, actor, action, targetType, targetID, details)`: write it inside the transaction of the action. |
| `platform/dbtest` | Real-Postgres test harness (see [Testing](testing.md)). |

## auth (schema `auth`)

Accounts, passwords, sessions, verification, password reset, and admin account management (FR-ACC).

- `password.go`: Argon2id (m=19456 KiB, t=2, p=1). `Hasher` runs at most N hashes at once with a queue of 200 and a 10 s acquire timeout; when full it returns `ErrBusy`, which becomes `503 Retry-After: 2`.
- `service.go`: `Register`, `Login`, `CreateSession`, `Authenticate` (idle and absolute timeouts per role), `Logout`, `VerifyEmail`, `RequestPasswordReset`, `ResetPassword`, `UsersByID`, `CreateAdmin`.
- `admin.go`: `ListUsers`, `SetStatus`, `DeleteUser` and `DeleteOwnAccount` (both anonymise), and `GetPolicy`/`SetPolicy` (teacher approval).
- `http.go`: `Middleware`, `RequireRole`, `CurrentUser`/`MustUser`, cookie helpers, and the routes.
- `ratelimit.go`: token bucket. Per IP: burst 300, refill every 200 ms (a class shares one NAT address). Per email: burst 10, refill every 30 s.

Tables: `users`, `sessions` (SHA-256 of the token), `tokens` (one-time verify/reset tokens), `policy`.

## mail (no schema)

Transactional email as a River job on the `email` queue (2 workers, 5 attempts), so a slow mail server never blocks a request. `mail.Args{Message}` is inserted in the same transaction as the token that the email carries. `Sender` has two implementations: `SMTPSender` (when `QP_SMTP_ADDR` is set) and `LogSender` (development: messages go to the log).

## settings (schema `settings`)

The configuration hierarchy (BA §7). `Overrides` is a struct of pointer fields; nil means "inherit". Struct tags declare the levels at which each key may be set and any enum values. `Resolve(base, layers...)` merges layers in order. `Store` persists the platform and teacher layers. See [Configuration hierarchy](settings.md).

## content (schema `content`)

Classrooms, modules, topics and enrolments (FR-CLS).

- Every query includes `teacher_id`, so another teacher's ids return 404 (BR-14).
- Classroom join codes are 8 characters and session codes 6, both drawn from an alphabet without `0/O/1/I`.
- `EnrolByCode` and `EnsureEnrolled` enforce BR-02/BR-03: the student number is required when configured, unique per classroom (case-insensitive), and changed only by the teacher once set.
- `TopicContext` gives downstream modules the owner and the classroom/module/topic setting layers.


`categories.go` holds student categories (V2-03, D-41): CRUD, bulk assignment limited to the classroom's own enrolments, and `CategoryMembers` for forming groups.
## quiz (schema `quiz`)

Quizzes, questions, resources and CSV formats (FR-QZ, BA §10).

- `question.go`: the type model (`Body`, `Key`, `Feedback`), `Normalise` (assigns choice ids, derives blanks from `[[n]]`), `Validate` (per-type rules) and `StudentView` (drops key, feedback and rubric).
- `csv.go`: `ParseQuestionsCSV` (row-level errors with real line numbers; 2 MB / 1000-row limit), `ParseResourcesCSV`, `ParseResourceName`, and the downloadable `Template`.
- `response.go`: the `Response` shape and `ValidateResponse`.
- `service.go`: CRUD, duplicate, reorder, `Readiness`/`SetStatus`, import, resources, and `CheckResources` (a River worker).

Depends on `imageurl` for Drive link normalisation and SSRF-safe checks.

## imageurl (no schema)

`Normalise` converts Google Drive share links to `https://drive.google.com/thumbnail?id=…&sz=w1000` (the legacy `uc?export=view` form has returned 403 since January 2024). `Checker` fetches with a dialer that refuses private, loopback and link-local addresses after DNS resolution, a 5 MB limit, an `image/*` content-type check, timeouts and no cookies (ADR-10).

## live (schema `live`)

Sessions, attempts, answers, violations and the real-time hub (FR-SS, FR-PR). This is the most intricate module; see [Live sessions & timing](live-sessions.md) and [Proctoring](proctoring.md).

- `model.go`: states, `Session`, `Attempt`, `Snapshot`, and `Session.Effective(question, studentOverrides)`.
- `timing.go`: pure functions (`admit`, `start`, `advance`, `pause`, `resume`, `extend`, `decide`, `newOrders`), unit-tested without a database.
- `service.go`: every state transition goes through `change()`, which locks the attempt row, mutates it, saves it and broadcasts after commit.
- `ticker.go`: `Run` (1 Hz `Tick` plus a 2 Hz dashboard flush) and `expire`.
- `hub.go`: connection registry, `StudentState` and `Dashboard` builders.
- `ws.go`: the two WebSocket endpoints.
- `results.go`: `MarkingData` for eval, release/unrelease, and the student's attempt list.

## eval (schema `eval`)

Marking and feedback (FR-EV).

- `marker.go`: `MarkByKey` (partial credit, negative marks, Levenshtein spelling tolerance) and `PredefinedFeedback`.
- `service.go`: `EvaluateAttempt` (River worker), `recompute` (attempt totals), `Override`, `SessionResults`, `StudentResult`, and the hooks `ApplyLLM`/`LLMFailed`/`MarkPendingTx` used by `llm`.

See [Marking & feedback](marking.md).

## llm (schema `llm`)

The LLM gateway (FR-EV-02/03/05, ADR-09, ADR-16).

- `vault.go`: AES-256-GCM envelope encryption. `LoadKEK` reads the master key file and refuses group- or world-readable files on Unix.
- `providers.go`: `Anthropic` (official Go SDK, structured JSON output, default model `claude-opus-5-5`), `OpenAI` and `Google` (plain HTTP with JSON-schema output), and error classification into permanent vs retryable.
- `prompt.go`: PII scrubbing, delimited untrusted answer, schema, `Check` (clamping and review flags).
- `service.go`: key CRUD (never readable), `EnqueueTx`, `Remark`, `Estimate`, and the `Worker`.

## analytics (schema `analytics`)

`compute.go` turns results into class, question and student statistics. `service.go` stores them per session (`session_stats`), rolls them up per quiz, handles share links and the public view, and exports CSV. See [Results & analytics](analytics.md).

## poll (schema `poll`)

Live polls (D-40). `model.go` defines the 20 question types, their validation and answer checking; `aggregate.go` turns answers into live results; `files.go` sniffs and stores file, audio and video answers; `hub.go` pushes results to presenters and participants twice a second; `score.go` holds answer keys and marking (partial credit, speed bonus) and `leaderboard.go` the ranking (D-42); `groups.go` forms groups, enforces first-answer and captain rules and combines group scores (D-43); `http.go` has the teacher, public and WebSocket routes and the CSV export. See [Live polls](polls.md).

## admin (no schema)

Usage counts and the audit log for admins, plus `/api/my/data` (own-data export) for every user.

## apidocs (no schema)

The embedded OpenAPI 3.1 document (`openapi.yaml`) and Swagger UI assets, served at `/api/docs`. See [HTTP API](api.md).

## app

The composition root (`app.go`): builds every service, wires interfaces and hooks, registers River workers, and mounts routes. `app/apptest` is the end-to-end test harness.
