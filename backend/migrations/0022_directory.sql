-- Directory sign-in: LDAP and Windows Active Directory (D-64).

-- An account either has its own password (local) or signs in with the
-- school's directory. directory_id is the entry's stable id (objectGUID in
-- Active Directory, entryUUID in OpenLDAP), so a renamed or moved account
-- keeps its history; directory_dn is where it was last found, for the hourly
-- check that signs out accounts disabled or removed in the directory.
ALTER TABLE auth.users
    ADD COLUMN auth_source  text NOT NULL DEFAULT 'local' CHECK (auth_source IN ('local', 'directory')),
    ADD COLUMN directory_id text,
    ADD COLUMN directory_dn text;
CREATE UNIQUE INDEX users_directory_id_key ON auth.users (directory_id) WHERE directory_id IS NOT NULL AND status <> 'deleted';

CREATE SCHEMA directory;

-- One row: the connection and how directory entries become accounts.
CREATE TABLE directory.config (
    id             boolean PRIMARY KEY DEFAULT true CHECK (id),
    enabled        boolean NOT NULL DEFAULT false,
    kind           text    NOT NULL DEFAULT 'ad' CHECK (kind IN ('ad', 'ldap')),
    url            text    NOT NULL DEFAULT '',          -- ldaps://dc1.school.local or ldap://… with start_tls
    start_tls      boolean NOT NULL DEFAULT false,
    ca_pem         text    NOT NULL DEFAULT '',          -- extra trusted CA, PEM; empty = the system's
    bind_dn        text    NOT NULL DEFAULT '',          -- the service account that searches
    -- The service account's password, sealed with the master key like the LLM keys (ADR-09).
    bind_ciphertext bytea, bind_nonce bytea, bind_wrapped_dek bytea, bind_dek_nonce bytea, bind_kek_id text,
    base_dn        text    NOT NULL DEFAULT '',
    user_filter    text    NOT NULL DEFAULT '',          -- {login} is replaced by the escaped sign-in name
    id_attr        text    NOT NULL DEFAULT 'objectGUID',
    email_attr     text    NOT NULL DEFAULT 'mail',
    name_attr      text    NOT NULL DEFAULT 'displayName',
    teacher_group  text    NOT NULL DEFAULT '',          -- DN; members sign in as teachers
    student_group  text    NOT NULL DEFAULT '',          -- DN; empty = everyone else found is a student
    nested_groups  boolean NOT NULL DEFAULT true,        -- AD: members of groups inside the group count too
    link_by_email  boolean NOT NULL DEFAULT true,        -- an existing student or teacher account with the same email moves to the directory
    secret_version integer NOT NULL DEFAULT 0,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     uuid
);
INSERT INTO directory.config DEFAULT VALUES;
