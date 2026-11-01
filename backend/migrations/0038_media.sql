-- Pictures inside rich text (D-113): redrawn on upload, served by id.
CREATE SCHEMA media;

CREATE TABLE media.images (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   uuid        REFERENCES auth.users (id) ON DELETE SET NULL,
    mime       text        NOT NULL CHECK (mime IN ('image/png', 'image/jpeg')),
    data       bytea       NOT NULL,
    width      int         NOT NULL,
    height     int         NOT NULL,
    bytes      int         NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX images_owner ON media.images (owner_id, created_at DESC);
