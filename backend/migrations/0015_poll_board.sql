-- Real-time whiteboard per poll (V2-09, D-47).
ALTER TABLE poll.polls
    ADD COLUMN board_open         boolean NOT NULL DEFAULT false, -- participants see the board
    ADD COLUMN board_mode         text    NOT NULL DEFAULT 'teacher' CHECK (board_mode IN ('teacher', 'everyone', 'selected')),
    ADD COLUMN board_groups       uuid[]  NOT NULL DEFAULT '{}',  -- board_mode=selected: these groups may draw
    ADD COLUMN board_participants uuid[]  NOT NULL DEFAULT '{}';  -- ... and these participants

CREATE TABLE poll.board_strokes (
    id             bigserial PRIMARY KEY,
    poll_id        uuid        NOT NULL REFERENCES poll.polls (id) ON DELETE CASCADE,
    participant_id uuid        REFERENCES poll.participants (id) ON DELETE CASCADE, -- NULL: the teacher
    gesture        text        NOT NULL, -- client id joining a gesture's pieces, for undo
    tool           text        NOT NULL CHECK (tool IN ('pen', 'highlighter', 'line', 'rect', 'ellipse', 'arrow', 'text')),
    color          text        NOT NULL,
    size           real        NOT NULL,
    points         double precision[] NOT NULL, -- x0,y0,x1,y1,… on a 1600×900 board
    text           text,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX poll_board_strokes_idx ON poll.board_strokes (poll_id, id);
