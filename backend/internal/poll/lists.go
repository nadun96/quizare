package poll

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

// Paginated teacher lists of a poll (PL-FR-02): its participants, and every
// answer to one question for moderation. Live results keep only the newest
// 300 text answers; these lists reach all of them. A poll has at most
// MaxParticipants people and real names live in the auth module, so a poll's
// rows are filtered and sorted here and only the page is sent (ADR-25).

// ParticipantRow is one participant on the teacher's list.
type ParticipantRow struct {
	ParticipantID string    `json:"participant_id"`
	Number        int       `json:"number"`              // "Participant N", by join order
	Name          string    `json:"name"`                // nickname, or "Participant N"
	Nickname      string    `json:"nickname,omitempty"`  // as typed
	RealName      string    `json:"real_name,omitempty"` // identified participants
	Group         string    `json:"group,omitempty"`
	Answered      int       `json:"answered"`
	Score         float64   `json:"score"`
	JoinedAt      time.Time `json:"joined_at"`
}

// ParticipantSorts are the orders of the participant list.
var ParticipantSorts = page.Sorts{"joined": "", "name": "", "answered": "", "score": ""}

func (s *Service) Participants(ctx context.Context, teacherID, pollID string, p page.Request) ([]ParticipantRow, int, error) {
	pl, err := s.ownedPoll(ctx, teacherID, pollID)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT pa.id, row_number() OVER (ORDER BY pa.created_at, pa.id), coalesce(pa.nickname, ''),
		pa.user_id, coalesce(g.name, ''), pa.created_at,
		(SELECT count(*) FROM poll.responses r WHERE r.participant_id = pa.id),
		(SELECT coalesce(sum(r.score) FILTER (WHERE NOT r.hidden), 0) FROM poll.responses r WHERE r.participant_id = pa.id)
		FROM poll.participants pa LEFT JOIN poll.groups g ON g.id = pa.group_id
		WHERE pa.poll_id = $1`, pl.ID)
	if err != nil {
		return nil, 0, err
	}
	type row struct {
		ParticipantRow
		userID *string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.ParticipantID, &x.Number, &x.Nickname, &x.userID, &x.Group, &x.JoinedAt, &x.Answered, &x.Score)
		return x, err
	})
	if err != nil {
		return nil, 0, err
	}
	names, err := s.realNames(ctx, len(list), func(i int) *string { return list[i].userID })
	if err != nil {
		return nil, 0, err
	}
	out := make([]ParticipantRow, 0, len(list))
	for _, x := range list {
		r := x.ParticipantRow
		if x.userID != nil {
			r.RealName = names[*x.userID]
		}
		r.Name = r.Nickname
		if r.Name == "" {
			r.Name = fmt.Sprintf("Participant %d", r.Number)
		}
		if p.Matches(r.Name, r.RealName, r.Group) {
			out = append(out, r)
		}
	}
	less := map[string]func(a, b ParticipantRow) int{
		"joined": func(a, b ParticipantRow) int { return a.Number - b.Number },
		"name": func(a, b ParticipantRow) int {
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		},
		"answered": func(a, b ParticipantRow) int { return a.Answered - b.Answered },
		"score": func(a, b ParticipantRow) int {
			switch {
			case a.Score < b.Score:
				return -1
			case a.Score > b.Score:
				return 1
			}
			return 0
		},
	}[p.Sort]
	sort.SliceStable(out, func(i, j int) bool {
		c := less(out[i], out[j])
		if c == 0 {
			c = out[i].Number - out[j].Number
			if p.Desc {
				c = -c
			}
		}
		return (c < 0) != p.Desc
	})
	pg, total := page.Slice(out, p)
	return pg, total, nil
}

// AnswerRow is one participant's answer to a question, for moderation.
type AnswerRow struct {
	ParticipantID string    `json:"participant_id"`
	Name          string    `json:"name"`
	RealName      string    `json:"real_name,omitempty"`
	Text          string    `json:"text"` // the answer as one line of text, as in the export
	Hidden        bool      `json:"hidden"`
	At            time.Time `json:"at"`
}

// Answers returns one page of every answer to a question, newest first by
// default, searched in the answer and the name, optionally only hidden or
// only shown ones.
func (s *Service) Answers(ctx context.Context, teacherID, pollID, questionID, hidden string, p page.Request) ([]AnswerRow, int, error) {
	q, err := s.ownedQuestion(ctx, teacherID, questionID)
	if err != nil {
		return nil, 0, err
	}
	if q.PollID != pollID {
		return nil, 0, httpx.ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `SELECT r.participant_id, pa.n, coalesce(pa.nickname, ''), pa.user_id, r.value, r.hidden, r.updated_at
		FROM poll.responses r JOIN (SELECT id, nickname, user_id, row_number() OVER (ORDER BY created_at, id) AS n
		      FROM poll.participants WHERE poll_id = $1) pa ON pa.id = r.participant_id
		WHERE r.question_id = $2 AND ($3 = '' OR r.hidden = ($3 = 'true'))`, pollID, q.ID, hidden)
	if err != nil {
		return nil, 0, err
	}
	type row struct {
		AnswerRow
		n      int
		nick   string
		userID *string
		raw    []byte
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.ParticipantID, &x.n, &x.nick, &x.userID, &x.raw, &x.Hidden, &x.At)
		return x, err
	})
	if err != nil {
		return nil, 0, err
	}
	names, err := s.realNames(ctx, len(list), func(i int) *string { return list[i].userID })
	if err != nil {
		return nil, 0, err
	}
	out := make([]AnswerRow, 0, len(list))
	for _, x := range list {
		r := x.AnswerRow
		var a Answer
		_ = json.Unmarshal(x.raw, &a)
		r.Text = describe(q, a)
		r.Name = x.nick
		if r.Name == "" {
			r.Name = fmt.Sprintf("Participant %d", x.n)
		}
		if x.userID != nil {
			r.RealName = names[*x.userID]
		}
		if p.Matches(r.Text, r.Name, r.RealName) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At) != p.Desc
		}
		return (out[i].ParticipantID < out[j].ParticipantID) != p.Desc
	})
	pg, total := page.Slice(out, p)
	return pg, total, nil
}

// realNames looks up the account names of identified participants.
func (s *Service) realNames(ctx context.Context, n int, userID func(int) *string) (map[string]string, error) {
	var ids []string
	for i := range n {
		if u := userID(i); u != nil {
			ids = append(ids, *u)
		}
	}
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	users, err := s.users.UsersByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	for id, u := range users {
		out[id] = u.Name
	}
	return out, nil
}

func (s *Service) listRoutes(r chi.Router) {
	r.Method("GET", "/polls/{id}/participants", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		p := page.Parse(r, ParticipantSorts, "joined", false)
		list, total, err := s.Participants(r.Context(), teacherID(r), chi.URLParam(r, "id"), p)
		if err != nil {
			return err
		}
		page.Write(w, "participants", list, total, p)
		return nil
	}))
	r.Method("GET", "/polls/{id}/questions/{qid}/answers", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		p := page.Parse(r, page.Sorts{"time": ""}, "time", true)
		list, total, err := s.Answers(r.Context(), teacherID(r), chi.URLParam(r, "id"), chi.URLParam(r, "qid"), r.URL.Query().Get("hidden"), p)
		if err != nil {
			return err
		}
		page.Write(w, "answers", list, total, p)
		return nil
	}))
}
