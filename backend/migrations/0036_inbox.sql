-- Teachers' announcements to a classroom and messages between a teacher
-- and a student (D-103).
CREATE SCHEMA inbox;

CREATE TABLE inbox.announcements (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    classroom_id uuid        NOT NULL REFERENCES content.classrooms (id) ON DELETE CASCADE,
    author_id    uuid        REFERENCES auth.users (id) ON DELETE SET NULL,
    title        text        NOT NULL,
    body         text        NOT NULL,
    emailed      boolean     NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX announcements_classroom ON inbox.announcements (classroom_id, created_at DESC);

-- Who has read each announcement; the teacher sees how many.
CREATE TABLE inbox.announcement_reads (
    announcement_id uuid        NOT NULL REFERENCES inbox.announcements (id) ON DELETE CASCADE,
    user_id         uuid        NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    read_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (announcement_id, user_id)
);

-- One conversation for each teacher and student; each side's read_at is
-- when they last read it.
CREATE TABLE inbox.conversations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    teacher_id      uuid        NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    student_id      uuid        NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    teacher_read_at timestamptz,
    student_read_at timestamptz,
    last_at         timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (teacher_id, student_id)
);
CREATE INDEX conversations_student ON inbox.conversations (student_id, last_at DESC);

CREATE TABLE inbox.messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid        NOT NULL REFERENCES inbox.conversations (id) ON DELETE CASCADE,
    sender_id       uuid        REFERENCES auth.users (id) ON DELETE SET NULL,
    body            text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX messages_conversation ON inbox.messages (conversation_id, created_at DESC);

-- Whether new announcements and messages also come by email (on unless
-- turned off).
CREATE TABLE inbox.prefs (
    user_id uuid    PRIMARY KEY REFERENCES auth.users (id) ON DELETE CASCADE,
    email   boolean NOT NULL DEFAULT true
);
