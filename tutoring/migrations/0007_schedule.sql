-- Scheduling (D-84, TS-FR-85 to TS-FR-89). A repeating schedule is a series;
-- each occurrence is an ordinary session with its own time, so one can be
-- moved or cancelled alone. A scheduled session opens for joining a while
-- before it starts, and the teacher still starts it.

CREATE TABLE tutoring.series (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    teacher_id   uuid        NOT NULL,
    classroom_id uuid        NOT NULL,
    title        text        NOT NULL,
    time_zone    text        NOT NULL,
    rule         jsonb       NOT NULL,  -- how it repeats, as entered
    created_at   timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE tutoring.sessions
    ADD COLUMN series_id      uuid REFERENCES tutoring.series (id) ON DELETE SET NULL,
    ADD COLUMN duration_min   int  NOT NULL DEFAULT 60 CHECK (duration_min BETWEEN 5 AND 600),
    ADD COLUMN time_zone      text NOT NULL DEFAULT 'UTC',
    ADD COLUMN open_early_min int  NOT NULL DEFAULT 10 CHECK (open_early_min BETWEEN 0 AND 120),
    -- Minutes before the start; empty: no reminders.
    ADD COLUMN reminders      int[] NOT NULL DEFAULT '{1440,15}',
    ADD COLUMN cancelled_at   timestamptz;
ALTER TABLE tutoring.sessions DROP CONSTRAINT sessions_status_check;
ALTER TABLE tutoring.sessions ADD CONSTRAINT sessions_status_check CHECK (status IN ('open', 'live', 'ended', 'cancelled'));
CREATE INDEX sessions_scheduled_idx ON tutoring.sessions (scheduled_at) WHERE scheduled_at IS NOT NULL;
CREATE INDEX sessions_series_idx ON tutoring.sessions (series_id) WHERE series_id IS NOT NULL;

-- Reminders sent, for the time they were about: moving a session sends them again.
CREATE TABLE tutoring.reminders_sent (
    session_id uuid        NOT NULL REFERENCES tutoring.sessions (id) ON DELETE CASCADE,
    minutes    int         NOT NULL,
    for_at     timestamptz NOT NULL,
    sent_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, minutes, for_at)
);

-- Each person's private calendar feed (TS-FR-88): only a hash of its token.
CREATE TABLE tutoring.feeds (
    user_id    uuid PRIMARY KEY,
    token_hash bytea       NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);
