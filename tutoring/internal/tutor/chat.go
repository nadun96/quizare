package tutor

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Chat (TS-FR-40 to TS-FR-45). Messages are plain text; the page never
// renders them as HTML (TS-FR-43, ADR-16).

type Message struct {
	ID          int64      `json:"id"`
	UserID      string     `json:"user_id"`
	Name        string     `json:"name"`
	FromTeacher bool       `json:"from_teacher"`
	Audience    string     `json:"audience"` // everyone, teachers (a student's question), one (a teacher's private reply)
	ToUser      *string    `json:"to_user,omitempty"`
	Text        string     `json:"text"`
	CreatedAt   time.Time  `json:"created_at"`
	DeletedAt   *time.Time `json:"-"`
}

const messageCols = `id, user_id, name, from_teacher, audience, to_user, text, created_at, deleted_at`

func scanMessage(row pgx.Row) (Message, error) {
	var m Message
	err := row.Scan(&m.ID, &m.UserID, &m.Name, &m.FromTeacher, &m.Audience, &m.ToUser, &m.Text, &m.CreatedAt, &m.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, web.ErrNotFound
	}
	return m, err
}

func (s *Service) message(ctx context.Context, id int64) (Message, error) {
	return scanMessage(s.pool.QueryRow(ctx, `SELECT `+messageCols+` FROM tutoring.messages WHERE id=$1`, id))
}

// visibleTo says whether a participant sees a message.
func (m Message) visibleTo(userID, role string) bool {
	switch {
	case m.Audience == "everyone", role == "teacher", role == "coteacher", m.UserID == userID:
		return true
	case m.Audience == "one":
		return m.ToUser != nil && *m.ToUser == userID
	}
	return false
}

var (
	errChatOff     = web.NewError(http.StatusForbidden, "chat_off", "chat is off")
	errChatTeacher = web.NewError(http.StatusForbidden, "chat_announcements", "only the teacher can write in the chat now")
	errChatMuted   = web.NewError(http.StatusForbidden, "chat_muted", "the teacher has muted you in the chat")
)

type PostInput struct {
	Text string `json:"text"`
	// To: a teacher's private reply to one student (TS-FR-41).
	To string `json:"to,omitempty"`
}

// Post sends a message within the session's chat mode.
func (s *Service) Post(ctx context.Context, p authn.Person, id string, in PostInput) (Message, error) {
	sess, err := s.session(ctx, id)
	if err != nil {
		return Message{}, err
	}
	me, err := s.participant(ctx, id, p.ID)
	if err != nil {
		return Message{}, err
	}
	if me.State != "admitted" || sess.Status == "ended" {
		return Message{}, web.ErrForbidden
	}
	text := strings.TrimSpace(in.Text)
	if n := len([]rune(text)); n < 1 || n > 1000 {
		return Message{}, web.Invalid(map[string]string{"text": "write 1 to 1,000 characters"})
	}
	audience := "everyone"
	var to *string
	now := s.now()
	if me.Staff() {
		if in.To != "" {
			if _, err := s.participant(ctx, id, in.To); err != nil {
				return Message{}, err
			}
			audience, to = "one", &in.To
		}
	} else {
		if in.To != "" {
			return Message{}, web.NewError(http.StatusForbidden, "forbidden", "students can't send private messages")
		}
		switch {
		case me.ChatMuted:
			return Message{}, errChatMuted
		case sess.ChatMode == "off":
			return Message{}, errChatOff
		case sess.ChatMode == "announcements":
			return Message{}, errChatTeacher
		case sess.ChatMode == "to_teacher":
			audience = "teachers"
		}
		var last *time.Time
		if err := s.pool.QueryRow(ctx, `UPDATE tutoring.participants SET last_chat_at=$3 WHERE session_id=$1 AND user_id=$2
			AND (last_chat_at IS NULL OR last_chat_at <= $3::timestamptz - make_interval(secs => $4::int)) RETURNING last_chat_at`, id, p.ID, now, sess.SlowSeconds).Scan(&last); errors.Is(err, pgx.ErrNoRows) {
			return Message{}, web.NewError(http.StatusTooManyRequests, "slow_mode", "slow mode is on: wait a moment before your next message")
		} else if err != nil {
			return Message{}, err
		}
	}
	m, err := scanMessage(s.pool.QueryRow(ctx, `INSERT INTO tutoring.messages (session_id, user_id, name, from_teacher, audience, to_user, text, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+messageCols, id, p.ID, me.Name, me.Staff(), audience, to, text, now))
	if err != nil {
		return m, err
	}
	s.hub.chat(id, m)
	return m, nil
}

// History returns the newest messages the caller may see, older than
// before (0 = from the newest), oldest first, so late joiners catch up (TS-FR-44).
func (s *Service) History(ctx context.Context, p authn.Person, id string, before int64, limit int) ([]Message, error) {
	me, err := s.participant(ctx, id, p.ID)
	if err != nil {
		return nil, err
	}
	if me.State != "admitted" {
		return nil, web.ErrForbidden
	}
	if limit < 1 || limit > 200 {
		limit = 100
	}
	if before <= 0 {
		before = 1 << 62
	}
	rows, _ := s.pool.Query(ctx, `SELECT `+messageCols+` FROM tutoring.messages WHERE session_id=$1 AND id < $2 AND deleted_at IS NULL
		AND ($3 OR audience='everyone' OR user_id=$4 OR to_user=$4) ORDER BY id DESC LIMIT $5`, id, before, me.Staff(), p.ID, limit)
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Message, error) { return scanMessage(r) })
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, err
}

// DeleteMessage removes a message for everyone (TS-FR-42).
func (s *Service) DeleteMessage(ctx context.Context, p authn.Person, id string, msgID int64) error {
	if _, err := s.staffed(ctx, p, id); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE tutoring.messages SET deleted_at=$3 WHERE session_id=$1 AND id=$2 AND deleted_at IS NULL`, id, msgID, s.now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return web.ErrNotFound
	}
	s.logAction(ctx, id, p.ID, "message_deleted", nil, map[string]int64{"message": msgID})
	s.hub.broadcast(id, event{Type: "chat_deleted", Data: map[string]int64{"id": msgID}})
	s.hub.changed(id) // the pinned message may be gone
	return nil
}

// Pin pins one message everyone can see, or unpins (msgID 0) (TS-FR-45).
func (s *Service) Pin(ctx context.Context, p authn.Person, id string, msgID int64) error {
	if _, err := s.staffed(ctx, p, id); err != nil {
		return err
	}
	var pinned *int64
	if msgID != 0 {
		m, err := s.message(ctx, msgID)
		if err != nil {
			return err
		}
		if m.Audience != "everyone" || m.DeletedAt != nil {
			return web.Invalid(map[string]string{"id": "pin a message everyone can see"})
		}
		pinned = &msgID
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET pinned_id=$2 WHERE id=$1`, id, pinned); err != nil {
		return err
	}
	s.hub.changed(id)
	return nil
}

// MuteChat stops or lets again one student write in the chat (TS-FR-42).
func (s *Service) MuteChat(ctx context.Context, p authn.Person, id, userID string, muted bool) error {
	if _, err := s.staffed(ctx, p, id); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE tutoring.participants SET chat_muted=$3 WHERE session_id=$1 AND user_id=$2 AND role='student'`, id, userID, muted)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return web.ErrNotFound
	}
	s.logAction(ctx, id, p.ID, "chat_muted", &userID, map[string]bool{"muted": muted})
	s.hub.changed(id)
	return nil
}
