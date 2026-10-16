-- Questions a student flagged to come back to (D-61). Question ids from the
-- session snapshot; only the student sees them.
ALTER TABLE live.attempts ADD COLUMN flagged text[] NOT NULL DEFAULT '{}';
