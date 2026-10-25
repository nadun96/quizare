-- Tutoring phase 3, breakout groups (D-79, TS-FR-46, TS-FR-50 to TS-FR-54).
-- Each group meets in its own media room, where its members see and hear
-- each other; everyone stays in the main broadcast too, so the teacher can
-- speak to all groups at once.

CREATE TABLE tutoring.groups (
    id         bigserial PRIMARY KEY,
    session_id uuid        NOT NULL REFERENCES tutoring.sessions (id) ON DELETE CASCADE,
    name       text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    position   int         NOT NULL,
    help_at    timestamptz,             -- "Ask for help" (TS-FR-54), until a teacher visits or dismisses it
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX groups_session_idx ON tutoring.groups (session_id, position);

-- One group per student. Absent: not in a group (yet).
CREATE TABLE tutoring.group_members (
    session_id uuid   NOT NULL REFERENCES tutoring.sessions (id) ON DELETE CASCADE,
    user_id    uuid   NOT NULL,
    group_id   bigint NOT NULL REFERENCES tutoring.groups (id) ON DELETE CASCADE,
    PRIMARY KEY (session_id, user_id)
);

ALTER TABLE tutoring.sessions
    ADD COLUMN groups_open     boolean     NOT NULL DEFAULT false,
    ADD COLUMN groups_close_at timestamptz,            -- the countdown before groups close
    ADD COLUMN groups_choose   boolean     NOT NULL DEFAULT false, -- students pick their group
    ADD COLUMN group_chat      boolean     NOT NULL DEFAULT true,  -- PO-14: on by default
    -- What every student may do in groups beyond their own permissions (TS-FR-53).
    ADD COLUMN group_mic       boolean     NOT NULL DEFAULT true,
    ADD COLUMN group_camera    boolean     NOT NULL DEFAULT false,
    ADD COLUMN group_screen    boolean     NOT NULL DEFAULT false;

-- Teachers visiting a group are seen and heard there (TS-FR-52).
ALTER TABLE tutoring.participants ADD COLUMN visiting bigint REFERENCES tutoring.groups (id) ON DELETE SET NULL;

-- Group chat: a message to one group, seen by its members and the teachers.
ALTER TABLE tutoring.messages ADD COLUMN group_id bigint REFERENCES tutoring.groups (id) ON DELETE CASCADE;
ALTER TABLE tutoring.messages DROP CONSTRAINT messages_audience_check;
ALTER TABLE tutoring.messages ADD CONSTRAINT messages_audience_check CHECK (audience IN ('everyone', 'teachers', 'one', 'group'));
ALTER TABLE tutoring.messages ADD CONSTRAINT messages_group_check CHECK ((audience = 'group') = (group_id IS NOT NULL));
