CREATE SCHEMA llm;

-- Teachers' LLM API keys under AES-256-GCM envelope encryption (ADR-09, NFR-01).
-- Only ciphertext, the wrapped DEK, the KEK id and the last 4 characters are stored.
CREATE TABLE llm.keys (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    teacher_id      uuid        NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    provider        text        NOT NULL CHECK (provider IN ('anthropic', 'openai', 'google')),
    label           text        NOT NULL DEFAULT '',
    model           text        NOT NULL,
    key_version     int         NOT NULL DEFAULT 1,
    ciphertext      bytea       NOT NULL,
    nonce           bytea       NOT NULL,
    wrapped_dek     bytea       NOT NULL,
    dek_nonce       bytea       NOT NULL,
    kek_id          text        NOT NULL,
    last4           text        NOT NULL,
    is_default      boolean     NOT NULL DEFAULT false,
    last_test_at    timestamptz,
    last_test_ok    boolean,
    last_test_error text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX keys_teacher_idx ON llm.keys (teacher_id);
CREATE UNIQUE INDEX keys_one_default ON llm.keys (teacher_id) WHERE is_default;
