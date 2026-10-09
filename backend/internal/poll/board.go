package poll

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
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

	raw json.RawMessage // Strokes, encoded once for every screen loading together
}

// MarshalJSON writes the shared encoding of the strokes when there is one.
func (v BoardView) MarshalJSON() ([]byte, error) {
	type plain BoardView
	if v.raw == nil {
		return json.Marshal(plain(v))
	}
	return json.Marshal(struct {
		plain
		Strokes json.RawMessage `json:"strokes"`
	}{plain(v), v.raw})
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

// boardCache keeps each board's access settings and stroke count in memory
// (one app process, ADR-01), so a stroke piece costs one write instead of an
// access read, a count over the whole board and a transaction (D-52).
type boardCache struct {
	mu     sync.Mutex
	access map[string]BoardAccess
	// count may run high after strokes go with a removed participant, and
	// low by a few concurrent writes; the board is recounted before a write
	// is refused as full.
	count map[string]int
}

func newBoardCache() *boardCache {
	return &boardCache{access: map[string]BoardAccess{}, count: map[string]int{}}
}

func (c *boardCache) setAccess(pollID string, a BoardAccess) {
	c.mu.Lock()
	c.access[pollID] = a
	c.mu.Unlock()
}

func (c *boardCache) adjust(pollID string, d int) {
	c.mu.Lock()
	if n, ok := c.count[pollID]; ok {
		c.count[pollID] = max(0, n+d)
	}
	c.mu.Unlock()
}

func (c *boardCache) reset(pollID string) {
	c.mu.Lock()
	c.count[pollID] = 0
	c.mu.Unlock()
}

// ---------- reading ----------

func (s *Service) boardAccess(ctx context.Context, pollID string) (BoardAccess, error) {
	s.bc.mu.Lock()
	a, ok := s.bc.access[pollID]
	s.bc.mu.Unlock()
	if ok {
		return a, nil
	}
	err := s.pool.QueryRow(ctx, `SELECT board_open, board_mode, board_groups::text[], board_participants::text[] FROM poll.polls WHERE id=$1`, pollID).
		Scan(&a.Open, &a.Mode, &a.Groups, &a.Participants)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, httpx.ErrNotFound
	}
	if err == nil {
		s.bc.setAccess(pollID, a)
	}
	return a, err
}

// boardRead is one read of a board, shared by every request waiting for it.
type boardRead struct {
	done    chan struct{}
	strokes []Stroke
	raw     json.RawMessage
	err     error
}

// boardReads lets screens that load a board at the same moment (a class
// joining, or reconnecting after a Wi-Fi blip) share one database read and
// one JSON encoding (D-52). A request never takes a read that was already
// under way when it arrived, which could miss strokes drawn just before:
// it waits for the next one, shared by everyone who arrived meanwhile.
type boardReads struct {
	mu      sync.Mutex
	running map[string]*boardRead
	next    map[string]*boardRead
}

func (s *Service) boardStrokes(ctx context.Context, p *Poll) ([]Stroke, json.RawMessage, error) {
	r := &s.reads
	r.mu.Lock()
	if r.running == nil {
		r.running, r.next = map[string]*boardRead{}, map[string]*boardRead{}
	}
	var rd *boardRead
	if cur := r.running[p.ID]; cur == nil {
		rd = &boardRead{done: make(chan struct{})}
		r.running[p.ID] = rd
		r.mu.Unlock()
		s.readBoard(p, rd)
	} else {
		rd = r.next[p.ID]
		if rd == nil {
			rd = &boardRead{done: make(chan struct{})}
			r.next[p.ID] = rd
			go func() {
				<-cur.done
				r.mu.Lock()
				delete(r.next, p.ID)
				r.running[p.ID] = rd
				r.mu.Unlock()
				s.readBoard(p, rd)
			}()
		}
		r.mu.Unlock()
	}
	select {
	case <-rd.done:
		return rd.strokes, rd.raw, rd.err
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

// readBoard runs a shared read; it doesn't stop when one waiting request goes away.
func (s *Service) readBoard(p *Poll, rd *boardRead) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rd.strokes, rd.err = s.strokes(ctx, p)
	if rd.err == nil {
		rd.raw, rd.err = json.Marshal(rd.strokes)
	}
	s.reads.mu.Lock()
	if s.reads.running[p.ID] == rd {
		delete(s.reads.running, p.ID)
	}
	s.reads.mu.Unlock()
	close(rd.done)
}

// reserveStrokes makes room for k more strokes, or refuses when the board is full.
func (s *Service) reserveStrokes(ctx context.Context, pollID string, k int) error {
	s.bc.mu.Lock()
	n, ok := s.bc.count[pollID]
	if ok && n+k <= MaxStrokes {
		s.bc.count[pollID] = n + k
		s.bc.mu.Unlock()
		return nil
	}
	s.bc.mu.Unlock()
	// Not counted since start, or apparently full: count for real.
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM poll.board_strokes WHERE poll_id=$1`, pollID).Scan(&n); err != nil {
		return err
	}
	s.bc.mu.Lock()
	defer s.bc.mu.Unlock()
	if n+k > MaxStrokes {
		s.bc.count[pollID] = n
		return httpx.Conflict("the board is full; clear it to keep drawing")
	}
	s.bc.count[pollID] = n + k
	return nil
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
	v.Strokes, v.raw, err = s.boardStrokes(ctx, p)
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
	v.Strokes, v.raw, err = s.boardStrokes(ctx, p)
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
	if err := s.reserveStrokes(ctx, p.ID, len(in.Strokes)); err != nil {
		return nil, err
	}
	// One statement for the whole batch: atomic without a transaction, and a
	// single round trip. Rows of a VALUES list are inserted and returned in order.
	var q strings.Builder
	q.WriteString(`INSERT INTO poll.board_strokes (poll_id, participant_id, gesture, tool, color, size, points, text) VALUES `)
	args := []any{p.ID, pid}
	for i, st := range in.Strokes {
		var text any
		if st.Text != "" {
			text = st.Text
		}
		if i > 0 {
			q.WriteString(",")
		}
		n := len(args)
		fmt.Fprintf(&q, "($1,$2,$%d,$%d,$%d,$%d,$%d,$%d)", n+1, n+2, n+3, n+4, n+5, n+6)
		args = append(args, st.Gesture, st.Tool, st.Color, st.Size, st.Points, text)
	}
	q.WriteString(" RETURNING id")
	rows, err := s.pool.Query(ctx, q.String(), args...)
	if err != nil {
		s.bc.adjust(p.ID, -len(in.Strokes))
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err == nil && len(ids) != len(in.Strokes) {
		err = fmt.Errorf("board insert returned %d ids for %d strokes", len(ids), len(in.Strokes))
	}
	if err != nil {
		s.bc.adjust(p.ID, -len(in.Strokes))
		return nil, err
	}
	out := make([]Stroke, len(in.Strokes))
	for i, st := range in.Strokes {
		st.ID, st.By = ids[i], byOf(p.ID, pid)
		out[i] = st
	}
	s.hub.board(p.ID, map[string]any{"type": "board", "op": "add", "strokes": out})
	return out, nil
}

// removeStrokes deletes strokes by id or by gesture. Participants can only
// remove their own; the teacher (pid nil, teacher true) can remove any.
// Erasing by id takes the whole gesture of each stroke hit: a pen line is
// saved in pieces, and erasing part of it must not leave the rest (D-52).
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
		q += `(id = ANY($2) OR (gesture, coalesce(participant_id::text, 't')) IN (
			SELECT gesture, coalesce(participant_id::text, 't') FROM poll.board_strokes WHERE poll_id=$1 AND id = ANY($2)))`
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
	s.bc.adjust(p.ID, -len(gone))
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
	s.bc.setAccess(p.ID, in)
	s.hub.board(p.ID, map[string]any{"type": "board", "op": "access", "open": in.Open})
	s.hub.stateChanged(p.ID)
	return in, nil
}

func (s *Service) ClearBoard(ctx context.Context, teacherID, pollID string) error {
	p, err := s.ownedPoll(ctx, teacherID, pollID)
	if err != nil {
		return err
	}
	// upto is the highest id cleared: a stroke saved just before the clear,
	// whose add event is still on its way, must not reappear on screens.
	var upto int64
	if err := s.pool.QueryRow(ctx, `WITH d AS (DELETE FROM poll.board_strokes WHERE poll_id=$1 RETURNING id) SELECT coalesce(max(id), 0) FROM d`, p.ID).Scan(&upto); err != nil {
		return err
	}
	s.bc.reset(p.ID)
	s.hub.board(p.ID, map[string]any{"type": "board", "op": "clear", "upto": upto})
	return nil
}

// ---------- hub ----------

// boardWindow groups board events (D-52). Everything that happens on a
// poll's board within it reaches each screen together, with consecutive adds
// (and removes) merged into one event. A busy board then sends a screen about
// 20 messages a second instead of one per piece drawn, which keeps phones and
// slow connections from falling behind and being dropped as slow consumers.
// It is still far inside the 1 s target for strokes to appear (V2-09).
const boardWindow = 50 * time.Millisecond

// board queues a board event for every open screen of the poll; it goes out
// within boardWindow, without waiting for the twice-a-second results flush.
func (h *Hub) board(pollID string, ev map[string]any) {
	h.bmu.Lock()
	q, waiting := h.boards[pollID]
	if n := len(q); n > 0 && q[n-1]["op"] == ev["op"] {
		switch ev["op"] {
		case "add":
			a, _ := q[n-1]["strokes"].([]Stroke)
			b, _ := ev["strokes"].([]Stroke)
			q[n-1]["strokes"] = append(append([]Stroke(nil), a...), b...)
			ev = nil
		case "remove":
			a, _ := q[n-1]["ids"].([]int64)
			b, _ := ev["ids"].([]int64)
			q[n-1]["ids"] = append(append([]int64(nil), a...), b...)
			ev = nil
		}
	}
	if ev != nil {
		q = append(q, ev)
	}
	h.boards[pollID] = q
	h.bmu.Unlock()
	if !waiting {
		time.AfterFunc(boardWindow, func() { h.flushBoard(pollID) })
	}
}

func (h *Hub) flushBoard(pollID string) {
	h.bmu.Lock()
	q := h.boards[pollID]
	delete(h.boards, pollID)
	h.bmu.Unlock()
	at := time.Now().UnixMilli()
	msgs := make([][]byte, 0, len(q))
	for _, ev := range q {
		ev["at"] = at
		msg, _ := json.Marshal(ev)
		msgs = append(msgs, msg)
	}
	h.mu.Lock()
	for _, set := range []map[*client]struct{}{h.audience[pollID], h.presenters[pollID]} {
		for c := range set {
			for _, m := range msgs {
				c.push(m)
			}
		}
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
