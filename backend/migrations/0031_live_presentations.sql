-- Presenting live (D-77, PR-FR-10 to PR-FR-16): the teacher moves the
-- slides and the students in it follow. Time on slides while live is kept
-- in presentation.views with mode 'live', apart from self-paced viewing.

CREATE TABLE presentation.lives (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    presentation_id uuid NOT NULL REFERENCES presentation.presentations(id) ON DELETE CASCADE,
    code            text NOT NULL UNIQUE,              -- to join with, or as a QR code
    slide           integer NOT NULL DEFAULT 1,        -- the teacher's slide
    max_shown       integer NOT NULL DEFAULT 1,        -- students may look back up to here, never ahead
    allow_browse    boolean NOT NULL DEFAULT true,     -- students may look back on their own
    started_by      uuid REFERENCES auth.users(id) ON DELETE SET NULL,
    started_at      timestamptz NOT NULL DEFAULT now(),
    ended_at        timestamptz
);
-- One live presentation at a time.
CREATE UNIQUE INDEX lives_open_idx ON presentation.lives (presentation_id) WHERE ended_at IS NULL;
