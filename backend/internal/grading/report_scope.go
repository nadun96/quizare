package grading

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Reports of part of a classroom (D-160): the class report of one module,
// topic, item (quiz, tutorial, poll or offline assignment), or one run of a
// quiz. Its totals and group marks are of what it covers; its setup
// (weights, which mark is used) stays the whole report's.

// ReportScope is what a report covers.
type ReportScope struct {
	Kind  string `json:"kind"`  // all, module, topic, item or session
	ID    string `json:"id"`    // the module, topic or session; an item's kind:id
	Label string `json:"label"` // its name, for headings and files
}

// SessionID is the quiz session a report is of, or "".
func (sc ReportScope) SessionID() string {
	if sc.Kind == "session" {
		return sc.ID
	}
	return ""
}

// ReportSession is one run of a quiz in the classroom.
type ReportSession struct {
	ID        string     `json:"id"`
	QuizID    string     `json:"quiz_id"`
	Title     string     `json:"title"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Students  int        `json:"students"` // who took part
}

var errScope = httpx.Invalid(map[string]string{"scope": "this isn't part of this classroom's report"})

// scopeReport lists the classroom's quiz sessions and keeps only the items
// the scope covers. An item or session asked for by name is shown even when
// the report leaves it out.
func (s *Service) scopeReport(ctx context.Context, classroomID, scope string, rep *ClassReport) error {
	rows, err := s.pool.Query(ctx, `SELECT se.id::text, se.quiz_id::text, se.title, coalesce(se.started_at, se.created_at), se.ended_at,
			(SELECT count(DISTINCT a.user_id)::int FROM live.attempts a WHERE a.session_id = se.id)
		FROM live.sessions se WHERE se.classroom_id = $1 ORDER BY coalesce(se.started_at, se.created_at) DESC, se.id LIMIT 500`, classroomID)
	if err != nil {
		return err
	}
	if rep.Sessions, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ReportSession, error) {
		var x ReportSession
		return x, r.Scan(&x.ID, &x.QuizID, &x.Title, &x.StartedAt, &x.EndedAt, &x.Students)
	}); err != nil {
		return err
	}
	rep.Scope = ReportScope{Kind: "all", Label: rep.Classroom}
	if scope == "" || scope == "all" {
		return nil
	}
	kind, id, _ := strings.Cut(scope, ":")
	keep := func(it ReportItem) bool { return false }
	switch kind {
	case "module", "topic":
		for _, it := range rep.Items {
			if (kind == "module" && it.ModuleID == id) || (kind == "topic" && it.TopicID == id) {
				rep.Scope = ReportScope{Kind: kind, ID: id, Label: it.Module}
				if kind == "topic" {
					rep.Scope.Label = it.Topic
				}
				break
			}
		}
		keep = func(it ReportItem) bool {
			return (kind == "module" && it.ModuleID == id) || (kind == "topic" && it.TopicID == id)
		}
	case "item":
		for _, it := range rep.Items {
			if Key(it.Kind, it.ID) == id {
				rep.Scope = ReportScope{Kind: kind, ID: id, Label: it.Title}
			}
		}
		keep = func(it ReportItem) bool { return Key(it.Kind, it.ID) == id }
	case "session":
		var quiz string
		for _, x := range rep.Sessions {
			if x.ID == id {
				quiz = x.QuizID
				rep.Scope = ReportScope{Kind: kind, ID: id, Label: x.Title + ", " + x.StartedAt.Format("2 Jan 2006 15:04")}
			}
		}
		keep = func(it ReportItem) bool { return quiz != "" && Key(it.Kind, it.ID) == Key("quiz", quiz) }
	default:
		return errScope
	}
	if rep.Scope.Kind == "all" {
		return errScope
	}
	kept := rep.Items[:0]
	for _, it := range rep.Items {
		if keep(it) {
			if kind == "item" || kind == "session" {
				it.Included = true
			}
			kept = append(kept, it)
		}
	}
	rep.Items = kept
	if len(rep.Items) == 0 {
		return errScope
	}
	return nil
}
