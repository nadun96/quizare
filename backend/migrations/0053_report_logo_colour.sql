-- A classroom's reports can carry a logo and its colour (D-143).
ALTER TABLE grading.report_headers
    ADD COLUMN logo        bytea,
    ADD COLUMN logo_type   text CHECK (logo_type IN ('image/png', 'image/jpeg')),
    ADD COLUMN accent      text    NOT NULL DEFAULT '#1e4fa3' CHECK (accent ~ '^#[0-9a-f]{6}$'),
    ADD COLUMN color_marks boolean NOT NULL DEFAULT true; -- tint marks by how high they are
