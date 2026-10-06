-- Scored polls and live ranking (V2-01, V2-02, D-42).
ALTER TABLE poll.polls
    ADD COLUMN scoring           boolean     NOT NULL DEFAULT false,
    ADD COLUMN speed_bonus       boolean     NOT NULL DEFAULT false,
    ADD COLUMN leaderboard       text        NOT NULL DEFAULT 'presenter' CHECK (leaderboard IN ('off', 'presenter', 'everyone')),
    ADD COLUMN show_answers      text        NOT NULL DEFAULT 'after_close' CHECK (show_answers IN ('never', 'after_answer', 'presenter', 'after_close')),
    ADD COLUMN names             text        NOT NULL DEFAULT 'nickname' CHECK (names IN ('nickname', 'name')),
    ADD COLUMN answers_revealed  boolean     NOT NULL DEFAULT false, -- presenter pacing: correct answer of the current question shown
    ADD COLUMN question_started_at timestamptz;                       -- presenter pacing: when the current question appeared

ALTER TABLE poll.questions
    ADD COLUMN key            jsonb, -- answer key; never sent to participants before it is revealed
    ADD COLUMN points         int NOT NULL DEFAULT 100 CHECK (points BETWEEN 0 AND 10000),
    ADD COLUMN time_limit_sec int CHECK (time_limit_sec BETWEEN 5 AND 3600);

ALTER TABLE poll.participants
    ADD COLUMN nickname text;

ALTER TABLE poll.responses
    ADD COLUMN score      double precision, -- NULL when the question isn't scored
    ADD COLUMN correct    boolean,
    ADD COLUMN elapsed_ms int;              -- presenter pacing: time from the question appearing, for the speed bonus
