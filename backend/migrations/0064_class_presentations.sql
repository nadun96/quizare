-- Group presentations in a classroom (D-166): the teacher sets presentation
-- tasks; each classroom group (D-158) uploads one presentation a task (a PDF,
-- or PowerPoint turned into PDF), rendered to slides in the browser. Once the
-- teacher allows it, a member presses Present: it goes on the classroom's
-- screen, they turn the slides and classmates follow. The teacher can take
-- over or stop it, marks it, and may delete the uploads later.
CREATE TABLE grading.pres_tasks (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    classroom_id uuid NOT NULL REFERENCES content.classrooms (id) ON DELETE CASCADE,
    topic_id     uuid REFERENCES content.topics (id) ON DELETE SET NULL,
    title        text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    instructions text NOT NULL DEFAULT '' CHECK (length(instructions) <= 5000),
    upload_by    text NOT NULL DEFAULT 'any' CHECK (upload_by IN ('any', 'leader')),
    out_of       numeric(9, 2) NOT NULL DEFAULT 0 CHECK (out_of >= 0 AND out_of <= 10000), -- 0: not marked
    rating       boolean NOT NULL DEFAULT false, -- classmates rate each one, 1-5
    due_at       timestamptz,
    created_by   uuid REFERENCES auth.users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pres_tasks_classroom_idx ON grading.pres_tasks (classroom_id);
CREATE INDEX pres_tasks_topic_idx ON grading.pres_tasks (topic_id);
CREATE INDEX pres_tasks_created_by_idx ON grading.pres_tasks (created_by);

-- A group's presentation for a task.
CREATE TABLE grading.pres_submissions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id       uuid NOT NULL REFERENCES grading.pres_tasks (id) ON DELETE CASCADE,
    category_id   uuid NOT NULL REFERENCES content.categories (id) ON DELETE CASCADE,
    uploaded_by   uuid REFERENCES auth.users (id) ON DELETE SET NULL,
    title         text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    file_name     text NOT NULL DEFAULT '',
    slides        int  NOT NULL DEFAULT 0 CHECK (slides BETWEEN 0 AND 100),
    ready         boolean NOT NULL DEFAULT false, -- every slide is in
    files_removed boolean NOT NULL DEFAULT false, -- the teacher deleted the slides, keeping the mark
    allowed_at    timestamptz,                    -- the teacher lets its group present it
    presented_at  timestamptz,
    mark          numeric(9, 2) CHECK (mark >= 0),
    comment       text NOT NULL DEFAULT '' CHECK (length(comment) <= 1000),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (task_id, category_id)
);
CREATE INDEX pres_submissions_category_idx ON grading.pres_submissions (category_id);
CREATE INDEX pres_submissions_uploaded_by_idx ON grading.pres_submissions (uploaded_by);

CREATE TABLE grading.pres_slides (
    submission_id uuid NOT NULL REFERENCES grading.pres_submissions (id) ON DELETE CASCADE,
    n             int  NOT NULL CHECK (n BETWEEN 1 AND 100),
    image         bytea NOT NULL,
    text          text NOT NULL DEFAULT '',
    PRIMARY KEY (submission_id, n)
);

CREATE TABLE grading.pres_ratings (
    submission_id uuid NOT NULL REFERENCES grading.pres_submissions (id) ON DELETE CASCADE,
    user_id       uuid NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    stars         int  NOT NULL CHECK (stars BETWEEN 1 AND 5),
    PRIMARY KEY (submission_id, user_id)
);
CREATE INDEX pres_ratings_user_idx ON grading.pres_ratings (user_id);

-- What's on the classroom's screen: one presentation, its slide, and who turns them.
CREATE TABLE grading.pres_stage (
    classroom_id  uuid PRIMARY KEY REFERENCES content.classrooms (id) ON DELETE CASCADE,
    submission_id uuid NOT NULL REFERENCES grading.pres_submissions (id) ON DELETE CASCADE,
    slide         int  NOT NULL DEFAULT 1,
    presenter     uuid REFERENCES auth.users (id) ON DELETE SET NULL, -- empty: the teacher
    started_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pres_stage_submission_idx ON grading.pres_stage (submission_id);
CREATE INDEX pres_stage_presenter_idx ON grading.pres_stage (presenter);
