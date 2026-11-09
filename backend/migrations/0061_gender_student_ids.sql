-- Gender, for balancing groups (D-162): set by the student, corrected by
-- their teachers; empty is "prefer not to say".
ALTER TABLE auth.users ADD COLUMN gender text CHECK (gender IN ('female', 'male'));

-- A classroom's student ID pattern (D-162): IDs must match it in full.
ALTER TABLE content.classrooms
    ADD COLUMN student_id_pattern text CHECK (student_id_pattern IS NULL OR length(student_id_pattern) BETWEEN 1 AND 200),
    ADD COLUMN student_id_hint    text CHECK (student_id_hint IS NULL OR length(student_id_hint) <= 100);
