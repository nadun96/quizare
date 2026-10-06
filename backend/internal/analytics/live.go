package analytics

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/poll"
)

// Public live leaderboards (V2-08, D-45). A live link is a share link with
// scope live_session or live_poll; anyone with it sees the current
// individual and team rankings, refreshed by polling. Views are cached for a
// couple of seconds per link, so a room full of viewers costs one query set.

// Polls is the slice of the poll module live links need.
type Polls interface {
	OwnsPoll(ctx context.Context, teacherID, pollID string) error
	PublicBoards(ctx context.Context, pollID string, nicknames bool, limit int) (poll.Boards, error)
}

// SetPolls connects the poll module (it is created after analytics).
func (s *Service) SetPolls(p Polls) { s.polls = p }

// SetClock lets tests control time.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

var liveViews = map[string]bool{"leaderboard": true, "teams": true}

const (
	liveLimit   = 50
	liveCache   = 2 * time.Second
	liveRefresh = 3000 // ms, suggested to the page
)

type LivePerson struct {
	Key    string  `json:"key"` // stable row key for animation; opaque
	Rank   int     `json:"rank"`
	Label  string  `json:"label"`
	Score  float64 `json:"score"`
	Detail string  `json:"detail,omitempty"`
	Color  int     `json:"color,omitempty"` // team colour, sessions
	Team   string  `json:"team,omitempty"`
}

type LiveTeam struct {
	Rank   int     `json:"rank"`
	Name   string  `json:"name"`
	Color  int     `json:"color"`
	Score  float64 `json:"score"`
	Detail string  `json:"detail,omitempty"`
}

// LiveView is what a live link shows. Unit tells the page how to read
// scores: points (polls) or percent (sessions).
type LiveView struct {
	Type      string       `json:"type"`
	Kind      string       `json:"kind"` // poll | session
	Title     string       `json:"title"`
	Subtitle  string       `json:"subtitle,omitempty"`
	Status    string       `json:"status"` // open, live, closed, ended, draft
	Unit      string       `json:"unit"`
	Views     []string     `json:"views"`
	People    []LivePerson `json:"people,omitempty"`
	Teams     []LiveTeam   `json:"teams,omitempty"`
	Joined    int          `json:"joined"`
	Finished  *int         `json:"finished,omitempty"`
	Scoring   bool         `json:"scoring"`
	RefreshMs int          `json:"refresh_ms"`
	UpdatedAt int64        `json:"updated_at"`
}

type cached struct {
	at   time.Time
	view LiveView
}

type liveCacheT struct {
	mu sync.Mutex
	m  map[string]cached
}

func (c *liveCacheT) get(id string, now time.Time) (LiveView, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	x, ok := c.m[id]
	if !ok || now.Sub(x.at) > liveCache {
		return LiveView{}, false
	}
	return x.view, true
}

func (c *liveCacheT) put(id string, v LiveView, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) > 1000 {
		c.m = map[string]cached{}
	}
	c.m[id] = cached{now, v}
}

func isLive(scope string) bool { return scope == "live_session" || scope == "live_poll" }

// Live returns the live view for a token.
func (s *Service) Live(ctx context.Context, token string) (LiveView, error) {
	h := sha256.Sum256([]byte(token))
	var l ShareLink
	err := s.pool.QueryRow(ctx, `SELECT `+linkCols+` FROM analytics.share_links WHERE token_hash=$1`, h[:]).
		Scan(&l.ID, &l.Scope, &l.TargetID, &l.Views, &l.Identify, &l.ShowAnswers, &l.Label, &l.ExpiresAt, &l.RevokedAt, &l.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !isLive(l.Scope)) {
		return LiveView{}, httpx.ErrNotFound
	}
	if err != nil {
		return LiveView{}, err
	}
	if l.RevokedAt != nil || (l.ExpiresAt != nil && s.now().After(*l.ExpiresAt)) {
		return LiveView{}, errLinkGone
	}
	now := s.now()
	if v, ok := s.live.get(l.ID, now); ok {
		return v, nil
	}
	var v LiveView
	if l.Scope == "live_poll" {
		v, err = s.livePoll(ctx, l)
	} else {
		v, err = s.liveSession(ctx, l)
	}
	if err != nil {
		return v, err
	}
	v.Type, v.Views, v.RefreshMs, v.UpdatedAt = "live", l.Views, liveRefresh, now.UnixMilli()
	if v.Status == "closed" || v.Status == "ended" {
		v.RefreshMs = 30000
	}
	s.live.put(l.ID, v, now)
	return v, nil
}

func has(views []string, v string) bool {
	for _, x := range views {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Service) livePoll(ctx context.Context, l ShareLink) (LiveView, error) {
	if s.polls == nil {
		return LiveView{}, httpx.ErrNotFound
	}
	b, err := s.polls.PublicBoards(ctx, l.TargetID, l.Identify != "anonymous", liveLimit)
	if errors.Is(err, httpx.ErrNotFound) {
		return LiveView{}, errLinkGone // the poll was deleted
	}
	if err != nil {
		return LiveView{}, err
	}
	v := LiveView{Kind: "poll", Title: b.Title, Status: b.Status, Unit: "points", Joined: b.Total, Scoring: b.Scoring, Subtitle: l.Label}
	if has(l.Views, "leaderboard") {
		v.People = []LivePerson{}
		for _, r := range b.People {
			v.People = append(v.People, LivePerson{Key: r.Key, Rank: r.Rank, Label: r.Name, Score: r.Score, Detail: fmt.Sprintf("%d right", r.Correct)})
		}
	}
	if has(l.Views, "teams") && len(b.Groups) > 0 {
		v.Teams = []LiveTeam{}
		for _, g := range b.Groups {
			v.Teams = append(v.Teams, LiveTeam{Rank: g.Rank, Name: g.Name, Color: g.Color, Score: g.Score, Detail: plural(g.Members, "member")})
		}
	}
	return v, nil
}

func (s *Service) liveSession(ctx context.Context, l ShareLink) (LiveView, error) {
	info, err := s.sessions.SessionInfo(ctx, "", l.TargetID)
	if errors.Is(err, httpx.ErrNotFound) {
		return LiveView{}, errLinkGone
	}
	if err != nil {
		return LiveView{}, err
	}
	v := LiveView{Kind: "session", Title: info.Title, Subtitle: info.QuizTitle, Status: info.Status, Unit: "percent", Scoring: true}
	if l.Label != "" {
		v.Subtitle = l.Label
	}
	people, joined, finished, err := s.results.PublicRanking(ctx, l.TargetID, l.Identify == "student_id", liveLimit)
	if err != nil {
		return v, err
	}
	v.Joined, v.Finished = joined, &finished
	if has(l.Views, "leaderboard") {
		v.People = []LivePerson{}
		for _, p := range people {
			detail := fmt.Sprintf("%s/%s", trim(p.Score), trim(p.MaxScore))
			if !p.Complete {
				detail += " · marking"
			}
			v.People = append(v.People, LivePerson{Key: p.Label, Rank: p.Rank, Label: p.Label, Score: p.Pct, Detail: detail, Color: p.TeamColor, Team: p.TeamName})
		}
	}
	teams, err := s.results.PublicTeamStandings(ctx, l.TargetID)
	if err != nil {
		return v, err
	}
	if has(l.Views, "teams") && len(teams) > 0 {
		v.Teams = []LiveTeam{}
		for _, t := range teams {
			v.Teams = append(v.Teams, LiveTeam{Rank: t.Rank, Name: t.Name, Color: t.Color, Score: t.Pct, Detail: fmt.Sprintf("%d/%d done", t.Finished, t.Members)})
		}
	}
	return v, nil
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func trim(x float64) string {
	if x == float64(int64(x)) {
		return fmt.Sprintf("%d", int64(x))
	}
	return fmt.Sprintf("%.1f", x)
}
