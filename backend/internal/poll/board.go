package poll

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Whiteboard (V2-09, D-47). Each poll has one board on a 1600×900 canvas.
// Strokes are stored, so late joiners see the whole board, and pushed to
// every open screen at once. Long pen lines arrive in pieces while being
// drawn; pieces share a gesture id so undo removes the whole line. The
// teacher always draws; participants draw when the board is open and the
// mode allows it (everyone, or selected groups and participants).

const (
	BoardW, BoardH   = 1600, 900
	MaxStrokes       = 20000
	maxPointsPer     = 4000 // numbers, i.e. 2000 points
	maxStrokesPerReq = 20
)

var (
	colorRe   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	gestureRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	errNoDraw = httpx.NewError(http.StatusForbidden, "cannot_draw", "the teacher hasn't let you draw on this board")
)

// Stroke is one mark on the board. By is "t" for the teacher or the
// participant's opaque rank key, so a browser can recognise its own strokes.
type Stroke struct {
	ID      int64     `json:"id"`
	By      string    `json:"by"`
	Gesture string    `json:"gesture"`
	Tool    string    `json:"tool"` // pen highlighter line rect ellipse arrow text
	Color   string    `json:"color"`
	Size    float64   `json:"size"`
	Points  []float64 `json:"points"`
	Text    string    `json:"text,omitempty"`
}

// BoardAccess says who may draw.
type BoardAccess struct {
	Open         bool     `json:"open"`
	Mode         string   `json:"mode"` // teacher | everyone | selected
	Groups       []string `json:"groups"`
	Participants []string `json:"participants"`
}

type BoardView struct {
	Type    string       `json:"type"` // board_state
	Open    bool         `json:"open"`
	Mode    string       `json:"mode"`
	CanDraw bool         `json:"can_draw"`
	Me      string       `json:"me"` // own "by" value
	Strokes []Stroke     `json:"strokes"`
	Access  *BoardAccess `json:"access,omitempty"` // teacher only
}

func (st *Stroke) clean() map[string]string {
	f := map[string]string{}
	switch st.Tool {
	case "pen", "highlighter":
		if len(st.Points) < 2 || len(st.Points)%2 != 0 || len(st.Points) > maxPointsPer {
			f["points"] = "a line needs 1-2000 points"
		}
	case "line", "rect", "ellipse", "arrow":
		if len(st.Points) != 4 {
			f["points"] = "a shape needs two corners"
		}
	case "text":
		if len(st.Points) != 2 {
			f["points"] = "text needs a position"
		}
		st.Text = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) && r != '\n' {
				return -1
			}
			return r
		}, strings.TrimSpace(st.Text))
		if n := utf8.RuneCountInString(st.Text); n < 1 || n > 200 {
			f["text"] = "text must be 1-200 characters"
		}
	default:
		f["tool"] = "unknown tool"
	}
	if st.Tool != "text" {
		st.Text = ""
	}
	for i, v := range st.Points {
		lim := float64(BoardW)
		if i%2 == 1 {
			lim = BoardH
		}
		if v < -50 || v > lim+50 || v != v {
			f["points"] = "points must be on the board"
			break
		}
	}
	if !colorRe.MatchString(st.Color) {
		f["color"] = "colour must be #rrggbb"
	}
	if st.Size < 1 || st.Size > 80 {
		f["size"] = "size must be 1-80"
	}
	if !gestureRe.MatchString(st.Gesture) {
		f["gesture"] = "invalid gesture id"
	}
	return f
}

// ---------- reading ----------

func (s *Service) boardAccess(ctx context.Context, pollID string) (BoardAccess, error) {
	var a BoardAccess
	err := s.pool.QueryRow(ctx, `SELECT board_open, board_mode, board_groups::text[], board_participants::text[] FROM poll.polls WHERE id=$1`, pollID).
		Scan(&a.Open, &a.Mode, &a.Groups, &a.Participants)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, httpx.ErrNotFound
	}
	return a, err
}

// mayDraw decides whether a participant can draw now.
func (s *Service) mayDraw(ctx context.Context, p *Poll, a BoardAccess, pid string) (bool, error) {
	if !a.Open || p.Status != "open" {
		return false, nil
	}
	switch a.Mode {
	case "everyone":
		return true, nil
	case "selected":
		if contains(a.Participants, pid) {
			return true, nil
		}
		var group *string
		if err := s.pool.QueryRow(ctx, `SELECT group_id::text FROM poll.participants WHERE id=$1`, pid).Scan(&group); err != nil {
			return false, err
		}
		return group != nil && contains(a.Groups, *group), nil
	}
	return false, nil
}

func (s *Service) strokes(ctx context.Context, p *Poll) ([]Stroke, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, participant_id::text, gesture, tool, color, size::float8, points, coalesce(text, '')
		FROM poll.board_strokes WHERE poll_id=$1 ORDER BY id`, p.ID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Stroke, error) {
		var st Stroke
		var pid *string
		err := r.Scan(&st.ID, &pid, &st.Gesture, &st.Tool, &st.Color, &st.Size, &st.Points, &st.Text)
		st.By = byOf(p.ID, pid)
		return st, err
	})
}

func byOf(pollID string, pid *string) string {
	if pid == nil {
		return "t"
	}
	return rankKey(pollID, *pid)
}

// Board is a participant's view of the board.
func (s *Service) Board(ctx context.Context, code string, c Caller) (BoardView, error) {
	p, err := s.pollByCode(ctx, code)
	if err != nil {
		return BoardView{}, err
	}
	if p.Status == "draft" {
		return BoardView{}, httpx.NewError(http.StatusConflict, "poll_not_open", "this poll hasn't started yet")
	}
	a, err := s.boardAccess(ctx, p.ID)
	if err != nil {
		return BoardView{}, err
	}
	v := BoardView{Type: "board_state", Open: a.Open, Mode: a.Mode, Strokes: []Stroke{}}
	if !a.Open {
		return v, nil // nothing to see until the teacher shows the board
	}
	pid, _, err := s.findParticipant(ctx, p, c)
	if err != nil {
		return v, err
	}
	if pid != "" {
		v.Me = rankKey(p.ID, pid)
		if v.CanDraw, err = s.mayDraw(ctx, p, a, pid); err != nil {
			return v, err
		}
	}
	v.Strokes, err = s.strokes(ctx, p)
	return v, err
}

// TeacherBoard is the owner's view, with the access settings.
func (s *Service) TeacherBoard(ctx context.Context, teacherID, pollID string) (BoardView, error) {
	p, err := s.ownedPoll(ctx, teacherID, pollID)
	if err != nil {
		return BoardView{}, err
	}
	a, err := s.boardAccess(ctx, p.ID)
	if err != nil {
		return BoardView{}, err
	}
	v := BoardView{Type: "board_state", Open: a.Open, Mode: a.Mode, CanDraw: true, Me: "t", Access: &a}
	v.Strokes, err = s.strokes(ctx, p)
	return v, err
}

// ---------- writing ----------

type StrokesInput struct {
	Strokes []Stroke `json:"strokes"`
}

func (s *Service) addStrokes(ctx context.Context, p *Poll, pid *string, in StrokesInput) ([]Stroke, error) {
	if len(in.Strokes) == 0 || len(in.Strokes) > maxStrokesPerReq {
		return nil, httpx.Invalid(map[string]string{"strokes": fmt.Sprintf("send 1-%d strokes", maxStrokesPerReq)})
	}
	for i := range in.Strokes {
		if f := in.Strokes[i].clean(); len(f) > 0 {
			return nil, httpx.Invalid(f)
		}
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM poll.board_strokes WHERE poll_id=$1`, p.ID).Scan(&n); err != nil {
		return nil, err
	}
	if n+len(in.Strokes) > MaxStrokes {
		return nil, httpx.Conflict("the board is full; clear it to keep drawing")
	}
	out := make([]Stroke, 0, len(in.Strokes))
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, st := range in.Strokes {
			var text any
			if st.Text != "" {
				text = st.Text
			}
			if err := tx.QueryRow(ctx, `INSERT INTO poll.board_strokes (poll_id, participant_id, gesture, tool, color, size, points, text)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, p.ID, pid, st.Gesture, st.Tool, st.Color, st.Size, st.Points, text).Scan(&st.ID); err != nil {
				return err
			}
			st.By = byOf(p.ID, pid)
			out = append(out, st)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.hub.board(p.ID, map[string]any{"type": "board", "op": "add", "strokes": out})
	return out, nil
}

// removeStrokes deletes strokes by id or by gesture. Participants can only
// remove their own; the teacher (pid nil, teacher true) can remove any.
func (s *Service) removeStrokes(ctx context.Context, p *Poll, pid *string, teacher bool, ids []int64, gesture string) ([]int64, error) {
	if len(ids) > 2000 {
		return nil, httpx.Invalid(map[string]string{"ids": "too many strokes at once"})
	}
	q := `DELETE FROM poll.board_strokes WHERE poll_id=$1 AND `
	args := []any{p.ID}
	if gesture != "" {
		q += `gesture=$2`
		args = append(args, gesture)
	} else {
		q += `id = ANY($2)`
		args = append(args, ids)
	}
	switch {
	case gesture != "" && teacher:
		q += ` AND participant_id IS NULL` // undo removes the teacher's own gesture
	case !teacher:
		q += ` AND participant_id=$3`
		args = append(args, *pid)
	}
	rows, err := s.pool.Query(ctx, q+` RETURNING id`, args...)
	if err != nil {
		return nil, err
	}
	gone, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	if len(gone) > 0 {
		s.hub.board(p.ID, map[string]any{"type": "board", "op": "remove", "ids": gone})
	}
	return gone, nil
}

type EraseInput struct {
	IDs     []int64 `json:"ids"`
	Gesture string  `json:"gesture"` // undo: remove a whole gesture
}

// participantBoard checks a participant may draw and returns their id.
func (s *Service) participantBoard(ctx context.Context, code string, c Caller) (*Poll, string, error) {
	p, err := s.pollByCode(ctx, code)
	if err != nil {
		return nil, "", err
	}
	if p.Status != "open" {
		return nil, "", errClosed
	}
	pid, _, err := s.findParticipant(ctx, p, c)
	if err != nil {
		return nil, "", err
	}
	if pid == "" {
		return nil, "", errNotJoined
	}
	a, err := s.boardAccess(ctx, p.ID)
	if err != nil {
		return nil, "", err
	}
	ok, err := s.mayDraw(ctx, p, a, pid)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", errNoDraw
	}
	if !s.boardRate.Allow(pid) {
		return nil, "", httpx.NewError(http.StatusTooManyRequests, "rate_limited", "slow down")
	}
	return p, pid, nil
}

func (s *Service) SetBoardAccess(ctx context.Context, teacherID, pollID string, in BoardAccess) (BoardAccess, error) {
	p, err := s.ownedPoll(ctx, teacherID, pollID)
	if err != nil {
		return in, err
	}
	if in.Mode == "" {
		in.Mode = "teacher"
	}
	if in.Mode != "teacher" && in.Mode != "everyone" && in.Mode != "selected" {
		return in, httpx.Invalid(map[string]string{"mode": "mode must be teacher, everyone or selected"})
	}
	if in.Groups == nil {
		in.Groups = []string{}
	}
	if in.Participants == nil {
		in.Participants = []string{}
	}
	// Keep only ids that belong to this poll.
	var groups, people []string
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(array_agg(id::text), '{}') FROM poll.groups WHERE poll_id=$1 AND id::text = ANY($2)`, p.ID, in.Groups).Scan(&groups); err != nil {
		return in, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(array_agg(id::text), '{}') FROM poll.participants WHERE poll_id=$1 AND id::text = ANY($2)`, p.ID, in.Participants).Scan(&people); err != nil {
		return in, err
	}
	in.Groups, in.Participants = groups, people
	if _, err := s.pool.Exec(ctx, `UPDATE poll.polls SET board_open=$2, board_mode=$3, board_groups=$4::uuid[], board_participants=$5::uuid[], updated_at=now() WHERE id=$1`,
		p.ID, in.Open, in.Mode, groups, people); err != nil {
		return in, err
	}
	s.hub.board(p.ID, map[string]any{"type": "board", "op": "access", "open": in.Open})
	s.hub.stateChanged(p.ID)
	return in, nil
}

func (s *Service) ClearBoard(ctx context.Context, teacherID, pollID string) error {
	p, err := s.ownedPoll(ctx, teacherID, pollID)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM poll.board_strokes WHERE poll_id=$1`, p.ID); err != nil {
		return err
	}
	s.hub.board(p.ID, map[string]any{"type": "board", "op": "clear"})
	return nil
}

// ---------- hub ----------

// board pushes a board event to every open screen of the poll right away,
// without waiting for the twice-a-second results flush.
func (h *Hub) board(pollID string, ev map[string]any) {
	ev["at"] = time.Now().UnixMilli()
	msg, _ := json.Marshal(ev)
	h.mu.Lock()
	for c := range h.audience[pollID] {
		c.push(msg)
	}
	for c := range h.presenters[pollID] {
		c.push(msg)
	}
	h.mu.Unlock()
}

// ---------- HTTP ----------

func (s *Service) boardTeacherRoutes(r chi.Router) {
	r.Method("GET", "/polls/{id}/board", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.TeacherBoard(r.Context(), teacherID(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, v)
		return nil
	}))
	r.Method("PUT", "/polls/{id}/board/access", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in BoardAccess
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		a, err := s.SetBoardAccess(r.Context(), teacherID(r), chi.URLParam(r, "id"), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, a)
		return nil
	}))
	r.Method("POST", "/polls/{id}/board/strokes", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in StrokesInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		p, err := s.ownedPoll(r.Context(), teacherID(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		out, err := s.addStrokes(r.Context(), p, nil, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, 201, map[string]any{"strokes": out})
		return nil
	}))
	r.Method("POST", "/polls/{id}/board/erase", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in EraseInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		p, err := s.ownedPoll(r.Context(), teacherID(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		gone, err := s.removeStrokes(r.Context(), p, nil, true, in.IDs, in.Gesture)
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, map[string]any{"removed": gone})
		return nil
	}))
	r.Method("POST", "/polls/{id}/board/clear", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.ClearBoard(r.Context(), teacherID(r), chi.URLParam(r, "id")); err != nil {
			return err
		}
		w.WriteHeader(204)
		return nil
	}))
}

func (s *Service) boardPublicRoutes(r chi.Router) {
	r.Method("GET", "/{code}/board", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.Board(r.Context(), chi.URLParam(r, "code"), caller(r))
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, v)
		return nil
	}))
	r.Method("POST", "/{code}/board/strokes", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in StrokesInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		p, pid, err := s.participantBoard(r.Context(), chi.URLParam(r, "code"), caller(r))
		if err != nil {
			return err
		}
		out, err := s.addStrokes(r.Context(), p, &pid, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, 201, map[string]any{"strokes": out})
		return nil
	}))
	r.Method("POST", "/{code}/board/erase", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in EraseInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		p, pid, err := s.participantBoard(r.Context(), chi.URLParam(r, "code"), caller(r))
		if err != nil {
			return err
		}
		gone, err := s.removeStrokes(r.Context(), p, &pid, false, in.IDs, in.Gesture)
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, map[string]any{"removed": gone})
		return nil
	}))
}
