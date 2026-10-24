-- Meeting limits an admin sets (D-72). 0 means unlimited.

-- A meeting's own size: 0 (no limit of its own) or at least 2.
ALTER TABLE meet.meetings DROP CONSTRAINT meetings_max_people_check;
ALTER TABLE meet.meetings ADD CONSTRAINT meetings_max_people_check CHECK (max_people = 0 OR max_people >= 2);

-- One row. The defaults keep what D-69 allowed: up to 8 people, any number of meetings.
CREATE TABLE meet.limits (
    id          boolean PRIMARY KEY DEFAULT true CHECK (id),
    max_people  integer NOT NULL DEFAULT 8 CHECK (max_people = 0 OR max_people >= 2),  -- per meeting
    max_open    integer NOT NULL DEFAULT 0 CHECK (max_open >= 0),                      -- open meetings per teacher
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid
);
INSERT INTO meet.limits DEFAULT VALUES;
