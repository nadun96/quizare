package tutor

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Recording the presenter (D-80, TS-FR-75 to TS-FR-79, TS-NFR-24). The
// browser of the teacher who pressed Record draws the broadcaster's screen
// and mixes their voice as the broadcast changes hands, and uploads what
// it records every few seconds. This service only stores it: each chunk is
// encrypted (AES-256-GCM, a key derived from the shared secret) and
// appended to the recording's file, never re-encoded. A recording is deleted
// 2 days after it ends, and all of them together stay under a limit.

const (
	DefaultRecordingLimit = 5 << 30 // PO-24
	recordingKeep         = 48 * time.Hour
	maxChunk              = 16 << 20
	staleAfter            = 2 * time.Minute // a recorder that went quiet (tab closed) has stopped
	playFor               = 5 * time.Minute // how long a play or download link works
)

// Recording is one recording of a session.
type Recording struct {
	ID            string     `json:"id"`
	SessionID     string     `json:"session_id"`
	SessionTitle  string     `json:"session_title,omitempty"`
	StartedBy     string     `json:"started_by"`
	StartedByName string     `json:"started_by_name"`
	Status        string     `json:"status"` // recording, paused, stopped
	Mime          string     `json:"mime"`
	Bytes         int64      `json:"bytes"`
	Chunks        int        `json:"chunks"`
	Viewers       string     `json:"viewers"` // teachers, students
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	// Lead: the person asking leads the session (or is the admin): they
	// download, choose who watches and delete (TS-FR-78).
	Lead      bool   `json:"lead"`
	teacherID string // the session's lead teacher
}

const recordingCols = `r.id, r.session_id, s.title, r.started_by, r.started_by_name, r.status, r.mime, r.bytes, r.chunks, r.viewers, r.started_at, r.ended_at, r.expires_at, s.teacher_id`

const recordingFrom = ` FROM tutoring.recordings r JOIN tutoring.sessions s ON s.id = r.session_id`

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func scanRecording(row pgx.Row) (Recording, error) {
	var r Recording
	err := row.Scan(&r.ID, &r.SessionID, &r.SessionTitle, &r.StartedBy, &r.StartedByName, &r.Status, &r.Mime, &r.Bytes, &r.Chunks, &r.Viewers, &r.StartedAt, &r.EndedAt, &r.ExpiresAt, &r.teacherID)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, web.ErrNotFound
	}
	return r, err
}

// recordings is where they're kept, and the key they're kept under.
type recordings struct {
	dir string
	key []byte // AES-256, for the files
	mac []byte // HMAC, for play links
}

var (
	errNoRecording = web.NewError(http.StatusNotFound, "recording_off", "recording isn't set up on this server")
	errRecFull     = web.NewError(http.StatusConflict, "recording_full", "the space for recordings is full: delete old recordings, or ask your administrator for more")
	errRecording   = web.NewError(http.StatusConflict, "already_recording", "this session is already being recorded")
	errRecStopped  = web.NewError(http.StatusConflict, "recording_stopped", "this recording has stopped")
	errRecOrder    = web.NewError(http.StatusConflict, "chunk_out_of_order", "a part of the recording is missing; send the parts in order")
)

// SetRecordings turns recording on: files go in dir, encrypted with a key
// derived from secret.
func (s *Service) SetRecordings(dir string, secret []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	key, err := hkdf.Key(sha256.New, secret, nil, "tutoring recordings v1", 32)
	if err != nil {
		return err
	}
	mac, err := hkdf.Key(sha256.New, secret, nil, "tutoring recording links v1", 32)
	if err != nil {
		return err
	}
	s.rec = &recordings{dir: dir, key: key, mac: mac}
	return nil
}

func (rs *recordings) path(id string) string { return filepath.Join(rs.dir, id+".rec") }

// seal encrypts one chunk; the recording and the chunk's number are bound
// in, so chunks can't be moved between recordings or reordered.
func (rs *recordings) seal(id string, n int, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(rs.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, plain, []byte(id+"/"+strconv.Itoa(n)))
	frame := make([]byte, 4, 4+len(nonce)+len(sealed))
	binary.BigEndian.PutUint32(frame, uint32(len(nonce)+len(sealed)))
	return append(append(frame, nonce...), sealed...), nil
}

// open writes the recording's plain bytes to w, chunk by chunk.
func (rs *recordings) open(id string, w io.Writer) error {
	f, err := os.Open(rs.path(id))
	if err != nil {
		return err
	}
	defer f.Close()
	block, err := aes.NewCipher(rs.key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	var size [4]byte
	for n := 0; ; n++ {
		if _, err := io.ReadFull(f, size[:]); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		frame := make([]byte, binary.BigEndian.Uint32(size[:]))
		if _, err := io.ReadFull(f, frame); err != nil {
			return err
		}
		ns := gcm.NonceSize()
		if len(frame) < ns {
			return fmt.Errorf("recording %s: chunk %d is damaged", id, n)
		}
		plain, err := gcm.Open(nil, frame[:ns], frame[ns:], []byte(id+"/"+strconv.Itoa(n)))
		if err != nil {
			return fmt.Errorf("recording %s: chunk %d: %w", id, n, err)
		}
		if _, err := w.Write(plain); err != nil {
			return err
		}
	}
}

// RecordingLimit is the space all recordings may use together.
func (s *Service) RecordingLimit(ctx context.Context) (int64, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM tutoring.settings WHERE key='recording_limit'`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultRecordingLimit, nil
	}
	if err != nil {
		return 0, err
	}
	var n int64
	err = json.Unmarshal(raw, &n)
	return n, err
}

func (s *Service) recordingUsed(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(bytes), 0) FROM tutoring.recordings`).Scan(&n)
	return n, err
}

// StartRecording starts recording the presenter (TS-FR-75): the lead
// teacher or a co-teacher, in a live session. Everyone is shown it at once
// (TS-FR-76).
func (s *Service) StartRecording(ctx context.Context, p authn.Person, id, mime string) (Recording, error) {
	if s.rec == nil {
		return Recording{}, errNoRecording
	}
	sess, err := s.staffed(ctx, p, id)
	if err != nil {
		return Recording{}, err
	}
	if sess.Status != "live" {
		return Recording{}, errNotLive
	}
	mime = strings.TrimSpace(mime)
	if !strings.HasPrefix(mime, "video/webm") && !strings.HasPrefix(mime, "video/mp4") || len(mime) > 100 {
		return Recording{}, web.Invalid(map[string]string{"mime": "video/webm or video/mp4"})
	}
	used, err := s.recordingUsed(ctx)
	if err != nil {
		return Recording{}, err
	}
	limit, err := s.RecordingLimit(ctx)
	if err != nil {
		return Recording{}, err
	}
	if used >= limit {
		return Recording{}, errRecFull
	}
	me, err := s.participant(ctx, id, p.ID)
	if err != nil {
		return Recording{}, err
	}
	var rid string
	err = s.pool.QueryRow(ctx, `INSERT INTO tutoring.recordings (session_id, started_by, started_by_name, mime, started_at, last_chunk_at)
		VALUES ($1,$2,$3,$4,$5,$5) RETURNING id`, id, p.ID, me.Name, mime, s.now()).Scan(&rid)
	if isUnique(err) {
		return Recording{}, errRecording
	}
	if err != nil {
		return Recording{}, err
	}
	if err := os.WriteFile(s.rec.path(rid), nil, 0o600); err != nil {
		return Recording{}, err
	}
	s.logAction(ctx, id, p.ID, "recording_started", nil, map[string]string{"recording": rid})
	s.hub.changed(id)
	return s.recording(ctx, rid)
}

func (s *Service) recording(ctx context.Context, rid string) (Recording, error) {
	return scanRecording(s.pool.QueryRow(ctx, `SELECT `+recordingCols+recordingFrom+` WHERE r.id=$1`, rid))
}

// sessionRecording finds a recording of a session.
func (s *Service) sessionRecording(ctx context.Context, id, rid string) (Recording, error) {
	r, err := s.recording(ctx, rid)
	if err == nil && r.SessionID != id {
		return r, web.ErrNotFound
	}
	return r, err
}

// Chunk appends the next part of a recording, from the browser recording
// it. Parts come in order; one sent again (a retry) is accepted and ignored.
func (s *Service) Chunk(ctx context.Context, p authn.Person, id, rid string, n int, body io.Reader) error {
	if s.rec == nil {
		return errNoRecording
	}
	r, err := s.sessionRecording(ctx, id, rid)
	if err != nil {
		return err
	}
	switch {
	case r.StartedBy != p.ID:
		return web.ErrForbidden
	case r.Status == "stopped":
		return errRecStopped
	case n < r.Chunks:
		return nil // already here
	case n > r.Chunks:
		return errRecOrder
	}
	data, err := io.ReadAll(io.LimitReader(body, maxChunk+1))
	if err != nil {
		return web.BadRequest("the part didn't arrive whole; send it again")
	}
	if len(data) > maxChunk {
		return web.Invalid(map[string]string{"chunk": "at most 16 MB a part"})
	}
	limit, err := s.RecordingLimit(ctx)
	if err != nil {
		return err
	}
	used, err := s.recordingUsed(ctx)
	if err != nil {
		return err
	}
	if used+int64(len(data)) > limit {
		_ = s.stopRecording(ctx, r, p.ID, "recording_full")
		return errRecFull
	}
	frame, err := s.rec.seal(rid, n, data)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.rec.path(rid), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(frame); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE tutoring.recordings SET chunks=chunks+1, bytes=bytes+$3, last_chunk_at=$4 WHERE id=$1 AND chunks=$2`, rid, n, len(frame), s.now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errRecOrder
	}
	// The teachers hear when recordings near the limit (TS-FR-79).
	if before, after := float64(used)/float64(limit), float64(used+int64(len(frame)))/float64(limit); before < 0.8 && after >= 0.8 {
		s.toStaff(id, event{Type: "recording_space", Data: map[string]any{"used": used + int64(len(frame)), "limit": limit}})
	}
	return nil
}

// PauseRecording pauses or resumes it (TS-FR-75); the indicator stays (TS-FR-76).
func (s *Service) PauseRecording(ctx context.Context, p authn.Person, id, rid string, paused bool) error {
	if _, err := s.staffed(ctx, p, id); err != nil {
		return err
	}
	r, err := s.sessionRecording(ctx, id, rid)
	if err != nil {
		return err
	}
	if r.Status == "stopped" {
		return errRecStopped
	}
	status := "recording"
	if paused {
		status = "paused"
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.recordings SET status=$2, last_chunk_at=$3 WHERE id=$1`, rid, status, s.now()); err != nil {
		return err
	}
	s.hub.changed(id)
	return nil
}

// StopRecording ends it; it is kept for 2 days (PO-22).
func (s *Service) StopRecording(ctx context.Context, p authn.Person, id, rid string) (Recording, error) {
	if _, err := s.staffed(ctx, p, id); err != nil {
		return Recording{}, err
	}
	r, err := s.sessionRecording(ctx, id, rid)
	if err != nil {
		return r, err
	}
	if r.Status != "stopped" {
		if err := s.stopRecording(ctx, r, p.ID, "recording_stopped"); err != nil {
			return r, err
		}
	}
	return s.recording(ctx, rid)
}

func (s *Service) stopRecording(ctx context.Context, r Recording, actorID, action string) error {
	now := s.now()
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.recordings SET status='stopped', ended_at=$2, expires_at=$3 WHERE id=$1 AND status <> 'stopped'`,
		r.ID, now, now.Add(recordingKeep)); err != nil {
		return err
	}
	s.logAction(ctx, r.SessionID, actorID, action, nil, map[string]string{"recording": r.ID})
	s.hub.changed(r.SessionID)
	return nil
}

// mayWatch: the session's teachers, and its students when the lead teacher allows (TS-FR-78).
func (s *Service) mayWatch(ctx context.Context, p authn.Person, r Recording) (lead bool, err error) {
	if p.Role == "admin" {
		return true, nil
	}
	sess, err := s.session(ctx, r.SessionID)
	if err != nil {
		return false, err
	}
	if sess.TeacherID == p.ID {
		return true, nil
	}
	me, err := s.participant(ctx, r.SessionID, p.ID)
	if err != nil {
		return false, web.ErrNotFound
	}
	switch {
	case me.Staff():
		return false, nil
	case me.State == "admitted" && r.Viewers == "students" && r.Status == "stopped":
		return false, nil
	}
	return false, web.ErrNotFound
}

// Recordings lists a session's recordings that p may watch.
func (s *Service) Recordings(ctx context.Context, p authn.Person, id string) ([]Recording, error) {
	rows, _ := s.pool.Query(ctx, `SELECT `+recordingCols+recordingFrom+` WHERE r.session_id=$1 ORDER BY r.started_at`, id)
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Recording, error) { return scanRecording(r) })
	if err != nil {
		return nil, err
	}
	out := []Recording{}
	for _, r := range all {
		if lead, err := s.mayWatch(ctx, p, r); err == nil {
			r.Lead = lead
			out = append(out, r)
		}
	}
	return out, nil
}

// MyRecordings lists the recordings a student may watch, of sessions they took part in.
func (s *Service) MyRecordings(ctx context.Context, p authn.Person) ([]Recording, error) {
	rows, _ := s.pool.Query(ctx, `SELECT `+recordingCols+recordingFrom+`
		JOIN tutoring.participants pa ON pa.session_id = r.session_id AND pa.user_id = $1 AND pa.state = 'admitted'
		WHERE r.status = 'stopped' AND (r.viewers = 'students' OR pa.role IN ('teacher', 'coteacher'))
		ORDER BY r.started_at DESC LIMIT 100`, p.ID)
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Recording, error) { return scanRecording(r) })
	if out == nil {
		out = []Recording{}
	}
	for i := range out {
		out[i].Lead = out[i].teacherID == p.ID
	}
	return out, err
}

// SetViewers chooses who may watch: the teachers, or the students too (the lead teacher's).
func (s *Service) SetViewers(ctx context.Context, p authn.Person, id, rid, viewers string) error {
	if _, err := s.owned(ctx, p, id); err != nil {
		return err
	}
	if viewers != "teachers" && viewers != "students" {
		return web.Invalid(map[string]string{"viewers": "teachers or students"})
	}
	tag, err := s.pool.Exec(ctx, `UPDATE tutoring.recordings SET viewers=$3 WHERE id=$1 AND session_id=$2`, rid, id, viewers)
	if err == nil && tag.RowsAffected() == 0 {
		return web.ErrNotFound
	}
	return err
}

// DeleteRecording deletes one: the lead teacher their session's, the admin any (TS-FR-79).
func (s *Service) DeleteRecording(ctx context.Context, p authn.Person, rid string) error {
	r, err := s.recording(ctx, rid)
	if err != nil {
		return err
	}
	if lead, err := s.mayWatch(ctx, p, r); err != nil || !lead {
		return web.ErrNotFound
	}
	return s.deleteRecording(ctx, r, p.ID)
}

func (s *Service) deleteRecording(ctx context.Context, r Recording, actorID string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM tutoring.recordings WHERE id=$1`, r.ID); err != nil {
		return err
	}
	if s.rec != nil {
		if err := os.Remove(s.rec.path(r.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("deleting a recording's file", "recording", r.ID, "err", err)
		}
	}
	s.logAction(ctx, r.SessionID, actorID, "recording_deleted", nil, map[string]string{"recording": r.ID})
	s.hub.changed(r.SessionID)
	return nil
}

// SweepRecordings deletes recordings 2 days after they end, and stops ones
// whose browser went quiet (the tab closed): what arrived is kept.
func (s *Service) SweepRecordings(ctx context.Context) error {
	now := s.now()
	rows, _ := s.pool.Query(ctx, `SELECT `+recordingCols+recordingFrom+` WHERE r.status <> 'stopped' AND r.last_chunk_at < $1 AND r.status = 'recording'`, now.Add(-staleAfter))
	stale, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Recording, error) { return scanRecording(r) })
	if err != nil {
		return err
	}
	for _, r := range stale {
		if err := s.stopRecording(ctx, r, r.StartedBy, "recording_stopped_quiet"); err != nil {
			return err
		}
	}
	rows, _ = s.pool.Query(ctx, `SELECT `+recordingCols+recordingFrom+` WHERE r.expires_at < $1`, now)
	old, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Recording, error) { return scanRecording(r) })
	if err != nil {
		return err
	}
	for _, r := range old {
		if err := s.deleteRecording(ctx, r, r.StartedBy); err != nil {
			return err
		}
	}
	return nil
}

// ---------- watching: signed links (a <video> can't send a token) ----------

func (rs *recordings) sign(rid string, exp int64, download bool) string {
	m := hmac.New(sha256.New, rs.mac)
	fmt.Fprintf(m, "%s|%d|%t", rid, exp, download)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// PlayLink returns a 5-minute link to watch a recording, or to download it
// (the lead teacher and the admin only, TS-FR-78).
func (s *Service) PlayLink(ctx context.Context, p authn.Person, rid string, download bool) (string, error) {
	if s.rec == nil {
		return "", errNoRecording
	}
	r, err := s.recording(ctx, rid)
	if err != nil {
		return "", err
	}
	lead, err := s.mayWatch(ctx, p, r)
	if err != nil {
		return "", err
	}
	if download && !lead {
		return "", web.NewError(http.StatusForbidden, "forbidden", "only the lead teacher can download it")
	}
	exp := s.now().Add(playFor).Unix()
	q := fmt.Sprintf("exp=%d&sig=%s", exp, s.rec.sign(rid, exp, download))
	if download {
		q += "&download=1"
	}
	return "/media/recordings/" + rid + "?" + q, nil
}

// ServeRecording is GET /media/recordings/{id}, with a link from PlayLink.
func (s *Service) ServeRecording(w http.ResponseWriter, r *http.Request, rid string) {
	if s.rec == nil {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	exp, _ := strconv.ParseInt(q.Get("exp"), 10, 64)
	download := q.Get("download") == "1"
	want := s.rec.sign(rid, exp, download)
	if exp < s.now().Unix() || !hmac.Equal([]byte(want), []byte(q.Get("sig"))) {
		http.Error(w, "this link has expired: open the recording again", http.StatusForbidden)
		return
	}
	rec, err := s.recording(r.Context(), rid)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", rec.Mime)
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	ext := ".webm"
	if strings.HasPrefix(rec.Mime, "video/mp4") {
		ext = ".mp4"
	}
	disp := "inline"
	if download {
		disp = "attachment"
	}
	name := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`"\/:*?<>|`, r) {
			return '-'
		}
		return r
	}, rec.SessionTitle) + " " + rec.StartedAt.Format("2006-01-02 15.04") + ext
	h.Set("Content-Disposition", fmt.Sprintf("%s; filename*=UTF-8''%s", disp, strings.ReplaceAll(urlEscape(name), "+", "%20")))
	if err := s.rec.open(rid, w); err != nil {
		s.log.Warn("reading a recording", "recording", rid, "err", err)
	}
}

func urlEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("-._~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// ---------- the admin's view (TS-FR-78, PO-22, PO-24) ----------

// RecordingSpace is how much recordings use, and may.
type RecordingSpace struct {
	Used  int64 `json:"used"`
	Limit int64 `json:"limit"`
}

func requireAdmin(p authn.Person) error {
	if p.Role != "admin" {
		return web.ErrForbidden
	}
	return nil
}

// AdminRecordings lists every recording, newest first, a page at a time.
func (s *Service) AdminRecordings(ctx context.Context, p authn.Person, pg Page) ([]Recording, int, RecordingSpace, error) {
	var space RecordingSpace
	if err := requireAdmin(p); err != nil {
		return nil, 0, space, err
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+recordingFrom+` WHERE ($1 = '' OR s.title ILIKE $1 OR r.started_by_name ILIKE $1)`, pg.Like()).Scan(&total); err != nil {
		return nil, 0, space, err
	}
	rows, _ := s.pool.Query(ctx, `SELECT `+recordingCols+recordingFrom+` WHERE ($1 = '' OR s.title ILIKE $1 OR r.started_by_name ILIKE $1)
		ORDER BY r.started_at DESC, r.id`+pg.Limit(), pg.Like())
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Recording, error) { return scanRecording(r) })
	if err != nil {
		return nil, 0, space, err
	}
	if list == nil {
		list = []Recording{}
	}
	for i := range list {
		list[i].Lead = true
	}
	if space.Used, err = s.recordingUsed(ctx); err != nil {
		return nil, 0, space, err
	}
	space.Limit, err = s.RecordingLimit(ctx)
	return list, total, space, err
}

// SetRecordingLimit changes the space recordings may use (PO-24): at least
// 100 MB, and no more than the disk has free beside what they use now.
func (s *Service) SetRecordingLimit(ctx context.Context, p authn.Person, limit int64) (RecordingSpace, error) {
	if err := requireAdmin(p); err != nil {
		return RecordingSpace{}, err
	}
	used, err := s.recordingUsed(ctx)
	if err != nil {
		return RecordingSpace{}, err
	}
	if limit < 100<<20 {
		return RecordingSpace{}, web.Invalid(map[string]string{"limit": "at least 100 MB"})
	}
	if free, ok := freeSpace(s.rec); ok && limit > used+free {
		return RecordingSpace{}, web.Invalid(map[string]string{"limit": fmt.Sprintf("the disk has room for at most %d MB", (used+free)>>20)})
	}
	raw, _ := json.Marshal(limit)
	if _, err := s.pool.Exec(ctx, `INSERT INTO tutoring.settings (key, value) VALUES ('recording_limit', $1) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, raw); err != nil {
		return RecordingSpace{}, err
	}
	return RecordingSpace{Used: used, Limit: limit}, nil
}

// AdminDeleteRecording deletes any recording.
func (s *Service) AdminDeleteRecording(ctx context.Context, p authn.Person, rid string) error {
	if err := requireAdmin(p); err != nil {
		return err
	}
	r, err := s.recording(ctx, rid)
	if err != nil {
		return err
	}
	return s.deleteRecording(ctx, r, p.ID)
}

// RecordingState is the recording under way, for the indicator.
type RecordingState struct {
	ID            string    `json:"id"`
	Status        string    `json:"status"` // recording, paused
	StartedBy     string    `json:"started_by"`
	StartedByName string    `json:"started_by_name"`
	StartedAt     time.Time `json:"started_at"`
}
