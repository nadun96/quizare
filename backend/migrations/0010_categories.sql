-- Student categories (V2-03, D-41): teacher-defined labels per classroom,
-- such as "Year 10 A" or "needs support". A student can be in several.
CREATE TABLE content.categories (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    classroom_id uuid        NOT NULL REFERENCES content.classrooms (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    color        int         NOT NULL DEFAULT 1 CHECK (color BETWEEN 1 AND 8), -- categorical palette slot
    position     int         NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX categories_name_key ON content.categories (classroom_id, lower(name));

CREATE TABLE content.enrolment_categories (
    enrolment_id uuid NOT NULL REFERENCES content.enrolments (id) ON DELETE CASCADE,
    category_id  uuid NOT NULL REFERENCES content.categories (id) ON DELETE CASCADE,
    PRIMARY KEY (enrolment_id, category_id)
);
CREATE INDEX enrolment_categories_category_idx ON content.enrolment_categories (category_id);
