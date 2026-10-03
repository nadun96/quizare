CREATE SCHEMA auth;

CREATE TABLE auth.users (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email             text        NOT NULL,          -- stored lower-cased
    name              text        NOT NULL,
    role              text        NOT NULL CHECK (role IN ('student', 'teacher', 'admin')),
    status            text        NOT NULL DEFAULT 'active'
                                  CHECK (status IN ('active', 'pending_approval', 'suspended', 'deleted')),
    password_hash     text        NOT NULL,
    email_verified_at timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
-- Deleted accounts are anonymised, not removed, so results stay intact (NFR-04, BR-16).
CREATE UNIQUE INDEX users_email_key ON auth.users (email) WHERE status <> 'deleted';

-- Server-side sessions (ADR-13). Only the SHA-256 of the token is stored.
CREATE TABLE auth.sessions (
    token_hash   bytea PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,                 -- absolute timeout
    user_agent   text        NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_idx ON auth.sessions (user_id);

-- One-time tokens for email verification and password reset.
CREATE TABLE auth.tokens (
    token_hash bytea PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    purpose    text        NOT NULL CHECK (purpose IN ('verify_email', 'reset_password')),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);

-- Platform-wide auth policy set by the admin (FR-ACC-05, Q-09).
CREATE TABLE auth.policy (
    id                       boolean PRIMARY KEY DEFAULT true CHECK (id),
    require_teacher_approval boolean NOT NULL DEFAULT false
);
INSERT INTO auth.policy DEFAULT VALUES;
