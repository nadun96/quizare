-- Classroom question pools (D-123): one per classroom, shared by all its
-- teachers, held as a quiz of kind 'pool' with no topic, like a teacher's
-- question bank. Its questions are grouped by category and drawn into the
-- classroom's quizzes by their pool rules ("class" source).
ALTER TABLE quiz.quizzes ADD COLUMN classroom_id uuid REFERENCES content.classrooms (id) ON DELETE CASCADE;
CREATE UNIQUE INDEX quizzes_one_class_pool_key ON quiz.quizzes (classroom_id) WHERE kind = 'pool';

ALTER TABLE quiz.quizzes DROP CONSTRAINT quizzes_kind_check;
ALTER TABLE quiz.quizzes ADD CONSTRAINT quizzes_kind_check CHECK (kind IN ('quiz', 'bank', 'tutorial', 'pool'));
ALTER TABLE quiz.quizzes DROP CONSTRAINT quizzes_bank_has_no_topic;
ALTER TABLE quiz.quizzes ADD CONSTRAINT quizzes_bank_has_no_topic CHECK ((kind IN ('bank', 'pool')) = (topic_id IS NULL));
ALTER TABLE quiz.quizzes ADD CONSTRAINT quizzes_pool_has_classroom CHECK ((kind = 'pool') = (classroom_id IS NOT NULL));
