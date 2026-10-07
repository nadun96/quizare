package poll

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// TokenHeader carries an anonymous participant's device token.
const TokenHeader = "X-Poll-Token"

// TeacherRoutes mounts poll management under /api/teacher.
func (s *Service) TeacherRoutes(r chi.Router) {
	r.Method("GET", "/polls", httpx.Handler(s.hList))
	r.Method("POST", "/polls", httpx.Handler(s.hCreate))
	r.Method("GET", "/polls/{id}", httpx.Handler(s.hGet))
	r.Method("PUT", "/polls/{id}", httpx.Handler(s.hUpdate))
	r.Method("DELETE", "/polls/{id}", httpx.Handler(s.hDelete))
	r.Method("POST", "/polls/{id}/status", httpx.Handler(s.hStatus))
	r.Method("POST", "/polls/{id}/present", httpx.Handler(s.hPresent))
	r.Method("POST", "/polls/{id}/reset", httpx.Handler(s.hReset))
	r.Method("POST", "/polls/{id}/questions", httpx.Handler(s.hAddQuestion))
	r.Method("PUT", "/polls/{id}/questions/order", httpx.Handler(s.hReorder))
	r.Method("PUT", "/poll-questions/{id}", httpx.Handler(s.hUpdateQuestion))
	r.Method("DELETE", "/poll-questions/{id}", httpx.Handler(s.hDeleteQuestion))
	r.Method("GET", "/polls/{id}/results", httpx.Handler(s.hResults))
	r.Method("GET", "/polls/{id}/export.csv", httpx.Handler(s.hExport))
	r.Method("POST", "/polls/{id}/moderation", httpx.Handler(s.hModerate))
	r.Method("GET", "/polls/{id}/files/{file}", httpx.Handler(s.hFile))
	r.Method("GET", "/polls/{id}/groups", httpx.Handler(s.hGroups))
	r.Method("POST", "/polls/{id}/groups", httpx.Handler(s.hCreateGroup))
	r.Method("POST", "/polls/{id}/groups/generate", httpx.Handler(s.hGenerateGroups))
	r.Method("POST", "/polls/{id}/groups/members", httpx.Handler(s.hAssignMembers))
	r.Method("PATCH", "/poll-groups/{id}", httpx.Handler(s.hUpdateGroup))
	r.Method("DELETE", "/poll-groups/{id}", httpx.Handler(s.hDeleteGroup))
}

func (s *Service) hGroups(w http.ResponseWriter, r *http.Request) error {
	v, err := s.Groups(r.Context(), teacherID(r), chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, v)
	return nil
}

func (s *Service) hCreateGroup(w http.ResponseWriter, r *http.Request) error {
	var in GroupInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	g, err := s.CreateGroup(r.Context(), teacherID(r), chi.URLParam(r, "id"), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 201, g)
	return nil
}

func (s *Service) hGenerateGroups(w http.ResponseWriter, r *http.Request) error {
	var in GenerateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	v, err := s.GenerateGroups(r.Context(), teacherID(r), chi.URLParam(r, "id"), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, v)
	return nil
}

func (s *Service) hAssignMembers(w http.ResponseWriter, r *http.Request) error {
	var in AssignInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.AssignMembers(r.Context(), teacherID(r), chi.URLParam(r, "id"), in); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

func (s *Service) hUpdateGroup(w http.ResponseWriter, r *http.Request) error {
	var in GroupInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	g, err := s.UpdateGroup(r.Context(), teacherID(r), chi.URLParam(r, "id"), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, g)
	return nil
}

func (s *Service) hDeleteGroup(w http.ResponseWriter, r *http.Request) error {
	if err := s.DeleteGroup(r.Context(), teacherID(r), chi.URLParam(r, "id")); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

// PublicRoutes mounts participation under /api/polls; no login is needed
// unless the poll identifies participants.
func (s *Service) PublicRoutes(r chi.Router) {
	r.Method("GET", "/{code}", httpx.Handler(s.hView))
	r.Method("POST", "/{code}/join", httpx.Handler(s.hJoin))
	r.Method("PUT", "/{code}/answers/{question}", httpx.Handler(s.hAnswer))
	r.Method("POST", "/{code}/files/{question}", httpx.Handler(s.hUpload))
}

// WSRoutes mounts the live sockets under /ws.
func (s *Service) WSRoutes(r chi.Router) {
	r.Get("/polls/{code}", s.wsAudience)
	r.Get("/teacher/polls/{id}", s.wsPresenter)
}

func teacherID(r *http.Request) string { return auth.MustUser(r.Context()).ID }

func caller(r *http.Request) Caller {
	c := Caller{Token: strings.TrimSpace(r.Header.Get(TokenHeader))}
	if u, ok := auth.CurrentUser(r.Context()); ok {
		c.User = &u
	}
	return c
}

func (s *Service) hList(w http.ResponseWriter, r *http.Request) error {
	l, err := s.ListPolls(r.Context(), teacherID(r))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, map[string]any{"polls": l})
	return nil
}

func (s *Service) hCreate(w http.ResponseWriter, r *http.Request) error {
	var in PollInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, err := s.CreatePoll(r.Context(), teacherID(r), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 201, p)
	return nil
}

func (s *Service) hGet(w http.ResponseWriter, r *http.Request) error {
	p, err := s.GetPoll(r.Context(), teacherID(r), chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, p)
	return nil
}

func (s *Service) hUpdate(w http.ResponseWriter, r *http.Request) error {
	var in PollInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, err := s.UpdatePoll(r.Context(), teacherID(r), chi.URLParam(r, "id"), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, p)
	return nil
}

func (s *Service) hDelete(w http.ResponseWriter, r *http.Request) error {
	if err := s.DeletePoll(r.Context(), teacherID(r), chi.URLParam(r, "id")); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

func (s *Service) hStatus(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Status string `json:"status"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, err := s.SetStatus(r.Context(), teacherID(r), chi.URLParam(r, "id"), in.Status)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, p)
	return nil
}

func (s *Service) hPresent(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Index           int  `json:"index"`
		Revealed        bool `json:"revealed"`
		AnswersRevealed bool `json:"answers_revealed"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, err := s.Present(r.Context(), teacherID(r), chi.URLParam(r, "id"), in.Index, in.Revealed, in.AnswersRevealed)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, p)
	return nil
}

func (s *Service) hReset(w http.ResponseWriter, r *http.Request) error {
	if err := s.Reset(r.Context(), teacherID(r), chi.URLParam(r, "id")); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

func (s *Service) hAddQuestion(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		QuestionInput
		AddOptions
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	q, _, err := s.AddQuestionAt(r.Context(), teacherID(r), chi.URLParam(r, "id"), in.QuestionInput, in.AddOptions)
	if err != nil {
		return err
	}
	httpx.JSON(w, 201, q)
	return nil
}

func (s *Service) hUpdateQuestion(w http.ResponseWriter, r *http.Request) error {
	var in QuestionInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	q, err := s.UpdateQuestion(r.Context(), teacherID(r), chi.URLParam(r, "id"), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, q)
	return nil
}

func (s *Service) hDeleteQuestion(w http.ResponseWriter, r *http.Request) error {
	if err := s.DeleteQuestion(r.Context(), teacherID(r), chi.URLParam(r, "id")); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

func (s *Service) hReorder(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.Reorder(r.Context(), teacherID(r), chi.URLParam(r, "id"), in.IDs); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

func (s *Service) hResults(w http.ResponseWriter, r *http.Request) error {
	res, err := s.Results(r.Context(), teacherID(r), chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, res)
	return nil
}

func (s *Service) hModerate(w http.ResponseWriter, r *http.Request) error {
	var in ModerationInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.Moderate(r.Context(), teacherID(r), chi.URLParam(r, "id"), in); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

func (s *Service) hFile(w http.ResponseWriter, r *http.Request) error {
	f, err := s.GetFile(r.Context(), teacherID(r), chi.URLParam(r, "id"), chi.URLParam(r, "file"))
	if err != nil {
		return err
	}
	disp := "attachment"
	if inlineSafe(f.ContentType) {
		disp = "inline"
	}
	h := w.Header()
	h.Set("Content-Type", f.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(f.Data)))
	h.Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": f.Name}))
	// Uploaded bytes never run as a page, even if a browser were to sniff them.
	h.Set("Content-Security-Policy", "default-src 'none'; media-src 'self'; img-src 'self'; sandbox")
	h.Set("Cache-Control", "private, no-store")
	_, err = w.Write(f.Data)
	return err
}

// hExport writes one row per participant and one column per question.
func (s *Service) hExport(w http.ResponseWriter, r *http.Request) error {
	p, err := s.GetPoll(r.Context(), teacherID(r), chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	rows, err := s.pool.Query(r.Context(), `SELECT pa.id, pa.user_id, r.question_id, r.value, r.hidden
		FROM poll.participants pa LEFT JOIN poll.responses r ON r.participant_id = pa.id WHERE pa.poll_id=$1 ORDER BY pa.created_at`, p.ID)
	if err != nil {
		return err
	}
	type person struct {
		user    *string
		answers map[string]string
	}
	people := map[string]*person{}
	var order []string
	byID := map[string]Question{}
	for _, q := range p.Questions {
		byID[q.ID] = q
	}
	for rows.Next() {
		var pid string
		var uid, qid *string
		var raw []byte
		var hidden *bool
		if err := rows.Scan(&pid, &uid, &qid, &raw, &hidden); err != nil {
			rows.Close()
			return err
		}
		if people[pid] == nil {
			people[pid] = &person{user: uid, answers: map[string]string{}}
			order = append(order, pid)
		}
		if qid != nil {
			var a Answer
			_ = json.Unmarshal(raw, &a)
			text := describe(byID[*qid], a)
			if hidden != nil && *hidden {
				text = "[hidden] " + text
			}
			people[pid].answers[*qid] = text
		}
	}
	rows.Close()
	var ids []string
	for _, x := range people {
		if x.user != nil {
			ids = append(ids, *x.user)
		}
	}
	users := map[string]auth.User{}
	if len(ids) > 0 {
		if users, err = s.users.UsersByID(r.Context(), ids); err != nil {
			return err
		}
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "poll-" + p.JoinCode + ".csv"}))
	cw := csv.NewWriter(w)
	head := []string{"participant", "name", "email"}
	for i, q := range p.Questions {
		head = append(head, fmt.Sprintf("Q%d %s", i+1, plain(q.Text)))
	}
	_ = cw.Write(head)
	for i, pid := range order {
		x := people[pid]
		row := []string{strconv.Itoa(i + 1), "", ""}
		if x.user != nil {
			row[1], row[2] = users[*x.user].Name, users[*x.user].Email
		}
		for _, q := range p.Questions {
			row = append(row, safeCSV(x.answers[q.ID]))
		}
		_ = cw.Write(row)
	}
	cw.Flush()
	return cw.Error()
}

// safeCSV stops spreadsheet formula injection from participant text.
func safeCSV(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func plain(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > 60 {
		s = string([]rune(s)[:60]) + "…"
	}
	return s
}

func name(cs []Choice, id string) string {
	for _, c := range cs {
		if c.ID == id {
			return c.Text
		}
	}
	return id
}

// describe renders an answer as text for the CSV export.
func describe(q Question, a Answer) string {
	b := q.Body
	join := func(m map[string]string, left, right []Choice) string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			l, r := k, m[k]
			if left != nil {
				l = name(left, k)
			}
			if right != nil {
				r = name(right, m[k])
			}
			parts = append(parts, l+" → "+r)
		}
		return strings.Join(parts, "; ")
	}
	switch q.Type {
	case Single, Multi:
		var parts []string
		for _, id := range a.Selected {
			parts = append(parts, name(b.Options, id))
		}
		return strings.Join(parts, "; ")
	case Match:
		return join(a.Pairs, b.Left, b.Right)
	case BlankOpt:
		return join(a.Blanks, nil, b.Options)
	case BlankText:
		return join(a.Blanks, nil, nil)
	case Drag:
		if len(b.Zones) == 0 {
			var parts []string
			for _, id := range a.Order {
				parts = append(parts, name(b.Options, id))
			}
			return strings.Join(parts, " > ")
		}
		return join(a.Pairs, b.Options, b.Zones)
	case Essay, ShortText, Code:
		if a.Text != nil {
			return *a.Text
		}
	case WordCloud:
		return strings.Join(a.Words, "; ")
	case Number, Slider, Rating:
		if a.Number != nil {
			return strconv.FormatFloat(*a.Number, 'f', -1, 64)
		}
	case Date:
		return a.Date
	case Time:
		return a.Time
	case Likert:
		labels := map[string]string{}
		for k, v := range a.Rows {
			n, _ := strconv.Atoi(v)
			if n >= 1 && n <= len(b.Scale) {
				labels[k] = b.Scale[n-1]
			}
		}
		return join(labels, b.Rows, nil)
	case Matrix:
		switch b.Mode {
		case "multi":
			m := map[string]string{}
			for row, cols := range a.Multi {
				var names []string
				for _, c := range cols {
					names = append(names, name(b.Columns, c))
				}
				m[row] = strings.Join(names, ", ")
			}
			return join(m, b.Rows, nil)
		case "text":
			m := map[string]string{}
			for k, v := range a.Cells {
				row, col, _ := strings.Cut(k, "|")
				m[name(b.Rows, row)+" / "+name(b.Columns, col)] = v
			}
			return join(m, nil, nil)
		}
		return join(a.Rows, b.Rows, b.Columns)
	case File, Audio, Video:
		if a.File != nil {
			return a.File.Name
		}
	}
	return ""
}

func (s *Service) hView(w http.ResponseWriter, r *http.Request) error {
	v, err := s.View(r.Context(), chi.URLParam(r, "code"), caller(r))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, v)
	return nil
}

func (s *Service) hJoin(w http.ResponseWriter, r *http.Request) error {
	var in JoinInput
	if r.ContentLength != 0 {
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
	}
	res, err := s.Join(r.Context(), chi.URLParam(r, "code"), caller(r), httpx.ClientIP(r), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, res)
	return nil
}

func (s *Service) hAnswer(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Value Answer `json:"value"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	res, err := s.Answer(r.Context(), chi.URLParam(r, "code"), chi.URLParam(r, "question"), caller(r), in.Value)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, res)
	return nil
}

// hUpload takes the raw file as the body; the file name comes URL-encoded
// in X-File-Name.
func (s *Service) hUpload(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, VideoMaxBytes+1024)
	name, _ := url.QueryUnescape(r.Header.Get("X-File-Name"))
	ref, err := s.Upload(r.Context(), chi.URLParam(r, "code"), chi.URLParam(r, "question"), caller(r), name, r.Header.Get("Content-Type"), r.Body)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, map[string]any{"value": Answer{File: &ref}})
	return nil
}

// ---------- sockets ----------

const (
	readTimeout  = 45 * time.Second
	writeTimeout = 10 * time.Second
)

func (s *Service) accept(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	u, _ := url.Parse(s.baseURL)
	return websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{u.Host}})
}

// pump writes queued messages and answers pings until the socket closes.
func (s *Service) pump(ctx context.Context, conn *websocket.Conn, c *client) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-c.send:
				wctx, cancel := context.WithTimeout(ctx, writeTimeout)
				err := conn.Write(wctx, websocket.MessageText, msg)
				cancel()
				if err != nil {
					c.cancel()
					return
				}
			}
		}
	}()
	for {
		rctx, cancel := context.WithTimeout(ctx, readTimeout)
		_, data, err := conn.Read(rctx)
		cancel()
		if err != nil {
			c.cancel()
			return
		}
		var in struct {
			Type string `json:"type"`
			T    int64  `json:"t"`
		}
		if len(data) <= 512 && json.Unmarshal(data, &in) == nil && in.Type == "ping" {
			msg, _ := json.Marshal(map[string]any{"type": "pong", "t": in.T, "server_time": s.now().UnixMilli()})
			c.push(msg)
		}
	}
}

// wsAudience streams shared updates to participants. It carries no identity:
// answers go over REST, so the socket is read-only.
func (s *Service) wsAudience(w http.ResponseWriter, r *http.Request) {
	p, err := s.pollByCode(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	conn, err := s.accept(w, r)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newClient(cancel)
	s.hub.addAudience(p.ID, c)
	defer s.hub.removeAudience(p.ID, c)
	if full, err := s.pollByID(ctx, p.ID); err == nil {
		if u, err := s.publicUpdate(ctx, full); err == nil {
			msg, _ := json.Marshal(u)
			c.push(msg)
		}
	}
	s.pump(ctx, conn, c)
}

// wsPresenter streams full results to the poll's owner.
func (s *Service) wsPresenter(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.CurrentUser(r.Context())
	if !ok || u.Role != auth.RoleTeacher {
		httpx.WriteError(w, r, httpx.ErrUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	res, err := s.Results(r.Context(), u.ID, id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	conn, err := s.accept(w, r)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newClient(cancel)
	s.hub.addPresenter(id, c)
	defer s.hub.removePresenter(id, c)
	msg, _ := json.Marshal(res)
	c.push(msg)
	s.pump(ctx, conn, c)
}
