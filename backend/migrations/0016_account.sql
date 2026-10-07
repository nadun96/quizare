-- Profile pictures (D-49). Uploads are decoded and re-encoded as a 256×256
-- JPEG, so no metadata (EXIF location) or crafted file content is kept.
CREATE TABLE auth.avatars (
    user_id    uuid PRIMARY KEY REFERENCES auth.users (id) ON DELETE CASCADE,
    image      bytea       NOT NULL CHECK (octet_length(image) <= 262144),
    updated_at timestamptz NOT NULL DEFAULT now()
);
-- Changes whenever the picture does, so browsers can cache /api/avatars/{id}?v=….
ALTER TABLE auth.users ADD COLUMN avatar_version text;
