package presentation

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Uploading a file (PR-FR-01, PR-FR-02, PR-FR-05). The teacher's browser
// renders the PDF's pages; the upload goes in beside the file in use, as a
// new version: the PDF, then each slide as a JPEG with its text. Finishing
// swaps it in, so students never see a half-uploaded deck; the old file and
// slides are then deleted. View records stay with their slide numbers.

var errUploadGone = httpx.NewError(http.StatusConflict, "upload_gone", "this upload was replaced or finished; start again")

// BeginInput starts an upload.
type BeginInput struct {
	Name   string `json:"name"`
	Slides int    `json:"slides"`
}

// Begin starts uploading a new file and returns its version.
func (s *Service) Begin(ctx context.Context, teacherID, id string, in BeginInput) (int, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return 0, err
	}
	in.Name = strings.TrimSpace(path.Base(strings.ReplaceAll(in.Name, `\`, "/")))
	f := map[string]string{}
	if in.Slides < 1 || in.Slides > MaxSlides {
		f["slides"] = "a presentation has 1 to 300 slides"
	}
	if !strings.HasSuffix(strings.ToLower(in.Name), ".pdf") || runes(in.Name) > 200 {
		f["name"] = "a PDF file (its name ending in .pdf)"
	}
	if len(f) > 0 {
		return 0, httpx.Invalid(f)
	}
	var version int
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Only one upload at a time: an unfinished one is dropped.
		if _, err := tx.Exec(ctx, `DELETE FROM presentation.files WHERE presentation_id=$1 AND version <> $2`, id, p.Version); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO presentation.files(presentation_id, version, name, slides)
			VALUES ($1, GREATEST((SELECT coalesce(max(version), 0) FROM presentation.files WHERE presentation_id=$1), $2) + 1, $3, $4) RETURNING version`,
			id, p.Version, in.Name, in.Slides).Scan(&version)
	})
	return version, err
}

// pending checks an upload is still waiting to finish, and returns its slide count.
func (s *Service) pending(ctx context.Context, teacherID, id string, version int) (Presentation, int, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return p, 0, err
	}
	var slides int
	err = s.pool.QueryRow(ctx, `SELECT slides FROM presentation.files WHERE presentation_id=$1 AND version=$2 AND version > $3`, id, version, p.Version).Scan(&slides)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, 0, errUploadGone
	}
	return p, slides, err
}

// PutPDF stores the upload's PDF.
func (s *Service) PutPDF(ctx context.Context, teacherID, id string, version int, body io.Reader) error {
	if _, _, err := s.pending(ctx, teacherID, id, version); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxPDFBytes+1))
	if err != nil {
		return httpx.BadRequest("the file didn't arrive whole; try again")
	}
	if len(data) > MaxPDFBytes {
		return httpx.Invalid(map[string]string{"file": "a PDF of at most 50 MB"})
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return httpx.Invalid(map[string]string{"file": "this isn't a PDF"})
	}
	_, err = s.pool.Exec(ctx, `UPDATE presentation.files SET pdf=$3, bytes=$4 WHERE presentation_id=$1 AND version=$2`, id, version, data, len(data))
	return err
}

// PutSlide stores one slide: a JPEG and the slide's text.
func (s *Service) PutSlide(ctx context.Context, teacherID, id string, version, n int, text string, body io.Reader) error {
	_, slides, err := s.pending(ctx, teacherID, id, version)
	if err != nil {
		return err
	}
	if n < 1 || n > slides {
		return httpx.ErrNotFound
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxSlideBytes+1))
	if err != nil {
		return httpx.BadRequest("the slide didn't arrive whole; try again")
	}
	if len(data) > MaxSlideBytes {
		return httpx.Invalid(map[string]string{"image": "a slide image of at most 3 MB"})
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4000 || cfg.Height > 4000 {
		return httpx.Invalid(map[string]string{"image": "a JPEG image, at most 4000 pixels a side"})
	}
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > maxSlideText {
		text = string(r[:maxSlideText])
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO presentation.slides(presentation_id, version, n, image, width, height, text) VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (presentation_id, version, n) DO UPDATE SET image=EXCLUDED.image, width=EXCLUDED.width, height=EXCLUDED.height, text=EXCLUDED.text`,
		id, version, n, data, cfg.Width, cfg.Height, text)
	return err
}

// Finish swaps the upload in once the PDF and every slide are there.
func (s *Service) Finish(ctx context.Context, teacherID, id string, version int) (Presentation, error) {
	p, slides, err := s.pending(ctx, teacherID, id, version)
	if err != nil {
		return p, err
	}
	var have int
	var hasPDF bool
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM presentation.slides WHERE presentation_id=$1 AND version=$2),
		(SELECT pdf IS NOT NULL FROM presentation.files WHERE presentation_id=$1 AND version=$2)`, id, version).Scan(&have, &hasPDF); err != nil {
		return p, err
	}
	if !hasPDF || have != slides {
		return p, httpx.NewError(http.StatusConflict, "upload_incomplete", "some slides or the PDF are missing; upload them first")
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE presentation.presentations SET version=$2, updated_at=now() WHERE id=$1`, id, version); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM presentation.files WHERE presentation_id=$1 AND version <> $2`, id, version); err != nil {
			return err
		}
		// Students past the end of a shorter deck come back within it.
		if _, err := tx.Exec(ctx, `UPDATE presentation.viewers SET current=LEAST(current, $2), reached=LEAST(reached, $2) WHERE presentation_id=$1`, id, slides); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "presentation_file_uploaded", "presentation", id, map[string]any{"slides": slides, "replaced": p.Version > 0})
	})
	if err != nil {
		return p, err
	}
	return s.owned(ctx, teacherID, id)
}

// Slide is a slide's details.
type Slide struct {
	N      int    `json:"n"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Text   string `json:"text"`
}

func (s *Service) slides(ctx context.Context, id string, version int) ([]Slide, error) {
	rows, err := s.pool.Query(ctx, `SELECT n, width, height, text FROM presentation.slides WHERE presentation_id=$1 AND version=$2 ORDER BY n`, id, version)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Slide, error) {
		var sl Slide
		return sl, r.Scan(&sl.N, &sl.Width, &sl.Height, &sl.Text)
	})
	if out == nil {
		out = []Slide{}
	}
	return out, err
}

// Slides lists the slides in use, for the teacher.
func (s *Service) Slides(ctx context.Context, teacherID, id string) ([]Slide, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return nil, err
	}
	return s.slides(ctx, id, p.Version)
}

func (s *Service) image(ctx context.Context, id string, version, n int) ([]byte, error) {
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT image FROM presentation.slides WHERE presentation_id=$1 AND version=$2 AND n=$3`, id, version, n).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	return data, err
}

// TeacherImage returns a slide of the file in use, or of an upload.
func (s *Service) TeacherImage(ctx context.Context, teacherID, id string, version, n int) ([]byte, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return nil, err
	}
	if version == 0 {
		version = p.Version
	}
	return s.image(ctx, id, version, n)
}

// PDF is a presentation's file.
type PDF struct {
	Name string
	Data []byte
}

func (s *Service) pdf(ctx context.Context, id string, version int) (PDF, error) {
	var f PDF
	err := s.pool.QueryRow(ctx, `SELECT name, pdf FROM presentation.files WHERE presentation_id=$1 AND version=$2 AND pdf IS NOT NULL`, id, version).Scan(&f.Name, &f.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, httpx.ErrNotFound
	}
	return f, err
}

// TeacherPDF returns the file in use.
func (s *Service) TeacherPDF(ctx context.Context, teacherID, id string) (PDF, error) {
	p, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return PDF{}, err
	}
	return s.pdf(ctx, id, p.Version)
}
