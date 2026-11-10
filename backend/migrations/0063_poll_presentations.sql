-- Presentations in polls (D-165): the teacher asks for them; each student,
-- poll group or classroom group submits one (a PDF, or PowerPoint turned
-- into PDF), rendered to slides in the browser; the teacher lets one present
-- live to the poll, marks it, and the audience may rate it.
CREATE TABLE poll.pres_settings (
    poll_id    uuid PRIMARY KEY REFERENCES poll.polls (id) ON DELETE CASCADE,
    mode       text    NOT NULL DEFAULT 'off' CHECK (mode IN ('off', 'student', 'poll_group', 'class_group')),
    upload_by  text    NOT NULL DEFAULT 'any' CHECK (upload_by IN ('any', 'leader')),
    out_of     numeric(9, 2) NOT NULL DEFAULT 0 CHECK (out_of >= 0 AND out_of <= 10000), -- 0: not marked
    rating     boolean NOT NULL DEFAULT false, -- the audience rates each one, 1-5
    instructions text  NOT NULL DEFAULT '' CHECK (length(instructions) <= 2000),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE poll.submissions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    poll_id      uuid NOT NULL REFERENCES poll.polls (id) ON DELETE CASCADE,
    owner_kind   text NOT NULL CHECK (owner_kind IN ('student', 'poll_group', 'class_group')),
    owner_id     uuid NOT NULL, -- the participant, poll group or classroom group
    uploaded_by  uuid REFERENCES poll.participants (id) ON DELETE SET NULL,
    title        text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    file_name    text NOT NULL DEFAULT '',
    slides       int  NOT NULL DEFAULT 0 CHECK (slides BETWEEN 0 AND 100),
    ready        boolean NOT NULL DEFAULT false, -- every slide is in
    presented_at timestamptz,
    mark         numeric(9, 2) CHECK (mark >= 0),
    comment      text NOT NULL DEFAULT '' CHECK (length(comment) <= 1000),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (poll_id, owner_kind, owner_id)
);
CREATE INDEX submissions_uploaded_by_idx ON poll.submissions (uploaded_by);

CREATE TABLE poll.submission_slides (
    submission_id uuid NOT NULL REFERENCES poll.submissions (id) ON DELETE CASCADE,
    n             int  NOT NULL CHECK (n BETWEEN 1 AND 100),
    image         bytea NOT NULL,
    text          text NOT NULL DEFAULT '',
    PRIMARY KEY (submission_id, n)
);

CREATE TABLE poll.submission_ratings (
    submission_id  uuid NOT NULL REFERENCES poll.submissions (id) ON DELETE CASCADE,
    participant_id uuid NOT NULL REFERENCES poll.participants (id) ON DELETE CASCADE,
    stars          int  NOT NULL CHECK (stars BETWEEN 1 AND 5),
    PRIMARY KEY (submission_id, participant_id)
);
CREATE INDEX submission_ratings_participant_idx ON poll.submission_ratings (participant_id);

-- What's on the poll's screen: one submission, its slide, and who controls it.
CREATE TABLE poll.stage (
    poll_id       uuid PRIMARY KEY REFERENCES poll.polls (id) ON DELETE CASCADE,
    submission_id uuid NOT NULL REFERENCES poll.submissions (id) ON DELETE CASCADE,
    slide         int  NOT NULL DEFAULT 1,
    presenter     uuid REFERENCES poll.participants (id) ON DELETE SET NULL, -- empty: the teacher
    started_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX stage_submission_idx ON poll.stage (submission_id);
CREATE INDEX stage_presenter_idx ON poll.stage (presenter);

-- The marks count in the class report as their own item (kind presentation, id the poll).
ALTER TABLE grading.weights DROP CONSTRAINT weights_kind_check;
ALTER TABLE grading.weights ADD CONSTRAINT weights_kind_check CHECK (kind IN ('quiz', 'tutorial', 'offline', 'presentation'));
ALTER TABLE grading.report_items DROP CONSTRAINT report_items_kind_check;
ALTER TABLE grading.report_items ADD CONSTRAINT report_items_kind_check CHECK (kind IN ('quiz', 'tutorial', 'poll', 'offline', 'presentation'));
ALTER TABLE grading.groups DROP CONSTRAINT groups_kind_check;
ALTER TABLE grading.groups ADD CONSTRAINT groups_kind_check CHECK (kind IN ('class', 'quiz', 'tutorial', 'poll', 'offline', 'presentation'));
