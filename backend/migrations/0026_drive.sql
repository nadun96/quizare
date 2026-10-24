-- Google Drive for teachers (D-71): files for classrooms and questions,
-- importing questions, and results saved to Drive.

CREATE SCHEMA drive;

-- One row: the Google Cloud app the platform uses. Teachers connect their
-- own Drive through it.
CREATE TABLE drive.config (
    id             boolean PRIMARY KEY DEFAULT true CHECK (id),
    enabled        boolean NOT NULL DEFAULT false,
    client_id      text    NOT NULL DEFAULT '',
    -- The client secret, sealed with the master key like the LLM keys (ADR-09).
    secret_ciphertext bytea, secret_nonce bytea, secret_wrapped_dek bytea, secret_dek_nonce bytea, secret_kek_id text,
    secret_version integer NOT NULL DEFAULT 0,
    api_key        text    NOT NULL DEFAULT '',  -- for Google's file picker; restricted to this site in Google's console
    app_id         text    NOT NULL DEFAULT '',  -- the Cloud project number, for the picker
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     uuid
);
INSERT INTO drive.config DEFAULT VALUES;

-- A teacher's connection: the refresh token, sealed.
CREATE TABLE drive.connections (
    teacher_id     uuid PRIMARY KEY REFERENCES auth.users(id) ON DELETE CASCADE,
    email          text NOT NULL DEFAULT '',
    token_ciphertext bytea NOT NULL, token_nonce bytea NOT NULL, token_wrapped_dek bytea NOT NULL, token_dek_nonce bytea NOT NULL, token_kek_id text,
    token_version  integer NOT NULL DEFAULT 1,
    status         text NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'reconnect')),  -- reconnect: Google no longer accepts the token
    folder_id      text NOT NULL DEFAULT '',  -- where results go; made on first use
    auto_backup    boolean NOT NULL DEFAULT false,  -- save each session's results when it ends
    connected_at   timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- Connections in progress: between leaving for Google and coming back.
CREATE TABLE drive.connect_states (
    state_hash  bytea PRIMARY KEY,
    teacher_id  uuid NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    verifier    text NOT NULL,
    next        text NOT NULL DEFAULT '/t/settings',
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Drive files attached to a classroom, for its students.
CREATE TABLE drive.classroom_files (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    classroom_id  uuid NOT NULL REFERENCES content.classrooms(id) ON DELETE CASCADE,
    file_id       text NOT NULL,
    name          text NOT NULL,
    mime_type     text NOT NULL DEFAULT '',
    web_link      text NOT NULL DEFAULT '',
    icon_link     text NOT NULL DEFAULT '',
    -- students: shared with each enrolled student's email; link: anyone with
    -- the link; none: the teacher shares it in Drive.
    sharing       text NOT NULL DEFAULT 'students' CHECK (sharing IN ('students', 'link', 'none')),
    link_permission_id text NOT NULL DEFAULT '',  -- the "anyone with the link" permission this app made
    added_by      uuid NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    position      integer NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    synced_at     timestamptz,
    sync_error    text NOT NULL DEFAULT '',
    UNIQUE (classroom_id, file_id)
);

-- Who this app shared a classroom file with, so it can take it back when
-- a student leaves the classroom or the file is removed.
CREATE TABLE drive.shares (
    classroom_file_id uuid NOT NULL REFERENCES drive.classroom_files(id) ON DELETE CASCADE,
    email         text NOT NULL,
    permission_id text NOT NULL,
    PRIMARY KEY (classroom_file_id, email)
);

-- Results saved to Drive.
CREATE TABLE drive.exports (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    teacher_id  uuid NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    kind        text NOT NULL CHECK (kind IN ('session', 'poll')),
    target_id   uuid NOT NULL,
    name        text NOT NULL DEFAULT '',
    file_id     text NOT NULL DEFAULT '',
    web_link    text NOT NULL DEFAULT '',
    automatic   boolean NOT NULL DEFAULT false,
    status      text NOT NULL DEFAULT 'done' CHECK (status IN ('done', 'failed')),
    error       text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX exports_teacher_idx ON drive.exports (teacher_id, created_at DESC);
CREATE INDEX exports_target_idx ON drive.exports (target_id, created_at DESC);
