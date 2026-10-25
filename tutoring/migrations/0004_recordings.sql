-- Recording the presenter (D-80, TS-FR-75 to TS-FR-79, TS-NFR-24). The
-- teacher's browser records the broadcaster's screen and voice and uploads
-- it in chunks; the file is kept encrypted on disk, and deleted 2 days after
-- it ends. All recordings together stay under a limit the admin sets.

CREATE TABLE tutoring.recordings (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      uuid        NOT NULL REFERENCES tutoring.sessions (id) ON DELETE CASCADE,
    started_by      uuid        NOT NULL,       -- whose browser records it
    started_by_name text        NOT NULL,
    status          text        NOT NULL DEFAULT 'recording' CHECK (status IN ('recording', 'paused', 'stopped')),
    mime            text        NOT NULL,
    bytes           bigint      NOT NULL DEFAULT 0,
    chunks          int         NOT NULL DEFAULT 0,
    viewers         text        NOT NULL DEFAULT 'teachers' CHECK (viewers IN ('teachers', 'students')),
    started_at      timestamptz NOT NULL DEFAULT now(),
    last_chunk_at   timestamptz NOT NULL DEFAULT now(),
    ended_at        timestamptz,
    expires_at      timestamptz                  -- 2 days after it ends (PO-22)
);
-- One recording at a time per session.
CREATE UNIQUE INDEX recordings_active_idx ON tutoring.recordings (session_id) WHERE status <> 'stopped';
CREATE INDEX recordings_session_idx ON tutoring.recordings (session_id, started_at);
CREATE INDEX recordings_expires_idx ON tutoring.recordings (expires_at);

-- Settings the admin changes: the recording limit (PO-24).
CREATE TABLE tutoring.settings (
    key   text PRIMARY KEY,
    value jsonb NOT NULL
);
