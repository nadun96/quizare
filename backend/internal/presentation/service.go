// Package presentation runs slide decks students go through at their own
// pace (D-76, PR-FR-01 to PR-FR-34). A teacher uploads a PDF; their browser
// renders its pages to JPEG slides, which the platform stores and serves.
// For each student it records which slides they saw and for how long, for
// the teacher. It owns the "presentation" schema.
package presentation

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

const (
	StatusDraft     = "draft"
	StatusPublished = "published"

	MaxPDFBytes   = 50 << 20 // PR-FR-01
	MaxSlides     = 300
	MaxSlideBytes = 3 << 20
	maxSlideText  = 4000
)

// Topics finds a topic's classroom (content).
type Topics interface {
	TopicContext(ctx context.Context, teacherID, topicID string) (content.TopicContext, error)
	GetEnrolment(ctx context.Context, classroomID, userID string) (content.Enrolment, bool, error)
	ListEnrolments(ctx context.Context, teacherID, classroomID, status string) ([]content.Enrolment, error)
}

// Users names the students (auth).
type Users interface {
	UsersByID(ctx context.Context, ids []string) (map[string]auth.User, error)
}

type Service struct {
	pool    *pgxpool.Pool
	topics  Topics
	users   Users
	baseURL string
	hub     *liveHub
	marks   Marks
	now     func() time.Time
}

func NewService(pool *pgxpool.Pool, topics Topics, users Users, baseURL string) *Service {
	s := &Service{pool: pool, topics: topics, users: users, baseURL: strings.TrimRight(baseURL, "/"), now: time.Now}
	s.hub = newLiveHub(s)
	return s
}

// Presentation is a slide deck under a classroom topic.
type Presentation struct {
	ID            string     `json:"id"`
	TopicID       string     `json:"topic_id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Status        string     `json:"status"`
	AllowJump     bool       `json:"allow_jump"`
	AllowDownload bool       `json:"allow_download"`
	Version       int        `json:"version"`
	Slides        int        `json:"slide_count"`
	FileName      string     `json:"file_name,omitempty"`
	FileBytes     int        `json:"file_bytes"`
	OpensAt       *time.Time `json:"opens_at,omitempty"`
	DueAt         *time.Time `json:"due_at,omitempty"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

const cols = `p.id, p.topic_id, p.title, p.description, p.status, p.allow_jump, p.allow_download, p.version,
	coalesce(f.slides, 0), coalesce(f.name, ''), coalesce(f.bytes, 0), p.opens_at, p.due_at, p.published_at, p.created_at, p.updated_at`

const from = ` FROM presentation.presentations p LEFT JOIN presentation.files f ON f.presentation_id = p.id AND f.version = p.version`

func scan(r pgx.Row) (Presentation, error) {
	var p Presentation
	err := r.Scan(&p.ID, &p.TopicID, &p.Title, &p.Description, &p.Status, &p.AllowJump, &p.AllowDownload, &p.Version,
		&p.Slides, &p.FileName, &p.FileBytes, &p.OpensAt, &p.DueAt, &p.PublishedAt, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, httpx.ErrNotFound
	}
	return p, err
}

var (
	errNoSlides = httpx.NewError(http.StatusConflict, "presentation_empty", "upload a PDF before publishing")
	errArchived = httpx.Conflict("the classroom is archived")
)

func runes(s string) int { return utf8.RuneCountInString(s) }

func (s *Service) owned(ctx context.Context, teacherID, id string) (Presentation, error) {
	return scan(s.pool.QueryRow(ctx, `SELECT `+cols+from+` WHERE p.id=$1 AND p.teacher_id=$2`, id, teacherID))
}

// Get returns one of the teacher's presentations.
func (s *Service) Get(ctx context.Context, teacherID, id string) (Presentation, error) {
	return s.owned(ctx, teacherID, id)
}

// Input creates or changes a presentation's details and settings.
type Input struct {
	Title         *string    `json:"title"`
	Description   *string    `json:"description"`
	AllowJump     *bool      `json:"allow_jump"`
	AllowDownload *bool      `json:"allow_download"`
	OpensAt       *time.Time `json:"opens_at"`
	DueAt         *time.Time `json:"due_at"`
	ClearDates    bool       `json:"clear_dates"`
}

// Create makes a draft presentation, without a file yet.
func (s *Service) Create(ctx context.Context, teacherID, topicID string, in Input) (Presentation, error) {
	tc, err := s.topics.TopicContext(ctx, teacherID, topicID)
	if err != nil {
		return Presentation{}, err
	}
	if tc.ClassroomArchived {
		return Presentation{}, errArchived
	}
	title := ""
	if in.Title != nil {
		title = strings.Join(strings.Fields(*in.Title), " ")
	}
	if n := runes(title); n < 1 || n > 200 {
		return Presentation{}, httpx.Invalid(map[string]string{"title": "give the presentation a title (at most 200 characters)"})
	}
	desc := ""
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	var id string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO presentation.presentations(topic_id, teacher_id, title, description) VALUES ($1,$2,$3,$4) RETURNING id`,
			topicID, teacherID, title, desc).Scan(&id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "presentation_created", "presentation", id, nil)
	})
	if err != nil {
		return Presentation{}, err
	}
	return s.owned(ctx, teacherID, id)
}

// List returns a topic's presentations.
func (s *Service) List(ctx context.Context, teacherID, topicID string) ([]Presentation, error) {
	if _, err := s.topics.TopicContext(ctx, teacherID, topicID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+cols+from+` WHERE p.topic_id=$1 AND p.teacher_id=$2 ORDER BY p.created_at`, topicID, teacherID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Presentation, error) { return scan(r) })
	if out == nil {
		out = []Presentation{}
	}
	return out, err
}

// Update changes a presentation's details and settings.
func (s *Service) Update(ctx context.Context, teacherID, id string, in Input) (Presentation, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return p, err
	}
	f := map[string]string{}
	if in.Title != nil {
		p.Title = strings.Join(strings.Fields(*in.Title), " ")
		if n := runes(p.Title); n < 1 || n > 200 {
			f["title"] = "give the presentation a title (at most 200 characters)"
		}
	}
	if in.Description != nil {
		p.Description = strings.TrimSpace(*in.Description)
		if runes(p.Description) > 5000 {
			f["description"] = "at most 5000 characters"
		}
	}
	if in.AllowJump != nil {
		p.AllowJump = *in.AllowJump
	}
	if in.AllowDownload != nil {
		p.AllowDownload = *in.AllowDownload
	}
	if in.ClearDates {
		p.OpensAt, p.DueAt = nil, nil
	}
	if in.OpensAt != nil {
		p.OpensAt = in.OpensAt
	}
	if in.DueAt != nil {
		p.DueAt = in.DueAt
	}
	if p.OpensAt != nil && p.DueAt != nil && !p.DueAt.After(*p.OpensAt) {
		f["due_at"] = "the end date comes after the opening date"
	}
	if len(f) > 0 {
		return p, httpx.Invalid(f)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE presentation.presentations SET title=$2, description=$3, allow_jump=$4, allow_download=$5, opens_at=$6, due_at=$7, updated_at=now() WHERE id=$1`,
		id, p.Title, p.Description, p.AllowJump, p.AllowDownload, p.OpensAt, p.DueAt); err != nil {
		return p, err
	}
	return s.owned(ctx, teacherID, id)
}

// SetStatus publishes a presentation to its classroom, or takes it back to a draft.
func (s *Service) SetStatus(ctx context.Context, teacherID, id, status string) (Presentation, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return p, err
	}
	switch status {
	case StatusPublished:
		if p.Slides == 0 {
			return p, errNoSlides
		}
		tc, err := s.topics.TopicContext(ctx, teacherID, p.TopicID)
		if err != nil {
			return p, err
		}
		if tc.ClassroomArchived {
			return p, errArchived
		}
	case StatusDraft:
	default:
		return p, httpx.Invalid(map[string]string{"status": "draft or published"})
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE presentation.presentations SET status=$2, published_at=CASE WHEN $2='published' THEN coalesce(published_at, now()) ELSE published_at END, updated_at=now() WHERE id=$1`, id, status); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "presentation_"+status, "presentation", id, nil)
	})
	if err != nil {
		return p, err
	}
	return s.owned(ctx, teacherID, id)
}

// Delete removes a presentation, its file, slides and view records (PR-NFR-04).
func (s *Service) Delete(ctx context.Context, teacherID, id string) error {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM presentation.presentations WHERE id=$1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "presentation_deleted", "presentation", id, map[string]string{"title": p.Title})
	})
	if err == nil {
		s.hub.end(id)
	}
	return err
}
