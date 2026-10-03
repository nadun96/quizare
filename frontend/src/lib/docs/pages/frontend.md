# Frontend app

A SvelteKit 2 / Svelte 5 single-page app built with `adapter-static` (ADR-11): no server rendering, no Node process in production. Caddy serves `frontend/build`, with `index.html` as the fallback for client routes.

## Routes

| Route | Who | What |
|-------|-----|------|
| `/`, `/login`, `/register`, `/forgot-password`, `/reset-password`, `/verify-email`, `/account` | everyone | Auth and account (own-data download, self-delete) |
| `/join`, `/j/[code]`, `/c/[code]` | student | Enter a code; QR landing for sessions; classroom enrolment |
| `/attempt/[id]` | student | The live quiz |
| `/my`, `/my/results/[id]` | student | History and released results |
| `/t`, `/t/classrooms/[id]`, `/t/topics/[id]`, `/t/quizzes/[id]` | teacher | Content, CSV import, question editor, preview, settings |
| `/t/sessions/[id]`, `/t/sessions/[id]/projector` | teacher | Live dashboard; full-screen QR |
| `/t/sessions/[id]/results`, `/t/quizzes/[id]/analytics` | teacher | Marking review, analytics, public links, integrity log |
| `/t/settings` | teacher | Teacher defaults, LLM keys |
| `/admin` | admin | Users, usage, platform settings, audit |
| `/r/[token]` | public | Shared results |
| `/docs/[slug]` | developers | This documentation |

`src/routes/+layout.ts` sets `ssr = false`. Pages guard themselves with `requireRole('teacher')` (`lib/guard.svelte.ts`), which redirects to `/login?next=…`; the server enforces roles regardless. After login or registration, `safeNext` returns the student to the QR page (FR-ACC-06) and only accepts same-site relative paths.

## Libraries (`src/lib`)

| File | Purpose |
|------|---------|
| `api.ts` | `fetch` wrapper: adds `X-Requested-With`, sends cookies `same-origin`, turns errors into `ApiError{status, code, fields}`. `withBusyRetry` retries 503s during login bursts. |
| `session.svelte.ts` | `auth` store (`$state` user, `load`, `logout`, `home`). |
| `socket.ts` | `LiveSocket`: reconnects with exponential backoff (0.5 s, doubling, capped at 10 s, with jitter); pings every 10 s and feeds pongs to the clock. |
| `clock.ts` | `ServerClock`: offset from the fastest round trip; `remaining(deadline)`; `formatDuration`. |
| `answerQueue.ts` | Offline answer queue (UC-03 8a): latest save per question, increasing `seq`, persisted in `localStorage`, flushed on reconnect and every 3 s. 5xx and network errors retry; 4xx drops the save. |
| `proctor.ts` | Browser integrity signals and copy/paste blocking (see [Proctoring](proctoring.md)). |
| `QuestionView.svelte` | Renders all seven types and emits a `Response` on every change. Drag and drop uses SortableJS; essays are debounced. |
| `QuestionEditor.svelte` | Authoring form for all seven types. Keyed by question so switching questions starts a fresh form. |
| `SettingsEditor.svelte` + `settingsMeta.ts` | Generic overrides editor showing inherited values. |
| `AnalyticsView.svelte` | Class, question and student analytics tables and the score histogram. |
| `QrCode.svelte` | QR rendered in the browser with `qrcode` (no server CPU). |
| `answerText.ts` | Human-readable responses and keys. |
| `types.ts` | TypeScript shapes of the API JSON. |
| `docs/` | This documentation: page registry, Markdown renderer, lazy Mermaid. |

## The attempt page

`routes/attempt/[id]/+page.svelte` is the critical path. It:

1. opens `LiveSocket('/ws/attempts/{id}')` and applies every `state` message (falling back to `GET /api/attempts/{id}`);
2. renders by `state`: waiting room, countdown with **Start now** (this tap also requests fullscreen), question, paused, submitted, invalidated;
3. renders timers every 250 ms from server deadlines and the clock offset;
4. saves every change through `AnswerQueue`. **Next** flushes the queue and then calls `advance` with the current question id, which is idempotent;
5. runs `Proctor` while `in_progress`, reporting over the socket or the beacon;
6. warns on `beforeunload` while running.

## Build, size and CSP

- `npm run build` runs `vite build`, then `scripts/precompress.mjs` writes `.br` and `.gz` siblings for Caddy's `precompressed br gzip`.
- Hashed assets in `_app/immutable/` are cached for a year; `index.html` is `no-cache`.
- The student quiz page loads about 54 KB of gzipped JS. Mermaid and Marked are loaded only by `/docs`.
- `svelte.config.js` enables Kit's **hash-based CSP**: the one inline boot script gets a `sha256-…` in a `<meta>` CSP, so no other inline script can run even though Caddy's header allows `'unsafe-inline'` (both policies must pass).

## Development

`npm run dev` serves on `:5173` and proxies `/api`, `/ws` and `/beacon` to `QP_BACKEND` (default `http://127.0.0.1:8080`). Keep the browser origin same-site so the `__Host-` cookie and Origin checks work, and set the backend's `QP_BASE_URL` to `http://localhost:5173`.

Checks: `npm run check` (svelte-check, strict TypeScript) and `npm test` (Vitest with jsdom: `src/**/*.test.ts`).
