package drive

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// ---------- one file, shared by link (for questions) ----------

// Shared is a Drive file anyone with the link can now open.
type Shared struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	WebLink  string `json:"web_link"`
	Image    bool   `json:"image"`
	// URL to use as a question image (imageurl turns it into a thumbnail).
	ImageURL string `json:"image_url,omitempty"`
}

// ShareByLink lets anyone with the link view a file the teacher picked, for
// a question's image or a link in its text: students see it without a
// Google account.
func (s *Service) ShareByLink(ctx context.Context, teacherID, fileID string) (Shared, error) {
	a, err := s.api(ctx, teacherID)
	if err != nil {
		return Shared{}, err
	}
	f, err := a.file(ctx, fileID)
	if err != nil {
		return Shared{}, googleErr(err)
	}
	if f.MimeType == MimeFolder {
		return Shared{}, httpx.Invalid(map[string]string{"file_id": "choose a file, not a folder"})
	}
	if _, err := a.share(ctx, f.ID, ""); err != nil {
		return Shared{}, googleErr(err)
	}
	out := Shared{ID: f.ID, Name: f.Name, MimeType: f.MimeType, WebLink: f.WebViewLink, Image: strings.HasPrefix(f.MimeType, "image/")}
	if out.Image {
		out.ImageURL = "https://drive.google.com/file/d/" + f.ID + "/view"
	}
	return out, nil
}

// ---------- importing questions ----------

// ImportQuestions imports a quiz's questions from a CSV file or a Google
// Sheet in the teacher's Drive (the first sheet), as the CSV upload does.
func (s *Service) ImportQuestions(ctx context.Context, teacherID, quizID, fileID string) (quiz.ImportReport, error) {
	a, err := s.api(ctx, teacherID)
	if err != nil {
		return quiz.ImportReport{}, err
	}
	f, err := a.file(ctx, fileID)
	if err != nil {
		return quiz.ImportReport{}, googleErr(err)
	}
	var exportAs string
	switch {
	case f.MimeType == MimeSheet:
		exportAs = "text/csv"
	case f.MimeType == "text/csv" || strings.HasSuffix(strings.ToLower(f.Name), ".csv"):
	default:
		return quiz.ImportReport{}, errNotCSV
	}
	data, err := a.content(ctx, f.ID, exportAs, quiz.MaxCSVBytes)
	if err != nil {
		return quiz.ImportReport{}, googleErr(err)
	}
	return s.questions.ImportQuestions(ctx, teacherID, quizID, data)
}

// ---------- classroom files ----------

// ClassroomFile is a Drive file attached to a classroom.
type ClassroomFile struct {
	ID        string     `json:"id"`
	FileID    string     `json:"file_id"`
	Name      string     `json:"name"`
	MimeType  string     `json:"mime_type"`
	WebLink   string     `json:"web_link"`
	IconLink  string     `json:"icon_link,omitempty"`
	Sharing   string     `json:"sharing,omitempty"`
	SharedTo  int        `json:"shared_with,omitempty"` // students it's shared with
	SyncedAt  *time.Time `json:"synced_at,omitempty"`
	SyncError string     `json:"sync_error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func validSharing(v string) bool { return v == "students" || v == "link" || v == "none" }

func (s *Service) files(ctx context.Context, classroomID string, teacher bool) ([]ClassroomFile, error) {
	rows, err := s.pool.Query(ctx, `SELECT f.id, f.file_id, f.name, f.mime_type, f.web_link, f.icon_link, f.sharing,
		(SELECT count(*) FROM drive.shares sh WHERE sh.classroom_file_id = f.id), f.synced_at, f.sync_error, f.created_at
		FROM drive.classroom_files f WHERE f.classroom_id=$1 ORDER BY f.position, f.created_at`, classroomID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ClassroomFile, error) {
		var f ClassroomFile
		err := r.Scan(&f.ID, &f.FileID, &f.Name, &f.MimeType, &f.WebLink, &f.IconLink, &f.Sharing, &f.SharedTo, &f.SyncedAt, &f.SyncError, &f.CreatedAt)
		if !teacher {
			f = ClassroomFile{ID: f.ID, Name: f.Name, MimeType: f.MimeType, WebLink: f.WebLink, IconLink: f.IconLink, CreatedAt: f.CreatedAt}
		}
		return f, err
	})
	if out == nil {
		out = []ClassroomFile{}
	}
	return out, err
}

// ClassroomFiles lists a classroom's files for its teacher.
func (s *Service) ClassroomFiles(ctx context.Context, teacherID, classroomID string) ([]ClassroomFile, error) {
	if _, err := s.classrooms.GetClassroom(ctx, teacherID, classroomID); err != nil {
		return nil, err
	}
	return s.files(ctx, classroomID, true)
}

// StudentFiles lists them for an enrolled student.
func (s *Service) StudentFiles(ctx context.Context, userID, classroomID string) ([]ClassroomFile, error) {
	e, ok, err := s.classrooms.GetEnrolment(ctx, classroomID, userID)
	if err != nil {
		return nil, err
	}
	if !ok || e.Status != "active" {
		return nil, httpx.ErrNotFound
	}
	return s.files(ctx, classroomID, false)
}

// AttachInput: files picked in the picker, and how to share them.
type AttachInput struct {
	FileIDs []string `json:"file_ids"`
	Sharing string   `json:"sharing"`
}

// Attach adds picked files to a classroom and shares them.
func (s *Service) Attach(ctx context.Context, teacherID, classroomID string, in AttachInput) ([]ClassroomFile, error) {
	if _, err := s.classrooms.GetClassroom(ctx, teacherID, classroomID); err != nil {
		return nil, err
	}
	if in.Sharing == "" {
		in.Sharing = "students"
	}
	if !validSharing(in.Sharing) {
		return nil, httpx.Invalid(map[string]string{"sharing": "choose students, link or none"})
	}
	in.FileIDs = slices.Compact(slices.Sorted(slices.Values(in.FileIDs)))
	if len(in.FileIDs) == 0 || len(in.FileIDs) > 20 {
		return nil, httpx.Invalid(map[string]string{"file_ids": "choose between 1 and 20 files"})
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM drive.classroom_files WHERE classroom_id=$1`, classroomID).Scan(&n); err != nil {
		return nil, err
	}
	if n+len(in.FileIDs) > 200 {
		return nil, httpx.Invalid(map[string]string{"file_ids": "a classroom holds at most 200 files"})
	}
	a, err := s.api(ctx, teacherID)
	if err != nil {
		return nil, err
	}
	for _, id := range in.FileIDs {
		f, err := a.file(ctx, id)
		if err != nil {
			return nil, googleErr(err)
		}
		if f.Trashed {
			return nil, httpx.Invalid(map[string]string{"file_ids": fmt.Sprintf("“%s” is in the Drive bin", f.Name)})
		}
		var rowID string
		err = s.pool.QueryRow(ctx, `INSERT INTO drive.classroom_files(classroom_id, file_id, name, mime_type, web_link, icon_link, sharing, added_by, position)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,(SELECT coalesce(max(position), 0) + 1 FROM drive.classroom_files WHERE classroom_id=$1))
			ON CONFLICT (classroom_id, file_id) DO UPDATE SET name=EXCLUDED.name, web_link=EXCLUDED.web_link, icon_link=EXCLUDED.icon_link
			RETURNING id`, classroomID, f.ID, f.Name, f.MimeType, f.WebViewLink, f.IconLink, in.Sharing, teacherID).Scan(&rowID)
		if err != nil {
			return nil, err
		}
		s.sync(ctx, rowID)
	}
	return s.files(ctx, classroomID, true)
}

// SetSharing changes how one file is shared.
func (s *Service) SetSharing(ctx context.Context, teacherID, classroomID, id, sharing string) ([]ClassroomFile, error) {
	if _, err := s.classrooms.GetClassroom(ctx, teacherID, classroomID); err != nil {
		return nil, err
	}
	if !validSharing(sharing) {
		return nil, httpx.Invalid(map[string]string{"sharing": "choose students, link or none"})
	}
	tag, err := s.pool.Exec(ctx, `UPDATE drive.classroom_files SET sharing=$3 WHERE id=$1 AND classroom_id=$2`, id, classroomID, sharing)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, httpx.ErrNotFound
	}
	s.sync(ctx, id)
	return s.files(ctx, classroomID, true)
}

// Detach removes a file from the classroom, and takes back what this
// platform shared. The file stays in the teacher's Drive.
func (s *Service) Detach(ctx context.Context, teacherID, classroomID, id string) error {
	if _, err := s.classrooms.GetClassroom(ctx, teacherID, classroomID); err != nil {
		return err
	}
	if _, err := s.SetSharing(ctx, teacherID, classroomID, id, "none"); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM drive.classroom_files WHERE id=$1 AND classroom_id=$2`, id, classroomID)
	if err == nil && tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return err
}

// sync makes one file's Drive sharing match its setting and the classroom's
// students now: it adds who's missing and removes what this platform added
// for people no longer there. Problems are kept on the row for the teacher.
func (s *Service) sync(ctx context.Context, id string) {
	if err := s.syncFile(ctx, id); err != nil {
		msg := err.Error()
		var he *httpx.Error
		if errors.As(err, &he) {
			msg = he.Message
		}
		s.log.Warn("sharing a classroom Drive file", "file", id, "err", err)
		_, _ = s.pool.Exec(ctx, `UPDATE drive.classroom_files SET sync_error=$2 WHERE id=$1`, id, msg)
	}
}

func (s *Service) syncFile(ctx context.Context, id string) error {
	var classroomID, fileID, sharing, linkPerm, teacherID string
	if err := s.pool.QueryRow(ctx, `SELECT classroom_id, file_id, sharing, link_permission_id, added_by FROM drive.classroom_files WHERE id=$1`, id).
		Scan(&classroomID, &fileID, &sharing, &linkPerm, &teacherID); err != nil {
		return err
	}
	a, err := s.api(ctx, teacherID)
	if err != nil {
		return err
	}
	// Who should have it, by email.
	want := map[string]bool{}
	if sharing == "students" {
		list, err := s.classrooms.ListEnrolments(ctx, teacherID, classroomID, "active")
		if err != nil {
			return err
		}
		for _, e := range list {
			if e.StudentEmail != "" {
				want[strings.ToLower(e.StudentEmail)] = true
			}
		}
	}
	have := map[string]string{}
	rows, err := s.pool.Query(ctx, `SELECT email, permission_id FROM drive.shares WHERE classroom_file_id=$1`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var email, perm string
		if err := rows.Scan(&email, &perm); err != nil {
			rows.Close()
			return err
		}
		have[email] = perm
	}
	rows.Close()
	for email, perm := range have {
		if !want[email] {
			if err := a.unshare(ctx, fileID, perm); err != nil {
				return googleErr(err)
			}
			if _, err := s.pool.Exec(ctx, `DELETE FROM drive.shares WHERE classroom_file_id=$1 AND email=$2`, id, email); err != nil {
				return err
			}
		}
	}
	// Drive can't share with an email that has no Google account without
	// sending an invitation; those students are counted, not fatal.
	var noAccount int
	for email := range want {
		if _, ok := have[email]; ok {
			continue
		}
		perm, err := a.share(ctx, fileID, email)
		if isStatus(err, 400) {
			noAccount++
			continue
		}
		if err != nil {
			return googleErr(err)
		}
		if _, err := s.pool.Exec(ctx, `INSERT INTO drive.shares(classroom_file_id, email, permission_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, id, email, perm); err != nil {
			return err
		}
	}
	switch {
	case sharing == "link" && linkPerm == "":
		perm, err := a.share(ctx, fileID, "")
		if err != nil {
			return googleErr(err)
		}
		linkPerm = perm
	case sharing != "link" && linkPerm != "":
		if err := a.unshare(ctx, fileID, linkPerm); err != nil {
			return googleErr(err)
		}
		linkPerm = ""
	}
	problem := ""
	if noAccount > 0 {
		problem = fmt.Sprintf("%d %s no Google account with their email, so Drive won't share with them; share by link instead", noAccount,
			map[bool]string{true: "student has", false: "students have"}[noAccount == 1])
	}
	_, err = s.pool.Exec(ctx, `UPDATE drive.classroom_files SET link_permission_id=$2, synced_at=$3, sync_error=$4 WHERE id=$1`, id, linkPerm, s.now(), problem)
	return err
}

// SyncAll brings every file shared with students up to date: students who
// joined get it, students who left lose it.
func (s *Service) SyncAll(ctx context.Context) error {
	if !s.Enabled(ctx) {
		return nil
	}
	rows, err := s.pool.Query(ctx, `SELECT f.id FROM drive.classroom_files f JOIN drive.connections c ON c.teacher_id = f.added_by AND c.status = 'ok'
		WHERE f.sharing = 'students' OR EXISTS (SELECT 1 FROM drive.shares sh WHERE sh.classroom_file_id = f.id)`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, id := range ids {
		s.sync(ctx, id)
	}
	return nil
}

// SyncArgs is the job that keeps classroom files shared with the right students.
type SyncArgs struct{}

func (SyncArgs) Kind() string { return "drive_sync" }

type SyncWorker struct {
	river.WorkerDefaults[SyncArgs]
	Service *Service
}

func (w *SyncWorker) Work(ctx context.Context, _ *river.Job[SyncArgs]) error {
	return w.Service.SyncAll(ctx)
}

// SyncPeriodic runs it every 15 minutes.
func SyncPeriodic() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(15*time.Minute), func() (river.JobArgs, *river.InsertOpts) { return SyncArgs{}, nil }, nil)
}
