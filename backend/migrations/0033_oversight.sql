-- Live activity (D-90): the admin pauses new quiz sessions, polls,
-- meetings, live presentations and tutoring sessions, for everyone (the
-- row with no teacher) or for one teacher. What's already live carries on.

CREATE SCHEMA IF NOT EXISTS oversight;

CREATE TABLE oversight.pauses (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    teacher_id uuid REFERENCES auth.users (id) ON DELETE CASCADE, -- NULL: everyone
    reason     text        NOT NULL DEFAULT '',
    paused_by  uuid REFERENCES auth.users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
-- One pause each (two indexes: NULLS NOT DISTINCT needs PostgreSQL 15).
CREATE UNIQUE INDEX pauses_teacher_idx ON oversight.pauses (teacher_id) WHERE teacher_id IS NOT NULL;
CREATE UNIQUE INDEX pauses_everyone_idx ON oversight.pauses ((true)) WHERE teacher_id IS NULL;
