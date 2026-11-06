-- A finished activity can be put in the archive, out of the everyday lists, and
-- deleted from there (D-149). Its marks and attendance stay until then.
ALTER TABLE live.sessions ADD COLUMN archived_at timestamptz;
ALTER TABLE poll.polls ADD COLUMN archived_at timestamptz;
ALTER TABLE meet.meetings ADD COLUMN archived_at timestamptz;
ALTER TABLE presentation.lives ADD COLUMN archived_at timestamptz;
