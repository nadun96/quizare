-- gen_random_uuid() is built in from PostgreSQL 13; before that it came with
-- pgcrypto. Nothing else of pgcrypto is used, so it's only asked for on older
-- servers: some builds (aaPanel's, for one) don't ship it at all.
DO $$
BEGIN
    IF current_setting('server_version_num')::int < 130000 THEN
        CREATE EXTENSION IF NOT EXISTS pgcrypto;
    END IF;
END
$$;

CREATE SCHEMA IF NOT EXISTS audit;

-- NFR-15: every teacher/system action on sessions and marks is logged.
CREATE TABLE audit.events (
    id          bigserial PRIMARY KEY,
    actor_id    uuid,
    action      text        NOT NULL,
    target_type text        NOT NULL,
    target_id   text        NOT NULL,
    details     jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX events_target_idx ON audit.events (target_type, target_id);
CREATE INDEX events_created_idx ON audit.events (created_at);
