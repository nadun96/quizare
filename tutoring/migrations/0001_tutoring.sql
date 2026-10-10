-- Tutoring sessions (TS-FR-01 to TS-FR-07, TS-FR-20 to TS-FR-24, TS-FR-40 to
-- TS-FR-45, TS-FR-60, TS-FR-61; D-59). The tutoring service owns this schema;
-- people and classrooms belong to the platform and appear here only by id,
-- with the names the platform vouched for when they joined (TS-NFR-51).
CREATE SCHEMA tutoring;

CREATE TABLE tutoring.sessions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    teacher_id     uuid        NOT NULL,
    teacher_name   text        NOT NULL,
    classroom_id   uuid        NOT NULL,
    classroom_name text        NOT NULL,
    title          text        NOT NULL,
    join_code      text        NOT NULL UNIQUE,
    status         text        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'live', 'ended')),
    admit_mode     text        NOT NULL DEFAULT 'auto' CHECK (admit_mode IN ('auto', 'manual')),
    locked         boolean     NOT NULL DEFAULT false,
    chat_mode      text        NOT NULL DEFAULT 'to_teacher' CHECK (chat_mode IN ('off', 'to_teacher', 'everyone', 'announcements')),
    slow_seconds   int         NOT NULL DEFAULT 0 CHECK (slow_seconds BETWEEN 0 AND 600),
    pinned_id      bigint,
    scheduled_at   timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    started_at     timestamptz,
    ended_at       timestamptz
);
CREATE INDEX sessions_teacher_idx ON tutoring.sessions (teacher_id, created_at DESC);
CREATE INDEX sessions_classroom_idx ON tutoring.sessions (classroom_id, created_at DESC);

-- Everyone who joined, with what they may do. Students may publish nothing
-- until the teacher allows it (TS-FR-20).
CREATE TABLE tutoring.participants (
    session_id   uuid        NOT NULL REFERENCES tutoring.sessions (id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL,
    name         text        NOT NULL,
    role         text        NOT NULL CHECK (role IN ('teacher', 'student')),
    state        text        NOT NULL CHECK (state IN ('waiting', 'admitted', 'refused', 'removed')),
    allow_mic    boolean     NOT NULL DEFAULT false,
    allow_camera boolean     NOT NULL DEFAULT false,
    allow_screen boolean     NOT NULL DEFAULT false,
    chat_muted   boolean     NOT NULL DEFAULT false,
    hand_at      timestamptz,
    last_chat_at timestamptz,
    first_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, user_id)
);

-- Attendance (TS-FR-07): one row per connection, from joining to leaving.
CREATE TABLE tutoring.visits (
    id         bigserial PRIMARY KEY,
    session_id uuid        NOT NULL REFERENCES tutoring.sessions (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL,
    joined_at  timestamptz NOT NULL,
    left_at    timestamptz
);
CREATE INDEX visits_session_idx ON tutoring.visits (session_id, user_id);

-- Chat (TS-FR-40 to TS-FR-45). to_user: a private message to one student
-- from a teacher, or a student's question that only teachers see.
CREATE TABLE tutoring.messages (
    id          bigserial PRIMARY KEY,
    session_id  uuid        NOT NULL REFERENCES tutoring.sessions (id) ON DELETE CASCADE,
    user_id     uuid        NOT NULL,
    name        text        NOT NULL,
    from_teacher boolean    NOT NULL,
    audience    text        NOT NULL CHECK (audience IN ('everyone', 'teachers', 'one')),
    to_user     uuid,
    text        text        NOT NULL CHECK (char_length(text) BETWEEN 1 AND 1000),
    created_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    CHECK ((audience = 'one') = (to_user IS NOT NULL))
);
CREATE INDEX messages_session_idx ON tutoring.messages (session_id, id);

-- What teachers did (TS-NFR-42): permission changes, removals, chat deletions.
CREATE TABLE tutoring.log (
    id         bigserial PRIMARY KEY,
    session_id uuid        NOT NULL REFERENCES tutoring.sessions (id) ON DELETE CASCADE,
    actor_id   uuid        NOT NULL,
    action     text        NOT NULL,
    target_id  uuid,
    details    jsonb       NOT NULL DEFAULT '{}',
    at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX log_session_idx ON tutoring.log (session_id, id);
