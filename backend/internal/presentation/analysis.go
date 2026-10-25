package presentation

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Time on slides against marks (D-78, PR-FR-32, PR-FR-33). The teacher links
// a presentation to quizzes and tutorials of its topic; the analysis sets
// each student's time on it beside their score on those, overall and slide
// by slide. It shows what went together, not what caused what.

// Linkable is a quiz or tutorial a presentation can be set beside.
type Linkable struct {
	Kind  string `json:"kind"` // quiz or tutorial
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// Marks finds what can be linked, and students' scores on it (quiz, eval, tutorial).
type Marks interface {
	Linkable(ctx context.Context, teacherID, topicID string) ([]Linkable, error)
	Scores(ctx context.Context, quizIDs, tutorialIDs []string) (map[string][2]float64, error)
}

func (s *Service) SetMarks(m Marks) { s.marks = m }

// Links is what a presentation is linked to, and what it could be.
type Links struct {
	Linked  []Linkable `json:"linked"`
	Options []Linkable `json:"options"`
}

func (s *Service) links(ctx context.Context, teacherID string, p Presentation) (Links, error) {
	out := Links{Linked: []Linkable{}, Options: []Linkable{}}
	if s.marks == nil {
		return out, nil
	}
	opts, err := s.marks.Linkable(ctx, teacherID, p.TopicID)
	if err != nil {
		return out, err
	}
	out.Options = append(out.Options, opts...)
	rows, err := s.pool.Query(ctx, `SELECT kind, target_id FROM presentation.links WHERE presentation_id=$1`, p.ID)
	if err != nil {
		return out, err
	}
	linked, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Linkable, error) {
		var l Linkable
		return l, r.Scan(&l.Kind, &l.ID)
	})
	if err != nil {
		return out, err
	}
	// Titles from the options; a quiz or tutorial deleted since drops off.
	for _, l := range linked {
		for _, o := range opts {
			if o.Kind == l.Kind && o.ID == l.ID {
				out.Linked = append(out.Linked, o)
			}
		}
	}
	return out, nil
}

// Links returns a presentation's links and the topic's quizzes and tutorials.
func (s *Service) Links(ctx context.Context, teacherID, id string) (Links, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return Links{}, err
	}
	return s.links(ctx, teacherID, p)
}

// SetLinks sets which of the topic's quizzes and tutorials it goes with.
func (s *Service) SetLinks(ctx context.Context, teacherID, id string, in []Linkable) (Links, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return Links{}, err
	}
	cur, err := s.links(ctx, teacherID, p)
	if err != nil {
		return Links{}, err
	}
	ok := map[string]bool{}
	for _, o := range cur.Options {
		ok[o.Kind+":"+o.ID] = true
	}
	for _, l := range in {
		if !ok[l.Kind+":"+l.ID] {
			return Links{}, httpx.Invalid(map[string]string{"links": "link quizzes and tutorials of this presentation's topic"})
		}
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM presentation.links WHERE presentation_id=$1`, id); err != nil {
			return err
		}
		for _, l := range in {
			if _, err := tx.Exec(ctx, `INSERT INTO presentation.links(presentation_id, kind, target_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, id, l.Kind, l.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Links{}, err
	}
	return s.links(ctx, teacherID, p)
}

// Point is one student: time on the presentation, and score.
type Point struct {
	UserID   string  `json:"user_id"`
	Name     string  `json:"name"`
	Seconds  int     `json:"seconds"` // own pace and live together
	Seen     int     `json:"seen"`
	ScorePct float64 `json:"score_pct"`
}

// Trend is the straight line through the points, and how closely they follow it.
type Trend struct {
	PerTenMinutes float64 `json:"per_ten_minutes"` // score points per ten more minutes
	Intercept     float64 `json:"intercept"`       // the score at no time
	R             float64 `json:"r"`               // correlation, -1 to 1
}

// SlideSplit compares, for one slide, students who spent at least the
// median time on it with those who spent less (or skipped it).
type SlideSplit struct {
	N             int      `json:"n"`
	Text          string   `json:"text"`
	MedianSeconds int      `json:"median_seconds"`
	LongerN       int      `json:"longer_n"`
	Longer        *float64 `json:"longer_pct,omitempty"`
	ShorterN      int      `json:"shorter_n"`
	Shorter       *float64 `json:"shorter_pct,omitempty"`
	Skipped       int      `json:"skipped"`
}

// Analysis is the presentation's time set beside the linked marks.
type Analysis struct {
	Linked    []Linkable   `json:"linked"`
	Points    []Point      `json:"points"`
	NoScore   int          `json:"no_score"`   // viewed, no marks yet
	NotViewed int          `json:"not_viewed"` // marks, never opened it
	Trend     *Trend       `json:"trend,omitempty"`
	Slides    []SlideSplit `json:"slides"`
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

// csvSafe neutralises spreadsheet formula injection (ADR-16).
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// fit is least squares, y on x, with Pearson's r.
func fit(xs, ys []float64) *Trend {
	n := float64(len(xs))
	if n < 3 {
		return nil
	}
	var mx, my float64
	for i := range xs {
		mx += xs[i]
		my += ys[i]
	}
	mx, my = mx/n, my/n
	var sxy, sxx, syy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 {
		return nil
	}
	slope := sxy / sxx
	t := &Trend{PerTenMinutes: round1(slope * 600), Intercept: round1(my - slope*mx)}
	if syy > 0 {
		t.R = math.Round(sxy/math.Sqrt(sxx*syy)*100) / 100
	}
	return t
}

type studentTime struct {
	total   int
	seen    int
	perN    map[int]int
	name    string
	number  string
	email   string
	live    int
	started bool
}

// times gathers every student's time per slide, own pace and live together.
func (s *Service) times(ctx context.Context, teacherID string, p Presentation) (map[string]*studentTime, []string, error) {
	rep, err := s.Report(ctx, teacherID, p.ID)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]*studentTime{}
	order := []string{}
	for _, st := range rep.Students {
		num := ""
		if st.StudentNumber != nil {
			num = *st.StudentNumber
		}
		out[st.UserID] = &studentTime{name: st.Name, number: num, email: st.Email, perN: map[int]int{}, live: st.LiveSeconds, started: st.Started}
		order = append(order, st.UserID)
	}
	rows, err := s.pool.Query(ctx, `SELECT v.user_id, w.n, sum(w.seconds) FROM presentation.views w JOIN presentation.viewers v ON v.id = w.viewer_id
		WHERE v.presentation_id=$1 AND w.n BETWEEN 1 AND $2 GROUP BY v.user_id, w.n`, p.ID, p.Slides)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var user string
		var n, secs int
		if err := rows.Scan(&user, &n, &secs); err != nil {
			return nil, nil, err
		}
		if st := out[user]; st != nil {
			st.perN[n] = secs
			st.total += secs
			st.seen++
		}
	}
	return out, order, rows.Err()
}

// Analysis sets time on the presentation beside the linked marks.
func (s *Service) Analysis(ctx context.Context, teacherID, id string) (Analysis, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return Analysis{}, err
	}
	links, err := s.links(ctx, teacherID, p)
	if err != nil {
		return Analysis{}, err
	}
	out := Analysis{Linked: links.Linked, Points: []Point{}, Slides: []SlideSplit{}}
	if len(links.Linked) == 0 || s.marks == nil {
		return out, nil
	}
	var quizzes, tutorials []string
	for _, l := range links.Linked {
		if l.Kind == "quiz" {
			quizzes = append(quizzes, l.ID)
		} else {
			tutorials = append(tutorials, l.ID)
		}
	}
	scores, err := s.marks.Scores(ctx, quizzes, tutorials)
	if err != nil {
		return out, err
	}
	times, order, err := s.times(ctx, teacherID, p)
	if err != nil {
		return out, err
	}
	var xs, ys []float64
	for _, user := range order {
		st := times[user]
		sc, scored := scores[user]
		viewed := st.total > 0
		switch {
		case viewed && !scored:
			out.NoScore++
		case !viewed && scored:
			out.NotViewed++
		}
		if !viewed || !scored {
			continue
		}
		pct := round1(sc[0] / sc[1] * 100)
		out.Points = append(out.Points, Point{UserID: user, Name: st.name, Seconds: st.total, Seen: st.seen, ScorePct: pct})
		xs, ys = append(xs, float64(st.total)), append(ys, pct)
	}
	out.Trend = fit(xs, ys)

	slides, err := s.slides(ctx, id, p.Version)
	if err != nil {
		return out, err
	}
	for _, sl := range slides {
		sp := SlideSplit{N: sl.N, Text: sl.Text}
		var viewed []int
		for _, pt := range out.Points {
			if secs := times[pt.UserID].perN[sl.N]; secs > 0 {
				viewed = append(viewed, secs)
			}
		}
		sp.MedianSeconds = median(viewed)
		var longer, shorter float64
		for _, pt := range out.Points {
			secs := times[pt.UserID].perN[sl.N]
			if secs == 0 {
				sp.Skipped++
			}
			if secs > 0 && secs >= sp.MedianSeconds {
				sp.LongerN++
				longer += pt.ScorePct
			} else {
				sp.ShorterN++
				shorter += pt.ScorePct
			}
		}
		if sp.LongerN > 0 {
			v := round1(longer / float64(sp.LongerN))
			sp.Longer = &v
		}
		if sp.ShorterN > 0 {
			v := round1(shorter / float64(sp.ShorterN))
			sp.Shorter = &v
		}
		out.Slides = append(out.Slides, sp)
	}
	return out, nil
}

// CSV is the viewing records, one row per student, with the linked score
// when there is one (PR-FR-33). The name is for a file or a Google Sheet.
func (s *Service) CSV(ctx context.Context, teacherID, id string) (string, []byte, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return "", nil, err
	}
	times, order, err := s.times(ctx, teacherID, p)
	if err != nil {
		return "", nil, err
	}
	var scores map[string][2]float64
	links, err := s.links(ctx, teacherID, p)
	if err != nil {
		return "", nil, err
	}
	if len(links.Linked) > 0 {
		var quizzes, tutorials []string
		for _, l := range links.Linked {
			if l.Kind == "quiz" {
				quizzes = append(quizzes, l.ID)
			} else {
				tutorials = append(tutorials, l.ID)
			}
		}
		if scores, err = s.marks.Scores(ctx, quizzes, tutorials); err != nil {
			return "", nil, err
		}
	}
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	head := []string{"student", "student_number", "email", "slides_seen", "seconds_total", "seconds_live"}
	if scores != nil {
		head = append(head, "linked_score_pct")
	}
	for n := 1; n <= p.Slides; n++ {
		head = append(head, fmt.Sprintf("slide_%d_seconds", n))
	}
	_ = w.Write(head)
	for _, user := range order {
		st := times[user]
		row := []string{csvSafe(st.name), csvSafe(st.number), csvSafe(st.email), strconv.Itoa(st.seen), strconv.Itoa(st.total), strconv.Itoa(st.live)}
		if scores != nil {
			if sc, ok := scores[user]; ok {
				row = append(row, strconv.FormatFloat(round1(sc[0]/sc[1]*100), 'f', 1, 64))
			} else {
				row = append(row, "")
			}
		}
		for n := 1; n <= p.Slides; n++ {
			row = append(row, strconv.Itoa(st.perN[n]))
		}
		_ = w.Write(row)
	}
	w.Flush()
	return p.Title + " – viewing", b.Bytes(), w.Error()
}
