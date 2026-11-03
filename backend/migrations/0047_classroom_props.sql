-- A classroom's optional details (D-128): department, course code, academic
-- year, semester, its term's dates and free tags, for finding classrooms
-- on the teacher's list.
ALTER TABLE content.classrooms
    ADD COLUMN department    text    NOT NULL DEFAULT '',
    ADD COLUMN course_code   text    NOT NULL DEFAULT '',
    ADD COLUMN academic_year integer CHECK (academic_year BETWEEN 1990 AND 2100),
    ADD COLUMN semester      text    NOT NULL DEFAULT '',
    ADD COLUMN starts_on     date,
    ADD COLUMN ends_on       date,
    ADD COLUMN tags          text[]  NOT NULL DEFAULT '{}',
    ADD CONSTRAINT classrooms_term_check CHECK (ends_on IS NULL OR starts_on IS NULL OR ends_on >= starts_on);
CREATE INDEX classrooms_tags_idx ON content.classrooms USING gin (tags);
