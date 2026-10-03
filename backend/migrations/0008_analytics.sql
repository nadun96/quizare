CREATE SCHEMA analytics;

-- Precomputed per-session aggregates; results pages are single-row reads (ADR-15).
CREATE TABLE analytics.session_stats (
    session_id  uuid PRIMARY KEY REFERENCES live.sessions (id) ON DELETE CASCADE,
    quiz_id     uuid        NOT NULL,
    teacher_id  uuid        NOT NULL,
    data        jsonb       NOT NULL,
    computed_at timestamptz NOT NULL
);
CREATE INDEX session_stats_quiz_idx ON analytics.session_stats (quiz_id);

-- Revocable public links to a results or analytics view (FR-RS-04, ADR-16).
CREATE TABLE analytics.share_links (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash   bytea       NOT NULL UNIQUE, -- 128-bit random token, stored hashed
    teacher_id   uuid        NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    scope        text        NOT NULL CHECK (scope IN ('session', 'quiz')),
    target_id    uuid        NOT NULL,
    views        text[]      NOT NULL,       -- individual, question_pct, pass_rate
    identify     text        NOT NULL DEFAULT 'anonymous' CHECK (identify IN ('anonymous', 'student_id')),
    show_answers boolean     NOT NULL DEFAULT false,
    label        text        NOT NULL DEFAULT '',
    expires_at   timestamptz,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX share_links_target_idx ON analytics.share_links (target_id);
