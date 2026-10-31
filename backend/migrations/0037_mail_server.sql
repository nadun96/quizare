-- The mail server, set by the admin in the console (D-104). One row; until
-- it is turned on, the server's config file (QP_SMTP_*) decides.
CREATE TABLE mailing.server (
    id         boolean PRIMARY KEY DEFAULT true CHECK (id),
    enabled    boolean     NOT NULL DEFAULT false,
    host       text        NOT NULL DEFAULT '',
    port       int         NOT NULL DEFAULT 587 CHECK (port BETWEEN 1 AND 65535),
    security   text        NOT NULL DEFAULT 'starttls' CHECK (security IN ('auto', 'starttls', 'tls', 'none')),
    username   text        NOT NULL DEFAULT '',
    -- The password, sealed with the master key like the LLM keys (ADR-09).
    pw_ciphertext bytea, pw_nonce bytea, pw_wrapped_dek bytea, pw_dek_nonce bytea, pw_kek_id text,
    secret_version int     NOT NULL DEFAULT 0,
    from_addr  text        NOT NULL DEFAULT '',
    from_name  text        NOT NULL DEFAULT '',
    updated_by uuid        REFERENCES auth.users (id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO mailing.server DEFAULT VALUES;
