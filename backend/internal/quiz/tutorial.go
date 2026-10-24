package quiz

import (
	"context"
	"strings"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/settings"
)

// A tutorial's questions (D-74) live in a quiz of kind "tutorial" under the
// tutorial's topic: the question editor, CSV import and resources work on it
// as on any quiz, and its questions inherit the classroom's settings. The
// tutorial module owns it; it's never listed, run or deleted on its own.

// CreateTutorialQuestions makes the quiz that holds a new tutorial's questions.
func (s *Service) CreateTutorialQuestions(ctx context.Context, teacherID, topicID, title string) (Quiz, error) {
	tc, err := s.topics.TopicContext(ctx, teacherID, topicID)
	if err != nil {
		return Quiz{}, err
	}
	if tc.ClassroomArchived {
		return Quiz{}, httpx.Conflict("the classroom is archived")
	}
	var id string
	if err := s.pool.QueryRow(ctx, `INSERT INTO quiz.quizzes(topic_id, teacher_id, title, kind) VALUES ($1,$2,$3,'tutorial') RETURNING id`,
		topicID, teacherID, strings.TrimSpace(title)).Scan(&id); err != nil {
		return Quiz{}, err
	}
	return s.GetQuiz(ctx, teacherID, id)
}

// DeleteTutorialQuestions removes a tutorial's quiz and its questions.
func (s *Service) DeleteTutorialQuestions(ctx context.Context, teacherID, quizID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM quiz.quizzes WHERE id=$1 AND teacher_id=$2 AND kind='tutorial'`, quizID, teacherID)
	return err
}

// TutorialQuestions returns a tutorial's questions, with each question's
// effective settings (classroom → … → quiz → question), for marking.
func (s *Service) TutorialQuestions(ctx context.Context, quizID string) ([]Question, map[string]settings.Effective, error) {
	var teacherID string
	if err := s.pool.QueryRow(ctx, `SELECT teacher_id FROM quiz.quizzes WHERE id=$1 AND kind='tutorial'`, quizID).Scan(&teacherID); err != nil {
		return nil, nil, err
	}
	q, err := s.GetQuiz(ctx, teacherID, quizID)
	if err != nil {
		return nil, nil, err
	}
	eff, err := s.effective(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	qs, err := s.loadQuestions(ctx, quizID)
	if err != nil {
		return nil, nil, err
	}
	effs := make(map[string]settings.Effective, len(qs))
	for _, x := range qs {
		effs[x.ID] = settings.Resolve(eff, x.Settings)
	}
	return qs, effs, nil
}
