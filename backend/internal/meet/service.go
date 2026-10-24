// Package meet runs peer-to-peer meetings (D-69): small video calls whose
// audio, video and screens go directly between the browsers (WebRTC, a full
// mesh), so they need no media server. The server stores the meeting and who
// joined, checks who may join, and relays only the connection set-up
// messages (offers, answers, ICE candidates) between the people in a room.
// A mesh sends every stream to every other person, so a meeting holds at
// most 8 people (6 by default); bigger classes use tutoring sessions (D-59).
package meet

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

const (
	StatusOpen  = "open"
	StatusEnded = "ended"
	// DefaultMax is a new meeting's size when the host doesn't choose; the
	// admin's limit (D-72) bounds it, since every person sends their streams
	// to every other person.
	DefaultMax = 6
	turnTTL    = 6 * time.Hour
)

type Meeting struct {
	ID          string     `json:"id"`
	HostID      string     `json:"host_id"`
	HostName    string     `json:"host_name,omitempty"`
	Title       string     `json:"title"`
	Code        string     `json:"code"`
	ClassroomID *string    `json:"classroom_id"`
	MaxPeople   int        `json:"max_people"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	// Online is how many are in the meeting right now.
	Online int `json:"online"`
}

// ICEServer is what RTCPeerConnection takes.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// Classrooms is the slice of the content module meetings need.
type Classrooms interface {
	GetClassroom(ctx context.Context, teacherID, id string) (content.Classroom, error)
	GetEnrolment(ctx context.Context, classroomID, userID string) (content.Enrolment, bool, error)
}

// Users gives names for the people in a room.
type Users interface {
	GetUser(ctx context.Context, id string) (auth.User, error)
}

type Service struct {
	pool       *pgxpool.Pool
	classrooms Classrooms
	users      Users
	baseURL    string
	stun       []string
	turnURLs   []string
	turnSecret []byte
	log        *slog.Logger
	hub        *hub
	now        func() time.Time
}

// Options are the ICE settings (STUN, and TURN with a coturn shared secret).
type Options struct {
	STUN           []string
	TURNURLs       []string
	TURNSecretFile string
}

func NewService(pool *pgxpool.Pool, classrooms Classrooms, users Users, baseURL string, opt Options, log *slog.Logger) (*Service, error) {
	s := &Service{pool: pool, classrooms: classrooms, users: users, baseURL: strings.TrimRight(baseURL, "/"), stun: opt.STUN, turnURLs: opt.TURNURLs, log: log, now: time.Now}
	if len(opt.TURNURLs) > 0 {
		if opt.TURNSecretFile == "" {
			return nil, errors.New("QP_MEET_TURN_URLS needs QP_MEET_TURN_SECRET_FILE")
		}
		raw, err := os.ReadFile(opt.TURNSecretFile)
		if err != nil {
			return nil, fmt.Errorf("TURN secret: %w", err)
		}
		s.turnSecret = []byte(strings.TrimSpace(string(raw)))
		if len(s.turnSecret) < 16 {
			return nil, errors.New("the TURN secret is too short (16 characters at least)")
		}
	}
	s.hub = newHub(s)
	return s, nil
}

// ICEServers for one person: the STUN servers, and the TURN relay with
// credentials that expire (coturn's use-auth-secret: the username is
// "expiry:user", the password its HMAC-SHA1 under the shared secret).
func (s *Service) ICEServers(userID string) []ICEServer {
	var out []ICEServer
	if len(s.stun) > 0 {
		out = append(out, ICEServer{URLs: s.stun})
	}
	if len(s.turnURLs) > 0 {
		user := fmt.Sprintf("%d:%s", s.now().Add(turnTTL).Unix(), userID)
		m := hmac.New(sha1.New, s.turnSecret)
		m.Write([]byte(user))
		out = append(out, ICEServer{URLs: s.turnURLs, Username: user, Credential: base64.StdEncoding.EncodeToString(m.Sum(nil))})
	}
	return out
}

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newCode() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b)
}

// MeetingInput creates a meeting.
type MeetingInput struct {
	Title       string  `json:"title"`
	ClassroomID *string `json:"classroom_id"`
	// MaxPeople: nil takes the default; 0 is no limit of the meeting's own,
	// when the admin allows that.
	MaxPeople *int `json:"max_people"`
}

// Limits are what an admin allows (D-72); 0 means unlimited.
type Limits struct {
	MaxPeople int `json:"max_people"` // people in one meeting
	MaxOpen   int `json:"max_open"`   // open meetings per teacher
}

// Limits returns the admin's limits.
func (s *Service) Limits(ctx context.Context) (Limits, error) {
	var l Limits
	err := s.pool.QueryRow(ctx, `SELECT max_people, max_open FROM meet.limits`).Scan(&l.MaxPeople, &l.MaxOpen)
	return l, err
}

// SetLimits saves them. Lowering the size applies to open meetings at once.
func (s *Service) SetLimits(ctx context.Context, actorID string, l Limits) (Limits, error) {
	f := map[string]string{}
	if l.MaxPeople < 0 || l.MaxPeople == 1 || l.MaxPeople > 1000 {
		f["max_people"] = "0 for no limit, or 2 to 1000 people"
	}
	if l.MaxOpen < 0 || l.MaxOpen > 1000 {
		f["max_open"] = "0 for no limit, or up to 1000 meetings"
	}
	if len(f) > 0 {
		return Limits{}, httpx.Invalid(f)
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE meet.limits SET max_people=$1, max_open=$2, updated_at=now(), updated_by=$3`, l.MaxPeople, l.MaxOpen, actorID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "meeting_limits_changed", "meeting_limits", "limits", l)
	})
	return l, err
}

// capOf is how many people a meeting takes now: its own size, within the
// admin's limit (0 = no limit).
func capOf(own, limit int) int {
	switch {
	case limit == 0:
		return own
	case own == 0 || own > limit:
		return limit
	}
	return own
}

const cols = `m.id, m.host_id, coalesce(u.name, ''), m.title, m.code, m.classroom_id, m.max_people, m.status, m.created_at, m.ended_at`
const from = ` FROM meet.meetings m LEFT JOIN auth.users u ON u.id = m.host_id`

func (s *Service) scan(r pgx.Row) (Meeting, error) {
	var m Meeting
	err := r.Scan(&m.ID, &m.HostID, &m.HostName, &m.Title, &m.Code, &m.ClassroomID, &m.MaxPeople, &m.Status, &m.CreatedAt, &m.EndedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, httpx.ErrNotFound
	}
	m.Online = s.hub.count(m.ID)
	return m, err
}

// Create opens a meeting for its host (a teacher).
func (s *Service) Create(ctx context.Context, host auth.User, in MeetingInput) (Meeting, error) {
	in.Title = strings.Join(strings.Fields(in.Title), " ")
	f := map[string]string{}
	if n := len([]rune(in.Title)); n < 1 || n > 120 {
		f["title"] = "give the meeting a title (at most 120 characters)"
	}
	lim, err := s.Limits(ctx)
	if err != nil {
		return Meeting{}, err
	}
	size := DefaultMax
	if lim.MaxPeople > 0 && lim.MaxPeople < size {
		size = lim.MaxPeople
	}
	if in.MaxPeople != nil {
		size = *in.MaxPeople
	}
	switch {
	case lim.MaxPeople > 0 && (size < 2 || size > lim.MaxPeople):
		f["max_people"] = fmt.Sprintf("a meeting here holds 2 to %d people", lim.MaxPeople)
	case lim.MaxPeople == 0 && (size == 1 || size < 0 || size > 1000):
		f["max_people"] = "2 to 1000 people, or 0 for no limit"
	}
	if in.ClassroomID != nil && *in.ClassroomID == "" {
		in.ClassroomID = nil
	}
	if in.ClassroomID != nil {
		if _, err := s.classrooms.GetClassroom(ctx, host.ID, *in.ClassroomID); err != nil {
			f["classroom_id"] = "choose one of your classrooms"
		}
	}
	if len(f) > 0 {
		return Meeting{}, httpx.Invalid(f)
	}
	if lim.MaxOpen > 0 {
		var open int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM meet.meetings WHERE host_id=$1 AND status='open'`, host.ID).Scan(&open); err != nil {
			return Meeting{}, err
		}
		if open >= lim.MaxOpen {
			return Meeting{}, httpx.NewError(http.StatusConflict, "too_many_meetings", fmt.Sprintf("you have %d open meetings, the most allowed here; end one first", open))
		}
	}
	var id string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for range 5 {
			err := tx.QueryRow(ctx, `INSERT INTO meet.meetings(host_id, title, code, classroom_id, max_people) VALUES ($1,$2,$3,$4,$5)
				ON CONFLICT (code) DO NOTHING RETURNING id`, host.ID, in.Title, newCode(), in.ClassroomID, size).Scan(&id)
			if err == nil {
				return audit.Log(ctx, tx, host.ID, "meeting_created", "meeting", id, nil)
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		return errors.New("could not make a meeting code")
	})
	if err != nil {
		return Meeting{}, err
	}
	return s.scan(s.pool.QueryRow(ctx, `SELECT `+cols+from+` WHERE m.id=$1`, id))
}

// Sorts are the orders of a host's meeting list.
var Sorts = page.Sorts{"created": "m.created_at", "title": "lower(m.title)", "status": "m.status"}

// List returns one page of a host's meetings.
func (s *Service) List(ctx context.Context, hostID string, p page.Request) ([]Meeting, int, error) {
	where := from + ` WHERE m.host_id=$1 AND ($2 = '' OR m.title ILIKE $2 OR m.code ILIKE $2)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, hostID, p.Like()).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+cols+where+p.OrderBy(Sorts, "m.id")+p.Limit(), hostID, p.Like())
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Meeting, error) { return s.scan(r) })
	return list, total, err
}

func (s *Service) byCode(ctx context.Context, code string) (Meeting, error) {
	return s.scan(s.pool.QueryRow(ctx, `SELECT `+cols+from+` WHERE m.code=$1`, strings.ToUpper(strings.TrimSpace(code))))
}

var (
	errEnded      = httpx.NewError(http.StatusGone, "meeting_ended", "this meeting has ended")
	errNotInClass = httpx.NewError(http.StatusForbidden, "not_in_classroom", "this meeting is for the students of one classroom")
	errFull       = httpx.NewError(http.StatusConflict, "meeting_full", "the meeting is full")
)

// Access is what a person may do in a meeting they're joining.
type Access struct {
	Meeting Meeting     `json:"meeting"`
	Host    bool        `json:"host"`
	ICE     []ICEServer `json:"ice_servers"`
}

// Join checks that a person may join: the meeting is open, and with a
// classroom they're the host or an enrolled student.
func (s *Service) Join(ctx context.Context, u auth.User, code string) (Access, error) {
	m, err := s.byCode(ctx, code)
	if err != nil {
		return Access{}, err
	}
	if m.Status != StatusOpen {
		return Access{}, errEnded
	}
	host := m.HostID == u.ID
	if !host && m.ClassroomID != nil {
		e, ok, err := s.classrooms.GetEnrolment(ctx, *m.ClassroomID, u.ID)
		if err != nil {
			return Access{}, err
		}
		if !ok || e.Status != "active" {
			return Access{}, errNotInClass
		}
	}
	lim, err := s.Limits(ctx)
	if err != nil {
		return Access{}, err
	}
	m.MaxPeople = capOf(m.MaxPeople, lim.MaxPeople)
	return Access{Meeting: m, Host: host, ICE: s.ICEServers(u.ID)}, nil
}

// End closes a meeting for everyone (host only).
func (s *Service) End(ctx context.Context, hostID, id string) error {
	var m Meeting
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE meet.meetings SET status='ended', ended_at=$3 WHERE id=$1 AND host_id=$2 AND status='open'`, id, hostID, s.now())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM meet.meetings WHERE id=$1 AND host_id=$2)`, id, hostID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return httpx.ErrNotFound
			}
			return nil // already ended
		}
		m.ID = id
		return audit.Log(ctx, tx, hostID, "meeting_ended", "meeting", id, nil)
	})
	if err == nil && m.ID != "" {
		s.hub.end(id)
	}
	return err
}

// AttendanceRow is one person's time in a meeting.
type AttendanceRow struct {
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	FirstJoined time.Time `json:"first_joined"`
	LastSeen    time.Time `json:"last_seen"`
	Joins       int       `json:"joins"`
	Online      bool      `json:"online"`
}

// Attendance lists who joined a meeting (host only).
func (s *Service) Attendance(ctx context.Context, hostID, id string) ([]AttendanceRow, error) {
	var ok bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM meet.meetings WHERE id=$1 AND host_id=$2)`, id, hostID).Scan(&ok); err != nil || !ok {
		if err == nil {
			err = httpx.ErrNotFound
		}
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT a.user_id, coalesce(u.name, ''), a.first_joined, a.last_seen, a.joins FROM meet.attendance a
		LEFT JOIN auth.users u ON u.id = a.user_id WHERE a.meeting_id=$1 ORDER BY a.first_joined`, id)
	if err != nil {
		return nil, err
	}
	online := s.hub.online(id)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AttendanceRow, error) {
		var a AttendanceRow
		err := r.Scan(&a.UserID, &a.Name, &a.FirstJoined, &a.LastSeen, &a.Joins)
		a.Online = online[a.UserID]
		return a, err
	})
}

func (s *Service) arrived(ctx context.Context, meetingID, userID string) {
	if _, err := s.pool.Exec(ctx, `INSERT INTO meet.attendance(meeting_id, user_id, first_joined, last_seen) VALUES ($1,$2,$3,$3)
		ON CONFLICT (meeting_id, user_id) DO UPDATE SET joins = meet.attendance.joins + 1, last_seen = $3`, meetingID, userID, s.now()); err != nil {
		s.log.Warn("meeting attendance", "err", err)
	}
}

func (s *Service) left(ctx context.Context, meetingID, userID string) {
	if _, err := s.pool.Exec(ctx, `UPDATE meet.attendance SET last_seen=$3 WHERE meeting_id=$1 AND user_id=$2`, meetingID, userID, s.now()); err != nil {
		s.log.Warn("meeting attendance", "err", err)
	}
}
