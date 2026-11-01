-- Local AI models with Ollama (D-115): the admin's server and its models,
-- and keys that use the school's server instead of a provider account.
CREATE TABLE llm.local (
    id            boolean PRIMARY KEY DEFAULT true CHECK (id),
    enabled       boolean     NOT NULL DEFAULT false,
    base_url      text        NOT NULL DEFAULT '',
    models        text[]      NOT NULL DEFAULT '{}',   -- teachers may use these; empty = every installed model
    default_model text        NOT NULL DEFAULT '',
    for_everyone  boolean     NOT NULL DEFAULT false,  -- teachers with no key of their own use default_model
    updated_by    uuid        REFERENCES auth.users (id) ON DELETE SET NULL,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE llm.keys DROP CONSTRAINT keys_provider_check;
ALTER TABLE llm.keys ADD CONSTRAINT keys_provider_check CHECK (provider IN ('anthropic', 'openai', 'google', 'ollama'));
