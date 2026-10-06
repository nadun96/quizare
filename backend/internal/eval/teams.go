package eval

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/platform/groupscore"
	"github.com/nadun96/quizplatform/internal/settings"
)

// TeamStanding is one team's combined result in a session (V2-06, V2-07,
// D-44). Marks arrive as members finish, so standings fill in during the
// session; Complete is false while any counted mark is still pending.
type TeamStanding struct {
	Rank     int     `json:"rank"`
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Color    int     `json:"color"`
	Members  int     `json:"members"`  // not counting invalidated attempts
	Finished int     `json:"finished"` // members whose attempt is marked
	Score    float64 `json:"score"`
	MaxScore float64 `json:"max_score"`
	Pct      float64 `json:"pct"`
	Complete bool    `json:"complete"`
}

// TeamStandings ranks a session's teams for its teacher.
func (s *Service) TeamStandings(ctx context.Context, teacherID, sessionID string) ([]TeamStanding, error) {
	eff, err := s.attempts.TeamRules(ctx, teacherID, sessionID)
	if err != nil {
		return nil, err
	}
	return s.teamStandings(ctx, sessionID, eff)
}

func (s *Service) teamStandings(ctx context.Context, sessionID string, eff settings.Effective) ([]TeamStanding, error) {
	out := []TeamStanding{}
	if eff.TeamMode == "off" {
		return out, nil
	}
	infos, err := s.attempts.TeamInfos(ctx, sessionID)
	if err != nil || len(infos) == 0 {
		return out, err
	}
	pos := map[string]int{}
	for i, t := range infos {
		out = append(out, TeamStanding{ID: t.ID, Name: t.Name, Color: t.Color, Complete: true})
		pos[t.ID] = i
	}
	// Members and finished members; invalidated attempts never count (D-27).
	rows, err := s.pool.Query(ctx, `SELECT a.team_id, count(*) FILTER (WHERE a.state <> 'invalidated'),
		count(r.attempt_id) FILTER (WHERE a.state <> 'invalidated')
		FROM live.attempts a LEFT JOIN eval.results r ON r.attempt_id=a.id
		WHERE a.session_id=$1 AND a.team_id IS NOT NULL GROUP BY a.team_id`, sessionID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var team string
		var members, finished int
		if err := rows.Scan(&team, &members, &finished); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := pos[team]; ok {
			out[i].Members, out[i].Finished = members, finished
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	quizMax, err := s.attempts.SessionMaxScore(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	// Marks in answer order, so "first" takes the team's earliest answer.
	rows, err = s.pool.Query(ctx, `SELECT a.team_id, m.question_id, coalesce(m.score, 0)::float8, m.status, a.captain
		FROM eval.marks m JOIN live.attempts a ON a.id=m.attempt_id
		LEFT JOIN live.answers ans ON ans.attempt_id=m.attempt_id AND ans.question_id=m.question_id
		WHERE a.session_id=$1 AND a.team_id IS NOT NULL AND a.state <> 'invalidated'
		ORDER BY ans.saved_at NULLS LAST, a.created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	type key struct{ team, q string }
	marks := map[key][]float64{}
	var order []key
	for rows.Next() {
		var team, q, status string
		var score float64
		var captain bool
		if err := rows.Scan(&team, &q, &score, &status, &captain); err != nil {
			rows.Close()
			return nil, err
		}
		if eff.TeamAcceptance == "captain" && !captain {
			continue
		}
		i, ok := pos[team]
		if !ok {
			continue
		}
		if status == StatusPending || status == StatusNeedsManual {
			out[i].Complete = false
		}
		k := key{team, q}
		if _, seen := marks[k]; !seen {
			order = append(order, k)
		}
		marks[k] = append(marks[k], score)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, k := range order {
		i := pos[k.team]
		out[i].Score += groupscore.Combine(eff.TeamAcceptance, eff.TeamCalc, marks[k], out[i].Members)
	}
	for i := range out {
		out[i].Score = groupscore.Round(out[i].Score)
		out[i].MaxScore = quizMax * float64(groupscore.MaxFactor(eff.TeamAcceptance, eff.TeamCalc, out[i].Members))
		if out[i].MaxScore > 0 {
			out[i].Pct = groupscore.Round(out[i].Score / out[i].MaxScore * 100)
		}
	}
	// Rank by percentage, so a total of every member's marks stays fair
	// between teams of different sizes.
	sort.SliceStable(out, func(a, b int) bool { return out[a].Pct > out[b].Pct })
	pcts := make([]float64, len(out))
	for i := range out {
		pcts[i] = out[i].Pct
	}
	for i, r := range groupscore.Ranks(pcts) {
		out[i].Rank = r
	}
	return out, nil
}

// StudentTeam is a student's team result, shown with their released result.
type StudentTeam struct {
	TeamStanding
	Of int `json:"of"` // number of teams
}

func (s *Service) studentTeam(ctx context.Context, sessionID, attemptID string) (*StudentTeam, error) {
	var team *string
	if err := s.pool.QueryRow(ctx, `SELECT team_id FROM live.attempts WHERE id=$1`, attemptID).Scan(&team); err != nil || team == nil {
		return nil, err
	}
	eff, err := s.attempts.SessionTeamRules(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	list, err := s.teamStandings(ctx, sessionID, eff)
	if err != nil {
		return nil, err
	}
	for _, t := range list {
		if t.ID == *team {
			return &StudentTeam{TeamStanding: t, Of: len(list)}, nil
		}
	}
	return nil, nil
}

// PublicTeamStandings is TeamStandings for a public live link (no owner check).
func (s *Service) PublicTeamStandings(ctx context.Context, sessionID string) ([]TeamStanding, error) {
	eff, err := s.attempts.SessionTeamRules(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return s.teamStandings(ctx, sessionID, eff)
}

// RankedStudent is one finished student on a public live leaderboard. Names
// and emails never appear: students show as "Student N" (join order) or by
// classroom student ID (BR-13, D-33).
type RankedStudent struct {
	Rank      int     `json:"rank"`
	Label     string  `json:"label"`
	Score     float64 `json:"score"`
	MaxScore  float64 `json:"max_score"`
	Pct       float64 `json:"pct"`
	Complete  bool    `json:"complete"`
	TeamName  string  `json:"team,omitempty"`
	TeamColor int     `json:"team_color,omitempty"`
}

// PublicRanking ranks a session's marked, valid attempts by percentage.
// It returns the top limit, how many joined and how many are ranked.
func (s *Service) PublicRanking(ctx context.Context, sessionID string, studentIDs bool, limit int) ([]RankedStudent, int, int, error) {
	teams, err := s.attempts.TeamInfos(ctx, sessionID)
	if err != nil {
		return nil, 0, 0, err
	}
	byID := map[string]live.TeamInfo{}
	for _, t := range teams {
		byID[t.ID] = t
	}
	rows, err := s.pool.Query(ctx, `SELECT a.student_number, a.team_id, row_number() OVER (ORDER BY a.created_at, a.id),
		r.score::float8, r.max_score::float8, r.pct::float8, r.complete, r.invalidated
		FROM live.attempts a LEFT JOIN eval.results r ON r.attempt_id=a.id
		WHERE a.session_id=$1 ORDER BY a.created_at, a.id`, sessionID)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	out := []RankedStudent{}
	joined := 0
	for rows.Next() {
		var number, team *string
		var n int
		var score, max, pct *float64
		var complete, invalid *bool
		if err := rows.Scan(&number, &team, &n, &score, &max, &pct, &complete, &invalid); err != nil {
			return nil, 0, 0, err
		}
		joined++
		if score == nil || (invalid != nil && *invalid) {
			continue // not finished, or invalidated (D-27)
		}
		x := RankedStudent{Label: fmt.Sprintf("Student %d", n), Score: *score, MaxScore: *max, Pct: *pct, Complete: complete != nil && *complete}
		if studentIDs && number != nil && strings.TrimSpace(*number) != "" {
			x.Label = *number
		}
		if team != nil {
			if t, ok := byID[*team]; ok {
				x.TeamName, x.TeamColor = t.Name, t.Color
			}
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Pct > out[b].Pct })
	pcts := make([]float64, len(out))
	for i := range out {
		pcts[i] = out[i].Pct
	}
	for i, r := range groupscore.Ranks(pcts) {
		out[i].Rank = r
	}
	ranked := len(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, joined, ranked, nil
}
