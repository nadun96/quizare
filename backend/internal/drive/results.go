package drive

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Results saved to Drive: a session's or a poll's results as a Google Sheet
// in the teacher's "Classroom Quiz results" folder.

const folderName = "Classroom Quiz results"

// Export is one save of results to Drive.
type Export struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	TargetID  string    `json:"target_id"`
	Name      string    `json:"name"`
	WebLink   string    `json:"web_link,omitempty"`
	Automatic bool      `json:"automatic"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// folder returns the results folder, making it again if it was deleted.
func (s *Service) folder(ctx context.Context, a api, teacherID string) (string, error) {
	var id string
	if err := s.pool.QueryRow(ctx, `SELECT folder_id FROM drive.connections WHERE teacher_id=$1`, teacherID).Scan(&id); err != nil {
		return "", err
	}
	if id != "" {
		f, err := a.file(ctx, id)
		if err == nil && !f.Trashed {
			return id, nil
		}
		if err != nil && !isStatus(err, http.StatusNotFound) {
			return "", err
		}
	}
	f, err := a.createFolder(ctx, folderName)
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `UPDATE drive.connections SET folder_id=$2 WHERE teacher_id=$1`, teacherID, f.ID)
	return f.ID, err
}

// Save puts a session's (kind "session") or poll's ("poll") results in
// the teacher's Drive and records it, whether it worked or not.
func (s *Service) Save(ctx context.Context, teacherID, kind, targetID string, automatic bool) (Export, error) {
	var name string
	var data []byte
	var err error
	switch kind {
	case "poll":
		name, data, err = s.results.PollCSV(ctx, teacherID, targetID)
	case "presentation":
		name, data, err = s.results.PresentationCSV(ctx, teacherID, targetID)
	default:
		name, data, err = s.results.SessionCSV(ctx, teacherID, targetID)
	}
	if err != nil {
		return Export{}, err // not found, or not theirs: nothing to record
	}
	e := Export{Kind: kind, TargetID: targetID, Name: name, Automatic: automatic, Status: "done"}
	f, uerr := s.upload(ctx, teacherID, name, data)
	if uerr != nil {
		e.Status, e.Error = "failed", errMessage(uerr)
	} else {
		e.WebLink = f.WebViewLink
	}
	if err := s.pool.QueryRow(ctx, `INSERT INTO drive.exports(teacher_id, kind, target_id, name, file_id, web_link, automatic, status, error, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id, created_at`,
		teacherID, kind, targetID, name, f.ID, e.WebLink, automatic, e.Status, e.Error, s.now()).Scan(&e.ID, &e.CreatedAt); err != nil {
		return e, err
	}
	return e, uerr
}

func (s *Service) upload(ctx context.Context, teacherID, name string, data []byte) (File, error) {
	a, err := s.api(ctx, teacherID)
	if err != nil {
		return File{}, err
	}
	folder, err := s.folder(ctx, a, teacherID)
	if err != nil {
		return File{}, googleErr(err)
	}
	f, err := a.upload(ctx, name, folder, "text/csv", MimeSheet, data)
	return f, googleErr(err)
}

// errMessage is what the teacher reads on a failed save.
func errMessage(err error) string {
	var he *httpx.Error
	if errors.As(err, &he) {
		return he.Message
	}
	return "Google Drive didn't take the file"
}

// Exports lists a teacher's recent saves, or those of one session or poll.
func (s *Service) Exports(ctx context.Context, teacherID, targetID string, limit int) ([]Export, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, kind, target_id, name, web_link, automatic, status, error, created_at FROM drive.exports
		WHERE teacher_id=$1 AND ($2 = '' OR target_id::text = $2) ORDER BY created_at DESC LIMIT $3`, teacherID, targetID, limit)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Export, error) {
		var e Export
		return e, r.Scan(&e.ID, &e.Kind, &e.TargetID, &e.Name, &e.WebLink, &e.Automatic, &e.Status, &e.Error, &e.CreatedAt)
	})
	if out == nil {
		out = []Export{}
	}
	return out, err
}

// ---------- after each session ----------

// BackupArgs saves an ended session's results, if its teacher asked for that.
type BackupArgs struct {
	SessionID string `json:"session_id"`
}

func (BackupArgs) Kind() string { return "drive_backup" }

// A few tries over about an hour ride out a short Google outage.
func (BackupArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 6} }

type BackupWorker struct {
	river.WorkerDefaults[BackupArgs]
	Service *Service
}

func (w *BackupWorker) Work(ctx context.Context, job *river.Job[BackupArgs]) error {
	return w.Service.backup(ctx, job.Args.SessionID, job.Attempt >= job.MaxAttempts)
}

func (s *Service) backup(ctx context.Context, sessionID string, last bool) error {
	teacherID, err := s.results.SessionTeacher(ctx, sessionID)
	if err != nil {
		return nil // the session is gone
	}
	var auto bool
	var status string
	err = s.pool.QueryRow(ctx, `SELECT auto_backup, status FROM drive.connections WHERE teacher_id=$1`, teacherID).Scan(&auto, &status)
	if errors.Is(err, pgx.ErrNoRows) || !auto || status != "ok" {
		return nil
	}
	if err != nil {
		return err
	}
	var done bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM drive.exports WHERE target_id=$1 AND automatic AND status='done')`, sessionID).Scan(&done); err != nil || done {
		return err
	}
	_, err = s.Save(ctx, teacherID, "session", sessionID, true)
	if err != nil && !last && !errors.Is(err, errReconnect) {
		// Try again later; the failed attempt stays on the list until one works.
		_, _ = s.pool.Exec(ctx, `DELETE FROM drive.exports WHERE target_id=$1 AND automatic AND status='failed'`, sessionID)
		return err
	}
	return nil
}

// OnSessionEnded queues the backup with the session's end, so it happens
// only if the session really ended.
func (s *Service) OnSessionEnded(ctx context.Context, tx pgx.Tx, sessionID string) error {
	if !s.Enabled(ctx) {
		return nil
	}
	_, err := s.jobs.InsertTx(ctx, tx, BackupArgs{SessionID: sessionID}, nil)
	return err
}
