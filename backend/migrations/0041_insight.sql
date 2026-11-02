-- Multi-dimensional analysis (D-120): a small warehouse inside the database,
-- owned by the insight module. A loader copies what changed from the other
-- modules every 5 minutes; reports read only these tables. No foreign keys
-- to other schemas: the warehouse is kept in step by the loader, not by
-- cascades, and could move to another database as it is.
CREATE SCHEMA olap;

-- Dimensions: names and the tree a fact sits in, refreshed each run.
CREATE TABLE olap.classrooms (
    id           uuid PRIMARY KEY,
    name         text    NOT NULL,
    teacher_id   uuid    NOT NULL,
    teacher_name text    NOT NULL,
    archived     boolean NOT NULL
);
CREATE INDEX olap_classrooms_teacher_idx ON olap.classrooms (teacher_id);

CREATE TABLE olap.topics (
    id           uuid PRIMARY KEY,
    name         text NOT NULL,
    module_id    uuid NOT NULL,
    module_name  text NOT NULL,
    classroom_id uuid NOT NULL
);

-- Quizzes and tutorials: the activity a fact belongs to.
CREATE TABLE olap.items (
    id       uuid PRIMARY KEY,
    kind     text NOT NULL CHECK (kind IN ('quiz', 'tutorial')),
    title    text NOT NULL,
    topic_id uuid
);

CREATE TABLE olap.people (
    id   uuid PRIMARY KEY,
    name text NOT NULL
);

-- Facts: one row per quiz result (complete, not invalidated) and per
-- finished tutorial, at the moment it was handed in or finished.
CREATE TABLE olap.activity (
    kind         text        NOT NULL CHECK (kind IN ('quiz', 'tutorial')),
    source_id    uuid        NOT NULL,   -- the attempt or the tutorial run
    at           timestamptz NOT NULL,
    classroom_id uuid        NOT NULL,
    topic_id     uuid,
    item_id      uuid        NOT NULL,
    session_id   uuid,
    user_id      uuid        NOT NULL,
    pct          float8,                 -- quizzes: the result's percentage
    passed       boolean,
    minutes      float8,                 -- time taken: a quiz from start to hand-in, a tutorial on its pages
    PRIMARY KEY (kind, source_id)
);
CREATE INDEX activity_classroom_at_idx ON olap.activity (classroom_id, at);
CREATE INDEX activity_user_at_idx      ON olap.activity (user_id, at);
CREATE INDEX activity_at_idx           ON olap.activity (at);

-- One row per marked question of those quiz results.
CREATE TABLE olap.answers (
    attempt_id   uuid        NOT NULL,
    question_id  uuid        NOT NULL,
    at           timestamptz NOT NULL,
    classroom_id uuid        NOT NULL,
    topic_id     uuid,
    item_id      uuid        NOT NULL,
    user_id      uuid        NOT NULL,
    qtype        text        NOT NULL,
    fraction     float8      NOT NULL,   -- 0 to 1 of the question's marks
    correct      boolean     NOT NULL,
    PRIMARY KEY (attempt_id, question_id)
);
CREATE INDEX answers_classroom_at_idx ON olap.answers (classroom_id, at);
CREATE INDEX answers_at_idx           ON olap.answers (at);

-- Where the loader got to, and when it last checked for deleted sources.
CREATE TABLE olap.load_state (
    id          boolean     PRIMARY KEY DEFAULT true CHECK (id),
    last_at     timestamptz NOT NULL,
    reconciled  timestamptz NOT NULL
);
