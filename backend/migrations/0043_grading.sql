-- Weighted final results (D-122): how much each quiz or tutorial counts
-- towards a classroom's final result, as a percentage of it. Weights needn't
-- add up to 100: the rest may come from tests marked outside the platform.
CREATE SCHEMA grading;

CREATE TABLE grading.weights (
    classroom_id uuid         NOT NULL REFERENCES content.classrooms (id) ON DELETE CASCADE,
    kind         text         NOT NULL CHECK (kind IN ('quiz', 'tutorial')),
    item_id      uuid         NOT NULL,
    weight       numeric(6, 2) NOT NULL CHECK (weight > 0 AND weight <= 100),
    updated_by   uuid         REFERENCES auth.users (id) ON DELETE SET NULL,
    updated_at   timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (classroom_id, kind, item_id)
);
CREATE INDEX weights_updated_by_idx ON grading.weights (updated_by);
