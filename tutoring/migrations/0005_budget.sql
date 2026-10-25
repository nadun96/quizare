-- Tutoring phase 4 (D-81): the bandwidth budget. Each session has a plan
-- (TS-FR-08) that is checked against the server's budget before it starts,
-- and whatever the service switched off to stay within the budget
-- (TS-FR-38), so it isn't forgotten across a restart.

ALTER TABLE tutoring.sessions
    ADD COLUMN plan     jsonb  NOT NULL DEFAULT '{}',
    ADD COLUMN auto_off text[] NOT NULL DEFAULT '{}';

-- The service itself switches things off: its log rows have no actor.
ALTER TABLE tutoring.log ALTER COLUMN actor_id DROP NOT NULL;

-- A teacher's saved plans ("Lecture: 1 screen, chat to teacher").
CREATE TABLE tutoring.plan_presets (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    teacher_id uuid        NOT NULL,
    name       text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    plan       jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (teacher_id, name)
);
