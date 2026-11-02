-- Staying fast as the platform grows, and long-term trends (D-119).

-- 1. Indexes for every foreign key that is looked up or cascaded through:
-- without one, deleting a user or listing a teacher's sessions reads the
-- whole table, which is fine with a hundred rows and slow with a million.
CREATE INDEX IF NOT EXISTS sessions_teacher_idx       ON live.sessions (teacher_id);
CREATE INDEX IF NOT EXISTS sessions_classroom_idx     ON live.sessions (classroom_id);
CREATE INDEX IF NOT EXISTS attempts_user_idx          ON live.attempts (user_id);
CREATE INDEX IF NOT EXISTS results_user_idx           ON eval.results (user_id);
CREATE INDEX IF NOT EXISTS results_updated_idx        ON eval.results (updated_at);
CREATE INDEX IF NOT EXISTS tokens_user_idx            ON auth.tokens (user_id);
CREATE INDEX IF NOT EXISTS tutorials_teacher_idx      ON tutorial.tutorials (teacher_id);
CREATE INDEX IF NOT EXISTS runs_user_idx              ON tutorial.runs (user_id);
CREATE INDEX IF NOT EXISTS runs_updated_idx           ON tutorial.runs (updated_at);
CREATE INDEX IF NOT EXISTS presentations_teacher_idx  ON presentation.presentations (teacher_id);
CREATE INDEX IF NOT EXISTS viewers_user_idx           ON presentation.viewers (user_id);
CREATE INDEX IF NOT EXISTS lives_started_by_idx       ON presentation.lives (started_by);
CREATE INDEX IF NOT EXISTS meetings_classroom_idx     ON meet.meetings (classroom_id);
CREATE INDEX IF NOT EXISTS meet_attendance_user_idx   ON meet.attendance (user_id);
CREATE INDEX IF NOT EXISTS share_links_teacher_idx    ON analytics.share_links (teacher_id);
CREATE INDEX IF NOT EXISTS poll_files_participant_idx ON poll.files (participant_id);
CREATE INDEX IF NOT EXISTS board_strokes_participant_idx ON poll.board_strokes (participant_id);
CREATE INDEX IF NOT EXISTS download_links_backup_idx  ON storage.download_links (backup_id);
CREATE INDEX IF NOT EXISTS oidc_logins_user_idx       ON oidc.logins (link_user);
CREATE INDEX IF NOT EXISTS oidc_logins_provider_idx   ON oidc.logins (provider_id);
CREATE INDEX IF NOT EXISTS connect_states_teacher_idx ON drive.connect_states (teacher_id);
CREATE INDEX IF NOT EXISTS classroom_files_added_idx  ON drive.classroom_files (added_by);
CREATE INDEX IF NOT EXISTS pauses_by_idx              ON oversight.pauses (paused_by);
CREATE INDEX IF NOT EXISTS templates_created_idx      ON mailing.templates (created_by);
CREATE INDEX IF NOT EXISTS sends_by_idx               ON mailing.sends (sent_by);
CREATE INDEX IF NOT EXISTS sends_template_idx         ON mailing.sends (template_id);
CREATE INDEX IF NOT EXISTS announcements_author_idx   ON inbox.announcements (author_id);
CREATE INDEX IF NOT EXISTS announcement_reads_user_idx ON inbox.announcement_reads (user_id);
CREATE INDEX IF NOT EXISTS messages_sender_idx        ON inbox.messages (sender_id);

-- 2. Month by month, per student in a classroom and per classroom: small
-- tables that long-term charts read instead of every result ever recorded.
-- A background job keeps them up to date; months are UTC calendar months.
CREATE TABLE analytics.student_months (
    classroom_id uuid    NOT NULL REFERENCES content.classrooms (id) ON DELETE CASCADE,
    month        date    NOT NULL,           -- its first day
    user_id      uuid    NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    quizzes      int     NOT NULL,           -- complete, valid quiz results
    pct_sum      float8  NOT NULL,           -- of those results' percentages: the average is pct_sum / quizzes
    passed       int     NOT NULL,
    tutorials    int     NOT NULL,           -- tutorials finished
    active_days  int     NOT NULL,           -- days with a result or a finished tutorial
    PRIMARY KEY (classroom_id, month, user_id)
);
CREATE INDEX student_months_user_idx ON analytics.student_months (user_id, month);

CREATE TABLE analytics.classroom_months (
    classroom_id uuid        NOT NULL REFERENCES content.classrooms (id) ON DELETE CASCADE,
    month        date        NOT NULL,
    enrolled     int         NOT NULL,       -- members during the month; frozen once the month is over
    active       int         NOT NULL,       -- members with a result or a finished tutorial
    sessions     int         NOT NULL,       -- quiz sessions with a result in the month
    results      int         NOT NULL,
    pct_sum      float8      NOT NULL,
    passed       int         NOT NULL,
    tutorials    int         NOT NULL,
    computed_at  timestamptz NOT NULL,
    PRIMARY KEY (classroom_id, month)
);

-- Where the job got to: it looks only at what changed since.
CREATE TABLE analytics.rollup_state (
    id      boolean PRIMARY KEY DEFAULT true CHECK (id),
    last_at timestamptz NOT NULL
);
