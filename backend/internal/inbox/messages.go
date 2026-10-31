package inbox

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

// Conversation is a teacher's and a student's messages, seen from one side.
type Conversation struct {
	ID       string    `json:"id"`
	With     Person    `json:"with"`
	Last     string    `json:"last"` // the newest message, shortened
	LastMine bool      `json:"last_mine"`
	LastAt   time.Time `json:"last_at"`
	Unread   int       `json:"unread"`
	CanReply bool      `json:"can_reply"` // false once the student left the teacher's classrooms
}

// Message is one message.
type Message struct {
	ID        string    `json:"id"`
	Mine      bool      `json:"mine"`
	Sender    string    `json:"sender"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

var errNotYours = httpx.NewError(http.StatusForbidden, "not_your_class", "you can only message your own teachers and the students of your classrooms")

// sides gives a conversation's column for me and for the other person.
func sides(u auth.User) (me, other string, err error) {
	switch u.Role {
	case auth.RoleTeacher:
		return "teacher", "student", nil
	case auth.RoleStudent:
		return "student", "teacher", nil
	}
	return "", "", httpx.ErrForbidden
}

// conversationCols needs $1 = me, with c and o (the other person) joined.
func conversationCols(me string) string {
	return `SELECT c.id, o.id, o.name, coalesce(o.avatar_version, ''),
		coalesce((SELECT m.body FROM inbox.messages m WHERE m.conversation_id = c.id ORDER BY m.created_at DESC, m.id DESC LIMIT 1), ''),
		coalesce((SELECT m.sender_id = $1 FROM inbox.messages m WHERE m.conversation_id = c.id ORDER BY m.created_at DESC, m.id DESC LIMIT 1), false),
		c.last_at,
		(SELECT count(*) FROM inbox.messages m WHERE m.conversation_id = c.id AND m.sender_id IS DISTINCT FROM $1 AND m.created_at > coalesce(c.` + me + `_read_at, '-infinity'))::int,
		EXISTS (SELECT 1 FROM content.classrooms k JOIN content.enrolments e ON e.classroom_id = k.id
			WHERE k.teacher_id = c.teacher_id AND e.user_id = c.student_id AND e.status = 'active' AND k.archived_at IS NULL)`
}

func scanConversation(r pgx.Row) (Conversation, error) {
	var c Conversation
	err := r.Scan(&c.ID, &c.With.ID, &c.With.Name, &c.With.Avatar, &c.Last, &c.LastMine, &c.LastAt, &c.Unread, &c.CanReply)
	c.Last = excerpt(c.Last, 120)
	return c, notFound(err)
}

// Conversations lists a person's conversations, the latest first.
func (s *Service) Conversations(ctx context.Context, u auth.User, p page.Request) ([]Conversation, int, error) {
	me, other, err := sides(u)
	if err != nil {
		return []Conversation{}, 0, nil
	}
	from := ` FROM inbox.conversations c JOIN auth.users o ON o.id = c.` + other + `_id WHERE c.` + me + `_id = $1
		AND EXISTS (SELECT 1 FROM inbox.messages m WHERE m.conversation_id = c.id) AND ($2 = '' OR o.name ILIKE $2)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+from, u.ID, p.Like()).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, conversationCols(me)+from+` ORDER BY c.last_at DESC, c.id`+p.Limit(), u.ID, p.Like())
	if err != nil {
		return nil, 0, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Conversation, error) { return scanConversation(r) })
	if out == nil {
		out = []Conversation{}
	}
	return out, total, err
}

// Conversation is one of the person's conversations.
func (s *Service) Conversation(ctx context.Context, u auth.User, id string) (Conversation, error) {
	me, other, err := sides(u)
	if err != nil {
		return Conversation{}, err
	}
	return scanConversation(s.pool.QueryRow(ctx, conversationCols(me)+` FROM inbox.conversations c JOIN auth.users o ON o.id = c.`+other+`_id
		WHERE c.`+me+`_id = $1 AND c.id::text = $2`, u.ID, id))
}

// Messages pages a conversation's messages, the newest first.
func (s *Service) Messages(ctx context.Context, u auth.User, id string, p page.Request) ([]Message, int, error) {
	c, err := s.Conversation(ctx, u, id)
	if err != nil {
		return nil, 0, err
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM inbox.messages WHERE conversation_id = $1`, c.ID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT m.id, m.sender_id = $2, coalesce(u.name, ''), m.body, m.created_at FROM inbox.messages m
		LEFT JOIN auth.users u ON u.id = m.sender_id WHERE m.conversation_id = $1 ORDER BY m.created_at DESC, m.id DESC`+p.Limit(), c.ID, u.ID)
	if err != nil {
		return nil, 0, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Message, error) {
		var m Message
		var mine *bool
		err := r.Scan(&m.ID, &mine, &m.Sender, &m.Body, &m.CreatedAt)
		m.Mine = mine != nil && *mine
		return m, err
	})
	if out == nil {
		out = []Message{}
	}
	return out, total, err
}

// ReadConversation marks a conversation read, up to now.
func (s *Service) ReadConversation(ctx context.Context, u auth.User, id string) error {
	me, _, err := sides(u)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE inbox.conversations SET `+me+`_read_at = now() WHERE id::text = $1 AND `+me+`_id = $2`, id, u.ID)
	if err == nil && tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return err
}

// Start finds or opens the conversation with someone and sends the first
// message (or the next, if it already exists).
func (s *Service) Start(ctx context.Context, u auth.User, withID, body string) (Conversation, Message, error) {
	me, _, err := sides(u)
	if err != nil {
		return Conversation{}, Message{}, err
	}
	teacher, student := u.ID, withID
	if me == "student" {
		teacher, student = withID, u.ID
	}
	ok, err := s.teaches(ctx, teacher, student)
	if err != nil {
		return Conversation{}, Message{}, notFound(err)
	}
	if !ok {
		return Conversation{}, Message{}, errNotYours
	}
	var id string
	if err := s.pool.QueryRow(ctx, `INSERT INTO inbox.conversations (teacher_id, student_id) VALUES ($1, $2)
		ON CONFLICT (teacher_id, student_id) DO UPDATE SET teacher_id = excluded.teacher_id RETURNING id`, teacher, student).Scan(&id); err != nil {
		return Conversation{}, Message{}, err
	}
	m, err := s.Send(ctx, u, id, body)
	if err != nil {
		return Conversation{}, Message{}, err
	}
	c, err := s.Conversation(ctx, u, id)
	return c, m, err
}

// Send adds a message to a conversation, while the two still share a
// classroom. The other person gets an email for the first message they
// haven't read, not for every one, unless they turned email off.
func (s *Service) Send(ctx context.Context, u auth.User, id, body string) (Message, error) {
	c, err := s.Conversation(ctx, u, id)
	if err != nil {
		return Message{}, err
	}
	if !c.CanReply {
		return Message{}, errNotYours
	}
	if body, err = text("body", body, maxMessage); err != nil {
		return Message{}, err
	}
	me, other, _ := sides(u)
	m := Message{Mine: true, Sender: u.Name, Body: body}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var waiting bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM inbox.messages m JOIN inbox.conversations c ON c.id = m.conversation_id
			WHERE c.id = $1 AND m.sender_id = $2 AND m.created_at > coalesce(c.`+other+`_read_at, '-infinity'))`, c.ID, u.ID).Scan(&waiting); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO inbox.messages (conversation_id, sender_id, body) VALUES ($1, $2, $3) RETURNING id, created_at`,
			c.ID, u.ID, body).Scan(&m.ID, &m.CreatedAt); err != nil {
			return err
		}
		// Sending reads the conversation for the sender.
		if _, err := tx.Exec(ctx, `UPDATE inbox.conversations SET last_at = $2, `+me+`_read_at = $2 WHERE id = $1`, c.ID, m.CreatedAt); err != nil {
			return err
		}
		if waiting {
			return nil
		}
		to, err := wantsEmail(ctx, tx, []string{c.With.ID})
		if err != nil || len(to) == 0 {
			return err
		}
		return s.queue(ctx, tx, mail.Message{To: to[0].email, Subject: "New message from " + u.Name,
			Body: fmt.Sprintf("Hi %s,\n\n%s sent you a message:\n\n%s\n\nReply in your inbox: %s/inbox?c=%s\n\n--\nYou get one email until you read it, not one for every message. You can turn these emails off in your inbox.", to[0].name, u.Name, body, s.baseURL, c.ID)})
	})
	return m, err
}

// Contacts lists who a person can message: a student's teachers, a
// teacher's students (of one classroom, if given), with the classrooms they
// share.
func (s *Service) Contacts(ctx context.Context, u auth.User, classroomID string, p page.Request) ([]Person, int, error) {
	var from string
	switch u.Role {
	case auth.RoleStudent:
		from = ` FROM content.enrolments e JOIN content.classrooms k ON k.id = e.classroom_id AND k.archived_at IS NULL
			JOIN auth.users o ON o.id = k.teacher_id AND o.status = 'active'
			WHERE e.user_id = $1 AND e.status = 'active'`
	case auth.RoleTeacher:
		from = ` FROM content.classrooms k JOIN content.enrolments e ON e.classroom_id = k.id AND e.status = 'active'
			JOIN auth.users o ON o.id = e.user_id AND o.status = 'active'
			WHERE k.teacher_id = $1 AND k.archived_at IS NULL`
	default:
		return []Person{}, 0, nil
	}
	from += ` AND ($2 = '' OR k.id::text = $2) AND ($3 = '' OR o.name ILIKE $3 OR o.email ILIKE $3 OR e.student_number ILIKE $3)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(DISTINCT o.id)`+from, u.ID, classroomID, p.Like()).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT o.id, o.name, coalesce(o.avatar_version, ''), string_agg(DISTINCT k.name, ', ')`+from+
		` GROUP BY o.id ORDER BY lower(o.name), o.id`+p.Limit(), u.ID, classroomID, p.Like())
	if err != nil {
		return nil, 0, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Person, error) {
		var x Person
		return x, r.Scan(&x.ID, &x.Name, &x.Avatar, &x.Classrooms)
	})
	if out == nil {
		out = []Person{}
	}
	return out, total, err
}
