-- The class report (D-140): for each quiz and tutorial, whether it is in the
-- report, its weight (0: shown but not in the total) and which of a student's
-- submissions it uses, and what students see of their results and for how long.
-- An item with no row is in the report, at its gradebook weight, using the
-- highest mark, and shows what its own settings say, with no time limit.
CREATE TABLE grading.report_items (
    classroom_id   uuid          NOT NULL REFERENCES content.classrooms (id) ON DELETE CASCADE,
    kind           text          NOT NULL CHECK (kind IN ('quiz', 'tutorial')),
    item_id        uuid          NOT NULL,
    included       boolean       NOT NULL DEFAULT true,
    weight         numeric(7, 2) NOT NULL DEFAULT 0 CHECK (weight >= 0 AND weight <= 1000),
    pick           text          NOT NULL DEFAULT 'high' CHECK (pick IN ('high', 'low')),
    detail         text          NOT NULL DEFAULT '' CHECK (detail IN ('', 'marks', 'correctness', 'answers')),
    review_minutes integer       NOT NULL DEFAULT 0 CHECK (review_minutes >= 0), -- 0: no limit
    updated_by     uuid          REFERENCES auth.users (id) ON DELETE SET NULL,
    updated_at     timestamptz   NOT NULL DEFAULT now(),
    PRIMARY KEY (classroom_id, kind, item_id)
);
CREATE INDEX report_items_item_idx ON grading.report_items (kind, item_id);
CREATE INDEX report_items_updated_by_idx ON grading.report_items (updated_by);

-- One student's exception to an item's level or review time; '' and NULL keep the item's.
CREATE TABLE grading.result_exceptions (
    classroom_id   uuid    NOT NULL REFERENCES content.classrooms (id) ON DELETE CASCADE,
    kind           text    NOT NULL CHECK (kind IN ('quiz', 'tutorial')),
    item_id        uuid    NOT NULL,
    user_id        uuid    NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    detail         text    NOT NULL DEFAULT '' CHECK (detail IN ('', 'marks', 'correctness', 'answers')),
    review_minutes integer CHECK (review_minutes >= 0),
    PRIMARY KEY (classroom_id, kind, item_id, user_id)
);
CREATE INDEX result_exceptions_item_idx ON grading.result_exceptions (kind, item_id, user_id);
CREATE INDEX result_exceptions_user_idx ON grading.result_exceptions (user_id);
