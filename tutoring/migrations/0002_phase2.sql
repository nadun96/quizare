-- Tutoring phase 2 (D-60): co-teachers (TS-FR-70 to TS-FR-73), handing the
-- broadcast to someone else (TS-FR-17) and the teacher's layout (TS-FR-13).
ALTER TABLE tutoring.participants DROP CONSTRAINT participants_role_check;
ALTER TABLE tutoring.participants ADD CONSTRAINT participants_role_check CHECK (role IN ('teacher', 'coteacher', 'student'));

-- Co-teachers the lead teacher added; they join without the classroom check.
CREATE TABLE tutoring.coteachers (
    session_id uuid        NOT NULL REFERENCES tutoring.sessions (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL,
    name       text        NOT NULL,
    added_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, user_id)
);

-- The broadcaster: NULL is the lead teacher. broadcast_offer is someone
-- asked to broadcast who hasn't accepted yet.
ALTER TABLE tutoring.sessions
    ADD COLUMN broadcaster_id  uuid,
    ADD COLUMN broadcast_offer uuid,
    ADD COLUMN layout          text NOT NULL DEFAULT 'spotlight' CHECK (layout IN ('spotlight', 'side', 'grid'));
