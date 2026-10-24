-- Interactive tutorials (D-74, TU-FR): lessons in pages of content and
-- questions, which students work through at their own pace.

-- A tutorial's questions live in a quiz of kind 'tutorial' under the same
-- topic, so the question editor, CSV import and resources work as they do
-- for quizzes. Such a quiz is never listed, run or deleted on its own.
ALTER TABLE quiz.quizzes DROP CONSTRAINT quizzes_kind_check;
ALTER TABLE quiz.quizzes ADD CONSTRAINT quizzes_kind_check CHECK (kind IN ('quiz', 'bank', 'tutorial'));

CREATE SCHEMA tutorial;

CREATE TABLE tutorial.tutorials (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    topic_id     uuid NOT NULL REFERENCES content.topics(id) ON DELETE RESTRICT,
    teacher_id   uuid NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    quiz_id      uuid NOT NULL UNIQUE REFERENCES quiz.quizzes(id) ON DELETE RESTRICT,
    title        text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    description  text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    allow_back   boolean NOT NULL DEFAULT true,   -- students may go back to earlier pages
    lock_answers boolean NOT NULL DEFAULT false,  -- answers can't be changed once checked
    -- When students see marks and feedback: after each page, at the end, or when released.
    feedback     text NOT NULL DEFAULT 'page' CHECK (feedback IN ('page', 'end', 'release')),
    released_at  timestamptz,
    opens_at     timestamptz,
    due_at       timestamptz,
    published_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tutorials_topic_idx ON tutorial.tutorials (topic_id);

-- A page: content blocks, questions, or both. Questions are ids in the
-- tutorial's quiz, in the order shown.
CREATE TABLE tutorial.pages (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tutorial_id  uuid NOT NULL REFERENCES tutorial.tutorials(id) ON DELETE CASCADE,
    position     integer NOT NULL,
    title        text NOT NULL DEFAULT '',
    blocks       jsonb NOT NULL DEFAULT '[]',
    question_ids uuid[] NOT NULL DEFAULT '{}',
    -- Before going on: nothing, answering every question, or answering them all correctly.
    checkpoint   text NOT NULL DEFAULT 'none' CHECK (checkpoint IN ('none', 'answered', 'correct')),
    max_tries    integer NOT NULL DEFAULT 0 CHECK (max_tries BETWEEN 0 AND 20),  -- 0 = unlimited
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pages_tutorial_idx ON tutorial.pages (tutorial_id, position);

-- One student's way through a tutorial.
CREATE TABLE tutorial.runs (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tutorial_id  uuid NOT NULL REFERENCES tutorial.tutorials(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    current      integer NOT NULL DEFAULT 0,   -- the page index they're on
    reached      integer NOT NULL DEFAULT 0,   -- the furthest page index they've opened
    finished_at  timestamptz,
    started_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tutorial_id, user_id)
);

-- Their answers, marked when checked. Questions marked by the teacher wait as 'pending'.
CREATE TABLE tutorial.answers (
    run_id      uuid NOT NULL REFERENCES tutorial.runs(id) ON DELETE CASCADE,
    question_id uuid NOT NULL,
    response    jsonb NOT NULL,
    score       double precision NOT NULL DEFAULT 0,
    max_score   double precision NOT NULL DEFAULT 0,
    correct     boolean NOT NULL DEFAULT false,
    status      text NOT NULL DEFAULT 'marked' CHECK (status IN ('marked', 'pending')),
    tries       integer NOT NULL DEFAULT 1,
    comment     text NOT NULL DEFAULT '',      -- the teacher's, when they mark it
    answered_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, question_id)
);
