-- The admin's capacity test (D-125): virtual students and a teacher that
-- take a real quiz on this server, then are deleted with everything they
-- made. They're marked so lists, counts and group email leave them out and
-- only the running test can sign them in.
ALTER TABLE auth.users ADD COLUMN synthetic boolean NOT NULL DEFAULT false;
CREATE INDEX users_synthetic_idx ON auth.users (id) WHERE synthetic;

CREATE SCHEMA loadtest;

-- One test and its report. A test still "running" when the server starts
-- was cut short; it's marked "interrupted" and its accounts removed.
CREATE TABLE loadtest.runs (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id    uuid        REFERENCES auth.users (id) ON DELETE SET NULL,
    status      text        NOT NULL CHECK (status IN ('running', 'passed', 'failed', 'stopped', 'interrupted', 'error')),
    plan        jsonb       NOT NULL,
    report      jsonb       NOT NULL DEFAULT '{}',
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);
CREATE INDEX runs_admin_idx ON loadtest.runs (admin_id);
CREATE INDEX runs_started_idx ON loadtest.runs (started_at DESC);
