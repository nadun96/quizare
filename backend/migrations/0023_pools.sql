-- Question categories, pools and each teacher's question bank (D-66).

-- A category groups questions ("Easy", "Plants"); empty = none.
ALTER TABLE quiz.questions ADD COLUMN category text NOT NULL DEFAULT '';
CREATE INDEX questions_category_idx ON quiz.questions (quiz_id, category);

-- A question bank is a quiz of kind 'bank' with no topic: one per teacher.
-- Its questions are drawn into quizzes through pools and never run as a
-- session of their own.
ALTER TABLE quiz.quizzes ADD COLUMN kind text NOT NULL DEFAULT 'quiz' CHECK (kind IN ('quiz', 'bank'));
ALTER TABLE quiz.quizzes ALTER COLUMN topic_id DROP NOT NULL;
ALTER TABLE quiz.quizzes ADD CONSTRAINT quizzes_bank_has_no_topic CHECK ((kind = 'bank') = (topic_id IS NULL));
CREATE UNIQUE INDEX quizzes_one_bank_key ON quiz.quizzes (teacher_id) WHERE kind = 'bank';

-- Pool rules: [{"source": "quiz"|"bank", "category": "Easy", "count": 3}];
-- count 0 = all of them. Empty = no pools.
ALTER TABLE quiz.quizzes ADD COLUMN pools jsonb NOT NULL DEFAULT '[]'::jsonb;
