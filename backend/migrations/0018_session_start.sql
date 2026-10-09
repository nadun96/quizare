-- The teacher starts the quiz for everyone (start_mode=teacher, D-53).
ALTER TABLE live.sessions ADD COLUMN started_at timestamptz;
