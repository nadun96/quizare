-- What else a classroom's class report shows, if the teacher wants it (D-156).
CREATE TABLE grading.report_options (
    classroom_id    uuid PRIMARY KEY REFERENCES content.classrooms (id) ON DELETE CASCADE,
    show_violations boolean NOT NULL DEFAULT false,
    show_attendance boolean NOT NULL DEFAULT false,
    show_dates      boolean NOT NULL DEFAULT false,
    updated_at      timestamptz NOT NULL DEFAULT now()
);
