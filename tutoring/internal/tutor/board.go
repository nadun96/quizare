package tutor

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// The session's whiteboard (D-85, TS-FR-66, TS-FR-67): one board, the
// teachers', on the platform's fixed 1600×900 board with the strokes and
// tools of the poll and meeting boards (D-47, D-72), so the platform's
// Whiteboard component draws it. Teachers and co-teachers draw; students
// watch, unless the lead teacher lets everyone, or chosen students, draw.
// Drawing goes through the API; the socket carries it to everyone.

const (
	boardW, boardH  = 1600, 900
	maxPointsPer    = 4000 // numbers, i.e. 2000 points
	maxBoardStrokes = 20000
	maxPerRequest   = 20
)

var (
	colorRe   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	gestureRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
)

// Stroke is one mark on the board (the platform's whiteboard.Stroke).
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

// clean tidies a stroke and says what's wrong with it ("" when it may be kept).
func (st *Stroke) clean() string {
	switch st.Tool {
	case "pen", "highlighter":
		if len(st.Points) < 2 || len(st.Points)%2 != 0 || len(st.Points) > maxPointsPer {
			return "a line needs 1-2000 points"
		}
	case "line", "rect", "ellipse", "arrow":
		if len(st.Points) != 4 {
			return "a shape needs two corners"
		}
	case "text":
		if len(st.Points) != 2 {
			return "text needs a position"
		}
		st.Text = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) && r != '\n' {
				return -1
			}
			return r
		}, strings.TrimSpace(st.Text))
		if n := utf8.RuneCountInString(st.Text); n < 1 || n > 200 {
			return "text must be 1-200 characters"
		}
	default:
		return "unknown tool"
	}
	if st.Tool != "text" {
		st.Text = ""
	}
	for i, v := range st.Points {
		lim := float64(boardW)
		if i%2 == 1 {
			lim = boardH
		}
		if v < -50 || v > lim+50 || v != v {
			return "points must be on the board"
		}
	}
	if !colorRe.MatchString(st.Color) {
		return "colour must be #rrggbb"
	}
	if st.Size < 1 || st.Size > 80 {
		return "size must be 1-80"
	}
	if !gestureRe.MatchString(st.Gesture) {
		return "invalid gesture id"
	}
	return ""
}

// BoardAccess is who may draw, for the teachers' controls.
type BoardAccess struct {
	Open         bool     `json:"open"`
	Mode         string   `json:"mode"` // teacher, everyone or selected
	Groups       []string `json:"groups"`
	Participants []string `json:"participants"` // selected students' ids
}

// BoardView is the board as one person sees it (the platform's BoardView).
type BoardView struct {
	Type    string       `json:"type"` // board_state
	Open    bool         `json:"open"`
	Mode    string       `json:"mode"`
	CanDraw bool         `json:"can_draw"`
	Me      string       `json:"me"`
	Strokes []Stroke     `json:"strokes"`
	Access  *BoardAccess `json:"access,omitempty"` // teachers only
}

// mayDraw: teachers always; students while it's open, as the lead teacher chose.
func mayDraw(sess Session, p Participant) bool {
	if p.Staff() {
		return true
	}
	if !sess.BoardOpen || p.State != "admitted" {
		return false
	}
	return sess.BoardMode == "everyone" || (sess.BoardMode == "selected" && slices.Contains(sess.BoardUsers, p.UserID))
}

// boardSession returns the session and the caller, who must be admitted.
func (s *Service) boardSession(ctx context.Context, p authn.Person, id string) (Session, Participant, error) {
	sess, err := s.session(ctx, id)
	if err != nil {
		return sess, Participant{}, err
	}
	me, err := s.participant(ctx, id, p.ID)
	if err != nil || me.State != "admitted" {
		return sess, me, web.ErrNotFound
	}
	if sess.Status == "ended" || sess.Status == "cancelled" {
		return sess, me, errEnded
	}
	return sess, me, nil
}

// Board is the board as the caller sees it; students see it only while it's open.
func (s *Service) Board(ctx context.Context, p authn.Person, id string) (BoardView, error) {
	sess, me, err := s.boardSession(ctx, p, id)
	if err != nil {
		return BoardView{}, err
	}
	v := BoardView{Type: "board_state", Open: sess.BoardOpen, Mode: sess.BoardMode, CanDraw: mayDraw(sess, me), Me: p.ID, Strokes: []Stroke{}}
	if me.Staff() {
		users := sess.BoardUsers
		if users == nil {
			users = []string{}
		}
		v.Access = &BoardAccess{Open: sess.BoardOpen, Mode: sess.BoardMode, Groups: []string{}, Participants: users}
	} else if !sess.BoardOpen {
		return v, nil
	}
	rows, _ := s.pool.Query(ctx, `SELECT id, by_user::text, gesture, tool, color, size, points, coalesce(text, '')
		FROM tutoring.board_strokes WHERE session_id=$1 AND id > $2 ORDER BY id`, id, sess.boardCleared)
	v.Strokes, err = pgx.CollectRows(rows, scanStroke)
	if v.Strokes == nil {
		v.Strokes = []Stroke{}
	}
	return v, err
}

func scanStroke(r pgx.CollectableRow) (Stroke, error) {
	var st Stroke
	var pts []byte
	err := r.Scan(&st.ID, &st.By, &st.Gesture, &st.Tool, &st.Color, &st.Size, &pts, &st.Text)
	if err == nil {
		err = json.Unmarshal(pts, &st.Points)
	}
	return st, err
}

var errMayNotDraw = web.NewError(403, "may_not_draw", "the teacher hasn't let you draw")

// AddStrokes keeps new marks and sends them to everyone else.
func (s *Service) AddStrokes(ctx context.Context, p authn.Person, id string, strokes []Stroke) ([]Stroke, error) {
	sess, me, err := s.boardSession(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if !mayDraw(sess, me) {
		return nil, errMayNotDraw
	}
	if len(strokes) == 0 || len(strokes) > maxPerRequest {
		return nil, web.Invalid(map[string]string{"strokes": fmt.Sprintf("send 1-%d strokes", maxPerRequest)})
	}
	for i := range strokes {
		if why := strokes[i].clean(); why != "" {
			return nil, web.Invalid(map[string]string{"strokes": why})
		}
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM tutoring.board_strokes WHERE session_id=$1 AND id > $2`, id, sess.boardCleared).Scan(&n); err != nil {
			return err
		}
		if n+len(strokes) > maxBoardStrokes {
			return web.Conflict("the board is full: a teacher can clear it")
		}
		for i := range strokes {
			pts, _ := json.Marshal(strokes[i].Points)
			strokes[i].By = p.ID
			if err := tx.QueryRow(ctx, `INSERT INTO tutoring.board_strokes (session_id, by_user, gesture, tool, color, size, points, text)
				VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')) RETURNING id`, id, p.ID, strokes[i].Gesture, strokes[i].Tool, strokes[i].Color,
				strokes[i].Size, pts, strokes[i].Text).Scan(&strokes[i].ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.boardEvent(sess, map[string]any{"op": "add", "strokes": strokes})
	return strokes, nil
}

// EraseStrokes removes marks: teachers any, students their own; a gesture
// (undo) takes every piece of one of the caller's own lines.
func (s *Service) EraseStrokes(ctx context.Context, p authn.Person, id string, ids []int64, gesture string) ([]int64, error) {
	sess, me, err := s.boardSession(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if !mayDraw(sess, me) {
		return nil, errMayNotDraw
	}
	if gesture != "" && !gestureRe.MatchString(gesture) {
		return nil, web.Invalid(map[string]string{"gesture": "invalid gesture id"})
	}
	if ids == nil {
		ids = []int64{}
	}
	rows, _ := s.pool.Query(ctx, `DELETE FROM tutoring.board_strokes WHERE session_id=$1 AND
		((id = ANY($2) AND ($3 OR by_user=$4)) OR ($5 <> '' AND by_user=$4 AND gesture=$5)) RETURNING id`, id, ids, me.Staff(), p.ID, gesture)
	removed, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	if removed == nil {
		removed = []int64{}
	}
	if len(removed) > 0 {
		s.boardEvent(sess, map[string]any{"op": "remove", "ids": removed})
	}
	return removed, nil
}

// ClearBoard removes everything (teachers).
func (s *Service) ClearBoard(ctx context.Context, p authn.Person, id string) (int64, error) {
	sess, me, err := s.boardSession(ctx, p, id)
	if err != nil {
		return 0, err
	}
	if !me.Staff() {
		return 0, web.ErrForbidden
	}
	var upto int64
	if err := s.pool.QueryRow(ctx, `UPDATE tutoring.sessions SET board_cleared = coalesce((SELECT max(id) FROM tutoring.board_strokes WHERE session_id=$1), board_cleared)
		WHERE id=$1 RETURNING board_cleared`, id).Scan(&upto); err != nil {
		return 0, err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM tutoring.board_strokes WHERE session_id=$1 AND id <= $2`, id, upto); err != nil {
		return 0, err
	}
	s.logAction(ctx, id, p.ID, "board_cleared", nil, nil)
	s.boardEvent(sess, map[string]any{"op": "clear", "upto": upto})
	return upto, nil
}

// BoardAccessInput shows or hides the board and says who draws (the lead teacher's).
type BoardAccessInput struct {
	Open         bool     `json:"open"`
	Mode         string   `json:"mode"`
	Participants []string `json:"participants"`
}

func (s *Service) SetBoardAccess(ctx context.Context, p authn.Person, id string, in BoardAccessInput) error {
	if _, err := s.owned(ctx, p, id); err != nil {
		return err
	}
	if in.Mode == "" {
		in.Mode = "teacher"
	}
	if in.Mode != "teacher" && in.Mode != "everyone" && in.Mode != "selected" {
		return web.Invalid(map[string]string{"mode": "teacher, everyone or selected"})
	}
	if in.Participants == nil || in.Mode != "selected" {
		in.Participants = []string{}
	}
	if len(in.Participants) > 500 {
		return web.Invalid(map[string]string{"participants": "at most 500 students"})
	}
	tag, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET board_open=$2, board_mode=$3,
		board_users = ARRAY(SELECT user_id FROM tutoring.participants WHERE session_id=$1 AND role='student' AND user_id = ANY($4::uuid[]))
		WHERE id=$1 AND status IN ('open', 'live')`, id, in.Open, in.Mode, in.Participants)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return web.Conflict("the session has ended")
	}
	s.logAction(ctx, id, p.ID, "board_access", nil, in)
	sess, err := s.session(ctx, id)
	if err != nil {
		return err
	}
	// Who may draw is personal: everyone fetches the board again.
	s.boardEvent(sess, map[string]any{"op": "access", "open": sess.BoardOpen})
	s.hub.changed(id)
	return nil
}

// boardEvent tells the admitted people a board change. Students hear of
// marks only while the board is open: it isn't theirs to see otherwise.
func (s *Service) boardEvent(sess Session, data map[string]any) {
	data["type"] = "board"
	b, _ := json.Marshal(event{Type: "board", Data: data})
	s.hub.each(sess.ID, func(c *client) {
		if c.admitted() && (sess.BoardOpen || c.role == "teacher" || c.role == "coteacher" || data["op"] == "access") {
			c.push(b)
		}
	})
}
