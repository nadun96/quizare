-- Email to groups of accounts (D-102): templates with placeholders, and a
-- record of every send.
CREATE SCHEMA mailing;

CREATE TABLE mailing.templates (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL,
    subject    text        NOT NULL,
    body       text        NOT NULL,
    created_by uuid        REFERENCES auth.users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX templates_name_key ON mailing.templates (lower(name));

CREATE TABLE mailing.sends (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id    uuid        REFERENCES mailing.templates (id) ON DELETE SET NULL,
    subject        text        NOT NULL,             -- as written, before placeholders are filled
    body           text        NOT NULL,
    audience       jsonb       NOT NULL,
    audience_label text        NOT NULL,
    recipients     int         NOT NULL,
    sent_by        uuid        REFERENCES auth.users (id) ON DELETE SET NULL,
    sent_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sends_sent_at ON mailing.sends (sent_at DESC);
