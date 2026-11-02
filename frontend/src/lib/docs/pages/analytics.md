# Results & analytics

Code: `backend/internal/analytics`. Requirements: FR-RS-01…05, BA §11, BR-13, AC-12, ADR-15.

## Precomputation

Results pages are single-row reads (ADR-15). Every time `eval` recomputes an attempt total, its results hook inserts an `analytics_recompute` job in the same transaction; the session-end hook does the same.

```mermaid
flowchart LR
  M[mark change / session end] -->|INSERT in tx| J[analytics_recompute job]
  J --> W{stats computed after<br/>this job was created?}
  W -- yes --> Skip[skip]
  W -- no --> C[compute → analytics.session_stats]
  C --> R[GET .../analytics<br/>public links<br/>CSV export]
```

The worker **debounces**: a burst of 100 marks queues 100 jobs, but every job created before the last computation is skipped after one cheap read. If no stats exist yet, reads compute them on demand.

## Metrics

**Class** (`ClassStats`), over finished attempts that are not invalidated:

| Metric | Definition |
|--------|------------|
| `joined` | all attempts in any state |
| `finished` | submitted plus invalidated |
| `marked` | finished and not invalidated (the base for the statistics below) |
| `mean_pct`, `median_pct` | of attempt percentages |
| `pass_rate` | share with `pct ≥ pass_mark_pct` |
| `completion_rate` | submitted ÷ joined |
| `invalidation_rate` | invalidated ÷ joined |
| `distribution` | 10 buckets of 10 percentage points |
| `pending` | attempts still waiting for LLM or manual marks |

**Question** (`QuestionRow`):

| Metric | Definition |
|--------|------------|
| `answered` | attempts that answered it |
| `pct_correct`, `avg_score` | over answered attempts |
| `option_counts` | SINGLE/MULTI picks per option |
| `common_wrong` | top 5 wrong answers (option text or normalised blank text) |
| `discrimination` | correct rate of the top 27% of students minus the bottom 27%; needs at least 4 marked attempts. Low or negative values mark questions that don't separate strong from weak students |

**Student** (`StudentRow`): score, percentage, passed, complete, time taken (start to submit), violations, extra time, pauses, and per-question answers.

**Quiz level** (`QuizStats`) rolls stored session stats up across sessions: students concatenated, question metrics weighted by answers, class figures recomputed, plus a per-session `comparison`.

Average time per *question* isn't reported because the timing model doesn't record per-question view times (D-32).

## Public links

`POST /api/teacher/share-links {scope, target_id, views, identify, show_answers, expires_at}`:

- The token is 128 random bits, base64url-encoded, **stored as SHA-256**, and returned only once (on create and regenerate).
- `views` is any of `individual`, `question_pct` and `pass_rate`. For a session it defaults to the session's `results_view` setting.
- In the individual view students appear as **"Student N"**, or as their classroom student number if `identify = student_id`. Names and emails never appear (BR-13). Individual answers appear only with `show_answers`. Invalidated attempts are left out.
- Links can be revoked (410 afterwards), regenerated (the old token gives 404), or expire. `GET /api/public/results/{token}` sends `X-Robots-Tag: noindex` and `Cache-Control: no-store`.
- The public page is the SPA route `/r/{token}`.

## Live leaderboard links

A share link with scope `live_session` or `live_poll` opens `/live/{token}`: the individual and team (or group) leaderboards, updating without login (V2-08, D-45). Views are `leaderboard` and `teams` (both by default).

- **Who is shown.** Sessions: classroom student IDs, or "Student N" in join order. Polls: nicknames (the default), or "Participant N". Real names and emails never appear, even in polls whose own leaderboard shows real names.
- **What is shown.** Sessions rank marked, valid attempts by percentage, with each student's team; teams come from the team standings (D-44). Polls rank by points, with groups when the poll has them. Polls that aren't scored show no board.
- **Updating.** `GET /api/public/live/{token}` is polled by the page every `refresh_ms` (3 s while running, 30 s once finished, 15 s while the tab is hidden). The server caches each link's view for 2 s, so a room full of viewers costs one computation per link. Responses carry `X-Robots-Tag: noindex` and `Cache-Control: no-store`.
- **Ending a link.** Turning a link off, an expiry, or deleting the poll or session makes it return `410`; a new link replaces the token. A live token opens only the live page, and a results token only `/r/`.

Teachers create live links on the poll's **Share** tab and on the session dashboard.

## CSV export

`GET /api/teacher/sessions/{id}/export.csv` writes one row per student with a column per question score. Cells starting with `=`, `+`, `-`, `@`, tab or CR are prefixed with `'` to prevent formula injection when opened in a spreadsheet (ADR-16).

## Long-term trends (D-119)

Charts over months and years read small roll-up tables instead of every result ever recorded, so they stay quick however much history there is.

```mermaid
flowchart LR
  R[results, finished tutorials] -->|changed since last run| J[analytics_rollup<br/>every 15 min]
  E[enrolments] -->|current month| J
  J --> SM[analytics.student_months]
  J --> CM[analytics.classroom_months]
  SM --> T[classroom trends<br/>student trends<br/>my trends]
  CM --> T
```

The job finds the classroom-months touched since its last run (by `eval.results.updated_at` and `tutorial.runs.updated_at`, with a minute of overlap), adds every open classroom's current month, and recomputes just those in a few set-based statements. Its first run computes all history. One run at a time: the state row is locked.

| Figure | Definition |
|--------|------------|
| Results | complete, valid quiz results, in the month of the attempt's submission (UTC) |
| Average, pass rate | over those results |
| Tutorials | tutorials finished in the classroom |
| Members | active members while the month runs, frozen once it's over (never fewer than took part) |
| Taking part | members with a result or a finished tutorial ÷ members |
| Student's active days | days with a result or a finished tutorial |
| Recent / earlier average | a student's average over the last three months / the three before, for the trend arrow |

Teachers see it on a classroom's **Trends** tab (12, 24 or 60 months) and per student; students see their own on **My results**.

## Staying fast as it grows (D-119)

- Every foreign key has an index starting with it; `TestForeignKeysAreIndexed` fails when a new one doesn't, so deleting a user or a classroom, or listing by owner, never reads a whole table.
- Results pages read one precomputed row per session (above), and long-term charts read roll-ups.
- Lists are paged on the server everywhere (TS-FR-94).

## Explore: multi-dimensional analysis (D-120)

Code: `backend/internal/insight`. A warehouse inside the database (schema `olap`) and a query engine over it; the **Explore** page asks it.

```mermaid
flowchart LR
  S[results, marks, tutorial runs,<br/>classrooms, topics, people] -->|loader every 5 min,<br/>only what changed| F[(olap facts:<br/>activity, answers)]
  S -->|names and trees| D[(olap dimensions)]
  F --> Q[query engine]
  D --> Q
  Q -->|scope: teacher's classrooms,<br/>or staff without students| E[Explore page]
```

A query is measures × up to three dimensions × filters × a date range:

| Measures (activity) | Measures (questions) | Dimensions |
|---|---|---|
| quiz results, average score, pass rate, tutorials finished, students, minutes per quiz, minutes per tutorial | answers marked, marks gained, fully right, students | date (day, week, month, quarter, year), classroom, teacher (admins), module, topic, quiz or tutorial, activity, question type (questions), student (teachers) |

Every name in a request is looked up in the catalog; the SQL comes from the catalog, never from the request. Question measures come from another fact table, so they can't be mixed with activity measures in one query. With totals, the first dimension gets subtotals and there is a grand total (`GROUPING SETS`). At most 5,000 rows come back. Admins see every classroom but never a student, and a group of fewer than 5 students is shown without its values. The warehouse lags the live data by up to 5 minutes; a deleted source leaves it at the next daily reconcile. Reports never read the live tables, so analysis can't slow a quiz down, and the package could later read from a replica or another store without the page changing.
