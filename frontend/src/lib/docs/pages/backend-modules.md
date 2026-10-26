# Backend modules

Every module lives in `backend/internal/<name>`, owns one PostgreSQL schema, exposes a `Service` (or `Store`), and mounts its HTTP routes through `TeacherRoutes`, `StudentRoutes`, `AdminRoutes` and similar functions called from `internal/app/app.go`.

## platform (shared plumbing)

| Package | Purpose |
|---------|---------|
| `platform/config` | Reads `QP_*` environment variables. The KEK path falls back to `$CREDENTIALS_DIRECTORY/kek` (systemd `LoadCredential`). |
| `platform/db` | Opens the pgx pool; `Migrate` applies `migrations/*.sql` in order, each in its own transaction, recorded in `public.schema_migrations`. |
| `platform/httpx` | `Handler`, `Error` and its helpers (`Invalid`, `Conflict`, ...), `Decode` (1 MiB, unknown fields rejected), `Recover`, `SecurityHeaders`, `SameOrigin`, `ClientIP`, `SPA`. |
| `platform/jobs` | River client, queue names and caps, the `Inserter` interface (`InsertTx`), River migrations. |
| `platform/page` | Server pagination (D-55): `Parse` reads `page`, `size`, `q`, `sort` and `dir` against a whitelist of sort keys (`Sorts`), `OrderBy` adds a unique tie-breaker so no row shows on two pages, `Like` escapes the search for `ILIKE`, `Write` sends the list with `total`, `page` and `size`. `Slice` and `Matches` page lists that are bounded by something else and need another module's data to search or sort (a classroom's students by name, a poll's participants and answers). |
| `platform/audit` | `audit.Log(ctx, tx, actor, action, targetType, targetID, details)`: write it inside the transaction of the action. |
| `platform/dbtest` | Real-Postgres test harness (see [Testing](testing.md)). |

## auth (schema `auth`)

Accounts, passwords, sessions, verification, password reset, and admin account management (FR-ACC). `account.go` lets people change their password and profile picture (D-49).

- `password.go`: Argon2id (m=19456 KiB, t=2, p=1). `Hasher` runs at most N hashes at once with a queue of 200 and a 10 s acquire timeout; when full it returns `ErrBusy`, which becomes `503 Retry-After: 2`.
- `service.go`: `Register`, `Login`, `CreateSession`, `Authenticate` (idle and absolute timeouts per role), `Logout`, `VerifyEmail`, `RequestPasswordReset`, `ResetPassword`, `UsersByID`, `CreateAdmin`.
- `admin.go`: `ListUsers`, `SetStatus`, `DeleteUser` and `DeleteOwnAccount` (both anonymise), and `GetPolicy`/`SetPolicy` (teacher approval). A manager may act only on teachers and students, and the last active admin can't be suspended or deleted.
- `manager.go` (D-56): the admin features a manager can be given (`Features`), the `RequireStaff` and `RequireFeature` route checks, and `ListManagers`, `MakeManager` (an existing teacher, or a new manager-only account), `SetManagerFeatures` and `RemoveManager`.
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

Quizzes, questions, resources and CSV formats (FR-QZ, BA §10). `pools.go` holds question categories, pool rules and each teacher's question bank (D-66): the bank is a quiz of kind `bank` with no topic, so it reuses questions, import, resources and preview; it can't be marked ready, run or deleted. `SessionQuestions` gives a new session the quiz's questions plus the bank questions its pools need (`FromBank`, codes prefixed `B-`).

- `question.go`: the type model (`Body`, `Key`, `Feedback`), `Normalise` (assigns choice ids, derives blanks from `[[n]]`), `Validate` (per-type rules) and `StudentView` (drops key, feedback and rubric).
- `csv.go`: `ParseQuestionsCSV` (row-level errors with real line numbers; 2 MB / 1000-row limit), `ParseResourcesCSV`, `ParseResourceName`, and the downloadable `Template`.
- `response.go`: the `Response` shape and `ValidateResponse`.
- `service.go`: CRUD, duplicate, reorder, `Readiness`/`SetStatus`, import, resources, and `CheckResources` (a River worker).

Depends on `imageurl` for Drive link normalisation and SSRF-safe checks.

## imageurl (no schema)

`Normalise` converts Google Drive share links to `https://drive.google.com/thumbnail?id=…&sz=w1000` (the legacy `uc?export=view` form has returned 403 since January 2024). `Checker` fetches with a dialer that refuses private, loopback and link-local addresses after DNS resolution, a 5 MB limit, an `image/*` content-type check, timeouts and no cookies (ADR-10).

## live (schema `live`)

Sessions, attempts, answers, violations and the real-time hub (FR-SS, FR-PR); `teams.go` forms teams (D-44). This is the most intricate module; see [Live sessions & timing](live-sessions.md) and [Proctoring](proctoring.md).

- `model.go`: states, `Session`, `Attempt`, `Snapshot`, and `Session.Effective(question, studentOverrides)`.
- `timing.go`: pure functions (`admit`, `start`, `advance`, `pause`, `resume`, `extend`, `decide`, `newOrders`), unit-tested without a database.
- `service.go`: every state transition goes through `change()`, which locks the attempt row, mutates it, saves it and broadcasts after commit.
- `ticker.go`: `Run` (1 Hz `Tick` plus a 2 Hz dashboard flush) and `expire`.
- `hub.go`: connection registry, `StudentState` and `Dashboard` builders.
- `ws.go`: the two WebSocket endpoints.
- `results.go`: `MarkingData` for eval, release/unrelease, and the student's attempt list.

## eval (schema `eval`)

Marking and feedback (FR-EV). `teams.go` ranks session teams from members' marks; the rules for combining marks are shared with polls in `platform/groupscore`.

- `marker.go`: `MarkByKey` (partial credit, negative marks, Levenshtein spelling tolerance) and `PredefinedFeedback`.
- `service.go`: `EvaluateAttempt` (River worker), `recompute` (attempt totals), `Override`, `SessionResults`, `StudentResult`, and the hooks `ApplyLLM`/`LLMFailed`/`MarkPendingTx` used by `llm`.

See [Marking & feedback](marking.md).

## llm (schema `llm`)

The LLM gateway (FR-EV-02/03/05, ADR-09, ADR-16).

- `vault.go`: AES-256-GCM envelope encryption. `LoadKEK` reads the master key file and refuses group- or world-readable files on Unix.
- `providers.go`: `Anthropic` (official Go SDK, structured JSON output, default model `claude-opus-5-5`), `OpenAI` and `Google` (plain HTTP with JSON-schema output), and error classification into permanent vs retryable.
- `prompt.go`: PII scrubbing, delimited untrusted answer, schema, `Check` (clamping and review flags).
- `service.go`: key CRUD (never readable), `EnqueueTx`, `Remark`, `Estimate`, and the `Worker`.
- `tutorial.go`: the same for tutorial answers (D-75): `EnqueueTutorialTx`, the `llm_tutorial_mark` job and `TutorialWorker`, with the pseudonym made from the run.

## analytics (schema `analytics`)

`compute.go` turns results into class, question and student statistics. `service.go` stores them per session (`session_stats`), rolls them up per quiz, handles share links and the public view, and exports CSV. See [Results & analytics](analytics.md).

## poll (schema `poll`)

Live polls (D-40). `model.go` defines the 20 question types, their validation and answer checking; `aggregate.go` turns answers into live results; `files.go` sniffs and stores file, audio and video answers; `hub.go` pushes results to presenters and participants twice a second; `score.go` holds answer keys and marking (partial credit, speed bonus) and `leaderboard.go` the ranking (D-42); `groups.go` forms groups, enforces first-answer and captain rules and combines group scores (D-43); `board.go` is the whiteboard: strokes, who may draw, and instant pushes (D-47); `http.go` has the teacher, public and WebSocket routes and the CSV export. See [Live polls](polls.md).

## storage (schema `storage`)

Storage and backups (D-57, PL-FR-04 to PL-FR-09). `service.go` measures the database by area from PostgreSQL's catalogues (hourly, as a River periodic job, and on refresh), keeps the limits, runs clean-ups through areas other modules register (`AddArea`; polls offer their old uploaded files), makes console backups in the background, prunes them to the backup space, issues single-use download links and records the nightly job's files from `QP_NIGHTLY_BACKUP_DIR`. `archive.go` is the backup format: one `REPEATABLE READ, READ ONLY` snapshot written as COPY text per table, gzipped and encrypted with age under the admin's passphrase, plus `Check` and `Restore` (used by `server check-backup` and `server restore-backup`). `disk_*.go` reads free disk space.

## directory (schema `directory`)

School sign-in with Windows Active Directory or another LDAP directory (D-64); see [School sign-in](directory.md). `config.go` holds the one row of settings, with the service account's password sealed with the master key (`llm.Vault`), and checks them (encrypted connections only, a filter with `{login}`, valid DNs). `ldap.go` connects (LDAPS or StartTLS, certificates always checked), finds the person with the service account, binds as them with their password, and works out the role from the teacher and student groups (AD's in-chain rule for nested groups, or `memberOf`). `service.go` implements `auth.Directory`, caches the settings for 30 s, runs the admin's step-by-step test, and the hourly `directory_check` job that signs out accounts disabled, removed or regrouped in the directory. `directorytest` is an LDAP server for tests that really evaluates filters.

## drive (schema `drive`)

Teachers' Google Drive (D-71); see [Google Drive](drive.md). `service.go` keeps the platform's Google app (the client secret sealed with the master key), each teacher's connection (the OAuth consent with PKCE and `access_type=offline`, the refresh token sealed, access tokens cached and refreshed, a token Google refuses marks the connection for reconnecting), and hands Google's picker a short-lived token. `google.go` is a small client for the Drive API calls it needs, with only the `drive.file` scope. `files.go` attaches picked files to classrooms and keeps their sharing in step with enrolments (every 15 minutes, `drive_sync`), shares a file by link for questions, and imports questions from a CSV or Google Sheet through `quiz.ImportQuestions`. `results.go` saves a session's or poll's CSV as a Google Sheet in the teacher's results folder, on demand or after each session (`drive_backup`, queued in the transaction that ends it). `drivetest` is a fake Google for tests.

## oidc (schema `oidc`)

Sign-in with OpenID Connect providers (D-70); see [Sign-in with other accounts](sso.md). `providers.go` stores the providers an admin adds, with each client secret sealed with the master key (`llm.Vault`), and checks them (https issuers, or http on localhost; who becomes what). `service.go` runs the sign-in: the start redirect (state cookie, nonce, PKCE, the row in `oidc.logins`), and the callback, which exchanges the code with go-oidc and `x/oauth2`, checks the ID token and hands auth an `ExternalUser`. Which account that is — seen before, the same email, or new — is `auth.SignInExternal` (`auth/external.go`), which also keeps `auth.identities`, links from the account page and refuses to unlink an account's only way in. `oidctest` is a provider for tests that checks the client, the redirect and PKCE and signs real ID tokens.

`convert.go` (D-78) turns presentations into PDFs through the teacher's Drive: an uploaded PowerPoint, OpenDocument or Keynote file is uploaded as Google Slides, exported as PDF (through the export link past Drive's 10 MB export limit) and deleted; a file picked in Drive is exported, downloaded or converted. The server stays free of office software.

## meet (schema `meet`)

Peer-to-peer video meetings (D-69): small calls where every browser sends its microphone, camera and screen straight to every other one, separate from tutoring's media server. `service.go` opens meetings (a title, an optional classroom, 2–8 people), lists them with how many are in, ends them, keeps attendance (first joined, last seen, joins) and checks who may join: open meetings only, and with a classroom only the host and its actively enrolled students. It hands out the ICE servers: the STUN servers of `QP_MEET_STUN`, and a TURN relay when `QP_MEET_TURN_URLS` is set, with 6-hour credentials made the coturn `use-auth-secret` way from the secret in `QP_MEET_TURN_SECRET_FILE`. `hub.go` is the signalling relay at `/ws/meet/{code}`, in memory: who is in each meeting, their mic, camera, screen and hand state, offers, answers and candidates passed from one person to another, one connection per person (a new tab replaces the old), a full meeting closed with code 4009, and the host's remove and end. Media never reaches the server. `board.go` is the meeting whiteboard (D-72): the poll board's strokes (`platform/whiteboard`, shared with polls) over the same socket, kept in memory with the meeting until it ends (up to 20,000 marks); the host shows or hides it for everyone and lets everyone draw or only themselves; requests are answered with `board_ack`, so the browser's stroke queue works as it does over HTTP. Admins set the limits (`meet.limits`, D-72): people per meeting and open meetings per teacher, 0 for unlimited; a lower size applies to open meetings at once.

## tutorial (schema `tutorial`)

Interactive tutorials (D-74): lessons in pages that students work through at their own pace. A tutorial belongs to a topic, and its questions live in a hidden quiz of kind `tutorial` (`quiz/tutorial.go`), so teachers write them with the usual question editor and settings inheritance; quiz lists, pools and live sessions refuse that quiz. `model.go` checks pages: up to 100, each with up to 30 blocks (text, image, video, document, link) and 20 questions; videos become YouTube's privacy-enhanced player or Drive's preview, documents Google's viewer, so the browser frames nothing else. `service.go` is the teacher's side (create, settings, pages, order, publish, release, delete while no student started); a question sits on one page at most. `run.go` is the student's: open, read, move (forward only unless going back is allowed; a checkpoint holds the next page until the page's questions are answered or right), answer (marked at once by key with `eval.MarkByKey`; written answers wait for the teacher), finish. Feedback shows on the page, at the end, or when the teacher releases it; the answer key only once an answer can no longer change. Past the due date a tutorial is read-only. `progress.go` lists every enrolled student with how far they are, their time and how often they left the app, shows one student's answers and pages, and lets the teacher mark waiting ones. Phase 2 (D-75): `leaving.go` records time on the page a student is on (at most a minute a report, and no more than has passed since the last) and each time they left the app: allowed where the page allows it or through its own links, otherwise counted, and on a page that doesn't allow it, enough leaves lock the page until the teacher unlocks it (`forgiven` keeps the record while restarting the count). `analytics.go` works out per-page reach, median time, drop-off, leaves, locks, tries and question results, and for reading pages the score on the questions after it for students above and below the median time there. `llm.go` hands written answers to the LLM gateway (`tutorial.LLMQueue`) and takes its marks back, only if the answer hasn't changed since. Questions can be copied in from the question bank, and pages duplicated with copies of their questions (`quiz.CopyQuestions`).

## presentation (schema `presentation`)

Presentations, self-paced (D-76): slide decks from a PDF that students go through at their own pace, with the time each spent on each slide. The teacher's browser renders the PDF's pages with pdf.js (no eval, no scripts from the file) and uploads them: `upload.go` takes a new file beside the one in use (`Begin`, the PDF, each slide as a JPEG checked with `image/jpeg` and its text, then `Finish`), so students never see half a deck; the old file and slides are then deleted, and view records stay with their slide numbers. `view.go` is the student's side: open, move (any slide, or in order up to the next unseen one), time on the slide they're on (at most a minute a report, and no more than has passed), the slide images (cached a day by version; in order, the next one may load ahead) and the PDF when the teacher allows it. `report.go` gives the teacher each student's slides seen and time, each slide's viewers and median time, and one student's time per slide. Slides and files count as their own storage area. Presenting live (D-77): `live.go` starts a session with a 6-character code (one at a time), moves the slide, lets students look back or not, and ends it; while it's live, the classroom's students may join even a draft, and see slides up to the furthest shown, never ahead. `livehub.go` is the WebSocket at `/ws/presentations/{id}`: the teacher joins as presenter and gets the audience (joined, following, looking back, away) at most once a second; students report their slide, following and tab visibility when it changes and every 15 s, and the server counts live time from those reports (at most a minute per report), kept apart from time at their own pace. Against marks (D-78): `analysis.go` links a presentation to quizzes and tutorials of its topic (`Marks`, wired in the app from quiz, eval's `QuizScores`, best attempt not invalidated, and tutorial's `Scores`, marked answers), sets each student's time beside their combined score with a least-squares trend and r, splits each slide's students at the median time, and writes the records as CSV (formula-safe), also saved to Drive.

## tutorlink (no schema)

The platform's side of tutoring (D-59, ADR-23). `tutorlink.go` signs 5-minute tokens naming the logged-in person for the separate tutoring service (`POST /api/tutoring/token`), says whether tutoring is on (`GET /api/tutoring/config`), and answers the tutoring service's `POST /internal/tutoring/access` (from `content.ClassroomAccess`) and `POST /internal/tutoring/teacher` (an active teacher by email, for co-teachers) — not under `/api`, and only with the service's own one-minute token. Off unless `QP_TUTORING_URL` and `QP_TUTORING_SECRET_FILE` are set. The tutoring service itself is a separate Go module in `tutoring/`; see [Tutoring sessions](tutoring.md).

## admin (no schema)

Usage counts and the audit log for admins and managers given those features (filterable by person and by role acted in), plus `/api/my/data` (own-data export) for every user.

## oversight (schema `oversight`)

The admin's live activity (D-90), behind the `live_activity` feature. `Live` lists what's live across `live`, `poll`, `meet` and `presentation` in one read-only query; `End` calls that module's `ForceEnd`/`ForceClose`/`ForceEndLive` (which skip the ownership check), logs the reason and emails the teacher. `oversight.pauses` holds a pause for everyone (no teacher) and one per teacher; `Check` is installed in each module with `SetGate` and runs before anything starts, and `Paused` goes to tutoring in the `/internal/tutoring/access` answer (`paused`).

## apidocs (no schema)

The embedded OpenAPI 3.1 document (`openapi.yaml`) and Swagger UI assets, served at `/api/docs`. See [HTTP API](api.md).

## app

The composition root (`app.go`): builds every service, wires interfaces and hooks, registers River workers, and mounts routes. `app/apptest` is the end-to-end test harness.
