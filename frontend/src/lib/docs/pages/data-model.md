# Data model

Migrations live in `backend/migrations/NNNN_*.sql` and are embedded into the binary; `db.Migrate` runs pending ones on startup. **Never edit a migration that has shipped.** Add a new numbered file instead.

| Migration | Creates |
|-----------|---------|
| `0001_init.sql` | `audit` schema, `pgcrypto` |
| `0002_auth.sql` | `auth.users`, `auth.sessions`, `auth.tokens`, `auth.policy` |
| `0003_settings_content.sql` | `settings.platform`, `settings.teacher`, `content.*` |
| `0004_quiz.sql` | `quiz.quizzes`, `quiz.questions`, `quiz.resources` |
| `0005_live.sql` | `live.sessions`, `live.attempts`, `live.answers`, `live.violations`, `live.events` |
| `0006_eval.sql` | `eval.marks`, `eval.results` |
| `0007_llm.sql` | `llm.keys` |
| `0008_analytics.sql` | `analytics.session_stats`, `analytics.share_links` |
| `0009_poll.sql` | `poll.polls`, `poll.questions`, `poll.participants`, `poll.responses`, `poll.hidden_words`, `poll.files` |

River's own tables (`river_job`, ...) are created by `jobs.Migrate`.

## Content hierarchy

```mermaid
erDiagram
  USERS ||--o{ CLASSROOMS : "teaches"
  CLASSROOMS ||--o{ MODULES : contains
  MODULES ||--o{ TOPICS : contains
  TOPICS ||--o{ QUIZZES : "contains (RESTRICT)"
  QUIZZES ||--o{ QUESTIONS : contains
  QUESTIONS ||--o{ RESOURCES : "images by URL"
  CLASSROOMS ||--o{ ENROLMENTS : has
  USERS ||--o{ ENROLMENTS : "enrols"
```

- Classrooms, modules, topics and questions carry a `settings jsonb` column with sparse overrides; quizzes do too.
- `quiz.questions` stores `body`, `answer_key` and `feedback` as JSONB, plus `marks`, `negative_marks`, `partial_credit` and `settings`. `UNIQUE (quiz_id, code)`. `body.format` is `markdown` for text written in the rich text editor and absent for plain text (CSV import, older questions); it applies to the question text and the predefined feedback.
- `quiz.resources` is `UNIQUE (question_id, role, n)`, with `status` one of `unchecked`, `ok`, `broken`.
- `content.enrolments` is `UNIQUE (classroom_id, user_id)`, with a partial unique index on `(classroom_id, lower(student_number))` for current members (FR-CLS-06).
- `quiz.quizzes.topic_id` is `ON DELETE RESTRICT`: a topic with quizzes cannot be deleted (the API returns 409).

## Live runs

```mermaid
erDiagram
  QUIZZES ||--o{ SESSIONS : "runs as"
  SESSIONS ||--o{ ATTEMPTS : "one per student (BR-01)"
  ATTEMPTS ||--o{ ANSWERS : "per question"
  ATTEMPTS ||--o{ VIOLATIONS : logs
  SESSIONS ||--o{ EVENTS : "integrity log"
  ATTEMPTS ||--o{ MARKS : evaluated
  ATTEMPTS ||--|| RESULTS : totals
  SESSIONS ||--o| SESSION_STATS : aggregates
```

### `live.sessions`

`snapshot jsonb` freezes the quiz at creation: its title, the base effective settings (defaults → platform → teacher), the classroom/module/topic/quiz override layers, and every question including keys. `settings` holds session-level overrides (editable while live). `extension_sec` is the session-wide extension. `released_at` is set by a manual release.

### `live.attempts`

One row per `(session_id, user_id)`.

| Column(s) | Meaning |
|-----------|---------|
| `state` | `waiting`, `admitted`, `in_progress`, `paused`, `submitted`, `invalidated`, `not_started` |
| `countdown_deadline`, `quiz_deadline`, `question_deadline` | Server timestamps |
| `quiz_remaining_ms`, `question_remaining_ms` | Set instead of deadlines while paused |
| `current_index`, `question_order int[]`, `option_orders jsonb` | Position and this student's shuffles |
| `overrides`, `extension_sec` | Student-level settings and extra time |
| `warnings`, `violations`, `disconnected_at`, `invalid_reason` | Proctoring state |

The partial index `attempts_due_idx` covers `admitted` and `in_progress` rows for the ticker.

### `live.answers`

Primary key `(attempt_id, question_id)`. `question_id` refers to the **snapshot**, not `quiz.questions`, so editing or deleting a question never orphans answers. `client_seq` makes saves idempotent: an older sequence never overwrites a newer one. `student_number` is copied onto every answer (BR-03).

## Evaluation and LLM

- **`eval.marks`**, primary key `(attempt_id, question_id)`:
  - `method`: `key`, `llm` or `manual`.
  - `status`: `marked`, `pending`, `needs_manual` or `overridden`.
  - Scores: `score`, `max_score`, `fraction`, `correct`.
  - Feedback: `feedback` (predefined), `ai_feedback`, `ai_rationale`, `ai_marked`.
  - Review: `flagged`, `flag_reason`, `overridden_by`.
- **`eval.results`**: attempt totals: `score`, `max_score`, `pct`, `pass_mark_pct`, `passed`, `complete` (no pending or manual marks left) and `invalidated`.
- **`llm.keys`**: `ciphertext`, `nonce`, `wrapped_dek`, `dek_nonce`, `kek_id`, `last4`, `key_version`, `is_default` (at most one per teacher), and the last test result. **There is no plaintext column.**

## Analytics and audit

- `analytics.session_stats`: one JSON document per session (`SessionStats`) and `computed_at`.
- `analytics.share_links`: `token_hash` (SHA-256 of a 128-bit token), `scope` (`session` or `quiz`), `views text[]`, `identify`, `show_answers`, `expires_at`, `revoked_at`.
- `audit.events`: actor, action, target and details JSON. Written inside the transaction of the action (mark overrides, reinstatements, status changes, key changes, releases, share links).
- `live.events`: the per-session integrity timeline shown to teachers (`joined`, `admitted`, `started`, `paused`, `resumed`, `extended`, `violation`, `reinstated`, `question_timed_out`, `submitted`, `session_ended`, `results_released`, ...).

## Polls

`poll.polls` holds the settings (`identity`, `audience`, `pacing`, `show_results`, `allow_edit`), the status and, for presenter pacing, `current_index` and `revealed`. A check constraint makes classroom-only polls identified. `poll.participants` has **either** a `user_id` (identified) **or** a `token_hash` (anonymous), never both, unique per poll. `poll.responses` has one JSON value per participant and question, with `hidden` for moderation; `poll.hidden_words` hides words from word clouds. `poll.files` stores uploads as `bytea` (12 MB cap per row). Everything cascades from the poll. See [Live polls](polls.md).

## Deletion and anonymisation

- **User delete** (admin or self) rewrites the user's email to `deleted+<id>@invalid`, name to "Deleted user" and password to `!`, and removes their sessions and tokens. Attempts, answers and results stay for the teacher (D-04).
- **Quiz delete** archives the quiz instead if any session ran it (BR-16).
- **Classroom delete** is refused if any session ran in it (D-11).
