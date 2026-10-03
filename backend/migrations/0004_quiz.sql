CREATE SCHEMA quiz;

CREATE TABLE quiz.quizzes (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- RESTRICT: a topic with quizzes cannot be deleted out from under them.
    topic_id          uuid        NOT NULL REFERENCES content.topics (id) ON DELETE RESTRICT,
    teacher_id        uuid        NOT NULL REFERENCES auth.users (id),
    title             text        NOT NULL,
    description       text        NOT NULL DEFAULT '',
    status            text        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'ready', 'archived')),
    warnings_accepted boolean     NOT NULL DEFAULT false, -- UC-01 6a
    settings          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX quizzes_topic_idx ON quiz.quizzes (topic_id);
CREATE INDEX quizzes_teacher_idx ON quiz.quizzes (teacher_id);

CREATE TABLE quiz.questions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    quiz_id        uuid          NOT NULL REFERENCES quiz.quizzes (id) ON DELETE CASCADE,
    code           text          NOT NULL,
    position       int           NOT NULL,
    type           text          NOT NULL CHECK (type IN ('SINGLE', 'MULTI', 'MATCH', 'BLANK_OPT', 'BLANK_TEXT', 'DRAG', 'ESSAY')),
    text           text          NOT NULL,
    body           jsonb         NOT NULL DEFAULT '{}'::jsonb,
    answer_key     jsonb         NOT NULL DEFAULT '{}'::jsonb,
    feedback       jsonb         NOT NULL DEFAULT '{}'::jsonb,
    marks          numeric(8, 2) NOT NULL DEFAULT 1,
    negative_marks numeric(8, 2) NOT NULL DEFAULT 0,
    partial_credit boolean       NOT NULL DEFAULT false,
    settings       jsonb         NOT NULL DEFAULT '{}'::jsonb,
    created_at     timestamptz   NOT NULL DEFAULT now(),
    updated_at     timestamptz   NOT NULL DEFAULT now(),
    UNIQUE (quiz_id, code)
);
CREATE INDEX questions_quiz_idx ON quiz.questions (quiz_id, position);

-- Resources are URLs only (BR-15).
CREATE TABLE quiz.resources (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    question_id uuid        NOT NULL REFERENCES quiz.questions (id) ON DELETE CASCADE,
    role        text        NOT NULL CHECK (role IN ('Q', 'O', 'F')),
    n           int         NOT NULL CHECK (n BETWEEN 1 AND 50),
    source_url  text        NOT NULL,
    url         text        NOT NULL,
    alt_text    text        NOT NULL DEFAULT '',
    status      text        NOT NULL DEFAULT 'unchecked' CHECK (status IN ('unchecked', 'ok', 'broken')),
    message     text        NOT NULL DEFAULT '',
    checked_at  timestamptz,
    UNIQUE (question_id, role, n)
);
