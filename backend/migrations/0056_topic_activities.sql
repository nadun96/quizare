-- Polls and meetings can sit under a topic, like quizzes, tutorials and
-- presentations (D-147). Losing the topic leaves them in the classroom.
ALTER TABLE poll.polls ADD COLUMN topic_id uuid REFERENCES content.topics (id) ON DELETE SET NULL;
CREATE INDEX polls_topic_idx ON poll.polls (topic_id) WHERE topic_id IS NOT NULL;
CREATE INDEX polls_classroom_idx ON poll.polls (classroom_id) WHERE classroom_id IS NOT NULL;

ALTER TABLE meet.meetings ADD COLUMN topic_id uuid REFERENCES content.topics (id) ON DELETE SET NULL;
CREATE INDEX meetings_topic_idx ON meet.meetings (topic_id) WHERE topic_id IS NOT NULL;
