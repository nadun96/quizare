-- A live poll inside the session (D-86, TS-FR-68): the teacher runs one of
-- their polls (the platform's, D-40) without leaving; students answer it
-- in a panel beside the broadcast. The session only says which poll is on.

ALTER TABLE tutoring.sessions
    ADD COLUMN poll_id    uuid,
    ADD COLUMN poll_code  text,
    ADD COLUMN poll_title text;
