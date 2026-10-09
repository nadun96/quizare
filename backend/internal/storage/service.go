// Package storage shows the admin the space the platform uses, sets its
// limits, frees space, and makes and keeps encrypted backups (PO-25,
// PL-FR-04 to PL-FR-09, D-57). It reads only sizes from PostgreSQL's
// catalogues; other modules free their own data through clean-up areas.
package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/poll"
)

// Users names the people who made backups.
type Users interface {
	UsersByID(ctx context.Context, ids []string) (map[string]auth.User, error)
}

// Freed is what a clean-up removes, or would remove.
type Freed struct {
	Items int   `json:"items"`
	Bytes int64 `json:"bytes"`
}

// Area is one kind of data a clean-up can delete (PL-FR-06). Preview and
// Run must count the same rows, so the preview matches what goes.
type Area struct {
	ID      string                                                                     `json:"id"`
	Label   string                                                                     `json:"label"`
	Preview func(ctx context.Context, before time.Time) (Freed, error)                 `json:"-"`
	Run     func(ctx context.Context, actorID string, before time.Time) (Freed, error) `json:"-"`
}

type Service struct {
	pool       *pgxpool.Pool
	users      Users
	log        *slog.Logger
	dir        string // console backups
	nightlyDir string // deploy/backup.sh's folder, if any
	kekID      string
	now        func() time.Time
	disk       func(path string) (free, total uint64, err error)
	areas      []Area

	mu      sync.Mutex
	running bool
	wg      sync.WaitGroup
}

// NewService: kek is the master key, of which only a fingerprint is kept.
func NewService(pool *pgxpool.Pool, users Users, log *slog.Logger, dir, nightlyDir string, kek []byte) *Service {
	s := &Service{pool: pool, users: users, log: log, dir: dir, nightlyDir: nightlyDir, kekID: KEKID(kek),
		now: time.Now, disk: diskUsage}
	s.areas = []Area{{ID: "backups", Label: "Console backups made before the date that are still on the server", Preview: s.previewBackups, Run: s.cleanBackups}}
	return s
}

// KEKID is a fingerprint of the master key, recorded in backups so a
// restore can tell whether the right key file is in place. It reveals
// nothing about the key.
func KEKID(kek []byte) string {
	sum := sha256.Sum256(append([]byte("qp-kek-id:"), kek...))
	return hex.EncodeToString(sum[:8])
}

// AddArea lets another module offer its data for clean-up.
func (s *Service) AddArea(a Area) { s.areas = append(s.areas, a) }

// ---------- overview (PL-FR-04) ----------

// Area ids in the overview. Tutoring adds its chat, attendance and recordings.
func areaOf(table string) string {
	switch table {
	case "auth.avatars":
		return "profile_pictures"
	case "live.answers":
		return "answers"
	case "poll.files":
		return "poll_uploads"
	case "poll.board_strokes":
		return "whiteboards"
	}
	schema, _, _ := strings.Cut(table, ".")
	switch schema {
	case "auth":
		return "accounts"
	case "content":
		return "classrooms"
	case "quiz", "live":
		return "quizzes_sessions"
	case "poll":
		return "polls"
	case "eval", "analytics":
		return "results"
	case "audit":
		return "audit"
	case "llm":
		return "ai_marking"
	case "public":
		return "jobs"
	case "settings":
		return "settings"
	case "storage":
		return "storage"
	}
	return "other"
}

type Snapshot struct {
	TakenAt         time.Time        `json:"taken_at"`
	DBBytes         int64            `json:"db_bytes"`
	Areas           map[string]int64 `json:"areas"`
	BackupsBytes    int64            `json:"backups_bytes"`
	RecordingsBytes int64            `json:"recordings_bytes"`
	DiskFree        int64            `json:"disk_free"`
	DiskTotal       int64            `json:"disk_total"`
}

// TakeSnapshot measures and stores the space used now.
func (s *Service) TakeSnapshot(ctx context.Context) (Snapshot, error) {
	sn := Snapshot{TakenAt: s.now().Truncate(time.Microsecond), Areas: map[string]int64{}} // as PostgreSQL stores it
	if err := s.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&sn.DBBytes); err != nil {
		return sn, err
	}
	rows, _ := s.pool.Query(ctx, `SELECT n.nspname || '.' || c.relname, pg_total_relation_size(c.oid)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r' AND n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg\_%'`)
	type size struct {
		table string
		bytes int64
	}
	sizes, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (size, error) {
		var x size
		err := r.Scan(&x.table, &x.bytes)
		return x, err
	})
	if err != nil {
		return sn, err
	}
	var sum int64
	for _, x := range sizes {
		sn.Areas[areaOf(x.table)] += x.bytes
		sum += x.bytes
	}
	if rest := sn.DBBytes - sum; rest > 0 {
		sn.Areas["system"] = rest // PostgreSQL's own catalogues
	}
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(size_bytes), 0) FROM storage.backups WHERE on_server AND status = 'done'`).Scan(&sn.BackupsBytes); err != nil {
		return sn, err
	}
	free, total, err := s.diskNow()
	if err != nil {
		return sn, err
	}
	sn.DiskFree, sn.DiskTotal = free, total
	areas, _ := json.Marshal(sn.Areas)
	_, err = s.pool.Exec(ctx, `INSERT INTO storage.snapshots (taken_at, db_bytes, areas, backups_bytes, recordings_bytes, disk_free, disk_total)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, sn.TakenAt, sn.DBBytes, areas, sn.BackupsBytes, sn.RecordingsBytes, sn.DiskFree, sn.DiskTotal)
	return sn, err
}

// diskNow measures the disk the backups folder is on.
func (s *Service) diskNow() (free, total int64, err error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return 0, 0, fmt.Errorf("backup folder %s: %w", s.dir, err)
	}
	f, t, err := s.disk(s.dir)
	return int64(f), int64(t), err
}

func (s *Service) latest(ctx context.Context) (Snapshot, error) {
	var sn Snapshot
	var areas []byte
	err := s.pool.QueryRow(ctx, `SELECT taken_at, db_bytes, areas, backups_bytes, recordings_bytes, disk_free, disk_total
		FROM storage.snapshots ORDER BY id DESC LIMIT 1`).Scan(&sn.TakenAt, &sn.DBBytes, &areas, &sn.BackupsBytes, &sn.RecordingsBytes, &sn.DiskFree, &sn.DiskTotal)
	if err == nil {
		err = json.Unmarshal(areas, &sn.Areas)
	}
	return sn, err
}

// Point is one day of the growth chart.
type Point struct {
	Day          string `json:"day"`
	DBBytes      int64  `json:"db_bytes"`
	BackupsBytes int64  `json:"backups_bytes"`
}

type FixedLimit struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// fixedLimits are built into the platform (PL-FR-05 shows them).
var fixedLimits = []FixedLimit{
	{"Uploads per poll, all files together", fmt.Sprintf("%d MB", poll.PollStorageLimit>>20)},
	{"One file answer", fmt.Sprintf("up to %d MB (audio %d MB, video %d MB)", poll.MaxFileMB, poll.AudioMaxBytes>>20, poll.VideoMaxBytes>>20)},
	{"Participants per poll", fmt.Sprint(poll.MaxParticipants)},
	{"Whiteboard marks per poll", fmt.Sprint(poll.MaxStrokes)},
	{"Profile picture upload", fmt.Sprintf("%d KB", auth.MaxAvatarUpload>>10)},
}

type Overview struct {
	Snapshot
	Limits  Limits        `json:"limits"`
	Fixed   []FixedLimit  `json:"fixed_limits"`
	Growth  []Point       `json:"growth"`
	Nightly NightlyStatus `json:"nightly"`
	Running *Backup       `json:"running,omitempty"`
}

// Overview returns figures at most an hour old, measuring now if needed;
// refresh measures now (at most every 30 seconds).
func (s *Service) Overview(ctx context.Context, refresh bool) (Overview, error) {
	var o Overview
	sn, err := s.latest(ctx)
	maxAge := time.Hour
	if refresh {
		maxAge = 30 * time.Second
	}
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && s.now().Sub(sn.TakenAt) > maxAge) {
		s.scanNightly(ctx)
		sn, err = s.TakeSnapshot(ctx)
	}
	if err != nil {
		return o, err
	}
	o.Snapshot = sn
	if o.Limits, err = s.Limits(ctx); err != nil {
		return o, err
	}
	o.Fixed = fixedLimits
	rows, _ := s.pool.Query(ctx, `SELECT DISTINCT ON (taken_at::date) to_char(taken_at::date, 'YYYY-MM-DD'), db_bytes, backups_bytes
		FROM storage.snapshots WHERE taken_at > $1 ORDER BY taken_at::date, taken_at DESC`, s.now().AddDate(0, 0, -30))
	if o.Growth, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Point, error) {
		var p Point
		err := r.Scan(&p.Day, &p.DBBytes, &p.BackupsBytes)
		return p, err
	}); err != nil {
		return o, err
	}
	o.Nightly = s.nightlyStatus()
	running, err := s.listBackups(ctx, `WHERE b.status = 'running'`, 1, 0, "b.started_at DESC")
	if err == nil && len(running) > 0 {
		o.Running = &running[0]
	}
	return o, err
}

// ---------- limits (PL-FR-05) ----------

type Limits struct {
	RecordingLimitBytes int64 `json:"recording_limit_bytes"`
	BackupSpaceBytes    int64 `json:"backup_space_bytes"`
}

func (s *Service) Limits(ctx context.Context) (Limits, error) {
	var l Limits
	err := s.pool.QueryRow(ctx, `SELECT recording_limit_bytes, backup_space_bytes FROM storage.limits`).Scan(&l.RecordingLimitBytes, &l.BackupSpaceBytes)
	return l, err
}

const minLimit = 100 << 20

// SetLimits changes the limits at once. A limit can't exceed the free disk
// space plus what that kind already uses, since the space must exist.
func (s *Service) SetLimits(ctx context.Context, actorID string, l Limits) (Limits, error) {
	free, _, err := s.diskNow()
	if err != nil {
		return l, err
	}
	var backups, recordings int64
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(size_bytes), 0) FROM storage.backups WHERE on_server AND status = 'done'`).Scan(&backups); err != nil {
		return l, err
	}
	f := map[string]string{}
	check := func(field string, v, used int64) {
		switch {
		case v < minLimit:
			f[field] = "set at least 100 MB"
		case v > free+used:
			f[field] = fmt.Sprintf("the server has only %s free", Human(free+used))
		}
	}
	check("recording_limit_bytes", l.RecordingLimitBytes, recordings)
	check("backup_space_bytes", l.BackupSpaceBytes, backups)
	if len(f) > 0 {
		return l, httpx.Invalid(f)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var prev Limits
		if err := tx.QueryRow(ctx, `SELECT recording_limit_bytes, backup_space_bytes FROM storage.limits FOR UPDATE`).Scan(&prev.RecordingLimitBytes, &prev.BackupSpaceBytes); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE storage.limits SET recording_limit_bytes=$1, backup_space_bytes=$2`, l.RecordingLimitBytes, l.BackupSpaceBytes); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "storage_limits_changed", "storage", "limits", map[string]Limits{"from": prev, "to": l})
	})
	if err != nil {
		return l, err
	}
	s.prune(ctx) // a smaller backup space applies at once
	return l, nil
}

// Human prints a size as people read it (binary units, labelled MB/GB).
func Human(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ---------- backups (PL-FR-07, PL-FR-08) ----------

type Backup struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"` // console or nightly
	Name        string     `json:"name"` // the file's name
	Status      string     `json:"status"`
	CreatedBy   string     `json:"created_by,omitempty"` // the person's name
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	SizeBytes   int64      `json:"size_bytes"`
	TablesDone  int        `json:"tables_done"`
	TablesTotal int        `json:"tables_total"`
	RowsDone    int64      `json:"rows_done"`
	Error       string     `json:"error,omitempty"`
	OnServer    bool       `json:"on_server"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	file        string
	createdByID *string
}

var (
	errRunning     = httpx.NewError(http.StatusConflict, "backup_running", "a backup is already running; wait for it to finish")
	errNotOnServer = httpx.NewError(http.StatusGone, "backup_gone", "this backup is no longer on the server")
	minPassphrase  = 12
	backupTimeout  = 6 * time.Hour
)

// StartBackup checks the space, records the backup and makes it in the
// background, so the admin can leave the page (PL-FR-07). The passphrase is
// held in memory only while the backup runs and is never stored.
func (s *Service) StartBackup(ctx context.Context, actor auth.User, passphrase string) (Backup, error) {
	if n := len([]rune(passphrase)); n < minPassphrase || n > 1024 {
		return Backup{}, httpx.Invalid(map[string]string{"passphrase": fmt.Sprintf("use at least %d characters", minPassphrase)})
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return Backup{}, errRunning
	}
	s.running = true
	s.mu.Unlock()
	started := false
	defer func() {
		if !started {
			s.setRunning(false)
		}
	}()
	var dbSize int64
	if err := s.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&dbSize); err != nil {
		return Backup{}, err
	}
	free, _, err := s.diskNow()
	if err != nil {
		return Backup{}, err
	}
	// The compressed file is smaller than the database; twice its size leaves room (PL-NFR-05).
	if need := 2 * dbSize; free < need {
		return Backup{}, httpx.NewError(http.StatusConflict, "no_space", fmt.Sprintf("a backup needs about %s of free space; the server has %s", Human(need), Human(free)))
	}
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	name := fmt.Sprintf("qp-%s-%s.qpbackup.age", s.now().UTC().Format("20060102-150405"), hex.EncodeToString(suffix))
	file, err := filepath.Abs(filepath.Join(s.dir, name))
	if err != nil {
		return Backup{}, err
	}
	b := Backup{Kind: "console", Name: name, Status: "running", OnServer: true, file: file}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO storage.backups (kind, file, created_by, started_at) VALUES ('console', $1, $2, $3) RETURNING id, started_at`,
			file, actor.ID, s.now()).Scan(&b.ID, &b.StartedAt); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actor.ID, "backup_started", "backup", b.ID, nil)
	})
	if err != nil {
		return Backup{}, err
	}
	b.CreatedBy = actor.Name
	started = true
	s.wg.Add(1)
	go s.run(b.ID, file, passphrase)
	return b, nil
}

func (s *Service) setRunning(v bool) {
	s.mu.Lock()
	s.running = v
	s.mu.Unlock()
}

// Wait blocks until a running backup ends (tests and shutdown).
func (s *Service) Wait() { s.wg.Wait() }

func (s *Service) run(id, file, passphrase string) {
	defer s.wg.Done()
	defer s.setRunning(false)
	ctx, cancel := context.WithTimeout(context.Background(), backupTimeout)
	defer cancel()
	size, err := s.write(ctx, id, file, passphrase)
	if err != nil {
		s.log.Error("backup failed", "id", id, "err", err)
		_, _ = s.pool.Exec(ctx, `UPDATE storage.backups SET status='failed', error=$2, finished_at=$3, on_server=false WHERE id=$1`, id, err.Error(), s.now())
		_ = audit.Log(ctx, s.pool, "", "backup_failed", "backup", id, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.pool.Exec(ctx, `UPDATE storage.backups SET status='done', size_bytes=$2, finished_at=$3 WHERE id=$1`, id, size, s.now())
	_ = audit.Log(ctx, s.pool, "", "backup_finished", "backup", id, map[string]int64{"bytes": size})
	s.prune(ctx)
}

// write streams the encrypted backup to file.part and renames it when
// complete, so a half-written file is never taken for a backup.
func (s *Service) write(ctx context.Context, id, file, passphrase string) (int64, error) {
	part := file + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(part)
		}
	}()
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()
	w, err := seal(f, passphrase)
	if err != nil {
		return 0, err
	}
	var last time.Time
	progress := func(done, total int, rows int64) {
		if time.Since(last) < time.Second && done < total {
			return
		}
		last = time.Now()
		_, _ = s.pool.Exec(ctx, `UPDATE storage.backups SET tables_done=$2, tables_total=$3, rows_done=$4 WHERE id=$1`, id, done, total, rows)
	}
	if _, err := dump(ctx, conn.Conn(), w, s.kekID, progress); err != nil {
		return 0, err
	}
	if err := w.Close(); err != nil {
		return 0, err
	}
	if err := f.Sync(); err != nil {
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(part, file); err != nil {
		return 0, err
	}
	ok = true
	st, err := os.Stat(file)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// Recover marks backups that were running when the server stopped as
// failed and removes their partial files.
func (s *Service) Recover(ctx context.Context) error {
	rows, _ := s.pool.Query(ctx, `UPDATE storage.backups SET status='failed', error='the server stopped during the backup', finished_at=now(), on_server=false
		WHERE status='running' RETURNING file`)
	files, err := pgx.CollectRows(rows, pgx.RowTo[string])
	for _, f := range files {
		_ = os.Remove(f + ".part")
	}
	return err
}

// prune deletes the oldest console backups once they use more than the
// backup space (PL-FR-08). The newest is always kept.
func (s *Service) prune(ctx context.Context) {
	l, err := s.Limits(ctx)
	if err != nil {
		return
	}
	list, err := s.listBackups(ctx, `WHERE b.kind = 'console' AND b.status = 'done' AND b.on_server`, 1000, 0, "b.started_at DESC")
	if err != nil {
		return
	}
	var used int64
	for i, b := range list {
		used += b.SizeBytes
		if i == 0 || used <= l.BackupSpaceBytes {
			continue
		}
		if err := s.removeFile(ctx, b); err == nil {
			_ = audit.Log(ctx, s.pool, "", "backup_pruned", "backup", b.ID, map[string]int64{"bytes": b.SizeBytes, "space": l.BackupSpaceBytes})
		}
	}
}

func (s *Service) removeFile(ctx context.Context, b Backup) error {
	if err := os.Remove(b.file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE storage.backups SET on_server=false, deleted_at=$2 WHERE id=$1`, b.ID, s.now())
	return err
}

// BackupSorts: the history is read newest first.
const backupCols = `b.id, b.kind, b.file, b.status, b.created_by, b.started_at, b.finished_at, b.size_bytes, b.tables_done, b.tables_total, b.rows_done, b.error, b.on_server, b.deleted_at`

func (s *Service) listBackups(ctx context.Context, where string, limit, offset int, order string, args ...any) ([]Backup, error) {
	rows, _ := s.pool.Query(ctx, `SELECT `+backupCols+` FROM storage.backups b `+where+` ORDER BY `+order+fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset), args...)
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Backup, error) {
		var b Backup
		err := r.Scan(&b.ID, &b.Kind, &b.file, &b.Status, &b.createdByID, &b.StartedAt, &b.FinishedAt, &b.SizeBytes, &b.TablesDone, &b.TablesTotal, &b.RowsDone, &b.Error, &b.OnServer, &b.DeletedAt)
		b.Name = filepath.Base(b.file)
		return b, err
	})
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, b := range list {
		if b.createdByID != nil {
			ids = append(ids, *b.createdByID)
		}
	}
	if len(ids) > 0 && s.users != nil {
		people, err := s.users.UsersByID(ctx, ids)
		if err != nil {
			return nil, err
		}
		for i, b := range list {
			if b.createdByID != nil {
				list[i].CreatedBy = people[*b.createdByID].Name
			}
		}
	}
	return list, nil
}

func (s *Service) backup(ctx context.Context, id string) (Backup, error) {
	list, err := s.listBackups(ctx, `WHERE b.id = $1`, 1, 0, "b.id", id)
	if err != nil {
		return Backup{}, err
	}
	if len(list) == 0 {
		return Backup{}, httpx.ErrNotFound
	}
	return list[0], nil
}

// DeleteBackup removes a finished backup's file; the row stays as history.
func (s *Service) DeleteBackup(ctx context.Context, actorID, id string) error {
	b, err := s.backup(ctx, id)
	if err != nil {
		return err
	}
	if b.Status == "running" {
		return errRunning
	}
	if !b.OnServer {
		return errNotOnServer
	}
	if b.Kind == "nightly" { // its files belong to the nightly job, which rotates them
		return httpx.BadRequest("nightly backups are kept and rotated by the nightly job")
	}
	if err := s.removeFile(ctx, b); err != nil {
		return err
	}
	return audit.Log(ctx, s.pool, actorID, "backup_deleted", "backup", id, map[string]string{"name": b.Name})
}

// ---------- downloads (PL-NFR-03) ----------

const linkTTL = time.Hour

func hashToken(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

// DownloadLink issues a single-use link to a backup, valid for an hour, for
// the person asking only.
func (s *Service) DownloadLink(ctx context.Context, actorID, id string) (string, time.Time, error) {
	b, err := s.backup(ctx, id)
	if err != nil {
		return "", time.Time{}, err
	}
	if b.Status != "done" || !b.OnServer {
		return "", time.Time{}, errNotOnServer
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	exp := s.now().Add(linkTTL)
	_, err = s.pool.Exec(ctx, `INSERT INTO storage.download_links (token_hash, backup_id, user_id, expires_at) VALUES ($1, $2, $3, $4)`, hashToken(token), id, actorID, exp)
	return token, exp, err
}

// OpenDownload uses up a link and opens its backup. A used, expired or
// someone else's link is "not found".
func (s *Service) OpenDownload(ctx context.Context, actorID, token string) (*os.File, Backup, error) {
	var id string
	err := s.pool.QueryRow(ctx, `UPDATE storage.download_links SET used_at=$3 WHERE token_hash=$1 AND user_id=$2 AND used_at IS NULL AND expires_at > $3 RETURNING backup_id`,
		hashToken(token), actorID, s.now()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, Backup{}, httpx.ErrNotFound
	}
	if err != nil {
		return nil, Backup{}, err
	}
	b, err := s.backup(ctx, id)
	if err != nil {
		return nil, b, err
	}
	if !b.OnServer {
		return nil, b, errNotOnServer
	}
	f, err := os.Open(b.file)
	if err != nil {
		return nil, b, err
	}
	_ = audit.Log(ctx, s.pool, actorID, "backup_downloaded", "backup", id, map[string]string{"name": b.Name})
	return f, b, nil
}

// ---------- the nightly job (PL-FR-08) ----------

// NightlyStatus is what deploy/backup.sh last reported in its status folder.
type NightlyStatus struct {
	Configured  bool       `json:"configured"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	LastRunOK   bool       `json:"last_run_ok"`
	LastSuccess *time.Time `json:"last_success_at,omitempty"`
	// Stale: no success for 2 days.
	Stale bool `json:"stale"`
}

func (s *Service) nightlyStatus() NightlyStatus {
	n := NightlyStatus{Configured: s.nightlyDir != ""}
	if !n.Configured {
		return n
	}
	if raw, err := os.ReadFile(filepath.Join(s.nightlyDir, "status", "last-run")); err == nil {
		state, at, _ := strings.Cut(strings.TrimSpace(string(raw)), " ")
		if t, err := time.Parse(time.RFC3339, at); err == nil {
			n.LastRunAt = &t
		}
		n.LastRunOK = state == "ok"
	}
	if st, err := os.Stat(filepath.Join(s.nightlyDir, "status", "last-success")); err == nil {
		t := st.ModTime()
		n.LastSuccess = &t
	}
	n.Stale = n.LastSuccess == nil || s.now().Sub(*n.LastSuccess) > 48*time.Hour
	return n
}

// scanNightly records the nightly job's files in the history, and marks
// those its rotation deleted.
func (s *Service) scanNightly(ctx context.Context) {
	if s.nightlyDir == "" {
		return
	}
	var files []string
	for _, pattern := range []string{"*.age", "daily/*.age", "weekly/*.age"} {
		m, _ := filepath.Glob(filepath.Join(s.nightlyDir, pattern))
		files = append(files, m...)
	}
	seen := []string{}
	for _, f := range files {
		abs, err := filepath.Abs(f)
		st, err2 := os.Stat(f)
		if err != nil || err2 != nil {
			continue
		}
		seen = append(seen, abs)
		_, _ = s.pool.Exec(ctx, `INSERT INTO storage.backups (kind, file, status, started_at, finished_at, size_bytes)
			VALUES ('nightly', $1, 'done', $2, $2, $3)
			ON CONFLICT (file) DO UPDATE SET size_bytes=EXCLUDED.size_bytes, on_server=true, deleted_at=NULL`, abs, st.ModTime(), st.Size())
	}
	_, _ = s.pool.Exec(ctx, `UPDATE storage.backups SET on_server=false, deleted_at=$2 WHERE kind='nightly' AND on_server AND NOT (file = ANY($1))`, seen, s.now())
}

// ---------- clean-up (PL-FR-06) ----------

func (s *Service) area(id string) (Area, error) {
	for _, a := range s.areas {
		if a.ID == id {
			return a, nil
		}
	}
	return Area{}, httpx.Invalid(map[string]string{"area": "choose what to clean up"})
}

func (s *Service) checkBefore(before time.Time) error {
	// A date, read as midnight UTC; a day's slack covers time zones ahead of UTC.
	if before.IsZero() || before.After(s.now().Add(24*time.Hour)) {
		return httpx.Invalid(map[string]string{"before": "choose a date that isn't in the future"})
	}
	return nil
}

func (s *Service) PreviewCleanup(ctx context.Context, area string, before time.Time) (Freed, error) {
	a, err := s.area(area)
	if err == nil {
		err = s.checkBefore(before)
	}
	if err != nil {
		return Freed{}, err
	}
	return a.Preview(ctx, before)
}

// Cleanup deletes what the preview showed, and logs it.
func (s *Service) Cleanup(ctx context.Context, actorID, area string, before time.Time) (Freed, error) {
	a, err := s.area(area)
	if err == nil {
		err = s.checkBefore(before)
	}
	if err != nil {
		return Freed{}, err
	}
	f, err := a.Run(ctx, actorID, before)
	if err != nil {
		return f, err
	}
	err = audit.Log(ctx, s.pool, actorID, "cleanup_run", "storage", area, map[string]any{"before": before.Format(time.DateOnly), "items": f.Items, "bytes": f.Bytes})
	return f, err
}

const oldBackups = `WHERE b.kind = 'console' AND b.status = 'done' AND b.on_server AND b.started_at < $1`

func (s *Service) previewBackups(ctx context.Context, before time.Time) (Freed, error) {
	var f Freed
	err := s.pool.QueryRow(ctx, `SELECT count(*), coalesce(sum(size_bytes), 0) FROM storage.backups b `+oldBackups, before).Scan(&f.Items, &f.Bytes)
	return f, err
}

func (s *Service) cleanBackups(ctx context.Context, _ string, before time.Time) (Freed, error) {
	list, err := s.listBackups(ctx, oldBackups, 100000, 0, "b.started_at", before)
	if err != nil {
		return Freed{}, err
	}
	var f Freed
	for _, b := range list {
		if err := s.removeFile(ctx, b); err != nil {
			return f, err
		}
		f.Items++
		f.Bytes += b.SizeBytes
	}
	return f, nil
}

// ---------- the hourly job ----------

type SnapshotArgs struct{}

func (SnapshotArgs) Kind() string { return "storage_snapshot" }

// SnapshotWorker measures the space every hour, records the nightly job's
// files, and drops old measurements and links.
type SnapshotWorker struct {
	river.WorkerDefaults[SnapshotArgs]
	Service *Service
}

func (w *SnapshotWorker) Work(ctx context.Context, _ *river.Job[SnapshotArgs]) error {
	s := w.Service
	s.scanNightly(ctx)
	if _, err := s.TakeSnapshot(ctx); err != nil {
		return err
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM storage.snapshots WHERE taken_at < $1`, s.now().AddDate(0, 0, -90))
	_, err := s.pool.Exec(ctx, `DELETE FROM storage.download_links WHERE expires_at < $1`, s.now().Add(-24*time.Hour))
	return err
}

// Periodic is the hourly snapshot, also run when the server starts.
func Periodic() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(time.Hour), func() (river.JobArgs, *river.InsertOpts) {
		return SnapshotArgs{}, nil
	}, &river.PeriodicJobOpts{RunOnStart: true})
}
