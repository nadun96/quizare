package live

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

const (
	readTimeout  = 45 * time.Second // clients send a ping every ~10 s
	writeTimeout = 10 * time.Second
	timeEvery    = 15 * time.Second
)

// WSRoutes mounts the WebSocket endpoints; the auth middleware must run first.
func (s *Service) WSRoutes(r chi.Router) {
	r.Get("/attempts/{id}", s.wsStudent)
	r.Get("/sessions/{id}", s.wsTeacher)
}

func (s *Service) accept(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	u, _ := url.Parse(s.baseURL)
	// coder/websocket rejects cross-origin upgrades unless allowed here (ADR-13: check Origin).
	return websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{u.Host}})
}

// run pumps outbound messages and reads inbound ones until the socket closes.
func run(ctx context.Context, conn *websocket.Conn, c *client, onMessage func(context.Context, []byte), now func() time.Time) {
	go func() {
		t := time.NewTicker(timeEvery)
		defer t.Stop()
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
			case <-t.C:
				msg, _ := json.Marshal(map[string]any{"type": "time", "server_time": now().UnixMilli()})
				c.push(msg)
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
		if len(data) > 4096 {
			continue
		}
		onMessage(ctx, data)
	}
}

type inbound struct {
	Type     string `json:"type"`
	T        int64  `json:"t"`         // ping: client clock, echoed back for offset measurement
	Kind     string `json:"kind"`      // violation kind
	ClientTS int64  `json:"client_ts"` // violation client time
}

func pong(c *client, in inbound, now func() time.Time) {
	msg, _ := json.Marshal(map[string]any{"type": "pong", "t": in.T, "server_time": now().UnixMilli()})
	c.push(msg)
}

func (s *Service) wsStudent(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.CurrentUser(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.ErrUnauthorized)
		return
	}
	attemptID := chi.URLParam(r, "id")
	if _, err := s.StudentState(r.Context(), u.ID, attemptID); err != nil {
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
	s.hub.addStudent(attemptID, c)
	s.connected(ctx, attemptID)
	s.hub.markDirty(s.sessionIDOf(ctx, attemptID))
	if st, err := s.StudentState(ctx, u.ID, attemptID); err == nil {
		msg, _ := json.Marshal(st)
		c.push(msg)
	}
	device := r.UserAgent()
	run(ctx, conn, c, func(ctx context.Context, data []byte) {
		var in inbound
		if json.Unmarshal(data, &in) != nil {
			return
		}
		switch in.Type {
		case "ping":
			pong(c, in, s.now)
		case "start":
			if _, err := s.Start(ctx, u.ID, attemptID); err != nil {
				s.sendError(c, err)
			}
		case "violation":
			if _, err := s.ReportViolation(ctx, u.ID, attemptID, ViolationInput{Kind: in.Kind, ClientTS: in.ClientTS, Device: device}); err != nil {
				s.sendError(c, err)
			}
		}
	}, s.now)
	if s.hub.removeStudent(attemptID, c) == 0 {
		s.disconnected(context.Background(), attemptID)
	}
	s.hub.markDirty(s.sessionIDOf(context.Background(), attemptID))
}

func (s *Service) sessionIDOf(ctx context.Context, attemptID string) string {
	var id string
	_ = s.pool.QueryRow(ctx, `SELECT session_id FROM live.attempts WHERE id=$1`, attemptID).Scan(&id)
	return id
}

func (s *Service) sendError(c *client, err error) {
	var he *httpx.Error
	if !errors.As(err, &he) {
		he = httpx.NewError(500, "internal", "something went wrong")
	}
	msg, _ := json.Marshal(map[string]any{"type": "error", "code": he.Code, "message": he.Message})
	c.push(msg)
}

func (s *Service) wsTeacher(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.CurrentUser(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.ErrUnauthorized)
		return
	}
	sessionID := chi.URLParam(r, "id")
	d, err := s.Dashboard(r.Context(), u.ID, sessionID)
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
	s.hub.addTeacher(sessionID, c)
	defer s.hub.removeTeacher(sessionID, c)
	msg, _ := json.Marshal(d)
	c.push(msg)
	run(ctx, conn, c, func(ctx context.Context, data []byte) {
		var in inbound
		if json.Unmarshal(data, &in) == nil && in.Type == "ping" {
			pong(c, in, s.now)
		}
	}, s.now)
}
