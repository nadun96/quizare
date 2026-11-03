-- Reviewing a pool's questions (D-132): a teacher marks each question as
-- checked after importing. It's optional: unreviewed questions can still be
-- asked. Changing a question clears the mark.
ALTER TABLE quiz.questions
    ADD COLUMN reviewed_at timestamptz,
    ADD COLUMN reviewed_by uuid REFERENCES auth.users (id) ON DELETE SET NULL;
CREATE INDEX questions_reviewed_by_idx ON quiz.questions (reviewed_by);
