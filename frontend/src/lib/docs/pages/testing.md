# Testing

Every feature ships with tests. Backend integration tests run against a **real PostgreSQL 16**, never mocks (D-07).

## Layers

| Layer | Where | Example |
|-------|-------|---------|
| Pure unit tests | `*_test.go` beside the code | `live/timing_test.go` (deadlines, pause, AC-08 extension, policy, shuffles), `eval/marker_test.go`, `quiz/question_test.go` (the BA CSV examples), `settings/settings_test.go`, `imageurl` (Drive links, SSRF), `llm/llm_unit_test.go` (vault, prompt, adapters against fake HTTP servers) |
| API / integration | `<module>/*_api_test.go` | Full HTTP stack plus DB through `apptest`: auth flows, teacher isolation, CSV import AC-09, live sessions AC-01…08 with real WebSockets, marking AC-10, key privacy AC-11, public links AC-12 |
| Contract | `app/openapi_test.go` | Spec ↔ router in both directions, `$ref` resolution, operationIds |
| Frontend unit | `frontend/src/**/*.test.ts` | API client, server clock, offline answer queue, proctoring signals, socket backoff, docs registry and links, rich-text rendering and XSS payloads, word-cloud layout, display preferences, toasts and dialogs |
| Frontend components | `richtext/questionview.test.ts`, `poll/poll.test.ts`, `ui/ui.test.ts` | Svelte components mounted in jsdom: blanks inside formatted text, every poll input and the editor preview for all 20 types, results views, the confirm dialog. `src/test-setup.ts` stubs `matchMedia` and `Element.animate`, which jsdom lacks. |
| Design system | `ui/theme.test.ts` | WCAG AA contrast for every colour pair in both themes, read from `app.css` |
| End-to-end | ad hoc, headless Chrome | The real binary with the built SPA on a throwaway database: quiz flow, rich text editor, UI crawl (light/dark, phone/desktop, 320 px overflow), polls (every input type, live results, presenter, identity modes). Not part of CI yet. |
| Load | `loadtest/classroom.js` | k6, 300 sockets, the architecture §2.3 thresholds |

## The database harness

`platform/dbtest`:

- With `QP_TEST_DATABASE_URL` set, it uses that server (CI uses a Postgres 16 service container; locally, `go run ./cmd/testdb`).
- Otherwise it starts an embedded PostgreSQL 16 on a free port, once per test binary.
- It creates one **template** database per process, migrated (app plus River), and clones a fresh database for every `dbtest.New(t)` call with `CREATE DATABASE … TEMPLATE`. Tests are isolated and can run in parallel across packages.

A package using it needs:

```go
func TestMain(m *testing.M) { dbtest.Main(m) }
```

## The app harness

`app/apptest` builds the real app on a fresh database behind an `httptest` **TLS** server, so `__Host-` cookies and Origin checks behave as in production:

```go
e := apptest.New(t)                       // options: apptest.WithProviders(fakeLLM)
teacher := e.NewUser(auth.RoleTeacher)    // registered and logged in, own cookie jar
var c content.Classroom
teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
status, body := teacher.Raw("POST", path, "text/csv", csvBytes)
ws := teacher.Dial("/ws/sessions/" + id)  // real WebSocket
msg := apptest.ReadUntil(t, ws, "alert", 2*time.Second)
token := e.LastEmailToken(email, "Verify your email")
jobs := e.TakeJobs("evaluate_attempt")    // run job logic deterministically
```

River runs **insert-only** in tests. Drain queued jobs with `TakeJobs` and call the worker logic directly (`e.App.Eval.EvaluateAttempt`, `e.App.LLM.Process`, `e.App.Analytics.Recompute`).

**Time** is controlled by injecting a clock: `e.App.Live.SetClock(clk.now)`, then `clk.add(61 * time.Second)` and `e.App.Live.Tick(ctx)`. No sleeps, no flaky timers.

**LLM providers** are replaced with an in-process fake implementing `llm.Provider`, which also records requests so tests can assert that no personal data was sent.

## Running

```sh
cd backend
go vet ./... && go test ./...
go test -race ./internal/live ./internal/auth   # needs cgo (gcc) on Windows
go test -run TestQuizTimerSubmits -v ./internal/live

cd frontend
npm run check && npm test
```

CI (`.github/workflows/ci.yml`) runs gofmt, vet, `go test -race` against Postgres 16, the build, `redocly lint`, svelte-check, Vitest and the frontend build on every push to `main`, `test`, `dev` and `feature/**`.

## What a new feature needs

- Pure logic: table-driven unit tests, including the BA acceptance criterion it implements (name the AC in the test).
- Routes: an `_api_test.go` covering success, validation (422), authorisation (another teacher gets 404, the wrong role gets 403) and state conflicts.
- Spec: an `openapi.yaml` entry (the contract test enforces it).
- UI logic in `lib/`: a Vitest test; components that place or move DOM (like blank inputs or charts): a component test.
- New colours: add them to `app.css` themes so `theme.test.ts` checks their contrast; chart palettes are validated for colour-blind separation (see [Live polls](polls.md)).
