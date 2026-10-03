CREATE SCHEMA live;

CREATE TABLE live.sessions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    quiz_id       uuid        NOT NULL REFERENCES quiz.quizzes (id),
    teacher_id    uuid        NOT NULL REFERENCES auth.users (id),
    classroom_id  uuid        NOT NULL REFERENCES content.classrooms (id),
    title         text        NOT NULL,
    join_code     text        NOT NULL UNIQUE,
    status        text        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'live', 'ended')),
    settings      jsonb       NOT NULL DEFAULT '{}'::jsonb, -- session-level overrides
    extension_sec int         NOT NULL DEFAULT 0,           -- session-wide extension
    snapshot      jsonb       NOT NULL,                     -- frozen quiz (ADR-14)
    created_at    timestamptz NOT NULL DEFAULT now(),
    ended_at      timestamptz,
    released_at   timestamptz
);
CREATE INDEX sessions_quiz_idx ON live.sessions (quiz_id);
CREATE INDEX sessions_active_idx ON live.sessions (status) WHERE status <> 'ended';

CREATE TABLE live.attempts (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id            uuid        NOT NULL REFERENCES live.sessions (id) ON DELETE CASCADE,
    user_id               uuid        NOT NULL REFERENCES auth.users (id),
    student_number        text,
    state                 text        NOT NULL CHECK (state IN ('waiting', 'admitted', 'in_progress', 'paused', 'submitted', 'invalidated', 'not_started')),
    overrides             jsonb       NOT NULL DEFAULT '{}'::jsonb,
    extension_sec         int         NOT NULL DEFAULT 0,
    countdown_deadline    timestamptz,
    started_at            timestamptz,
    quiz_deadline         timestamptz,
    quiz_remaining_ms     bigint,
    current_index         int         NOT NULL DEFAULT 0,
    question_deadline     timestamptz,
    question_remaining_ms bigint,
    paused_at             timestamptz,
    question_order        int[]       NOT NULL DEFAULT '{}',
    option_orders         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    warnings              int         NOT NULL DEFAULT 0,
    violations            int         NOT NULL DEFAULT 0,
    disconnected_at       timestamptz,
    submitted_at          timestamptz,
    invalidated_at        timestamptz,
    invalid_reason        text        NOT NULL DEFAULT '',
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (session_id, user_id) -- BR-01: one attempt per student per session
);
-- The 1 Hz ticker scans only attempts that have a deadline to enforce.
CREATE INDEX attempts_due_idx ON live.attempts (state) WHERE state IN ('admitted', 'in_progress');

-- Answers are saved on every change (NFR-11). question_id refers to the
-- session snapshot, not quiz.questions, so quiz edits never orphan answers.
CREATE TABLE live.answers (
    attempt_id     uuid        NOT NULL REFERENCES live.attempts (id) ON DELETE CASCADE,
    question_id    uuid        NOT NULL,
    student_number text,                       -- BR-03: attached to every answer
    response       jsonb       NOT NULL,
    client_seq     bigint      NOT NULL DEFAULT 0,
    saved_at       timestamptz NOT NULL,
    PRIMARY KEY (attempt_id, question_id)
);

-- FR-PR-04: every violation with time, type and device info.
CREATE TABLE live.violations (
    id         bigserial PRIMARY KEY,
    attempt_id uuid        NOT NULL REFERENCES live.attempts (id) ON DELETE CASCADE,
    kind       text        NOT NULL,
    action     text        NOT NULL CHECK (action IN ('invalidated', 'warned', 'logged')),
    client_ts  timestamptz,
    server_ts  timestamptz NOT NULL,
    device     text        NOT NULL DEFAULT '',
    details    jsonb       NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX violations_attempt_idx ON live.violations (attempt_id);

-- Integrity log timeline (BA §11): joins, admissions, pauses, extensions,
-- violations, reinstatements.
CREATE TABLE live.events (
    id         bigserial PRIMARY KEY,
    session_id uuid        NOT NULL REFERENCES live.sessions (id) ON DELETE CASCADE,
    attempt_id uuid,
    actor_id   uuid,
    kind       text        NOT NULL,
    details    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL
);
CREATE INDEX events_session_idx ON live.events (session_id, id);
