package presentation

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Presenting live (D-77, PR-FR-10 to PR-FR-16). The teacher starts it, gets a
// code, and moves the slides; students who join follow over a WebSocket
// (livehub.go), and may look back on their own if the teacher allows, but
// never ahead of what's been shown. Ending keeps the time records.

var errNoLive = httpx.NewError(http.StatusNotFound, "not_live", "this presentation isn't being presented now")

// Live is a presentation being presented.
type Live struct {
	ID             string     `json:"id"`
	PresentationID string     `json:"presentation_id"`
	Code           string     `json:"code"`
	Slide          int        `json:"slide"`
	MaxShown       int        `json:"max_shown"`
	AllowBrowse    bool       `json:"allow_browse"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
}

const liveCols = `id, presentation_id, code, slide, max_shown, allow_browse, started_at, ended_at`

func scanLive(r pgx.Row) (Live, error) {
	var l Live
	err := r.Scan(&l.ID, &l.PresentationID, &l.Code, &l.Slide, &l.MaxShown, &l.AllowBrowse, &l.StartedAt, &l.EndedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, errNoLive
	}
	return l, err
}

// openLive is the presentation's live session, if one is open.
func (s *Service) openLive(ctx context.Context, id string) (*Live, error) {
	l, err := scanLive(s.pool.QueryRow(ctx, `SELECT `+liveCols+` FROM presentation.lives WHERE presentation_id=$1 AND ended_at IS NULL`, id))
	if errors.Is(err, errNoLive) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newCode() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b)
}

// StartLive starts presenting, or returns the session already open.
func (s *Service) StartLive(ctx context.Context, teacherID, id string) (Live, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return Live{}, err
	}
	if p.Slides == 0 {
		return Live{}, errNoSlides
	}
	if l, err := s.openLive(ctx, id); err != nil || l != nil {
		if l != nil {
			return *l, nil
		}
		return Live{}, err
	}
	if err := s.gate.Check(ctx, teacherID); err != nil {
		return Live{}, err
	}
	tc, err := s.topics.TopicContext(ctx, teacherID, p.TopicID)
	if err != nil {
		return Live{}, err
	}
	if tc.ClassroomArchived {
		return Live{}, errArchived
	}
	var l Live
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		for range 5 { // a clash of codes is rare; try another
			l, err = scanLive(tx.QueryRow(ctx, `INSERT INTO presentation.lives(presentation_id, code, started_by) VALUES ($1,$2,$3)
				ON CONFLICT DO NOTHING RETURNING `+liveCols, id, newCode(), teacherID))
			if !errors.Is(err, errNoLive) {
				break
			}
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "presentation_live_started", "presentation", id, nil)
	})
	if errors.Is(err, errNoLive) { // started at the same moment elsewhere
		if l, err := s.openLive(ctx, id); l != nil {
			return *l, err
		}
	}
	return l, err
}

// CurrentLive returns the open session, for its teacher.
func (s *Service) CurrentLive(ctx context.Context, teacherID, id string) (Live, error) {
	if _, err := s.owned(ctx, teacherID, id); err != nil {
		return Live{}, err
	}
	l, err := s.openLive(ctx, id)
	if err != nil {
		return Live{}, err
	}
	if l == nil {
		return Live{}, errNoLive
	}
	return *l, nil
}

// ShowSlide moves everyone following to slide n.
func (s *Service) ShowSlide(ctx context.Context, teacherID, id string, n int) (Live, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return Live{}, err
	}
	if n < 1 || n > p.Slides {
		return Live{}, httpx.Invalid(map[string]string{"slide": "a slide of this presentation"})
	}
	l, err := scanLive(s.pool.QueryRow(ctx, `UPDATE presentation.lives SET slide=$2, max_shown=GREATEST(max_shown, $2)
		WHERE presentation_id=$1 AND ended_at IS NULL RETURNING `+liveCols, id, n))
	if err != nil {
		return l, err
	}
	s.hub.update(l)
	return l, nil
}

// LiveInput changes a live session's settings.
type LiveInput struct {
	AllowBrowse *bool `json:"allow_browse"`
}

// SetLive changes whether students may look back on their own.
func (s *Service) SetLive(ctx context.Context, teacherID, id string, in LiveInput) (Live, error) {
	l, err := s.CurrentLive(ctx, teacherID, id)
	if err != nil {
		return l, err
	}
	if in.AllowBrowse != nil {
		if l, err = scanLive(s.pool.QueryRow(ctx, `UPDATE presentation.lives SET allow_browse=$2 WHERE id=$1 RETURNING `+liveCols, l.ID, *in.AllowBrowse)); err != nil {
			return l, err
		}
	}
	s.hub.update(l)
	return l, nil
}

// EndLive ends presenting. Time records stay (PR-FR-16).
func (s *Service) EndLive(ctx context.Context, teacherID, id string) error {
	l, err := s.CurrentLive(ctx, teacherID, id)
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE presentation.lives SET ended_at=now() WHERE id=$1`, l.ID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "presentation_live_ended", "presentation", id, nil)
	})
	if err == nil {
		s.hub.end(id)
	}
	return err
}

// access finds a presentation a student may see now: they're actively
// enrolled, and it's published and open, or being presented live.
func (s *Service) access(ctx context.Context, userID, id string) (Presentation, *Live, error) {
	p, err := scan(s.pool.QueryRow(ctx, `SELECT `+cols+from+` WHERE p.id=$1 AND p.version > 0`, id))
	if err != nil {
		return p, nil, err
	}
	tc, err := s.topics.TopicContext(ctx, "", p.TopicID)
	if err != nil {
		return p, nil, err
	}
	e, ok, err := s.topics.GetEnrolment(ctx, tc.ClassroomID, userID)
	if err != nil {
		return p, nil, err
	}
	if !ok || e.Status != "active" || tc.ClassroomArchived {
		return p, nil, httpx.ErrNotFound
	}
	l, err := s.openLive(ctx, id)
	if err != nil {
		return p, nil, err
	}
	if l == nil && p.Status != StatusPublished {
		return p, nil, httpx.ErrNotFound
	}
	return p, l, nil
}

// LiveView is what a student's live viewer needs.
type LiveView struct {
	Presentation struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Version   int    `json:"version"`
		Published bool   `json:"published"` // so it can be seen at one's own pace afterwards
	} `json:"presentation"`
	Slides []Slide `json:"slides"`
	Live   Live    `json:"live"`
}

// JoinLive is a student joining the live presentation.
func (s *Service) JoinLive(ctx context.Context, userID, id string) (LiveView, error) {
	p, l, err := s.access(ctx, userID, id)
	if err != nil {
		return LiveView{}, err
	}
	if l == nil {
		return LiveView{}, errNoLive
	}
	if _, err := s.viewer(ctx, p, userID, true); err != nil {
		return LiveView{}, err
	}
	var v LiveView
	v.Presentation.ID, v.Presentation.Title, v.Presentation.Version, v.Presentation.Published = p.ID, p.Title, p.Version, p.Status == StatusPublished
	v.Live = *l
	v.Slides, err = s.slides(ctx, id, p.Version)
	return v, err
}

// ByCode finds the presentation being presented with a code, for one of its students.
func (s *Service) ByCode(ctx context.Context, userID, code string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT presentation_id FROM presentation.lives WHERE code=$1 AND ended_at IS NULL`,
		strings.ToUpper(strings.TrimSpace(code))).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errNoLive
	}
	if err != nil {
		return "", err
	}
	if _, _, err := s.access(ctx, userID, id); err != nil {
		return "", err
	}
	return id, nil
}
