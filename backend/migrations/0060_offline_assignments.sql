-- Offline assignments (D-159): work done outside the platform - a project, a
-- practical, a paper test - kept in a topic. The teacher enters the marks, or
-- downloads the class list, fills it in and uploads it. A group assignment's
-- mark is the group's, and each member gets it unless they have their own.
CREATE TABLE grading.offline_assignments (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    classroom_id uuid NOT NULL REFERENCES content.classrooms (id) ON DELETE CASCADE,
    topic_id     uuid NOT NULL REFERENCES content.topics (id) ON DELETE CASCADE,
    title        text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    instructions text NOT NULL DEFAULT '' CHECK (length(instructions) <= 5000),
    max_marks    numeric(9, 2) NOT NULL CHECK (max_marks > 0 AND max_marks <= 10000),
    mode         text NOT NULL DEFAULT 'individual' CHECK (mode IN ('individual', 'group')),
    due_at       timestamptz,
    released_at  timestamptz, -- students see their marks from then
    created_by   uuid REFERENCES auth.users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX offline_assignments_classroom_idx ON grading.offline_assignments (classroom_id);
CREATE INDEX offline_assignments_topic_idx ON grading.offline_assignments (topic_id);
CREATE INDEX offline_assignments_created_by_idx ON grading.offline_assignments (created_by);

-- A student's own mark: in a group assignment, it replaces the group's.
CREATE TABLE grading.offline_marks (
    assignment_id uuid NOT NULL REFERENCES grading.offline_assignments (id) ON DELETE CASCADE,
    user_id       uuid NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    score         numeric(9, 2) NOT NULL CHECK (score >= 0),
    comment       text NOT NULL DEFAULT '' CHECK (length(comment) <= 1000),
    updated_by    uuid REFERENCES auth.users (id) ON DELETE SET NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (assignment_id, user_id)
);
CREATE INDEX offline_marks_user_idx ON grading.offline_marks (user_id);
CREATE INDEX offline_marks_updated_by_idx ON grading.offline_marks (updated_by);

-- A classroom group's mark (D-158 groups).
CREATE TABLE grading.offline_group_marks (
    assignment_id uuid NOT NULL REFERENCES grading.offline_assignments (id) ON DELETE CASCADE,
    category_id   uuid NOT NULL REFERENCES content.categories (id) ON DELETE CASCADE,
    score         numeric(9, 2) NOT NULL CHECK (score >= 0),
    comment       text NOT NULL DEFAULT '' CHECK (length(comment) <= 1000),
    updated_by    uuid REFERENCES auth.users (id) ON DELETE SET NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (assignment_id, category_id)
);
CREATE INDEX offline_group_marks_category_idx ON grading.offline_group_marks (category_id);
CREATE INDEX offline_group_marks_updated_by_idx ON grading.offline_group_marks (updated_by);

-- They count in the gradebook and the class report, with their own groups.
ALTER TABLE grading.weights DROP CONSTRAINT weights_kind_check;
ALTER TABLE grading.weights ADD CONSTRAINT weights_kind_check CHECK (kind IN ('quiz', 'tutorial', 'offline'));
ALTER TABLE grading.report_items DROP CONSTRAINT report_items_kind_check;
ALTER TABLE grading.report_items ADD CONSTRAINT report_items_kind_check CHECK (kind IN ('quiz', 'tutorial', 'poll', 'offline'));
ALTER TABLE grading.groups DROP CONSTRAINT groups_kind_check;
ALTER TABLE grading.groups ADD CONSTRAINT groups_kind_check CHECK (kind IN ('class', 'quiz', 'tutorial', 'poll', 'offline'));
