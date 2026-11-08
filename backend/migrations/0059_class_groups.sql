-- A classroom's groups (D-158). Categories (D-41) become groups: a group can
-- have a leader and assistant leaders, a public name its leader chooses, and
-- students may choose their own while the teacher allows it.
ALTER TABLE content.categories
    ADD COLUMN public_name text CHECK (public_name IS NULL OR length(public_name) BETWEEN 1 AND 40);
CREATE UNIQUE INDEX categories_public_name_key ON content.categories (classroom_id, lower(public_name)) WHERE public_name IS NOT NULL;

ALTER TABLE content.enrolment_categories
    ADD COLUMN role        text        NOT NULL DEFAULT 'member' CHECK (role IN ('member', 'leader', 'assistant')),
    ADD COLUMN self_joined boolean     NOT NULL DEFAULT false, -- the student chose it
    ADD COLUMN joined_at   timestamptz NOT NULL DEFAULT now();
-- One leader a group.
CREATE UNIQUE INDEX enrolment_categories_leader_key ON content.enrolment_categories (category_id) WHERE role = 'leader';

-- Whether, when and how students choose their own groups.
CREATE TABLE content.group_policy (
    classroom_id    uuid PRIMARY KEY REFERENCES content.classrooms (id) ON DELETE CASCADE,
    self_join       boolean     NOT NULL DEFAULT false,
    opens_at        timestamptz,          -- empty: from now
    closes_at       timestamptz,          -- empty: until turned off
    max_per_student int         NOT NULL DEFAULT 1 CHECK (max_per_student BETWEEN 1 AND 50),
    max_size        int         CHECK (max_size IS NULL OR max_size BETWEEN 1 AND 1000),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- The class report's own class groups (D-141) become the classroom's groups,
-- merged into one of the same name if there is one.
INSERT INTO content.categories (classroom_id, name, color, position)
SELECT g.classroom_id, g.name, (row_number() OVER (PARTITION BY g.classroom_id ORDER BY g.position) - 1) % 8 + 1,
       1000 + g.position
FROM grading.groups g
WHERE g.kind = 'class'
ON CONFLICT DO NOTHING;

INSERT INTO content.enrolment_categories (enrolment_id, category_id)
SELECT e.id, c.id
FROM grading.groups g
JOIN grading.group_members m ON m.group_id = g.id
JOIN content.categories c ON c.classroom_id = g.classroom_id AND lower(c.name) = lower(g.name)
JOIN content.enrolments e ON e.classroom_id = g.classroom_id AND e.user_id = m.user_id
WHERE g.kind = 'class'
ON CONFLICT DO NOTHING;

DELETE FROM grading.groups WHERE kind = 'class';
