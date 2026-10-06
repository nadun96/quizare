package poll

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Rank is one participant on the leaderboard (V2-02, D-42). Participants get
// a stable opaque Key so a browser can find itself in a shared top-10 without
// any participant id being published.
type Rank struct {
	Rank          int     `json:"rank"`
	Key           string  `json:"key"`
	Name          string  `json:"name"`
	Score         float64 `json:"score"`
	Correct       int     `json:"correct"`
	Answered      int     `json:"answered"`
	ParticipantID string  `json:"participant_id,omitempty"` // teacher view only
	RealName      string  `json:"real_name,omitempty"`      // teacher view only
	Nickname      string  `json:"nickname,omitempty"`       // teacher view only
}

// rankKey is a short opaque id for a participant, stable within a poll.
func rankKey(pollID, participantID string) string {
	h := sha256.Sum256([]byte(pollID + "/" + participantID))
	return hex.EncodeToString(h[:6])
}

// CleanNickname trims a nickname to one line of at most 30 printable characters.
func CleanNickname(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '​' {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > 30 {
		s = string([]rune(s)[:30])
	}
	return s
}

// leaderboard ranks every participant by points, then right answers, then
// who finished their scored answers first. Equal points and right answers
// share a rank (1, 2, 2, 4).
func (s *Service) leaderboard(ctx context.Context, p *Poll, teacher bool) ([]Rank, error) {
	rows, err := s.pool.Query(ctx, `SELECT pa.id, pa.user_id, coalesce(pa.nickname, ''),
		row_number() OVER (ORDER BY pa.created_at, pa.id),
		coalesce(sum(r.score) FILTER (WHERE NOT r.hidden), 0),
		count(*) FILTER (WHERE r.correct AND NOT r.hidden),
		count(r.score) FILTER (WHERE NOT r.hidden),
		max(r.updated_at) FILTER (WHERE r.score IS NOT NULL)
		FROM poll.participants pa LEFT JOIN poll.responses r ON r.participant_id = pa.id
		WHERE pa.poll_id = $1 GROUP BY pa.id`, p.ID)
	if err != nil {
		return nil, err
	}
	type row struct {
		id       string
		userID   *string
		nickname string
		n        int
		score    float64
		correct  int
		answered int
		last     *time.Time
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.id, &x.userID, &x.nickname, &x.n, &x.score, &x.correct, &x.answered, &x.last)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	var ids []string
	for _, x := range list {
		if x.userID != nil {
			ids = append(ids, *x.userID)
		}
	}
	if len(ids) > 0 && (teacher || p.Names == "name") {
		users, err := s.users.UsersByID(ctx, ids)
		if err != nil {
			return nil, err
		}
		for id, u := range users {
			names[id] = u.Name
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.correct != b.correct {
			return a.correct > b.correct
		}
		if (a.last == nil) != (b.last == nil) {
			return a.last != nil
		}
		if a.last != nil && !a.last.Equal(*b.last) {
			return a.last.Before(*b.last)
		}
		return a.n < b.n
	})
	out := make([]Rank, len(list))
	for i, x := range list {
		r := Rank{Key: rankKey(p.ID, x.id), Score: x.score, Correct: x.correct, Answered: x.answered}
		if i > 0 && x.score == list[i-1].score && x.correct == list[i-1].correct {
			r.Rank = out[i-1].Rank
		} else {
			r.Rank = i + 1
		}
		real := ""
		if x.userID != nil {
			real = names[*x.userID]
		}
		switch {
		case p.Names == "name" && real != "":
			r.Name = real
		case x.nickname != "":
			r.Name = x.nickname
		default:
			r.Name = fmt.Sprintf("Participant %d", x.n)
		}
		if teacher {
			r.ParticipantID, r.RealName, r.Nickname = x.id, real, x.nickname
		}
		out[i] = r
	}
	return out, nil
}
