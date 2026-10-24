-- Tutorials, phase 2 (D-75, TU-FR-22 to TU-FR-42): leaving the app, set per
-- page; time on each page; and AI marking of written answers.

-- Leaving the app on a page: allowed, recorded for the teacher, or not
-- allowed (the page warns, records it, and can lock after lock_after times).
ALTER TABLE tutorial.pages
    ADD COLUMN leaving    text    NOT NULL DEFAULT 'allowed' CHECK (leaving IN ('allowed', 'recorded', 'blocked')),
    ADD COLUMN lock_after integer NOT NULL DEFAULT 0 CHECK (lock_after BETWEEN 0 AND 20);  -- 0 = never locks

-- A student's time on a page, and the times they left the app from it.
-- Kept by page id, so reordering pages keeps it.
CREATE TABLE tutorial.visits (
    run_id         uuid NOT NULL REFERENCES tutorial.runs(id) ON DELETE CASCADE,
    page_id        uuid NOT NULL REFERENCES tutorial.pages(id) ON DELETE CASCADE,
    seconds        integer NOT NULL DEFAULT 0,  -- on the page, visible and in use
    leaves         integer NOT NULL DEFAULT 0,  -- left the app where it's recorded or not allowed
    allowed_leaves integer NOT NULL DEFAULT 0,  -- left where allowed, or through the page's own links
    away_seconds   integer NOT NULL DEFAULT 0,
    forgiven       integer NOT NULL DEFAULT 0,  -- leaves before the teacher last unlocked the page
    locked_at      timestamptz,
    first_at       timestamptz NOT NULL DEFAULT now(),
    last_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, page_id)
);
CREATE INDEX visits_page_idx ON tutorial.visits (page_id);

-- How an answer is marked, and what the AI said when it marked one.
ALTER TABLE tutorial.answers
    ADD COLUMN method      text    NOT NULL DEFAULT 'key' CHECK (method IN ('key', 'llm', 'manual')),
    ADD COLUMN ai_feedback text    NOT NULL DEFAULT '',
    ADD COLUMN ai_marked   boolean NOT NULL DEFAULT false,
    ADD COLUMN flagged     boolean NOT NULL DEFAULT false,
    ADD COLUMN flag_reason text    NOT NULL DEFAULT '';
UPDATE tutorial.answers SET method = 'manual' WHERE status = 'pending';
