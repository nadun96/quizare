CREATE SCHEMA settings;

-- Platform defaults set by the admin (BA §7 level 9).
CREATE TABLE settings.platform (
    id        boolean PRIMARY KEY DEFAULT true CHECK (id),
    overrides jsonb NOT NULL DEFAULT '{}'::jsonb
);
INSERT INTO settings.platform DEFAULT VALUES;

-- Teacher defaults (BA §7 level 8).
CREATE TABLE settings.teacher (
    teacher_id uuid PRIMARY KEY REFERENCES auth.users (id) ON DELETE CASCADE,
    overrides  jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE SCHEMA content;

CREATE TABLE content.classrooms (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    teacher_id  uuid        NOT NULL REFERENCES auth.users (id),
    name        text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    join_code   text        NOT NULL UNIQUE,
    settings    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    archived_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX classrooms_teacher_idx ON content.classrooms (teacher_id);

CREATE TABLE content.modules (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    classroom_id uuid        NOT NULL REFERENCES content.classrooms (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    position     int         NOT NULL DEFAULT 0,
    settings     jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX modules_classroom_idx ON content.modules (classroom_id, position);

CREATE TABLE content.topics (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id  uuid        NOT NULL REFERENCES content.modules (id) ON DELETE CASCADE,
    name       text        NOT NULL,
    position   int         NOT NULL DEFAULT 0,
    settings   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX topics_module_idx ON content.topics (module_id, position);

-- A student's membership of a classroom, with the classroom-specific
-- student ID (FR-CLS-05, BR-03).
CREATE TABLE content.enrolments (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    classroom_id   uuid        NOT NULL REFERENCES content.classrooms (id) ON DELETE CASCADE,
    user_id        uuid        NOT NULL REFERENCES auth.users (id),
    student_number text,
    status         text        NOT NULL DEFAULT 'active' CHECK (status IN ('pending', 'active', 'rejected', 'removed')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (classroom_id, user_id)
);
-- FR-CLS-06: unique within the classroom among current members.
CREATE UNIQUE INDEX enrolments_student_number_key ON content.enrolments (classroom_id, lower(student_number))
    WHERE student_number IS NOT NULL AND status IN ('pending', 'active');
CREATE INDEX enrolments_user_idx ON content.enrolments (user_id);
