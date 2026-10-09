package analytics

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

type Sessions interface {
	SessionInfo(ctx context.Context, teacherID, sessionID string) (live.SessionInfo, error)
	SessionIDsForQuiz(ctx context.Context, teacherID, quizID string) ([]string, error)
	SessionMarkingData(ctx context.Context, teacherID, sessionID string) ([]*live.MarkingData, error)
	AttemptCounts(ctx context.Context, sessionID string) (map[string]int, error)
	PauseCounts(ctx context.Context, sessionID string) (map[string]int, error)
}

type Results interface {
	SessionResults(ctx context.Context, teacherID, sessionID string) ([]eval.AttemptResult, error)
	PublicTeamStandings(ctx context.Context, sessionID string) ([]eval.TeamStanding, error)
	PublicRanking(ctx context.Context, sessionID string, studentIDs bool, limit int) ([]eval.RankedStudent, int, int, error)
}

type Users interface {
	UsersByID(ctx context.Context, ids []string) (map[string]auth.User, error)
}

type Service struct {
	pool     *pgxpool.Pool
	sessions Sessions
	results  Results
	users    Users
	jobs     jobs.Inserter
	now      func() time.Time
	polls    Polls
	live     liveCacheT
}

func NewService(pool *pgxpool.Pool, s Sessions, r Results, u Users, inserter jobs.Inserter) *Service {
	return &Service{pool: pool, sessions: s, results: r, users: u, jobs: inserter, now: time.Now}
}

// ---------- precomputation (ADR-15) ----------

type RecomputeArgs struct {
	SessionID string `json:"session_id"`
}

func (RecomputeArgs) Kind() string { return "analytics_recompute" }
func (RecomputeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueAnalytics, MaxAttempts: 5}
}

type Worker struct {
	river.WorkerDefaults[RecomputeArgs]
	Service *Service
}

// Work debounces: a burst of result changes queues many jobs, but every job
// created before the last computation is skipped with one cheap read.
func (w *Worker) Work(ctx context.Context, job *river.Job[RecomputeArgs]) error {
	var at time.Time
	err := w.Service.pool.QueryRow(ctx, `SELECT computed_at FROM analytics.session_stats WHERE session_id=$1`, job.Args.SessionID).Scan(&at)
	if err == nil && !at.Before(job.CreatedAt) {
		return nil
	}
	_, err = w.Service.Recompute(ctx, job.Args.SessionID)
	return err
}

// OnResults is the eval hook: queue a recomputation in the same transaction.
func (s *Service) OnResults(ctx context.Context, tx pgx.Tx, sessionID string) error {
	_, err := s.jobs.InsertTx(ctx, tx, RecomputeArgs{SessionID: sessionID}, nil)
	return err
}

// Recompute builds and stores a session's aggregates.
func (s *Service) Recompute(ctx context.Context, sessionID string) (SessionStats, error) {
	info, err := s.sessions.SessionInfo(ctx, "", sessionID)
	if err != nil {
		return SessionStats{}, err
	}
	results, err := s.results.SessionResults(ctx, info.TeacherID, sessionID)
	if err != nil {
		return SessionStats{}, err
	}
	all, err := s.sessions.SessionMarkingData(ctx, info.TeacherID, sessionID)
	if err != nil {
		return SessionStats{}, err
	}
	in := input{info: info, results: results, data: map[string]*live.MarkingData{}}
	ids := make([]string, 0, len(results))
	for _, d := range all {
		in.data[d.AttemptID] = d
	}
	for _, r := range results {
		ids = append(ids, r.UserID)
	}
	users, err := s.users.UsersByID(ctx, ids)
	if err != nil {
		return SessionStats{}, err
	}
	in.names = map[string]string{}
	for id, u := range users {
		in.names[id] = u.Name
	}
	if in.counts, err = s.sessions.AttemptCounts(ctx, sessionID); err != nil {
		return SessionStats{}, err
	}
	if in.pauses, err = s.sessions.PauseCounts(ctx, sessionID); err != nil {
		return SessionStats{}, err
	}
	st := compute(in, s.now())
	raw, _ := json.Marshal(st)
	_, err = s.pool.Exec(ctx, `INSERT INTO analytics.session_stats(session_id, quiz_id, teacher_id, data, computed_at) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (session_id) DO UPDATE SET data=EXCLUDED.data, computed_at=EXCLUDED.computed_at`,
		sessionID, info.QuizID, info.TeacherID, raw, st.ComputedAt)
	return st, err
}

// stats returns stored aggregates, computing them on first use.
func (s *Service) stats(ctx context.Context, sessionID string) (SessionStats, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT data FROM analytics.session_stats WHERE session_id=$1`, sessionID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.Recompute(ctx, sessionID)
	}
	if err != nil {
		return SessionStats{}, err
	}
	var st SessionStats
	return st, json.Unmarshal(raw, &st)
}

// SessionStats is the teacher's per-session analytics (FR-RS-03).
func (s *Service) SessionStats(ctx context.Context, teacherID, sessionID string) (SessionStats, error) {
	if _, err := s.sessions.SessionInfo(ctx, teacherID, sessionID); err != nil {
		return SessionStats{}, err
	}
	return s.stats(ctx, sessionID)
}

// SessionSummary is one line of the session comparison (BA §11).
type SessionSummary struct {
	SessionID string     `json:"session_id"`
	Title     string     `json:"title"`
	CreatedAt time.Time  `json:"created_at"`
	Class     ClassStats `json:"class"`
}

type QuizStats struct {
	QuizID     string           `json:"quiz_id"`
	QuizTitle  string           `json:"quiz_title"`
	Class      ClassStats       `json:"class"`
	Questions  []QuestionRow    `json:"questions"`
	Students   []StudentRow     `json:"students"`
	Comparison []SessionSummary `json:"comparison"`
}

// QuizStats rolls up every session of a quiz (BA §11 "per quiz (across sessions)").
func (s *Service) QuizStats(ctx context.Context, teacherID, quizID string) (QuizStats, error) {
	ids, err := s.sessions.SessionIDsForQuiz(ctx, teacherID, quizID)
	if err != nil {
		return QuizStats{}, err
	}
	qs := QuizStats{QuizID: quizID, Comparison: []SessionSummary{}, Students: []StudentRow{}}
	byQ := map[string]*QuestionRow{}
	var order []string
	var pcts []float64
	passed := 0
	c := ClassStats{Distribution: make([]int, 10)}
	for _, id := range ids {
		st, err := s.stats(ctx, id)
		if err != nil {
			return QuizStats{}, err
		}
		qs.QuizTitle = st.QuizTitle
		qs.Comparison = append(qs.Comparison, SessionSummary{SessionID: id, Title: st.Title, CreatedAt: st.CreatedAt, Class: st.Class})
		qs.Students = append(qs.Students, st.Students...)
		c.Joined += st.Class.Joined
		c.Finished += st.Class.Finished
		c.Pending += st.Class.Pending
		c.PassMarkPct = st.Class.PassMarkPct
		for i, n := range st.Class.Distribution {
			c.Distribution[i] += n
		}
		for _, r := range st.Students {
			if r.State != live.StateInvalidated {
				pcts = append(pcts, r.Pct)
				if r.Passed {
					passed++
				}
			}
		}
		for _, q := range st.Questions {
			agg, ok := byQ[q.Code]
			if !ok {
				cp := q
				cp.Discrimination, cp.CommonWrong = nil, nil
				cp.PctCorrect = q.PctCorrect * float64(q.Answered)
				cp.AvgScore = q.AvgScore * float64(q.Answered)
				byQ[q.Code] = &cp
				order = append(order, q.Code)
				continue
			}
			agg.PctCorrect += q.PctCorrect * float64(q.Answered)
			agg.AvgScore += q.AvgScore * float64(q.Answered)
			agg.Answered += q.Answered
			for k, v := range q.OptionCounts {
				agg.OptionCounts[k] += v
			}
		}
	}
	for _, code := range order {
		q := byQ[code]
		if q.Answered > 0 {
			q.PctCorrect, q.AvgScore = round(q.PctCorrect/float64(q.Answered)), round(q.AvgScore/float64(q.Answered))
		}
		qs.Questions = append(qs.Questions, *q)
	}
	c.Marked = len(pcts)
	if len(pcts) > 0 {
		sum := 0.0
		for _, p := range pcts {
			sum += p
		}
		c.Mean = round(sum / float64(len(pcts)))
		sort.Float64s(pcts)
		if n := len(pcts); n%2 == 1 {
			c.Median = pcts[n/2]
		} else {
			c.Median = round((pcts[n/2-1] + pcts[n/2]) / 2)
		}
		c.PassRate = round(float64(passed) / float64(len(pcts)) * 100)
	}
	qs.Class = c
	return qs, nil
}

// ---------- CSV export (FR-RS-05) ----------

// csvSafe neutralises spreadsheet formula injection (ADR-16).
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func (s *Service) ExportSessionCSV(ctx context.Context, teacherID, sessionID string) ([]byte, error) {
	st, err := s.SessionStats(ctx, teacherID, sessionID)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	header := []string{"student_number", "name", "state", "score", "max_score", "pct", "passed", "time_taken_sec", "violations", "extension_sec", "pauses"}
	for _, q := range st.Questions {
		header = append(header, q.Code)
	}
	w.Write(header)
	for _, r := range st.Students {
		num := ""
		if r.StudentNumber != nil {
			num = *r.StudentNumber
		}
		tt := ""
		if r.TimeTakenSec != nil {
			tt = fmt.Sprint(*r.TimeTakenSec)
		}
		rec := []string{csvSafe(num), csvSafe(r.Name), r.State, fmt.Sprint(r.Score), fmt.Sprint(r.MaxScore), fmt.Sprint(r.Pct),
			fmt.Sprint(r.Passed), tt, fmt.Sprint(r.Violations), fmt.Sprint(r.ExtensionSec), fmt.Sprint(r.Pauses)}
		scores := map[string]string{}
		for _, a := range r.Answers {
			if a.Score != nil {
				scores[a.QuestionID] = fmt.Sprint(*a.Score)
			}
		}
		for _, q := range st.Questions {
			rec = append(rec, scores[q.QuestionID])
		}
		w.Write(rec)
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// ---------- share links (FR-RS-01/02/04, BR-13, AC-12) ----------

var validViews = map[string]bool{"individual": true, "question_pct": true, "pass_rate": true}

type ShareInput struct {
	Scope       string     `json:"scope"` // session or quiz
	TargetID    string     `json:"target_id"`
	Views       []string   `json:"views"`
	Identify    string     `json:"identify"` // anonymous (default) or student_id
	ShowAnswers bool       `json:"show_answers"`
	Label       string     `json:"label"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

type ShareLink struct {
	ID          string     `json:"id"`
	Token       string     `json:"token,omitempty"` // only returned when created or regenerated
	Scope       string     `json:"scope"`
	TargetID    string     `json:"target_id"`
	Views       []string   `json:"views"`
	Identify    string     `json:"identify"`
	ShowAnswers bool       `json:"show_answers"`
	Label       string     `json:"label"`
	ExpiresAt   *time.Time `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func newToken() (string, []byte) {
	b := make([]byte, 16) // 128-bit
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	t := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(t))
	return t, h[:]
}

const linkCols = `id, scope, target_id, views, identify, show_answers, label, expires_at, revoked_at, created_at`

func scanLink(r pgx.Row) (ShareLink, error) {
	var l ShareLink
	err := r.Scan(&l.ID, &l.Scope, &l.TargetID, &l.Views, &l.Identify, &l.ShowAnswers, &l.Label, &l.ExpiresAt, &l.RevokedAt, &l.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, httpx.ErrNotFound
	}
	return l, err
}

func (s *Service) checkTarget(ctx context.Context, teacherID, scope, id string) error {
	switch scope {
	case "session":
		_, err := s.sessions.SessionInfo(ctx, teacherID, id)
		return err
	case "live_session":
		_, err := s.sessions.SessionInfo(ctx, teacherID, id)
		return err
	case "live_poll":
		if s.polls == nil {
			return httpx.ErrNotFound
		}
		return s.polls.OwnsPoll(ctx, teacherID, id)
	case "quiz":
		ids, err := s.sessions.SessionIDsForQuiz(ctx, teacherID, id)
		if err == nil && len(ids) == 0 {
			return httpx.ErrNotFound // nothing has been run yet
		}
		return err
	}
	return httpx.Invalid(map[string]string{"scope": "scope must be session, quiz, live_session or live_poll"})
}

// CreateLink makes a public read-only link. Views default to the session's
// configured results view (BA §7); names and emails are never shared (BR-13).
func (s *Service) CreateLink(ctx context.Context, teacherID string, in ShareInput) (ShareLink, error) {
	if err := s.checkTarget(ctx, teacherID, in.Scope, in.TargetID); err != nil {
		return ShareLink{}, err
	}
	live := isLive(in.Scope)
	if len(in.Views) == 0 && in.Scope == "session" {
		info, _ := s.sessions.SessionInfo(ctx, teacherID, in.TargetID)
		in.Views = []string{info.Effective.ResultsView}
	}
	if len(in.Views) == 0 && live {
		in.Views = []string{"leaderboard", "teams"}
	}
	f := map[string]string{}
	if len(in.Views) == 0 {
		f["views"] = "choose at least one view"
	}
	for _, v := range in.Views {
		if live && !liveViews[v] {
			f["views"] = "live views are leaderboard and teams"
		} else if !live && !validViews[v] {
			f["views"] = "views are individual, question_pct and pass_rate"
		}
	}
	// Who can be told apart: student IDs for sessions, nicknames for polls.
	// Real names never appear on a public link (BR-13).
	if in.Identify == "" {
		in.Identify = "anonymous"
		if in.Scope == "live_poll" {
			in.Identify = "nickname"
		}
	}
	allowed := map[string]bool{"anonymous": true, "student_id": in.Scope != "live_poll", "nickname": in.Scope == "live_poll"}
	if !allowed[in.Identify] {
		f["identify"] = "identify must be anonymous, or student_id for sessions and nickname for polls"
	}
	if live {
		in.ShowAnswers = false
	}
	if in.ExpiresAt != nil && in.ExpiresAt.Before(s.now()) {
		f["expires_at"] = "expiry must be in the future"
	}
	if len(f) > 0 {
		return ShareLink{}, httpx.Invalid(f)
	}
	token, hash := newToken()
	var l ShareLink
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		l, err = scanLink(tx.QueryRow(ctx, `INSERT INTO analytics.share_links(token_hash, teacher_id, scope, target_id, views, identify, show_answers, label, expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+linkCols,
			hash, teacherID, in.Scope, in.TargetID, in.Views, in.Identify, in.ShowAnswers, strings.TrimSpace(in.Label), in.ExpiresAt))
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "share_link_created", "share_link", l.ID, in)
	})
	l.Token = token
	return l, err
}

// LinkSorts are the orders of the share-link list (PL-FR-02 "result links").
var LinkSorts = page.Sorts{"created": "created_at"}

// ListLinks returns one page of a teacher's share links, for one target or
// all, of some scopes (comma-separated) or all, searched by label, and how
// many there are.
func (s *Service) ListLinks(ctx context.Context, teacherID, targetID, scopes string, p page.Request) ([]ShareLink, int, error) {
	where := ` FROM analytics.share_links WHERE teacher_id=$1 AND ($2='' OR target_id::text=$2) AND ($3 = '' OR coalesce(label, '') ILIKE $3)
		AND ($4 = '' OR scope = ANY(string_to_array($4, ',')))`
	args := []any{teacherID, targetID, p.Like(), scopes}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+linkCols+where+p.OrderBy(LinkSorts, "id")+p.Limit(), args...)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ShareLink, error) { return scanLink(r) })
	return list, total, err
}

func (s *Service) RevokeLink(ctx context.Context, teacherID, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE analytics.share_links SET revoked_at=coalesce(revoked_at,now()) WHERE id=$1 AND teacher_id=$2`, id, teacherID)
	if err == nil && tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return err
}

// RegenerateLink replaces the token; the old URL stops working.
func (s *Service) RegenerateLink(ctx context.Context, teacherID, id string) (ShareLink, error) {
	token, hash := newToken()
	l, err := scanLink(s.pool.QueryRow(ctx, `UPDATE analytics.share_links SET token_hash=$3, revoked_at=NULL WHERE id=$1 AND teacher_id=$2 RETURNING `+linkCols, id, teacherID, hash))
	l.Token = token
	return l, err
}

// PublicView is what anyone with the link sees. It never carries names or
// emails; students appear by classroom student ID or as "Student N" (BR-13).
type PublicView struct {
	Title     string           `json:"title"`
	QuizTitle string           `json:"quiz_title"`
	Views     []string         `json:"views"`
	PassRate  *PublicPassRate  `json:"pass_rate,omitempty"`
	Questions []PublicQuestion `json:"questions,omitempty"`
	Students  []PublicStudent  `json:"students,omitempty"`
}

type PublicPassRate struct {
	PassRate    float64 `json:"pass_rate"`
	PassMarkPct int     `json:"pass_mark_pct"`
	Finished    int     `json:"finished"`
	Mean        float64 `json:"mean_pct"`
}

type PublicQuestion struct {
	Code       string  `json:"code"`
	Text       string  `json:"text"`
	Format     string  `json:"format,omitempty"`
	PctCorrect float64 `json:"pct_correct"`
	Answered   int     `json:"answered"`
}

type PublicStudent struct {
	Label   string   `json:"label"`
	Score   float64  `json:"score"`
	Max     float64  `json:"max_score"`
	Pct     float64  `json:"pct"`
	Passed  bool     `json:"passed"`
	Answers []Answer `json:"answers,omitempty"`
}

var errLinkGone = httpx.NewError(410, "link_unavailable", "this link has expired or been revoked")

func (s *Service) Public(ctx context.Context, token string) (PublicView, error) {
	h := sha256.Sum256([]byte(token))
	var l ShareLink
	var teacherID string
	err := s.pool.QueryRow(ctx, `SELECT `+linkCols+`, teacher_id FROM analytics.share_links WHERE token_hash=$1`, h[:]).
		Scan(&l.ID, &l.Scope, &l.TargetID, &l.Views, &l.Identify, &l.ShowAnswers, &l.Label, &l.ExpiresAt, &l.RevokedAt, &l.CreatedAt, &teacherID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicView{}, httpx.ErrNotFound
	}
	if err != nil {
		return PublicView{}, err
	}
	if isLive(l.Scope) {
		return PublicView{}, httpx.ErrNotFound // live links have their own page
	}
	if l.RevokedAt != nil || (l.ExpiresAt != nil && s.now().After(*l.ExpiresAt)) {
		return PublicView{}, errLinkGone
	}
	var title, quizTitle string
	var class ClassStats
	var questions []QuestionRow
	var students []StudentRow
	if l.Scope == "session" {
		st, err := s.stats(ctx, l.TargetID)
		if err != nil {
			return PublicView{}, err
		}
		title, quizTitle, class, questions, students = st.Title, st.QuizTitle, st.Class, st.Questions, st.Students
	} else {
		qs, err := s.QuizStats(ctx, teacherID, l.TargetID)
		if err != nil {
			return PublicView{}, err
		}
		title, quizTitle, class, questions, students = qs.QuizTitle, qs.QuizTitle, qs.Class, qs.Questions, qs.Students
	}
	v := PublicView{Title: title, QuizTitle: quizTitle, Views: l.Views}
	for _, view := range l.Views {
		switch view {
		case "pass_rate":
			v.PassRate = &PublicPassRate{PassRate: class.PassRate, PassMarkPct: class.PassMarkPct, Finished: class.Marked, Mean: class.Mean}
		case "question_pct":
			for _, q := range questions {
				v.Questions = append(v.Questions, PublicQuestion{Code: q.Code, Text: q.Text, Format: q.Format, PctCorrect: q.PctCorrect, Answered: q.Answered})
			}
		case "individual":
			n := 0
			for _, r := range students {
				if r.State == live.StateInvalidated {
					continue
				}
				n++
				ps := PublicStudent{Label: fmt.Sprintf("Student %d", n), Score: r.Score, Max: r.MaxScore, Pct: r.Pct, Passed: r.Passed}
				if l.Identify == "student_id" && r.StudentNumber != nil {
					ps.Label = *r.StudentNumber
				}
				if l.ShowAnswers { // individual answers only when explicitly enabled (ADR-16)
					ps.Answers = r.Answers
				}
				v.Students = append(v.Students, ps)
			}
		}
	}
	return v, nil
}
