-- Polls in the class report, groups of students for mark sheets, and a
-- classroom's report header and footer (D-141).
ALTER TABLE grading.report_items DROP CONSTRAINT report_items_kind_check;
ALTER TABLE grading.report_items ADD CONSTRAINT report_items_kind_check CHECK (kind IN ('quiz', 'tutorial', 'poll'));
-- How a group's mark comes from its members' marks.
ALTER TABLE grading.report_items
    ADD COLUMN group_rule text NOT NULL DEFAULT 'average' CHECK (group_rule IN ('average', 'total', 'highest', 'lowest'));

-- Groups for the whole class (kind 'class'), or for one quiz, tutorial or poll.
-- These are for mark sheets: live quiz and poll teams are separate.
CREATE TABLE grading.groups (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    classroom_id uuid        NOT NULL REFERENCES content.classrooms (id) ON DELETE CASCADE,
    kind         text        NOT NULL CHECK (kind IN ('class', 'quiz', 'tutorial', 'poll')),
    item_id      uuid,
    name         text        NOT NULL,
    position     integer     NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CHECK ((kind = 'class') = (item_id IS NULL))
);
CREATE INDEX groups_classroom_idx ON grading.groups (classroom_id, kind, item_id, position);

CREATE TABLE grading.group_members (
    group_id uuid NOT NULL REFERENCES grading.groups (id) ON DELETE CASCADE,
    user_id  uuid NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX group_members_user_idx ON grading.group_members (user_id);

-- What every exported report of the classroom carries at its top and foot.
CREATE TABLE grading.report_headers (
    classroom_id uuid PRIMARY KEY REFERENCES content.classrooms (id) ON DELETE CASCADE,
    title        text        NOT NULL DEFAULT '', -- blank: the report's own name
    subtitle     text        NOT NULL DEFAULT '',
    institution  text        NOT NULL DEFAULT '',
    date_mode    text        NOT NULL DEFAULT 'today' CHECK (date_mode IN ('today', 'custom', 'none')),
    date_value   date,
    show_teacher boolean     NOT NULL DEFAULT true,
    footer       text        NOT NULL DEFAULT '', -- a copyright line, for instance
    page_numbers boolean     NOT NULL DEFAULT true,
    updated_by   uuid        REFERENCES auth.users (id) ON DELETE SET NULL,
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX report_headers_updated_by_idx ON grading.report_headers (updated_by);
