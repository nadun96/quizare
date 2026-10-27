package content

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Deleting a classroom, module or topic takes everything inside with it
// (D-92): quizzes with their sessions and results, tutorials with their
// students' work, and presentations. When results would go, the teacher
// types the name to confirm; nothing goes while a quiz session is live.

// Contents is what deleting a classroom, module or topic takes with it.
type Contents struct {
	Name          string `json:"name"`
	Modules       int    `json:"modules"`
	Topics        int    `json:"topics"`
	Quizzes       int    `json:"quizzes"`
	Tutorials     int    `json:"tutorials"`
	Presentations int    `json:"presentations"`
	Sessions      int    `json:"sessions"`      // quiz sessions, with their results
	Attempts      int    `json:"attempts"`      // students' attempts in them
	TutorialRuns  int    `json:"tutorial_runs"` // students' work on tutorials
	Enrolments    int    `json:"enrolments"`
	LiveSessions  int    `json:"live_sessions"` // not ended: deleting waits for them
	// NeedsConfirm: results would be deleted, so the name must be typed.
	NeedsConfirm bool `json:"needs_confirm"`
}

var (
	errLiveSession  = httpx.NewError(http.StatusConflict, "session_live", "a quiz session in it is still open or live: end it first")
	errConfirmName  = httpx.NewError(http.StatusConflict, "confirm_needed", "this deletes students' results too: type the name to confirm")
	errUnknownLevel = httpx.ErrNotFound
)

// scope finds the topics under a classroom, module or topic the teacher
// owns, and its name.
func (s *Service) scope(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, teacherID, level, id string) (name string, topics []string, err error) {
	var sql string
	switch level {
	case "classroom":
		sql = `SELECT c.name, coalesce(array_agg(t.id) FILTER (WHERE t.id IS NOT NULL), '{}')::text[]
			FROM content.classrooms c LEFT JOIN content.modules m ON m.classroom_id=c.id LEFT JOIN content.topics t ON t.module_id=m.id
			WHERE c.id::text=$1 AND c.teacher_id=$2 GROUP BY c.id`
	case "module":
		sql = `SELECT m.name, coalesce(array_agg(t.id) FILTER (WHERE t.id IS NOT NULL), '{}')::text[]
			FROM content.modules m JOIN content.classrooms c ON c.id=m.classroom_id LEFT JOIN content.topics t ON t.module_id=m.id
			WHERE m.id::text=$1 AND c.teacher_id=$2 GROUP BY m.id`
	case "topic":
		sql = `SELECT t.name, ARRAY[t.id::text]
			FROM content.topics t JOIN content.modules m ON m.id=t.module_id JOIN content.classrooms c ON c.id=m.classroom_id
			WHERE t.id::text=$1 AND c.teacher_id=$2`
	default:
		return "", nil, errUnknownLevel
	}
	err = q.QueryRow(ctx, sql, id, teacherID).Scan(&name, &topics)
	if err == pgx.ErrNoRows {
		return "", nil, httpx.ErrNotFound
	}
	return name, topics, err
}

// DeletePreview says what deleting a classroom, module or topic would take.
func (s *Service) DeletePreview(ctx context.Context, teacherID, level, id string) (Contents, error) {
	name, topics, err := s.scope(ctx, s.pool, teacherID, level, id)
	if err != nil {
		return Contents{}, err
	}
	c := Contents{Name: name}
	err = s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM content.modules WHERE $2 = 'classroom' AND classroom_id::text = $3),
		cardinality($1::text[]),
		(SELECT count(*) FROM quiz.quizzes WHERE topic_id::text = ANY($1) AND kind = 'quiz'),
		(SELECT count(*) FROM tutorial.tutorials WHERE topic_id::text = ANY($1)),
		(SELECT count(*) FROM presentation.presentations WHERE topic_id::text = ANY($1)),
		(SELECT count(*) FROM live.sessions s JOIN quiz.quizzes q ON q.id = s.quiz_id WHERE q.topic_id::text = ANY($1)),
		(SELECT count(*) FROM live.attempts a JOIN live.sessions s ON s.id = a.session_id JOIN quiz.quizzes q ON q.id = s.quiz_id WHERE q.topic_id::text = ANY($1)),
		(SELECT count(*) FROM tutorial.runs r JOIN tutorial.tutorials t ON t.id = r.tutorial_id WHERE t.topic_id::text = ANY($1)),
		(SELECT count(*) FROM content.enrolments WHERE $2 = 'classroom' AND classroom_id::text = $3),
		(SELECT count(*) FROM live.sessions s JOIN quiz.quizzes q ON q.id = s.quiz_id WHERE q.topic_id::text = ANY($1) AND s.status <> 'ended')`,
		topics, level, id).Scan(&c.Modules, &c.Topics, &c.Quizzes, &c.Tutorials, &c.Presentations, &c.Sessions, &c.Attempts, &c.TutorialRuns, &c.Enrolments, &c.LiveSessions)
	if level == "classroom" {
		// A classroom's sessions are its own even if their quiz moved away.
		var n, live int
		if err == nil {
			err = s.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE status <> 'ended') FROM live.sessions WHERE classroom_id::text=$1`, id).Scan(&n, &live)
		}
		c.Sessions, c.LiveSessions = max(c.Sessions, n), max(c.LiveSessions, live)
	}
	c.NeedsConfirm = c.Sessions > 0 || c.TutorialRuns > 0
	return c, err
}

// DeleteNode deletes a classroom, module or topic with everything inside.
// confirm must be its name when results would be deleted.
func (s *Service) DeleteNode(ctx context.Context, teacherID, level, id, confirm string) error {
	c, err := s.DeletePreview(ctx, teacherID, level, id)
	if err != nil {
		return err
	}
	if c.LiveSessions > 0 {
		return errLiveSession
	}
	if c.NeedsConfirm && strings.TrimSpace(confirm) != strings.TrimSpace(c.Name) {
		return errConfirmName
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, topics, err := s.scope(ctx, tx, teacherID, level, id)
		if err != nil {
			return err
		}
		if level == "classroom" {
			if _, err := tx.Exec(ctx, `DELETE FROM live.sessions WHERE classroom_id::text=$1`, id); err != nil {
				return err
			}
		}
		if err := purgeTopics(ctx, tx, topics); err != nil {
			return err
		}
		var tag interface{ RowsAffected() int64 }
		switch level {
		case "classroom":
			tag, err = tx.Exec(ctx, `DELETE FROM content.classrooms WHERE id::text=$1 AND teacher_id=$2`, id, teacherID)
		case "module":
			tag, err = tx.Exec(ctx, `DELETE FROM content.modules WHERE id::text=$1`, id)
		case "topic":
			tag, err = tx.Exec(ctx, `DELETE FROM content.topics WHERE id::text=$1`, id)
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit.Log(ctx, tx, teacherID, level+"_deleted", level, id, c)
	})
}

// purgeTopics deletes what lives in topics, children before parents: the
// quiz sessions (their attempts and results go with them), tutorials (their
// students' work goes with them) and their questions, quizzes, and
// presentations. The topics themselves are left to the caller.
func purgeTopics(ctx context.Context, tx pgx.Tx, topics []string) error {
	if len(topics) == 0 {
		return nil
	}
	for _, sql := range []string{
		`DELETE FROM live.sessions WHERE quiz_id IN (SELECT id FROM quiz.quizzes WHERE topic_id::text = ANY($1))`,
		`DELETE FROM tutorial.tutorials WHERE topic_id::text = ANY($1)`,
		`DELETE FROM quiz.quizzes WHERE topic_id::text = ANY($1)`,
		`DELETE FROM presentation.presentations WHERE topic_id::text = ANY($1)`,
	} {
		if _, err := tx.Exec(ctx, sql, topics); err != nil {
			return err
		}
	}
	return nil
}
