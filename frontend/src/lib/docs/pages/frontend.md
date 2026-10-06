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
| `/t/polls`, `/t/polls/[id]`, `/t/polls/[id]/present` | teacher | Polls: questions, settings, live results, share; full-screen presenter |
| `/p/[code]` | anyone | Taking part in a poll (login only if the poll asks) |
| `/t/settings` | teacher | Teacher defaults, LLM keys |
| `/admin` | admin | Users, usage, platform settings, audit |
| `/r/[token]` | public | Shared results |
| `/live/[token]` | public | Live leaderboards (polls and sessions), full screen (D-45) |
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
| `QuestionEditor.svelte` | Authoring form for all seven types. Keyed by question so switching questions starts a fresh form. Question text and feedback use the rich text editor. |
| `richtext/` | Rich text: `RichTextEditor.svelte` (Tiptap, loaded on first use), `RichText.svelte` (display, with blank inputs placed into the text), `syntax.ts` (the shared Markdown dialect), `render.ts` (Markdown → sanitised HTML) and `plain.ts` (plain text, KaTeX loader). See [Rich text](#rich-text). |
| `LiveLinks.svelte` | Create, copy (with QR), turn off and renew public live leaderboard links (D-45). |
| `SessionTeams.svelte` | Teams on the live dashboard: standings, random or category teams, moving students, captains (D-44). |
| `StudentsPanel.svelte` | A classroom's students with categories: filter chips, search, multi-select, add to or remove from a category (D-41). |
| `SettingsEditor.svelte` + `settingsMeta.ts` | Generic overrides editor showing inherited values. |
| `AnalyticsView.svelte` | Class, question and student analytics tables and the score histogram. |
| `QrCode.svelte` | QR rendered in the browser with `qrcode` (no server CPU). |
| `answerText.ts` | Human-readable responses and keys. |
| `types.ts` | TypeScript shapes of the API JSON. |
| `docs/` | This documentation: page registry, Markdown renderer, lazy Mermaid. |
| `poll/` | Polls: `PollInput` (all 20 inputs; `inputs/` holds rating, slider, Likert, matrix, word cloud, code, file and recorder), `PollResults` (charts and tables), `WordCloud` + `wordcloud.ts` (layout), `PollQuestionEditor`, `PollSettingsForm`, `client.ts` (participant API with the anonymous token); scored polls: `KeyEditor` (correct answer by type), `Leaderboard` (animated ranking, own row pinned; also draws groups), `GroupsPanel` (teacher: form groups, move people, captains) and `scoring.ts` (key clean-up, countdown, answer in words). See [Live polls](polls.md). |
| `ui/` | Shared UI: `Icon` (inline SVG), `Toaster` + `toast.svelte.ts`, `DialogHost` + `confirmDialog()`, `DisplayMenu` + `prefs.svelte.ts` (theme, text size, motion, quiz timer), `StatCounter`, `Skeleton`, `EmptyState`, and `motion.ts` (transitions that switch off for reduced motion). See [Design system](#design-system). |

## Design system

The interface follows the [UX research and design rules](ux-research.md) (D-39).

- **UI kit:** [daisyUI 5](https://daisyui.com) on Tailwind CSS 4. It is CSS only, so it adds no JavaScript to the student page. Tailwind scans only `.svelte`, `.ts` and `.html` files (not the docs Markdown), so only the classes in use are shipped.
- **Themes:** `quiz` (light) and `quiz-dark` are defined in `app.css` with `@plugin 'daisyui/theme'`. Dark follows the OS unless the user picks one under *Display settings*. `theme.test.ts` checks every text and colour pair for WCAG AA contrast, and form-field borders for 3:1.
- **Components:** buttons `btn`, fields `input`/`select`/`textarea`, `card card-border`, `alert alert-soft`, `badge badge-soft`, `tabs tabs-border` with `tab`, `table`, `progress`, `modal`, `toast`, `loading`, `skeleton`. State modifiers set from expressions (`ok`, `warn`, `danger` on a badge or alert) map onto daisyUI's colour variables in `app.css`.
- **Overrides:** daisyUI's own rules sit in nested cascade layers, so the app's overrides (touch heights, field border colour) live in `@layer utilities`. Don't redefine daisyUI's variables (`--border`, `--radius-*`, `--size-*`) for other purposes: `--border` is its border *width*.
- **Layout helpers:** `page-container`, `narrow`, `vstack`, `row`, `spacer`, `auto-grid`, `readable`, `muted`, `small`, `tabular`. These names avoid daisyUI's `stack`, `hero` and similar components and Tailwind's `container`.
- **Status:** state is shown with an icon, text and colour together (`STATE_ICON` in `types.ts`).
- **Motion:** use `flyIn`, `fadeIn`, `scaleIn` and `slideIn` from `lib/ui/motion.ts` instead of `svelte/transition` directly, so the OS setting and the in-app *Reduce motion* switch both apply. `app.css` also stops CSS animations in both cases.
- **Feedback:** `toast()` confirms actions, `await confirmDialog({...})` replaces `window.confirm` (destructive dialogs focus *Cancel* first), and `Skeleton` and `EmptyState` replace "Loading…" and blank lists.

## Rich text

Teachers format question text and predefined feedback in a WYSIWYG editor: headings, bold, italic, underline, strikethrough, inline code, links, bulleted and numbered lists, quotes, code blocks, dividers, tables, inline and display math (KaTeX), blanks for fill-in-the-blank questions, undo and redo. An **MD** button shows the Markdown source. Pictures stay question resources (BR-15), so there is no inline image button.

The text is stored as Markdown in the existing `text` field, with `body.format = "markdown"` (D-38). Plain text (CSV import, older questions) has no format and renders exactly as before. Opening such a question in the editor converts it with every character kept literal, so `5*3*2` stays `5*3*2`.

The dialect (`richtext/syntax.ts`) is GFM plus:

| Syntax | Meaning |
|--------|---------|
| `[[1]]` | A blank, as in the CSV format. In the editor it is a single chip (**+ Blank**, or type `[[1]]`). |
| `++text++` | Underline |
| `$x^2$` | Inline math. Pandoc rule: no space just inside the dollars and no digit after the closing one, so `$5 or $10` stays text. |
| `$$ … $$` | Display math |

Safety: teacher text is shown to students and on public result pages, so `render.ts` escapes raw HTML, drops images, allows only `http(s):` and `mailto:` links (opened with `rel="noopener noreferrer nofollow"`), and passes the result through DOMPurify with a short allowlist. The CSP still forbids inline script on top of that. Tests in `richtext/*.test.ts` cover XSS payloads, round trips through the editor, and blank inputs placed inside formatted text.

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
- The student quiz page loads about 62 KB of gzipped JS and 23 KB of gzipped CSS. Mermaid loads only on `/docs`. The Markdown renderer (marked and DOMPurify, about 20 KB) loads only when a question uses formatting, and KaTeX only when it has math. Tiptap loads only in the editor.
- `svelte.config.js` enables Kit's **hash-based CSP**: the one inline boot script gets a `sha256-…` in a `<meta>` CSP, so no other inline script can run even though Caddy's header allows `'unsafe-inline'` (both policies must pass).

## Development

`npm run dev` serves on `:5173` and proxies `/api`, `/ws` and `/beacon` to `QP_BACKEND` (default `http://127.0.0.1:8080`). Keep the browser origin same-site so the `__Host-` cookie and Origin checks work, and set the backend's `QP_BASE_URL` to `http://localhost:5173`.

Checks: `npm run check` (svelte-check, strict TypeScript) and `npm test` (Vitest with jsdom: `src/**/*.test.ts`).
