-- A session can sit under one of its classroom's topics (D-147). The topic is
-- the platform's, so like the classroom it has no foreign key here.
ALTER TABLE tutoring.sessions ADD COLUMN topic_id uuid;
CREATE INDEX sessions_topic_idx ON tutoring.sessions (topic_id) WHERE topic_id IS NOT NULL;
