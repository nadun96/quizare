package poll

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// File answers (D-40): sniffed, size-limited and stored in PostgreSQL. The
// type is decided from the bytes, not from what the browser claims.

var (
	imageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}
	docExt     = map[string]bool{".docx": true, ".xlsx": true, ".pptx": true, ".odt": true, ".ods": true, ".odp": true}
	// Browsers record audio as WebM/Ogg (Chrome, Firefox) or MP4 (Safari);
	// content sniffing reports WebM and MP4 as video even for audio only.
	audioTypes = map[string]bool{"video/webm": true, "audio/webm": true, "application/ogg": true, "audio/ogg": true, "video/mp4": true, "audio/mp4": true, "audio/mpeg": true, "audio/wave": true}
	videoTypes = map[string]bool{"video/webm": true, "video/mp4": true}
)

func baseType(ct string) string {
	t, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	}
	return t
}

// allowedFile decides whether sniffed content of name is acceptable for q
// and returns the content type to store.
func allowedFile(q Question, name, sniffed, claimed string) (string, bool) {
	sniffed, claimed = baseType(sniffed), baseType(claimed)
	switch q.Type {
	case Audio:
		if audioTypes[sniffed] {
			// Keep the browser's audio/* label for playback when the bytes agree.
			if strings.HasPrefix(claimed, "audio/") && audioTypes[claimed] {
				return claimed, true
			}
			return sniffed, true
		}
		return "", false
	case Video:
		return sniffed, videoTypes[sniffed]
	case File:
		ext := strings.ToLower(path.Ext(name))
		for _, a := range q.Body.Accept {
			switch {
			case (a == "image" || a == "any") && imageTypes[sniffed]:
				return sniffed, true
			case (a == "pdf" || a == "document" || a == "any") && sniffed == "application/pdf":
				return sniffed, true
			case (a == "document" || a == "any") && sniffed == "text/plain":
				return "text/plain; charset=utf-8", true
			case (a == "document" || a == "any") && sniffed == "application/zip" && docExt[ext]:
				return mime.TypeByExtension(ext), true
			}
		}
	}
	return "", false
}

func maxBytes(q Question) int64 {
	switch q.Type {
	case Audio:
		return AudioMaxBytes
	case Video:
		return VideoMaxBytes
	}
	return int64(q.Body.MaxMB) << 20
}

func cleanName(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == "/" {
		name = "answer"
	}
	if utf8.RuneCountInString(name) > 120 {
		name = string([]rune(name)[:120])
	}
	return name
}

// Upload stores a file answer for the caller, replacing any earlier one.
func (s *Service) Upload(ctx context.Context, code, questionID string, c Caller, name, claimedType string, body io.Reader) (FileRef, error) {
	p, q, pid, err := s.answerTarget(ctx, code, questionID, c)
	if err != nil {
		return FileRef{}, err
	}
	if !q.Type.IsMedia() {
		return FileRef{}, httpx.BadRequest("this question doesn't take a file")
	}
	if !s.uploads.Allow(pid) {
		return FileRef{}, httpx.NewError(http.StatusTooManyRequests, "rate_limited", "too many uploads; wait a moment")
	}
	limit := maxBytes(q)
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return FileRef{}, httpx.BadRequest("could not read the upload")
	}
	if len(data) == 0 {
		return FileRef{}, httpx.Invalid(map[string]string{"file": "the file is empty"})
	}
	if int64(len(data)) > limit {
		return FileRef{}, httpx.Invalid(map[string]string{"file": fmt.Sprintf("the file is larger than %d MB", limit>>20)})
	}
	name = cleanName(name)
	ct, ok := allowedFile(q, name, http.DetectContentType(data), claimedType)
	if !ok {
		return FileRef{}, httpx.Invalid(map[string]string{"file": "this kind of file isn't accepted here"})
	}
	var used int64
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(size), 0) FROM poll.files WHERE poll_id=$1 AND NOT (participant_id=$2 AND question_id=$3)`,
		p.ID, pid, q.ID).Scan(&used); err != nil {
		return FileRef{}, err
	}
	if used+int64(len(data)) > PollStorageLimit {
		return FileRef{}, httpx.Conflict("this poll has no room for more files")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return FileRef{}, err
	}
	defer tx.Rollback(ctx)
	ref := FileRef{Name: name, Size: len(data), ContentType: ct}
	if err := tx.QueryRow(ctx, `INSERT INTO poll.files (poll_id, question_id, participant_id, name, content_type, size, data) VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (question_id, participant_id) DO UPDATE SET id=gen_random_uuid(), name=EXCLUDED.name, content_type=EXCLUDED.content_type,
		size=EXCLUDED.size, data=EXCLUDED.data, created_at=now() RETURNING id`, p.ID, q.ID, pid, name, ct, len(data), data).Scan(&ref.ID); err != nil {
		return FileRef{}, err
	}
	raw, _ := json.Marshal(Answer{File: &ref})
	if _, err := tx.Exec(ctx, `INSERT INTO poll.responses (participant_id, question_id, poll_id, value, updated_at) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (participant_id, question_id) DO UPDATE SET value=EXCLUDED.value, updated_at=EXCLUDED.updated_at`, pid, q.ID, p.ID, raw, s.now()); err != nil {
		return FileRef{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return FileRef{}, err
	}
	s.hub.markDirty(p.ID, q.ID)
	return ref, nil
}

type StoredFile struct {
	Name        string
	ContentType string
	Data        []byte
}

// GetFile returns a file answer to the poll's owner.
func (s *Service) GetFile(ctx context.Context, teacherID, pollID, fileID string) (StoredFile, error) {
	if _, err := s.ownedPoll(ctx, teacherID, pollID); err != nil {
		return StoredFile{}, err
	}
	var f StoredFile
	err := s.pool.QueryRow(ctx, `SELECT name, content_type, data FROM poll.files WHERE id=$1 AND poll_id=$2`, fileID, pollID).Scan(&f.Name, &f.ContentType, &f.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, httpx.ErrNotFound
	}
	return f, err
}

// inlineSafe types can be shown in the page (players, images); everything
// else downloads.
func inlineSafe(ct string) bool {
	t := baseType(ct)
	return imageTypes[t] || strings.HasPrefix(t, "audio/") || strings.HasPrefix(t, "video/") || t == "application/ogg"
}
