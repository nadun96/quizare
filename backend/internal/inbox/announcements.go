package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

// Announcement is one post to a classroom. A student sees Read; its teacher
// sees Reads of Students.
type Announcement struct {
	ID          string    `json:"id"`
	ClassroomID string    `json:"classroom_id"`
	Classroom   string    `json:"classroom"`
	Author      string    `json:"author"`
	AuthorID    *string   `json:"author_id"`
	Avatar      string    `json:"avatar,omitempty"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Emailed     bool      `json:"emailed"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Read        bool      `json:"read"`
	Reads       int       `json:"reads"`
	Students    int       `json:"students"`
}

// studentAnnouncements limits "inbox.announcements a" to a student's ($1)
// classrooms: active enrolments in classrooms that aren't archived, and
// posts from at most 30 days before they asked to join, so a student who
// joins late sees this month's news without the whole year's. Callers
// append more conditions with AND.
const studentAnnouncements = ` JOIN content.classrooms c ON c.id = a.classroom_id AND c.archived_at IS NULL
	JOIN content.enrolments e ON e.classroom_id = c.id AND e.user_id = $1 AND e.status = 'active'
	WHERE a.created_at >= e.created_at - interval '30 days'`

const announcementCols = `SELECT a.id, a.classroom_id, c.name, coalesce(w.name, ''), a.author_id, coalesce(w.avatar_version, ''), a.title, a.body, a.emailed, a.created_at, a.updated_at`

// Announcements lists a person's announcements, newest first: a student's
// from their classrooms, a teacher's in theirs. classroomID narrows it to one.
func (s *Service) Announcements(ctx context.Context, u auth.User, classroomID string, p page.Request) ([]Announcement, int, error) {
	from := ` FROM inbox.announcements a LEFT JOIN auth.users w ON w.id = a.author_id`
	switch u.Role {
	case auth.RoleStudent:
		from += studentAnnouncements
	case auth.RoleTeacher:
		from += ` JOIN content.classrooms c ON c.id = a.classroom_id WHERE c.teacher_id = $1`
	default:
		return []Announcement{}, 0, nil
	}
	from += ` AND ($2 = '' OR a.classroom_id::text = $2) AND ($3 = '' OR a.title ILIKE $3 OR a.body ILIKE $3 OR c.name ILIKE $3)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+from, u.ID, classroomID, p.Like()).Scan(&total); err != nil {
		return nil, 0, err
	}
	extra := `, EXISTS (SELECT 1 FROM inbox.announcement_reads r WHERE r.announcement_id = a.id AND r.user_id = $1), 0, 0`
	if u.Role == auth.RoleTeacher {
		extra = `, false,
			(SELECT count(*) FROM inbox.announcement_reads r JOIN content.enrolments e2 ON e2.user_id = r.user_id AND e2.classroom_id = a.classroom_id AND e2.status = 'active' WHERE r.announcement_id = a.id)::int,
			(SELECT count(*) FROM content.enrolments e2 WHERE e2.classroom_id = a.classroom_id AND e2.status = 'active')::int`
	}
	rows, err := s.pool.Query(ctx, announcementCols+extra+from+` ORDER BY a.created_at DESC, a.id`+p.Limit(), u.ID, classroomID, p.Like())
	if err != nil {
		return nil, 0, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Announcement, error) {
		var a Announcement
		return a, r.Scan(&a.ID, &a.ClassroomID, &a.Classroom, &a.Author, &a.AuthorID, &a.Avatar, &a.Title, &a.Body, &a.Emailed, &a.CreatedAt, &a.UpdatedAt, &a.Read, &a.Reads, &a.Students)
	})
	if out == nil {
		out = []Announcement{}
	}
	return out, total, err
}

// owned is an announcement in one of the teacher's classrooms.
func (s *Service) owned(ctx context.Context, teacherID, id string) (Announcement, error) {
	var a Announcement
	err := s.pool.QueryRow(ctx, announcementCols+`, false, 0, 0 FROM inbox.announcements a LEFT JOIN auth.users w ON w.id = a.author_id
		JOIN content.classrooms c ON c.id = a.classroom_id WHERE a.id::text = $1 AND c.teacher_id = $2`, id, teacherID).
		Scan(&a.ID, &a.ClassroomID, &a.Classroom, &a.Author, &a.AuthorID, &a.Avatar, &a.Title, &a.Body, &a.Emailed, &a.CreatedAt, &a.UpdatedAt, &a.Read, &a.Reads, &a.Students)
	return a, notFound(err)
}

// Announce posts to one of the teacher's classrooms and, if asked, emails
// its students.
func (s *Service) Announce(ctx context.Context, teacherID, classroomID, title, body string, email bool) (Announcement, error) {
	var classroom, teacher string
	err := s.pool.QueryRow(ctx, `SELECT c.name, u.name FROM content.classrooms c JOIN auth.users u ON u.id = c.teacher_id
		WHERE c.id::text = $1 AND c.teacher_id = $2`, classroomID, teacherID).Scan(&classroom, &teacher)
	if err != nil {
		return Announcement{}, notFound(err)
	}
	if title, err = text("title", title, maxTitle); err != nil {
		return Announcement{}, err
	}
	if body, err = text("body", body, maxBody); err != nil {
		return Announcement{}, err
	}
	var id string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO inbox.announcements (classroom_id, author_id, title, body, emailed) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			classroomID, teacherID, title, body, email).Scan(&id); err != nil {
			return err
		}
		if email {
			rows, err := tx.Query(ctx, `SELECT user_id::text FROM content.enrolments WHERE classroom_id = $1 AND status = 'active'`, classroomID)
			if err != nil {
				return err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
			to, err := wantsEmail(ctx, tx, ids)
			if err != nil {
				return err
			}
			for _, m := range to {
				if err := s.queue(ctx, tx, mail.Message{To: m.email, Subject: fmt.Sprintf("%s: %s", classroom, title),
					Body: fmt.Sprintf("Hi %s,\n\n%s posted in %s:\n\n%s\n\n%s\n\nRead it in your inbox: %s/inbox\n\n--\nYou can turn these emails off in your inbox.", m.name, teacher, classroom, title, body, s.baseURL)}); err != nil {
					return err
				}
			}
		}
		return audit.Log(ctx, tx, teacherID, "announcement_posted", "announcement", id, map[string]any{"classroom": classroomID, "emailed": email})
	})
	if err != nil {
		return Announcement{}, err
	}
	return s.owned(ctx, teacherID, id)
}

// EditAnnouncement changes its title or text; nothing is emailed again.
func (s *Service) EditAnnouncement(ctx context.Context, teacherID, id string, title, body *string) (Announcement, error) {
	a, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return a, err
	}
	if title != nil {
		if a.Title, err = text("title", *title, maxTitle); err != nil {
			return a, err
		}
	}
	if body != nil {
		if a.Body, err = text("body", *body, maxBody); err != nil {
			return a, err
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE inbox.announcements SET title = $2, body = $3, updated_at = now() WHERE id = $1`, a.ID, a.Title, a.Body); err != nil {
		return a, err
	}
	return s.owned(ctx, teacherID, id)
}

func (s *Service) DeleteAnnouncement(ctx context.Context, teacherID, id string) error {
	a, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM inbox.announcements WHERE id = $1`, a.ID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "announcement_deleted", "announcement", a.ID, map[string]string{"title": a.Title})
	})
}

// MarkRead records that a student read announcements of their classrooms;
// with no ids, all of them.
func (s *Service) MarkRead(ctx context.Context, studentID string, ids []string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO inbox.announcement_reads (announcement_id, user_id)
		SELECT a.id, $1 FROM inbox.announcements a`+studentAnnouncements+` AND (cardinality($2::text[]) = 0 OR a.id::text = ANY($2))
		ON CONFLICT DO NOTHING`, studentID, ids)
	return err
}

// Reader is a student and whether they read an announcement.
type Reader struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	StudentNumber *string    `json:"student_number"`
	ReadAt        *time.Time `json:"read_at"`
}

// Readers lists the classroom's students with when they read it, those who
// haven't first.
func (s *Service) Readers(ctx context.Context, teacherID, id string, p page.Request) ([]Reader, int, error) {
	a, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return nil, 0, err
	}
	from := ` FROM content.enrolments e JOIN auth.users u ON u.id = e.user_id
		LEFT JOIN inbox.announcement_reads r ON r.announcement_id = $1 AND r.user_id = e.user_id
		WHERE e.classroom_id = $2 AND e.status = 'active' AND ($3 = '' OR u.name ILIKE $3 OR e.student_number ILIKE $3)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+from, a.ID, a.ClassroomID, p.Like()).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.name, e.student_number, r.read_at`+from+` ORDER BY r.read_at IS NOT NULL, lower(u.name), u.id`+p.Limit(), a.ID, a.ClassroomID, p.Like())
	if err != nil {
		return nil, 0, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Reader, error) {
		var x Reader
		return x, r.Scan(&x.ID, &x.Name, &x.StudentNumber, &x.ReadAt)
	})
	if out == nil {
		out = []Reader{}
	}
	return out, total, err
}
