-- Sign-in with OpenID Connect providers: Google, Microsoft Entra ID,
-- Keycloak, Okta… (D-70).

-- Which outside sign-ins belong to which account. The provider's subject
-- ("sub") never changes, unlike the email; one sign-in per provider per account.
CREATE TABLE auth.identities (
    provider_id  uuid        NOT NULL,
    subject      text        NOT NULL,
    user_id      uuid        NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    email        text        NOT NULL DEFAULT '',   -- as the provider last gave it, for the account page
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_id, subject),
    UNIQUE (provider_id, user_id)
);
CREATE INDEX identities_user_idx ON auth.identities (user_id);

CREATE SCHEMA oidc;

CREATE TABLE oidc.providers (
    id             uuid    PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text    NOT NULL CHECK (length(name) BETWEEN 1 AND 60),  -- on the button: "Sign in with <name>"
    enabled        boolean NOT NULL DEFAULT false,
    position       integer NOT NULL DEFAULT 0,
    issuer         text    NOT NULL,                     -- https://accounts.google.com
    client_id      text    NOT NULL,
    -- The client secret, sealed with the master key like the LLM keys (ADR-09).
    secret_ciphertext bytea, secret_nonce bytea, secret_wrapped_dek bytea, secret_dek_nonce bytea, secret_kek_id text,
    secret_version integer NOT NULL DEFAULT 0,
    scopes         text[]  NOT NULL DEFAULT '{openid,email,profile}',
    email_claim    text    NOT NULL DEFAULT 'email',     -- Entra ID: preferred_username
    trust_email    boolean NOT NULL DEFAULT false,       -- treat emails as verified when the provider doesn't say
    allowed_domains text[] NOT NULL DEFAULT '{}',        -- empty = any
    new_accounts   text    NOT NULL DEFAULT 'none' CHECK (new_accounts IN ('none', 'student', 'teacher')),
    teacher_claim  text    NOT NULL DEFAULT '',          -- e.g. groups or roles; new accounts with a matching value become teachers
    teacher_values text[]  NOT NULL DEFAULT '{}',
    link_by_email  boolean NOT NULL DEFAULT true,        -- a first sign-in may join the student or teacher account with the same email
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     uuid
);

ALTER TABLE auth.identities ADD FOREIGN KEY (provider_id) REFERENCES oidc.providers(id) ON DELETE CASCADE;

-- Sign-ins in progress: between leaving for the provider and coming back.
-- The browser holds the state in a cookie; the rest stays here.
CREATE TABLE oidc.logins (
    state_hash  bytea PRIMARY KEY,
    provider_id uuid  NOT NULL REFERENCES oidc.providers(id) ON DELETE CASCADE,
    nonce       text  NOT NULL,
    verifier    text  NOT NULL,                -- PKCE
    next        text  NOT NULL DEFAULT '/',
    link_user   uuid  REFERENCES auth.users(id) ON DELETE CASCADE,  -- linking a signed-in account, not signing in
    created_at  timestamptz NOT NULL DEFAULT now()
);
