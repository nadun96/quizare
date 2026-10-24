-- Presentations, self-paced (D-76, PR-FR-01 to PR-FR-34): a slide deck from
-- a PDF, which students go through at their own pace, and the time each
-- student spent on each slide. The teacher's browser renders the PDF's pages
-- to JPEG slides and uploads them, so the server needs no PDF software.

CREATE SCHEMA presentation;

CREATE TABLE presentation.presentations (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    topic_id       uuid NOT NULL REFERENCES content.topics(id) ON DELETE RESTRICT,
    teacher_id     uuid NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    title          text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    description    text NOT NULL DEFAULT '',
    status         text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    allow_jump     boolean NOT NULL DEFAULT true,   -- any slide, or in order
    allow_download boolean NOT NULL DEFAULT false,  -- the PDF
    version        integer NOT NULL DEFAULT 0,      -- the file in use; 0 = none yet
    opens_at       timestamptz,
    due_at         timestamptz,
    published_at   timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX presentations_topic_idx ON presentation.presentations (topic_id);

-- Each uploaded file. A new one is uploaded beside the one in use and
-- replaces it when every slide is in; the old one is then deleted.
CREATE TABLE presentation.files (
    presentation_id uuid NOT NULL REFERENCES presentation.presentations(id) ON DELETE CASCADE,
    version         integer NOT NULL,
    name            text NOT NULL,
    slides          integer NOT NULL CHECK (slides BETWEEN 1 AND 300),
    pdf             bytea,
    bytes           integer NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (presentation_id, version)
);

CREATE TABLE presentation.slides (
    presentation_id uuid NOT NULL,
    version         integer NOT NULL,
    n               integer NOT NULL,           -- from 1
    image           bytea NOT NULL,             -- JPEG
    width           integer NOT NULL,
    height          integer NOT NULL,
    text            text NOT NULL DEFAULT '',   -- the slide's own text, for alt text
    PRIMARY KEY (presentation_id, version, n),
    FOREIGN KEY (presentation_id, version) REFERENCES presentation.files(presentation_id, version) ON DELETE CASCADE
);

-- One student's way through a presentation.
CREATE TABLE presentation.viewers (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    presentation_id uuid NOT NULL REFERENCES presentation.presentations(id) ON DELETE CASCADE,
    user_id         uuid NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    current         integer NOT NULL DEFAULT 1,
    reached         integer NOT NULL DEFAULT 1,
    finished_at     timestamptz,                -- when they first reached the last slide
    started_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (presentation_id, user_id)
);

-- Time on each slide, by slide number (PR-FR-05); live views come later.
CREATE TABLE presentation.views (
    viewer_id uuid NOT NULL REFERENCES presentation.viewers(id) ON DELETE CASCADE,
    n         integer NOT NULL,
    mode      text NOT NULL DEFAULT 'self' CHECK (mode IN ('self', 'live')),
    seconds   integer NOT NULL DEFAULT 0,
    first_at  timestamptz NOT NULL DEFAULT now(),
    last_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (viewer_id, n, mode)
);
