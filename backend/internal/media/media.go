// Package media is pictures inside rich text (D-113): a teacher drops,
// pastes or picks a picture in the question, feedback, poll or tutorial
// editor, and it's stored here and shown in the text where it was put.
// Every picture is decoded and drawn again (PNG when it has transparency,
// JPEG otherwise), at most 1600 pixels wide, so nothing but pixels reaches
// a student: no scripts, no metadata, no location. Pictures are served by
// an unguessable id to anyone who has the text (students, and anonymous
// poll participants), cached for good. It owns the "media" schema.
package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // decoders for image.Decode
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

const (
	// MaxUpload is the largest file accepted, before it's redrawn.
	MaxUpload = 8 << 20
	maxPixels = 40_000_000
	maxWidth  = 1600
)

var (
	errTooBig = httpx.NewError(http.StatusRequestEntityTooLarge, "too_large", "pictures can be at most 8 MB")
	errFormat = httpx.Invalid(map[string]string{"file": "a PNG, JPEG, GIF or WebP picture"})
	errPixels = httpx.Invalid(map[string]string{"file": "the picture is too large: at most 40 megapixels"})
)

var allowed = map[string]bool{"png": true, "jpeg": true, "gif": true, "webp": true}

// Image is a stored picture.
type Image struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Bytes  int    `json:"bytes"`
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// URL is where a picture is served; the rich text renderer shows only these.
func URL(id string) string { return "/api/images/" + id }

// process decodes a picture and draws it again, scaled to at most maxWidth.
func process(data []byte) ([]byte, string, int, int, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || !allowed[format] {
		return nil, "", 0, 0, errFormat
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width*cfg.Height > maxPixels {
		return nil, "", 0, 0, errPixels
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", 0, 0, errFormat
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxWidth {
		h = h * maxWidth / w
		w = maxWidth
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	var out bytes.Buffer
	if hasAlpha(dst) {
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&out, dst); err != nil {
			return nil, "", 0, 0, err
		}
		return out.Bytes(), "image/png", w, h, nil
	}
	flat := image.NewRGBA(dst.Bounds())
	draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), dst, image.Point{}, draw.Over)
	if err := jpeg.Encode(&out, flat, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", 0, 0, err
	}
	return out.Bytes(), "image/jpeg", w, h, nil
}

func hasAlpha(img *image.RGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff {
			return true
		}
	}
	return false
}

// Upload stores a teacher's picture.
func (s *Service) Upload(ctx context.Context, ownerID string, r io.Reader) (Image, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxUpload+1))
	if err != nil {
		return Image{}, err
	}
	if len(data) > MaxUpload {
		return Image{}, errTooBig
	}
	if len(data) == 0 {
		return Image{}, errFormat
	}
	img, mime, w, h, err := process(data)
	if err != nil {
		return Image{}, err
	}
	out := Image{Width: w, Height: h, Bytes: len(img)}
	if err := s.pool.QueryRow(ctx, `INSERT INTO media.images (owner_id, mime, data, width, height, bytes) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		ownerID, mime, img, w, h, len(img)).Scan(&out.ID); err != nil {
		return Image{}, err
	}
	out.URL = URL(out.ID)
	return out, nil
}

// Get returns a picture and its type.
func (s *Service) Get(ctx context.Context, id string) ([]byte, string, error) {
	var data []byte
	var mime string
	err := s.pool.QueryRow(ctx, `SELECT data, mime FROM media.images WHERE id::text = $1`, id).Scan(&data, &mime)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", httpx.ErrNotFound
	}
	return data, mime, err
}

// unused is SQL for pictures no text refers to: questions (and their
// copies frozen in quiz sessions), polls, and tutorial pages.
const unused = ` FROM media.images i WHERE i.created_at < $1
	AND NOT EXISTS (SELECT 1 FROM quiz.questions q WHERE strpos(q.text || q.feedback::text || q.body::text, i.id::text) > 0)
	AND NOT EXISTS (SELECT 1 FROM live.sessions s WHERE strpos(s.snapshot::text, i.id::text) > 0)
	AND NOT EXISTS (SELECT 1 FROM poll.questions p WHERE strpos(p.text || p.body::text, i.id::text) > 0)
	AND NOT EXISTS (SELECT 1 FROM tutorial.pages t WHERE strpos(t.blocks::text, i.id::text) > 0)
	AND NOT EXISTS (SELECT 1 FROM tutorial.tutorials t WHERE strpos(t.description, i.id::text) > 0)`

// PreviewUnused counts pictures uploaded before the date that nothing uses.
func (s *Service) PreviewUnused(ctx context.Context, before time.Time) (int, int64, error) {
	var n int
	var b int64
	err := s.pool.QueryRow(ctx, `SELECT count(*), coalesce(sum(i.bytes), 0)`+unused, before).Scan(&n, &b)
	return n, b, err
}

// DeleteUnused deletes them.
func (s *Service) DeleteUnused(ctx context.Context, before time.Time) (int, int64, error) {
	var n int
	var b int64
	err := s.pool.QueryRow(ctx, `WITH gone AS (DELETE FROM media.images WHERE id IN (SELECT i.id`+unused+`) RETURNING bytes)
		SELECT count(*), coalesce(sum(bytes), 0) FROM gone`, before).Scan(&n, &b)
	return n, b, err
}
