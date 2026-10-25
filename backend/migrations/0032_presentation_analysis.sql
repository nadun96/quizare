-- Presentations against marks (D-78, PR-FR-32, PR-FR-33): the quizzes and
-- tutorials a presentation goes with, whose scores its viewing is set beside;
-- and its records saved to Google Drive like session and poll results.

CREATE TABLE presentation.links (
    presentation_id uuid NOT NULL REFERENCES presentation.presentations(id) ON DELETE CASCADE,
    kind            text NOT NULL CHECK (kind IN ('quiz', 'tutorial')),
    target_id       uuid NOT NULL,   -- a quiz or tutorial of the same topic; checked when linked
    PRIMARY KEY (presentation_id, kind, target_id)
);

ALTER TABLE drive.exports DROP CONSTRAINT exports_kind_check;
ALTER TABLE drive.exports ADD CONSTRAINT exports_kind_check CHECK (kind IN ('session', 'poll', 'presentation'));
