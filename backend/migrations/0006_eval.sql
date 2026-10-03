CREATE SCHEMA eval;

-- One mark per answered question of a finished attempt.
CREATE TABLE eval.marks (
    attempt_id    uuid          NOT NULL REFERENCES live.attempts (id) ON DELETE CASCADE,
    question_id   uuid          NOT NULL,
    method        text          NOT NULL CHECK (method IN ('key', 'llm', 'manual')),
    status        text          NOT NULL CHECK (status IN ('marked', 'pending', 'needs_manual', 'overridden')),
    score         numeric(8, 2),
    max_score     numeric(8, 2) NOT NULL,
    fraction      numeric(6, 4),
    correct       boolean,
    feedback      text          NOT NULL DEFAULT '', -- predefined (FR-EV-04)
    ai_feedback   text          NOT NULL DEFAULT '', -- AI-generated (FR-EV-05)
    ai_rationale  text          NOT NULL DEFAULT '',
    ai_marked     boolean       NOT NULL DEFAULT false,
    flagged       boolean       NOT NULL DEFAULT false,
    flag_reason   text          NOT NULL DEFAULT '',
    overridden_by uuid,
    overridden_at timestamptz,
    updated_at    timestamptz   NOT NULL DEFAULT now(),
    PRIMARY KEY (attempt_id, question_id)
);
CREATE INDEX marks_status_idx ON eval.marks (status) WHERE status IN ('pending', 'needs_manual');

-- Attempt totals, recomputed whenever a mark changes.
CREATE TABLE eval.results (
    attempt_id    uuid PRIMARY KEY REFERENCES live.attempts (id) ON DELETE CASCADE,
    session_id    uuid          NOT NULL,
    user_id       uuid          NOT NULL,
    score         numeric(10, 2) NOT NULL,
    max_score     numeric(10, 2) NOT NULL,
    pct           numeric(6, 2) NOT NULL,
    pass_mark_pct int           NOT NULL,
    passed        boolean       NOT NULL,
    complete      boolean       NOT NULL, -- false while LLM or manual marks are outstanding
    invalidated   boolean       NOT NULL,
    updated_at    timestamptz   NOT NULL DEFAULT now()
);
CREATE INDEX results_session_idx ON eval.results (session_id);
