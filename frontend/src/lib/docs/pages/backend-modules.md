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
- `roles.go` (D-101) changes roles: admins any but their own, managers students and teachers only, never the last admin, never a teacher who still owns classrooms. `manager.go` (D-56): the admin features a manager can be given (`Features`), the `RequireStaff` and `RequireFeature` route checks, and `ListManagers`, `MakeManager` (an existing teacher, or a new manager-only account), `SetManagerFeatures` and `RemoveManager`.
- `http.go`: `Middleware`, `RequireRole`, `CurrentUser`/`MustUser`, cookie helpers, and the routes.
- `ratelimit.go`: token bucket. Per IP: burst 300, refill every 200 ms (a class shares one NAT address). Per email: burst 10, refill every 30 s.

Tables: `users`, `sessions` (SHA-256 of the token), `tokens` (one-time verify/reset tokens), `policy`.

## mail (no schema)

Transactional email as a River job on the `email` queue (2 workers, 5 attempts), so a slow mail server never blocks a request. `mail.Args{Message}` is inserted in the same transaction as the token that the email carries. `Sender` has two implementations: `SMTPSender` (when `QP_SMTP_ADDR` is set, or the mail server set in the console, D-104) and `LogSender` (development: messages go to the log). `SMTPSender` connects with `Security` `auto` (STARTTLS when offered), `starttls`, `tls` (port 465) or `none`, logs in with AUTH PLAIN, writes `Date`, `Message-ID` and MIME headers, and returns a `StepError` naming the step that failed (`connect`, `tls`, `login`, `send`). `mailtest` is a small SMTP server for tests. `Args.SendID` ties an email to a send to a group.

## settings (schema `settings`)

The configuration hierarchy (BA §7). `Overrides` is a struct of pointer fields; nil means "inherit". Struct tags declare the levels at which each key may be set and any enum values. `Resolve(base, layers...)` merges layers in order. `Store` persists the platform and teacher layers. See [Configuration hierarchy](settings.md).

## content (schema `content`)

Classrooms, modules, topics and enrolments (FR-CLS). `oversee.go` (D-101) is the staff's view of every classroom: list, students in and out, rename, archive, delete, and moving one to another teacher with its work. `purge.go` (D-92) deletes a classroom, module or topic with everything inside (quiz sessions and results, tutorials, quizzes, presentations) in one transaction, after `DeletePreview` has counted it; results go only when the name is typed, and nothing while a quiz session in it is open. Co-teachers (D-121, `teachers.go`): a classroom's owner invites another teacher by their account's email (an email goes out, and the invitation waits on their Classrooms page); once accepted, `content.teaches` is true for them, and every module's teacher checks ask it rather than comparing owners: classrooms, modules and topics, quizzes (`quiz.mayTeach`), sessions and marking (`live.ownedSession`, eval and LLM overrides), tutorials, presentations, the dashboard, trends, Explore and the inbox. Owner-only: the classroom's name and settings, join code, archiving and deleting, its teachers, and removing students (`owner_only`, 403). Someone who leaves or is removed loses the classroom's content, including what they made, which stays in the classroom; a question bank stays its own teacher's, and pools draw from the bank of the quiz's author.

- Every query includes `teacher_id`, so another teacher's ids return 404 (BR-14).
- Classroom join codes are 8 characters and session codes 6, both drawn from an alphabet without `0/O/1/I`.
- `EnrolByCode` and `EnsureEnrolled` enforce BR-02/BR-03: the student number is required when configured, unique per classroom (case-insensitive), and changed only by the teacher once set.
- `TopicContext` gives downstream modules the owner and the classroom/module/topic setting layers.


`categories.go` holds student categories (V2-03, D-41): CRUD, bulk assignment limited to the classroom's own enrolments, and `CategoryMembers` for forming groups.
## quiz (schema `quiz`)

Quizzes, questions, resources and CSV formats (FR-QZ, BA §10). `pools.go` holds question categories, pool rules and each teacher's question bank (D-66): the bank is a quiz of kind `bank` with no topic, so it reuses questions, import, resources and preview; it can't be marked ready, run or deleted. `SessionQuestions` gives a new session the quiz's questions plus the bank questions its pools need (`FromBank`, codes prefixed `B-`). Classroom pools (D-123, `poolbook.go`): each classroom has one question pool, a quiz of kind `pool` with its classroom and no topic, shared by its teachers (`mayTeach` checks the classroom). Quizzes in the classroom draw from it by pool rules of source `class`; a session copies the questions those rules need, marked `from_class` with codes `P-…`, and each student gets the rule's count from each category. Import has a preview (`?preview=1`, nothing saved, every row as it would be), a category for rows with none (`?category=`, from the file's name `Classroom__Category.csv`), and renumbering of codes already used (`?rename=1`: Q1 → Q1-2). Questions move between categories or are deleted in bulk; a category is renamed (merging into an existing one) or deleted with or without its questions; `GET /pools` lists the bank and every classroom's pool with their categories.

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


Local models (D-115, `local.go`): `llm.local` holds the admin's Ollama address and allowed models. `Ollama` is a provider that calls `POST /api/chat` with the marking JSON schema as `format`, `stream: false` and temperature 0, at an address only the admin sets; its keys carry no secret (`call` skips the vault). `resolveKey` falls back to `schoolKey` (id `local`, the default model) for a teacher with no key when the admin offers the default to everyone. `LocalState` asks the server for `/api/tags`; `TestLocal` marks a tiny answer and times it. Routes: `/api/admin/ai/local` (admins), `GET /api/teacher/llm-local`.

## grading (schema `grading`)

A classroom's gradebook and attendance (D-122). `Gradebook` lists the quizzes run in the classroom and its published tutorials with questions, each with its weight (a percent of the final result, set by any teacher of the classroom; they needn't add up to 100, since tests marked elsewhere may make up the rest), and every active student's marks: a quiz's best valid attempt, a finished tutorial's score out of its questions' marks. Points are mark × weight / 100 (55 % on a quiz worth 10 is 5.5); a weighted item without a mark counts 0; the final result is the points as a percentage of the weights' total. `Attendance` lists the classroom's quiz sessions and meetings between two dates and who came to each (admitted to the session, joined the meeting, with minutes); the page adds tutoring sessions from the tutoring service and builds the reports (CSV, print). Each item carries its module and topic, and the gradebook lists the classroom's quiz sessions, so the page can report on one module, topic, quiz or tutorial (D-130). Like the dashboard it reads other modules' tables and writes only its weights.

## loadtest (schema `loadtest`)

The admin's capacity test (D-125). `Start` refuses while real students are in a live quiz, makes a teacher and up to 2,000 students marked `synthetic` (one Argon2 hash shared by a random password), and runs in the background: through this server's own address, past the proxy (`selfURL`: the listening address on loopback), the teacher builds a classroom and a quiz over the API; then each step starts a session, students arrive over the ramp, sign in (retrying a busy server or the per-network limit as the app does), join, open their live connection, are admitted by the teacher every 2 s, answer every question after a random think time and submit, while the teacher's dashboard stays connected. Each student has a fake address from 198.18.0.0/15, or all share one (`one_network`), which the server believes because loopback is a trusted proxy. Sign-ins of synthetic accounts need the running test's secret in `X-QP-Load-Test` (`auth.SetLoadGate`), so nobody else can use them. Every second a sampler records the machine's CPU, free memory and load (Linux `/proc`), the Go heap and goroutines, database connections in use and waits, and the traffic the students' connections carried. A step passes when 99 % finish, answer saves are p95 < 150 ms and p99 < 400 ms, sign-in p95 < 3 s, join p95 < 1 s, under 1 % of requests fail and no live connection drops; the test stops at the first step that fails. `Cleanup` then deletes the accounts and everything they made (sessions, attempts, results, classroom, quiz, warehouse copies, audit events); `Recover` does the same at start-up after a crash. The report also has the app's first download, compressed, measured once. Routes: `/api/admin/loadtest` (admins only).

## insight (schema `olap`)

Multi-dimensional analysis (D-120). `load.go` is a small warehouse loader: every 5 minutes (River periodic job `insight_load`) it refreshes the dimension tables in full, writing only rows that changed, and copies facts whose sources changed since its last run (by `eval.results.updated_at` and `tutorial.runs.updated_at`, a minute of overlap): complete, valid quiz results and finished tutorials into `olap.activity`, their marked questions into `olap.answers`; once a day it removes facts whose source is gone. It is the only code here that reads other modules' tables, and only to copy. `query.go` is the engine: a catalog of measures (results, average, pass rate, tutorials, students, minutes; answers, marks gained, fully right) and dimensions (date by day, week, month, quarter or year; classroom, teacher, module, topic, quiz or tutorial, activity, question type, student), and `Run`, which turns a request for up to three dimensions, filters and a date range into one SQL statement built from the catalog alone (`GROUPING SETS` for subtotals and the grand total). Scope: a teacher sees their classrooms; an admin sees all but no student, and any group of fewer than 5 students is returned without values. `POST /api/insight/query`, `GET /api/insight/catalog`.

## analytics (schema `analytics`)

`compute.go` turns results into class, question and student statistics. `trends.go` (D-119) keeps month-by-month roll-ups per student in a classroom and per classroom (`student_months`, `classroom_months`): a periodic job every 15 minutes recomputes only the classroom-months whose results or finished tutorials changed since its last run, plus each open classroom's current month, and serves a classroom's trends with every student's figures, one student's months beside the class average, and a student's own months (`/api/my/trends`). Like the dashboard, it reads the other modules' tables without changing them. `service.go` stores them per session (`session_stats`), rolls them up per quiz, handles share links and the public view, and exports CSV. See [Results & analytics](analytics.md).

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

Interactive tutorials (D-74): lessons in pages that students work through at their own pace. A tutorial belongs to a topic, and its questions live in a hidden quiz of kind `tutorial` (`quiz/tutorial.go`), so teachers write them with the usual question editor and settings inheritance; quiz lists, pools and live sessions refuse that quiz. `model.go` checks pages: up to 100, each with up to 30 blocks (text, image, video, document, link) and 20 questions; videos become YouTube's privacy-enhanced player or Drive's preview, documents Google's viewer, so the browser frames nothing else. `service.go` is the teacher's side (create, settings, pages, order, publish, release, delete; once students have started, only with its title typed, D-92); a question sits on one page at most. `run.go` is the student's: open, read, move (forward only unless going back is allowed; a checkpoint holds the next page until the page's questions are answered or right), answer (marked at once by key with `eval.MarkByKey`; written answers wait for the teacher), finish. Feedback shows on the page, at the end, or when the teacher releases it; the answer key only once an answer can no longer change. Past the due date a tutorial is read-only. `progress.go` lists every enrolled student with how far they are, their time and how often they left the app, shows one student's answers and pages, and lets the teacher mark waiting ones. Phase 2 (D-75): `leaving.go` records time on the page a student is on (at most a minute a report, and no more than has passed since the last) and each time they left the app: allowed where the page allows it or through its own links, otherwise counted, and on a page that doesn't allow it, enough leaves lock the page until the teacher unlocks it (`forgiven` keeps the record while restarting the count). `analytics.go` works out per-page reach, median time, drop-off, leaves, locks, tries and question results, and for reading pages the score on the questions after it for students above and below the median time there. `llm.go` hands written answers to the LLM gateway (`tutorial.LLMQueue`) and takes its marks back, only if the answer hasn't changed since. Questions can be copied in from the question bank, and pages duplicated with copies of their questions (`quiz.CopyQuestions`).

## presentation (schema `presentation`)

Presentations, self-paced (D-76): slide decks from a PDF that students go through at their own pace, with the time each spent on each slide. The teacher's browser renders the PDF's pages with pdf.js (no eval, no scripts from the file) and uploads them: `upload.go` takes a new file beside the one in use (`Begin`, the PDF, each slide as a JPEG checked with `image/jpeg` and its text, then `Finish`), so students never see half a deck; the old file and slides are then deleted, and view records stay with their slide numbers. `view.go` is the student's side: open, move (any slide, or in order up to the next unseen one), time on the slide they're on (at most a minute a report, and no more than has passed), the slide images (cached a day by version; in order, the next one may load ahead) and the PDF when the teacher allows it. `report.go` gives the teacher each student's slides seen and time, each slide's viewers and median time, and one student's time per slide. Slides and files count as their own storage area. Presenting live (D-77): `live.go` starts a session with a 6-character code (one at a time), moves the slide, lets students look back or not, and ends it; while it's live, the classroom's students may join even a draft, and see slides up to the furthest shown, never ahead. `livehub.go` is the WebSocket at `/ws/presentations/{id}`: the teacher joins as presenter and gets the audience (joined, following, looking back, away) at most once a second; students report their slide, following and tab visibility when it changes and every 15 s, and the server counts live time from those reports (at most a minute per report), kept apart from time at their own pace. Against marks (D-78): `analysis.go` links a presentation to quizzes and tutorials of its topic (`Marks`, wired in the app from quiz, eval's `QuizScores`, best attempt not invalidated, and tutorial's `Scores`, marked answers), sets each student's time beside their combined score with a least-squares trend and r, splits each slide's students at the median time, and writes the records as CSV (formula-safe), also saved to Drive.

## tutorlink (no schema)

The platform's side of tutoring (D-59, ADR-23). `tutorlink.go` signs 5-minute tokens naming the logged-in person for the separate tutoring service (`POST /api/tutoring/token`), says whether tutoring is on (`GET /api/tutoring/config`), and answers the tutoring service's `POST /internal/tutoring/access` (from `content.ClassroomAccess`) and `POST /internal/tutoring/teacher` (an active teacher by email, for co-teachers) — not under `/api`, and only with the service's own one-minute token. Off unless `QP_TUTORING_URL` and `QP_TUTORING_SECRET_FILE` are set. The tutoring service itself is a separate Go module in `tutoring/`; see [Tutoring sessions](tutoring.md).

## admin (no schema)

Usage counts and the audit log for admins and managers given those features (filterable by person and by role acted in), plus `/api/my/data` (own-data export) for every user.

## oversight (schema `oversight`)

The admin's live activity (D-90), behind the `live_activity` feature. `Live` lists what's live across `live`, `poll`, `meet` and `presentation` in one read-only query; `End` calls that module's `ForceEnd`/`ForceClose`/`ForceEndLive` (which skip the ownership check), logs the reason and emails the teacher. `oversight.pauses` holds a pause for everyone (no teacher) and one per teacher; `Check` is installed in each module with `SetGate` and runs before anything starts, and `Paused` goes to tutoring in the `/internal/tutoring/access` answer (`paused`).

## mailing (schema `mailing`)

Email to groups of accounts (D-102), behind the `send_email` feature. `mailing.templates` are saved subjects and bodies; `check` refuses a placeholder that isn't in `Placeholders` (`name`, `first_name`, `email`, `role`, `classroom`, `site_url`) by name, and `Render` fills them in for each `Recipient`. `Recipients` turns an audience (everyone, students, teachers, staff, one classroom's active students, or up to 500 listed addresses) into active accounts, with the listed addresses that aren't any. `SendAll` queues one `mail.Args` job per recipient and the `mailing.sends` row in one transaction, and logs `email_sent`; `SendTest` sends only to the writer, marked [Test]. Routes under `/api/admin/email`.

The mail server (D-104, `server.go`): `mailing.server` is one row, its password sealed with the master key (`mailing`/`smtp`). `Sender()` is what the email worker uses: the console's settings while they're on (read again at most every 30 s, at once after a save), the config file's `SMTPSender` or `LogSender` otherwise. `TestServer` sends one email at once, outside the queue, with settings not yet saved or with what sends now, and reports the failing step. The queue (`queue.go`) reads River's `river_job` rows of kind `email` (kept 7 days: `jobs.NewClient` sets `CompletedJobRetentionPeriod`) as waiting, sent, retrying or failed with the last error, never the body; `Retry` uses River's `JobRetry`. The server settings are admins only; the queue goes with `send_email`.

## media (schema `media`)

Pictures inside rich text (D-113). `POST /api/teacher/images` takes the picture as the request body (PNG, JPEG, GIF or WebP, at most 8 MB and 40 megapixels), decodes it and draws it again at most 1600 px wide: PNG when it has transparency, JPEG otherwise, so only pixels are stored. `GET /api/images/{id}` serves it to anyone with the id (students and anonymous poll participants read the text it's in), with `nosniff`, a sandboxing CSP and a year's cache. The renderer shows only these addresses. The storage clean-up `images` deletes pictures uploaded before a date that no question (or its copy frozen in a quiz session), poll or tutorial page mentions.

## inbox (schema `inbox`)

Teachers' announcements and teacher–student messages (D-103). `inbox.announcements` belong to a classroom; `studentAnnouncements` scopes them to a student's active enrolments in classrooms that aren't archived, from 30 days before they joined; `inbox.announcement_reads` counts who read them. One `inbox.conversations` row for each teacher and student, with each side's `read_at`; `inbox.messages` in it. `teaches` (an active enrolment in a teacher's classroom that isn't archived) decides who may start or reply; the conversation stays readable after. New announcements (when the teacher asks) and the first unread message in a conversation are emailed to those whose `inbox.prefs.email` isn't off. Routes under `/api/my` (`inbox`, `announcements`, `contacts`, `conversations`) and `/api/teacher` (posting, editing and deleting announcements, readers).

## dashboard (no schema)

Each person's own dashboard (D-99), read-only across the modules. For a student: quiz sessions open in their classrooms that they haven't finished, presentations being presented and meetings open there (live); tutorials and presentations by their dates and the student's progress (opening later or due: coming up; past due and unfinished: overdue; no date: to do); and quizzes they missed, ended in the last 60 days after they joined without an attempt that got going. For a teacher: their live sessions, polls, meetings and presentations; their tutorials and presentations opening or due, or past due within 30 days with students still to finish; and what waits on them (answers to mark in quizzes and tutorials, students asking to join). `GET /api/my/dashboard` counts each list, `GET /api/my/dashboard/{list}` pages one. Tutoring sessions come from the tutoring service, asked by the page.

Insights (D-105, `insights.go`): `GET /api/my/dashboard/insights?days=30|90|365` gives the charts, each figure against the period before. A student: average score, quizzes, pass rate and tutorials; their last 30 released results with each quiz's class average; twelve weeks of quizzes and tutorials; their average per classroom. A teacher: students enrolled and active, quizzes run, average score and pass rate; the last 12 quizzes with results (average, pass rate, how many); twelve weeks of active students; each classroom's participation (students with a result in the period over students enrolled).

## progress (no schema)

A student's own progress (D-97), worked out on each request from released, fully marked quiz results and finished tutorials; nothing is stored. `progress.go` is pure: points (10 for taking part, the percentage, 20 for passing; 60 a tutorial), levels starting at 0, 100, 300, 600… XP, a weekly streak (Monday weeks, UTC; it holds until the current week ends) and seven badges with when each was earned. `GET /api/my/progress` serves students only; there is no ranking between students.

## apidocs (no schema)

The embedded OpenAPI 3.1 document (`openapi.yaml`) and Swagger UI assets, served at `/api/docs`. See [HTTP API](api.md).

## app

The composition root (`app.go`): builds every service, wires interfaces and hooks, registers River workers, and mounts routes. `app/apptest` is the end-to-end test harness.
