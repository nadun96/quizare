-- The session's whiteboard (D-85, TS-FR-66, TS-FR-67): one board, the
-- teachers', shown to everyone in the main room while it's open. Teachers
-- draw; students only watch, unless the lead teacher lets everyone, or
-- chosen students, draw. Strokes are kept with the session, so a restart
-- of the service loses nothing (TS-NFR-11).

ALTER TABLE tutoring.sessions
    ADD COLUMN board_open    boolean NOT NULL DEFAULT false,
    ADD COLUMN board_mode    text    NOT NULL DEFAULT 'teacher' CHECK (board_mode IN ('teacher', 'everyone', 'selected')),
    ADD COLUMN board_users   uuid[]  NOT NULL DEFAULT '{}',
    ADD COLUMN board_cleared bigint  NOT NULL DEFAULT 0; -- strokes up to this id were cleared

CREATE TABLE tutoring.board_strokes (
    id         bigserial PRIMARY KEY,
    session_id uuid        NOT NULL REFERENCES tutoring.sessions (id) ON DELETE CASCADE,
    by_user    uuid        NOT NULL,
    gesture    text        NOT NULL,
    tool       text        NOT NULL,
    color      text        NOT NULL,
    size       real        NOT NULL,
    points     jsonb       NOT NULL,
    text       text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX board_strokes_session_idx ON tutoring.board_strokes (session_id, id);
