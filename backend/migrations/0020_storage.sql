-- Storage and backups (PL-FR-04 to PL-FR-09, D-57). Owned by the storage module.
CREATE SCHEMA storage;

-- Hourly measurements of the space the platform uses (PL-FR-04); kept 90 days.
CREATE TABLE storage.snapshots (
    id             bigserial PRIMARY KEY,
    taken_at       timestamptz NOT NULL DEFAULT now(),
    db_bytes       bigint      NOT NULL,
    areas          jsonb       NOT NULL,   -- {"accounts": bytes, ...}
    backups_bytes  bigint      NOT NULL,
    recordings_bytes bigint    NOT NULL DEFAULT 0,
    disk_free      bigint      NOT NULL,
    disk_total     bigint      NOT NULL
);
CREATE INDEX snapshots_taken_idx ON storage.snapshots (taken_at);

-- Limits the admin sets (PL-FR-05).
CREATE TABLE storage.limits (
    id                    boolean PRIMARY KEY DEFAULT true CHECK (id),
    recording_limit_bytes bigint NOT NULL DEFAULT 5368709120,   -- 5 GB (PO-24)
    backup_space_bytes    bigint NOT NULL DEFAULT 10737418240   -- 10 GB of backups kept on the server
);
INSERT INTO storage.limits DEFAULT VALUES;

-- Backups made from the admin console, and those the nightly job left on the
-- server (PL-FR-08). Rows stay after the file is deleted, as history.
CREATE TABLE storage.backups (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind         text        NOT NULL CHECK (kind IN ('console', 'nightly')),
    file         text        NOT NULL UNIQUE,  -- path on the server
    status       text        NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'done', 'failed')),
    created_by   uuid,                         -- the admin or manager; NULL for the nightly job
    started_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    size_bytes   bigint      NOT NULL DEFAULT 0,
    tables_done  int         NOT NULL DEFAULT 0,
    tables_total int         NOT NULL DEFAULT 0,
    rows_done    bigint      NOT NULL DEFAULT 0,
    error        text        NOT NULL DEFAULT '',
    on_server    boolean     NOT NULL DEFAULT true,
    deleted_at   timestamptz
);
CREATE INDEX backups_started_idx ON storage.backups (started_at DESC);

-- Single-use download links, valid for an hour (PL-NFR-03). Only the hash is kept.
CREATE TABLE storage.download_links (
    token_hash bytea PRIMARY KEY,
    backup_id  uuid        NOT NULL REFERENCES storage.backups (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);
