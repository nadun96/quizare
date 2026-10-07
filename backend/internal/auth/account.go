package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // decoders for uploads
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	qmail "github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Account self-service (D-49): changing your password and your profile picture.

// ---------- password ----------

// ChangePassword checks the current password, sets a new one and signs out
// every other session (keepToken is the caller's own session cookie).
func (s *Service) ChangePassword(ctx context.Context, u User, current, next, keepToken string) error {
	if !s.emailLimiter.Allow("pwchange:" + u.ID) {
		return errRateLimited // guessing the current password is throttled like logins
	}
	var hash string
	if err := s.pool.QueryRow(ctx, `SELECT password_hash FROM auth.users WHERE id=$1`, u.ID).Scan(&hash); err != nil {
		return err
	}
	ok, err := s.hasher.Verify(ctx, current, hash)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Invalid(map[string]string{"current_password": "that isn't your current password"})
	}
	if msg := checkPassword(next); msg != "" {
		return httpx.Invalid(map[string]string{"new_password": msg})
	}
	if next == current {
		return httpx.Invalid(map[string]string{"new_password": "choose a password different from your current one"})
	}
	newHash, err := s.hasher.Hash(ctx, next)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE auth.users SET password_hash=$2, updated_at=$3 WHERE id=$1`, u.ID, newHash, s.now()); err != nil {
			return err
		}
		// Other devices are signed out; this one stays.
		if _, err := tx.Exec(ctx, `DELETE FROM auth.sessions WHERE user_id=$1 AND token_hash <> $2`, u.ID, hashToken(keepToken)); err != nil {
			return err
		}
		s.dropUserCache(u.ID)
		msg := qmail.Message{To: u.Email, Subject: "Your password was changed",
			Body: fmt.Sprintf("Hi %s,\n\nThe password for your Classroom Quiz account was just changed, and your other devices were signed out.\n\nIf this wasn't you, reset your password now:\n%s/forgot-password", u.Name, s.baseURL)}
		if _, err := s.jobs.InsertTx(ctx, tx, qmail.Args{Message: msg}, nil); err != nil {
			return err
		}
		return audit.Log(ctx, tx, u.ID, "password_changed", "user", u.ID, nil)
	})
}

// ---------- profile picture ----------

const (
	// MaxAvatarUpload is the largest picture accepted (the stored one is much smaller).
	MaxAvatarUpload = 512 << 10
	avatarSize      = 256
	maxAvatarPixels = 40_000_000 // refuse decompression bombs before decoding
)

var (
	errAvatarTooBig  = httpx.NewError(http.StatusRequestEntityTooLarge, "too_large", "pictures must be 512 KB or smaller")
	errAvatarFormat  = httpx.Invalid(map[string]string{"avatar": "use a PNG, JPEG, WebP or GIF picture"})
	errAvatarPixels  = httpx.Invalid(map[string]string{"avatar": "that picture is too large; use one under 8000 × 8000 pixels"})
	allowedAvatarFmt = map[string]bool{"png": true, "jpeg": true, "gif": true, "webp": true}
)

// Relations says whether two people share a classroom (a teacher and their
// student), which lets them see each other's pictures. The content module
// provides it.
type Relations interface {
	SharesClassroom(ctx context.Context, a, b string) (bool, error)
}

// SetRelations connects the content module (created after auth).
func (s *Service) SetRelations(r Relations) { s.relations = r }

// processAvatar decodes an upload and re-encodes it as a square 256×256
// JPEG on white. Only pixels survive: no EXIF (location, camera), no
// comments, no polyglot tricks.
func processAvatar(data []byte) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || !allowedAvatarFmt[format] {
		return nil, errAvatarFormat
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8000 || cfg.Height > 8000 || cfg.Width*cfg.Height > maxAvatarPixels {
		return nil, errAvatarPixels
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errAvatarFormat
	}
	// Centre square crop.
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	crop := image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, 0, 0)
	crop.Max = crop.Min.Add(image.Pt(side, side))
	dst := image.NewRGBA(image.Rect(0, 0, avatarSize, avatarSize))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, xdraw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// SetAvatar stores a new profile picture and returns its version.
func (s *Service) SetAvatar(ctx context.Context, u User, r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxAvatarUpload+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxAvatarUpload {
		return "", errAvatarTooBig
	}
	img, err := processAvatar(data)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(img)
	version := hex.EncodeToString(sum[:6])
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO auth.avatars (user_id, image, updated_at) VALUES ($1,$2,$3)
			ON CONFLICT (user_id) DO UPDATE SET image=EXCLUDED.image, updated_at=EXCLUDED.updated_at`, u.ID, img, s.now()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE auth.users SET avatar_version=$2, updated_at=$3 WHERE id=$1`, u.ID, version, s.now())
		return err
	})
	s.dropUserCache(u.ID)
	return version, err
}

// DeleteAvatar removes the profile picture.
func (s *Service) DeleteAvatar(ctx context.Context, u User) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM auth.avatars WHERE user_id=$1`, u.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE auth.users SET avatar_version=NULL, updated_at=$2 WHERE id=$1`, u.ID, s.now())
		return err
	})
	s.dropUserCache(u.ID)
	return err
}

// Avatar returns a user's picture for a viewer allowed to see it: the person
// themself, an admin, or someone who shares a classroom with them. Anyone
// else gets "not found", so pictures can't be probed.
func (s *Service) Avatar(ctx context.Context, viewer User, userID string) ([]byte, string, error) {
	allowed := viewer.ID == userID || viewer.Role == RoleAdmin
	if !allowed && s.relations != nil {
		var err error
		if allowed, err = s.relations.SharesClassroom(ctx, viewer.ID, userID); err != nil {
			return nil, "", err
		}
	}
	if !allowed {
		return nil, "", httpx.ErrNotFound
	}
	var img []byte
	var version string
	err := s.pool.QueryRow(ctx, `SELECT a.image, coalesce(u.avatar_version, '') FROM auth.avatars a JOIN auth.users u ON u.id=a.user_id
		WHERE a.user_id=$1 AND u.status <> 'deleted'`, userID).Scan(&img, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", httpx.ErrNotFound
	}
	return img, version, err
}

// ---------- HTTP ----------

func (s *Service) accountRoutes(r chi.Router) {
	r.Method("POST", "/me/password", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, ok := CurrentUser(r.Context())
		if !ok {
			return httpx.ErrUnauthorized
		}
		var in struct {
			Current string `json:"current_password"`
			New     string `json:"new_password"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		token := ""
		if c, err := r.Cookie(CookieName); err == nil {
			token = c.Value
		}
		err := s.ChangePassword(r.Context(), u, in.Current, in.New, token)
		if writeBusy(w, err) {
			return nil
		}
		if err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("PUT", "/me/avatar", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, ok := CurrentUser(r.Context())
		if !ok {
			return httpx.ErrUnauthorized
		}
		v, err := s.SetAvatar(r.Context(), u, r.Body)
		if err != nil {
			return err
		}
		httpx.JSON(w, 200, map[string]string{"avatar": v})
		return nil
	}))
	r.Method("DELETE", "/me/avatar", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, ok := CurrentUser(r.Context())
		if !ok {
			return httpx.ErrUnauthorized
		}
		if err := s.DeleteAvatar(r.Context(), u); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("GET", "/users/{id}/avatar", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		u, ok := CurrentUser(r.Context())
		if !ok {
			return httpx.ErrUnauthorized
		}
		img, version, err := s.Avatar(r.Context(), u, chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		h := w.Header()
		h.Set("Content-Type", "image/jpeg")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		h.Set("ETag", `"`+version+`"`)
		// Versioned URLs never change; others are revalidated.
		if r.URL.Query().Get("v") == version {
			h.Set("Cache-Control", "private, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "private, no-cache")
		}
		if r.Header.Get("If-None-Match") == `"`+version+`"` {
			w.WriteHeader(http.StatusNotModified)
			return nil
		}
		_, err = w.Write(img)
		return err
	}))
}
