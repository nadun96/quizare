-- Live polls (D-40): anonymous or identified participation, every question
-- input type, results aggregated live. Owned by the poll module.
CREATE SCHEMA poll;

CREATE TABLE poll.polls (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    teacher_id     uuid        NOT NULL,
    classroom_id   uuid,
    title          text        NOT NULL,
    join_code      text        NOT NULL UNIQUE,
    -- anonymous: nobody is identified; identified: login required;
    -- optional: each participant chooses.
    identity       text        NOT NULL DEFAULT 'anonymous' CHECK (identity IN ('anonymous', 'identified', 'optional')),
    audience       text        NOT NULL DEFAULT 'anyone' CHECK (audience IN ('anyone', 'classroom')),
    pacing         text        NOT NULL DEFAULT 'self' CHECK (pacing IN ('self', 'presenter')),
    -- What participants see: live, after they answer, when the presenter reveals, or never.
    show_results   text        NOT NULL DEFAULT 'after_answer' CHECK (show_results IN ('live', 'after_answer', 'presenter', 'never')),
    allow_edit     boolean     NOT NULL DEFAULT true,
    status         text        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'open', 'closed')),
    current_index  int         NOT NULL DEFAULT 0, -- presenter pacing: the question everyone sees
    revealed       boolean     NOT NULL DEFAULT false, -- presenter pacing: results of the current question shown
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    opened_at      timestamptz,
    closed_at      timestamptz,
    CHECK (audience = 'anyone' OR identity = 'identified')
);
CREATE INDEX polls_teacher_idx ON poll.polls (teacher_id, created_at DESC);

CREATE TABLE poll.questions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    poll_id    uuid        NOT NULL REFERENCES poll.polls (id) ON DELETE CASCADE,
    position   int         NOT NULL,
    type       text        NOT NULL,
    text       text        NOT NULL,
    body       jsonb       NOT NULL DEFAULT '{}'::jsonb, -- options and limits per type
    required   boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX poll_questions_idx ON poll.questions (poll_id, position);

-- One row per person (identified) or per device token (anonymous). An
-- anonymous participant has no user id, ever, even when logged in.
CREATE TABLE poll.participants (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    poll_id    uuid        NOT NULL REFERENCES poll.polls (id) ON DELETE CASCADE,
    user_id    uuid,
    token_hash bytea,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((user_id IS NULL) <> (token_hash IS NULL)),
    UNIQUE (poll_id, user_id),
    UNIQUE (poll_id, token_hash)
);

CREATE TABLE poll.responses (
    participant_id uuid        NOT NULL REFERENCES poll.participants (id) ON DELETE CASCADE,
    question_id    uuid        NOT NULL REFERENCES poll.questions (id) ON DELETE CASCADE,
    poll_id        uuid        NOT NULL REFERENCES poll.polls (id) ON DELETE CASCADE,
    value          jsonb       NOT NULL,
    hidden         boolean     NOT NULL DEFAULT false, -- moderated out of shared results
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (participant_id, question_id)
);
CREATE INDEX poll_responses_question_idx ON poll.responses (question_id);
CREATE INDEX poll_responses_poll_idx ON poll.responses (poll_id);

-- Words the presenter removed from a word cloud or text summary.
CREATE TABLE poll.hidden_words (
    question_id uuid NOT NULL REFERENCES poll.questions (id) ON DELETE CASCADE,
    word        text NOT NULL,
    PRIMARY KEY (question_id, word)
);

-- File, audio and video answers, kept small (D-40). Only the poll owner can
-- download them; they are deleted with the poll, the question or the answer.
CREATE TABLE poll.files (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    poll_id        uuid        NOT NULL REFERENCES poll.polls (id) ON DELETE CASCADE,
    question_id    uuid        NOT NULL REFERENCES poll.questions (id) ON DELETE CASCADE,
    participant_id uuid        NOT NULL REFERENCES poll.participants (id) ON DELETE CASCADE,
    name           text        NOT NULL,
    content_type   text        NOT NULL,
    size           int         NOT NULL CHECK (size > 0 AND size <= 12582912),
    data           bytea       NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (question_id, participant_id)
);
CREATE INDEX poll_files_poll_idx ON poll.files (poll_id);
