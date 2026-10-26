// Package oversight is the admin's live activity (D-90): everything live
// across the platform, ending any of it, and pausing new activities for
// everyone or for one teacher, to manage the server's load.
package oversight

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// The kinds of activity, as the API names them.
const (
	KindSession      = "quiz_session"
	KindPoll         = "poll"
	KindMeeting      = "meeting"
	KindPresentation = "presentation"
)

var kindLabels = map[string]string{
	KindSession: "quiz session", KindPoll: "poll", KindMeeting: "meeting", KindPresentation: "live presentation",
}

// Kind is how the admin ends one kind of activity, and, for those whose
// people are counted in memory, how many are in one now.
type Kind struct {
	End    func(ctx context.Context, actorID, id string) error
	Online func(id string) int
}

// Mailer queues emails (the platform's email jobs).
type Mailer func(ctx context.Context, msgs []mail.Message) error

type Service struct {
	pool  *pgxpool.Pool
	kinds map[string]Kind
	send  Mailer
	now   func() time.Time
}

func NewService(pool *pgxpool.Pool, kinds map[string]Kind, send Mailer) *Service {
	return &Service{pool: pool, kinds: kinds, send: send, now: time.Now}
}

// Activity is one thing live now.
type Activity struct {
	Kind         string    `json:"kind"`
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Status       string    `json:"status"`
	TeacherID    string    `json:"teacher_id"`
	TeacherName  string    `json:"teacher_name"`
	TeacherEmail string    `json:"teacher_email"`
	Classroom    string    `json:"classroom"`
	StartedAt    time.Time `json:"started_at"`
	// People: students in a quiz session, a poll's participants, and
	// those in a meeting or a live presentation now.
	People int `json:"people"`
}

// Each kind's live rows, with the teacher's name and address.
const liveSQL = `SELECT x.kind, x.id, x.title, x.status, x.teacher_id, coalesce(u.name, '') AS teacher_name,
	coalesce(u.email, '') AS teacher_email, coalesce(x.classroom, '') AS classroom, x.started_at, x.people
FROM (
	SELECT 'quiz_session' AS kind, s.id, s.title, s.status, s.teacher_id, c.name AS classroom, coalesce(s.started_at, s.created_at) AS started_at,
		(SELECT count(*) FROM live.attempts a WHERE a.session_id = s.id AND a.state IN ('waiting', 'admitted', 'in_progress', 'paused'))::int AS people
	FROM live.sessions s LEFT JOIN content.classrooms c ON c.id = s.classroom_id WHERE s.status <> 'ended'
	UNION ALL
	SELECT 'poll', p.id, p.title, p.status, p.teacher_id, c.name, coalesce(p.opened_at, p.created_at),
		(SELECT count(*) FROM poll.participants x WHERE x.poll_id = p.id)::int
	FROM poll.polls p LEFT JOIN content.classrooms c ON c.id = p.classroom_id WHERE p.status = 'open'
	UNION ALL
	SELECT 'meeting', m.id, m.title, m.status, m.host_id, c.name, m.created_at, 0
	FROM meet.meetings m LEFT JOIN content.classrooms c ON c.id = m.classroom_id WHERE m.status = 'open'
	UNION ALL
	SELECT 'presentation', p.id, p.title, 'live', p.teacher_id, c.name, l.started_at, 0
	FROM presentation.lives l JOIN presentation.presentations p ON p.id = l.presentation_id
		JOIN content.topics t ON t.id = p.topic_id JOIN content.modules mo ON mo.id = t.module_id
		JOIN content.classrooms c ON c.id = mo.classroom_id
	WHERE l.ended_at IS NULL
) x LEFT JOIN auth.users u ON u.id = x.teacher_id`

func (s *Service) query(ctx context.Context, where string, args ...any) ([]Activity, error) {
	rows, err := s.pool.Query(ctx, `SELECT * FROM (`+liveSQL+`) y `+where+` ORDER BY started_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Activity, error) {
		var a Activity
		err := r.Scan(&a.Kind, &a.ID, &a.Title, &a.Status, &a.TeacherID, &a.TeacherName, &a.TeacherEmail, &a.Classroom, &a.StartedAt, &a.People)
		return a, err
	})
	if err != nil {
		return nil, err
	}
	for i, a := range out {
		if k, ok := s.kinds[a.Kind]; ok && k.Online != nil {
			out[i].People = k.Online(a.ID)
		}
	}
	return out, nil
}

// Live lists everything live, newest first.
func (s *Service) Live(ctx context.Context) ([]Activity, error) {
	return s.query(ctx, "")
}

// End ends one activity for the admin, logs it with the reason and emails
// its teacher why.
func (s *Service) End(ctx context.Context, actorID, kind, id, reason string) error {
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 1 || n > 500 {
		return httpx.Invalid(map[string]string{"reason": "say why, in at most 500 characters: the teacher is told"})
	}
	k, ok := s.kinds[kind]
	if !ok {
		return httpx.ErrNotFound
	}
	list, err := s.query(ctx, "WHERE kind = $1 AND id::text = $2", kind, id)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return httpx.ErrNotFound
	}
	a := list[0]
	if err := k.End(ctx, actorID, id); err != nil {
		return err
	}
	if err := audit.Log(ctx, s.pool, actorID, "activity_force_ended", kind, id,
		map[string]string{"reason": reason, "teacher_id": a.TeacherID, "title": a.Title}); err != nil {
		return err
	}
	if a.TeacherEmail == "" || s.send == nil {
		return nil
	}
	return s.send(ctx, []mail.Message{{
		To:      a.TeacherEmail,
		Subject: fmt.Sprintf("Your %s \"%s\" was ended", kindLabels[kind], a.Title),
		Body: fmt.Sprintf("Hello %s,\n\nAn administrator ended your %s \"%s\" at %s.\n\nWhy: %s\n\nWhat was done until then is kept.\n",
			a.TeacherName, kindLabels[kind], a.Title, s.now().UTC().Format("2 Jan 2006 15:04 UTC"), reason),
	}})
}

// Pause stops new activities: for everyone when TeacherID is null.
type Pause struct {
	ID           string    `json:"id"`
	TeacherID    *string   `json:"teacher_id"`
	TeacherName  string    `json:"teacher_name,omitempty"`
	TeacherEmail string    `json:"teacher_email,omitempty"`
	Reason       string    `json:"reason"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Service) Pauses(ctx context.Context) ([]Pause, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id, p.teacher_id, coalesce(u.name, ''), coalesce(u.email, ''), p.reason, p.created_at
		FROM oversight.pauses p LEFT JOIN auth.users u ON u.id = p.teacher_id ORDER BY p.teacher_id NULLS FIRST, p.created_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Pause, error) {
		var p Pause
		err := r.Scan(&p.ID, &p.TeacherID, &p.TeacherName, &p.TeacherEmail, &p.Reason, &p.CreatedAt)
		return p, err
	})
}

// PauseInput names the teacher by id or email; neither pauses everyone.
type PauseInput struct {
	TeacherID    string `json:"teacher_id"`
	TeacherEmail string `json:"teacher_email"`
	Reason       string `json:"reason"`
}

// SetPause pauses new activities, or changes a pause's reason.
func (s *Service) SetPause(ctx context.Context, actorID string, in PauseInput) (Pause, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if len([]rune(in.Reason)) > 500 {
		return Pause{}, httpx.Invalid(map[string]string{"reason": "at most 500 characters"})
	}
	var teacher *string
	if in.TeacherID != "" || in.TeacherEmail != "" {
		var id string
		err := s.pool.QueryRow(ctx, `SELECT id FROM auth.users WHERE role IN ('teacher', 'admin') AND status <> 'deleted'
			AND (id::text = $1 OR email = lower($2))`, in.TeacherID, strings.TrimSpace(in.TeacherEmail)).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return Pause{}, httpx.Invalid(map[string]string{"teacher": "no teacher with that address"})
		}
		if err != nil {
			return Pause{}, err
		}
		teacher = &id
	}
	var id string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conflict := `(teacher_id) WHERE teacher_id IS NOT NULL`
		if teacher == nil {
			conflict = `((true)) WHERE teacher_id IS NULL`
		}
		if err := tx.QueryRow(ctx, `INSERT INTO oversight.pauses (teacher_id, reason, paused_by) VALUES ($1, $2, $3)
			ON CONFLICT `+conflict+` DO UPDATE SET reason = EXCLUDED.reason, paused_by = EXCLUDED.paused_by RETURNING id`,
			teacher, in.Reason, actorID).Scan(&id); err != nil {
			return err
		}
		target := "everyone"
		if teacher != nil {
			target = *teacher
		}
		return audit.Log(ctx, tx, actorID, "activities_paused", "pause", id, map[string]string{"teacher": target, "reason": in.Reason})
	})
	if err != nil {
		return Pause{}, err
	}
	list, err := s.Pauses(ctx)
	for _, p := range list {
		if p.ID == id {
			return p, err
		}
	}
	return Pause{}, err
}

// Resume lifts a pause.
func (s *Service) Resume(ctx context.Context, actorID, id string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM oversight.pauses WHERE id::text = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit.Log(ctx, tx, actorID, "activities_resumed", "pause", id, nil)
	})
}

// Paused gives the reason new activities are paused for a teacher, the
// teacher's own pause first; ok is false when they aren't.
func (s *Service) Paused(ctx context.Context, teacherID string) (reason string, ok bool, err error) {
	err = s.pool.QueryRow(ctx, `SELECT reason FROM oversight.pauses WHERE teacher_id IS NULL OR teacher_id::text = $1
		ORDER BY teacher_id NULLS LAST LIMIT 1`, teacherID).Scan(&reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return reason, err == nil, err
}

// Check refuses a teacher's new activity while they're paused: the modules
// call it before anything starts.
func (s *Service) Check(ctx context.Context, teacherID string) error {
	reason, paused, err := s.Paused(ctx, teacherID)
	if err != nil || !paused {
		return err
	}
	return httpx.NewError(http.StatusConflict, "activities_paused", PausedMessage(reason))
}

// PausedMessage is what a paused teacher is told.
func PausedMessage(reason string) string {
	msg := "the admin has paused new activities for now"
	if reason != "" {
		msg += ": " + reason
	}
	return msg
}
