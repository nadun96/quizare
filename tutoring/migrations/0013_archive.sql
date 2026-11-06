-- A finished session can be archived, then deleted (D-149).
ALTER TABLE tutoring.sessions ADD COLUMN archived_at timestamptz;
