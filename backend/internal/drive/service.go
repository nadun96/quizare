// Package drive connects teachers' Google Drive (D-71): Drive files for
// classrooms and questions, importing questions from a CSV or Google Sheet,
// and results saved to Drive, on demand or after each session. It owns the
// "drive" schema: the platform's Google app, each teacher's connection (the
// refresh token sealed with the master key, ADR-09), classroom files and
// who they were shared with, and exports.
//
// Teachers grant only drive.file: the platform sees the files they pick in
// Google's picker and the ones it makes, never the rest of their Drive.
package drive

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/llm"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// Sealer seals and opens secrets with the master key.
type Sealer interface {
	Seal(plaintext []byte, owner, kind string, version int) (llm.Sealed, error)
	Open(s llm.Sealed, owner, kind string, version int) ([]byte, error)
}

// Classrooms is the slice of the content module drive needs.
type Classrooms interface {
	GetClassroom(ctx context.Context, teacherID, id string) (content.Classroom, error)
	GetEnrolment(ctx context.Context, classroomID, userID string) (content.Enrolment, bool, error)
	ListEnrolments(ctx context.Context, teacherID, classroomID, status string) ([]content.Enrolment, error)
}

// Questions imports questions into a quiz (the quiz module).
type Questions interface {
	ImportQuestions(ctx context.Context, teacherID, quizID string, data []byte) (quiz.ImportReport, error)
}

// Results makes the CSV of a session's or a poll's results, and its name.
type Results interface {
	SessionCSV(ctx context.Context, teacherID, sessionID string) (name string, data []byte, err error)
	PollCSV(ctx context.Context, teacherID, pollID string) (name string, data []byte, err error)
	PresentationCSV(ctx context.Context, teacherID, presentationID string) (name string, data []byte, err error)
	SessionTeacher(ctx context.Context, sessionID string) (string, error)
}

var (
	errOff       = httpx.NewError(http.StatusNotFound, "drive_off", "Google Drive isn't set up on this platform; ask your administrator")
	errNotLinked = httpx.NewError(http.StatusConflict, "drive_not_connected", "connect your Google Drive first")
	errReconnect = httpx.NewError(http.StatusConflict, "drive_reconnect", "Google no longer accepts the connection to your Drive; connect it again")
	errNoAccess  = httpx.NewError(http.StatusForbidden, "drive_no_access", "the platform can't open that file: choose it with the Google Drive picker")
	errDriveDown = httpx.NewError(http.StatusBadGateway, "drive_unavailable", "Google Drive didn't answer; try again shortly")
	errNotCSV    = httpx.Invalid(map[string]string{"file_id": "choose a CSV file or a Google Sheet"})
	errCSVTooBig = httpx.Invalid(map[string]string{"file_id": "the file is larger than 2 MB"})
)

// Config is the platform's Google Cloud app.
type Config struct {
	Enabled  bool   `json:"enabled"`
	ClientID string `json:"client_id"`
	// ClientSecret is write-only: empty on read, and empty on save keeps the stored one.
	ClientSecret    string     `json:"client_secret,omitempty"`
	ClientSecretSet bool       `json:"client_secret_set"`
	APIKey          string     `json:"api_key"`
	AppID           string     `json:"app_id"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

// Service is the Drive integration.
type Service struct {
	pool       *pgxpool.Pool
	vault      Sealer
	auth       *auth.Service
	classrooms Classrooms
	questions  Questions
	results    Results
	jobs       jobs.Inserter
	baseURL    string
	ep         Endpoints
	client     *http.Client
	log        *slog.Logger
	now        func() time.Time

	mu     sync.Mutex
	tokens map[string]*oauth2.Token // teacher → current access token
}

// NewService: client talks to Google (nil: a default with a timeout); ep
// zero means Google itself.
func NewService(pool *pgxpool.Pool, vault Sealer, a *auth.Service, classrooms Classrooms, questions Questions, results Results,
	jobs jobs.Inserter, baseURL string, ep Endpoints, client *http.Client, log *slog.Logger) *Service {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	if ep.API == "" {
		ep = Google
	}
	return &Service{pool: pool, vault: vault, auth: a, classrooms: classrooms, questions: questions, results: results, jobs: jobs,
		baseURL: strings.TrimRight(baseURL, "/"), ep: ep, client: client, log: log, now: time.Now, tokens: map[string]*oauth2.Token{}}
}

// ---------- the platform's Google app ----------

func (s *Service) config(ctx context.Context) (Config, string, error) {
	var c Config
	var at time.Time
	var sealed llm.Sealed
	var version int
	err := s.pool.QueryRow(ctx, `SELECT enabled, client_id, api_key, app_id, updated_at, secret_ciphertext, secret_nonce, secret_wrapped_dek, secret_dek_nonce,
		coalesce(secret_kek_id, ''), secret_version FROM drive.config`).
		Scan(&c.Enabled, &c.ClientID, &c.APIKey, &c.AppID, &at, &sealed.Ciphertext, &sealed.Nonce, &sealed.WrappedDEK, &sealed.DEKNonce, &sealed.KEKID, &version)
	if err != nil {
		return Config{}, "", err
	}
	c.UpdatedAt = &at
	c.ClientSecretSet = len(sealed.Ciphertext) > 0
	if !c.ClientSecretSet {
		return c, "", nil
	}
	secret, err := s.vault.Open(sealed, "drive", "client_secret", version)
	if err != nil {
		return c, "", err
	}
	defer clear(secret)
	return c, string(secret), nil
}

// Enabled says whether teachers can connect Drive.
func (s *Service) Enabled(ctx context.Context) bool {
	var on bool
	_ = s.pool.QueryRow(ctx, `SELECT enabled FROM drive.config`).Scan(&on)
	return on
}

func (s *Service) saveConfig(ctx context.Context, actorID string, c Config) (Config, error) {
	c.ClientID = strings.TrimSpace(c.ClientID)
	c.ClientSecret = strings.TrimSpace(c.ClientSecret)
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.AppID = strings.TrimSpace(c.AppID)
	_, stored, err := s.config(ctx)
	if err != nil {
		return Config{}, err
	}
	f := map[string]string{}
	if c.Enabled {
		if c.ClientID == "" {
			f["client_id"] = "enter the OAuth client ID"
		}
		if c.ClientSecret == "" && stored == "" {
			f["client_secret"] = "enter the OAuth client secret"
		}
		if c.APIKey == "" {
			f["api_key"] = "enter the API key, for Google's file picker"
		}
		if c.AppID == "" {
			f["app_id"] = "enter the project number"
		}
	}
	for k, v := range map[string]string{"client_id": c.ClientID, "api_key": c.APIKey, "app_id": c.AppID} {
		if len(v) > 300 || strings.ContainsAny(v, " \t\r\n") {
			f[k] = "this doesn't look right: no spaces"
		}
	}
	if len(f) > 0 {
		return Config{}, httpx.Invalid(f)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if c.ClientSecret != "" {
			var version int
			if err := tx.QueryRow(ctx, `UPDATE drive.config SET secret_version = secret_version + 1 RETURNING secret_version`).Scan(&version); err != nil {
				return err
			}
			sealed, err := s.vault.Seal([]byte(c.ClientSecret), "drive", "client_secret", version)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE drive.config SET secret_ciphertext=$1, secret_nonce=$2, secret_wrapped_dek=$3, secret_dek_nonce=$4, secret_kek_id=$5`,
				sealed.Ciphertext, sealed.Nonce, sealed.WrappedDEK, sealed.DEKNonce, sealed.KEKID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE drive.config SET enabled=$1, client_id=$2, api_key=$3, app_id=$4, updated_at=now(), updated_by=$5`,
			c.Enabled, c.ClientID, c.APIKey, c.AppID, actorID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "drive_settings_changed", "drive", "config", map[string]any{"enabled": c.Enabled})
	})
	if err != nil {
		return Config{}, err
	}
	saved, _, err := s.config(ctx)
	return saved, err
}

// RedirectURL is where Google sends teachers back after they connect.
func (s *Service) RedirectURL() string { return s.baseURL + "/api/teacher/drive/callback" }

func (s *Service) oauth(c Config, secret string) *oauth2.Config {
	return &oauth2.Config{ClientID: c.ClientID, ClientSecret: secret, RedirectURL: s.RedirectURL(), Scopes: []string{Scope},
		Endpoint: oauth2.Endpoint{AuthURL: s.ep.AuthURL, TokenURL: s.ep.TokenURL, AuthStyle: oauth2.AuthStyleInParams}}
}

// ---------- a teacher's connection ----------

// Status is what a teacher's Drive page shows.
type Status struct {
	Available  bool       `json:"available"` // set up on this platform
	Connected  bool       `json:"connected"`
	Email      string     `json:"email,omitempty"`
	Reconnect  bool       `json:"reconnect,omitempty"` // Google stopped accepting the token
	AutoBackup bool       `json:"auto_backup"`
	FolderLink string     `json:"folder_link,omitempty"`
	Since      *time.Time `json:"connected_at,omitempty"`
}

func (s *Service) Status(ctx context.Context, teacherID string) (Status, error) {
	st := Status{Available: s.Enabled(ctx)}
	var status, folder string
	var since time.Time
	err := s.pool.QueryRow(ctx, `SELECT email, status, auto_backup, folder_id, connected_at FROM drive.connections WHERE teacher_id=$1`, teacherID).
		Scan(&st.Email, &status, &st.AutoBackup, &folder, &since)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Connected, st.Reconnect, st.Since = true, status == "reconnect", &since
	if folder != "" {
		st.FolderLink = "https://drive.google.com/drive/folders/" + folder
	}
	return st, nil
}

func random(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hash(v string) []byte {
	h := sha256.Sum256([]byte(v))
	return h[:]
}

func localPath(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") || strings.ContainsAny(next, "\r\n") {
		return "/t/settings"
	}
	return next
}

const (
	stateCookie = "qp_drive"
	cookiePath  = "/api/teacher/drive"
	connectTTL  = 10 * time.Minute
)

// connect sends the teacher to Google to allow access.
func (s *Service) connect(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	c, secret, err := s.config(ctx)
	if err != nil {
		return err
	}
	if !c.Enabled {
		return errOff
	}
	u := auth.MustUser(ctx)
	state, verifier := random(32), oauth2.GenerateVerifier()
	if _, err := s.pool.Exec(ctx, `DELETE FROM drive.connect_states WHERE created_at < $1`, s.now().Add(-connectTTL)); err != nil {
		return err
	}
	next := r.URL.Query().Get("next")
	if next == "" {
		next = "/t/settings"
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO drive.connect_states(state_hash, teacher_id, verifier, next, created_at) VALUES ($1,$2,$3,$4,$5)`,
		hash(state), u.ID, verifier, localPath(next), s.now()); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: state, Path: cookiePath, MaxAge: int(connectTTL.Seconds()), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	// offline + consent: Google gives a refresh token, so results can be
	// saved after a session ends with nobody signed in.
	http.Redirect(w, r, s.oauth(c, secret).AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "consent"), oauth2.SetAuthURLParam("include_granted_scopes", "true"),
		oauth2.SetAuthURLParam("login_hint", u.Email)), http.StatusFound)
	return nil
}

type driveError struct{ code string }

func (e driveError) Error() string { return e.code }

// callback stores the connection and sends the teacher back.
func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	next := "/t/settings"
	err := s.finishConnect(r, &next)
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", Path: cookiePath, MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	sep := "?"
	if strings.Contains(next, "?") {
		sep = "&"
	}
	if err != nil {
		code := "drive_failed"
		var de driveError
		if errors.As(err, &de) {
			code = de.code
		} else {
			s.log.Warn("connecting Google Drive failed", "err", err)
		}
		http.Redirect(w, r, next+sep+"drive_error="+url.QueryEscape(code), http.StatusFound)
		return
	}
	http.Redirect(w, r, next+sep+"drive=connected", http.StatusFound)
}

func (s *Service) finishConnect(r *http.Request, next *string) error {
	ctx := r.Context()
	q := r.URL.Query()
	u := auth.MustUser(ctx)
	cookie, err := r.Cookie(stateCookie)
	if err != nil || q.Get("state") == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(q.Get("state"))) != 1 {
		return driveError{"drive_expired"}
	}
	var teacher, verifier string
	var created time.Time
	err = s.pool.QueryRow(ctx, `DELETE FROM drive.connect_states WHERE state_hash=$1 RETURNING teacher_id, verifier, next, created_at`, hash(cookie.Value)).
		Scan(&teacher, &verifier, next, &created)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (s.now().Sub(created) > connectTTL || teacher != u.ID)) {
		return driveError{"drive_expired"}
	}
	if err != nil {
		return err
	}
	if q.Get("error") != "" {
		return driveError{"drive_cancelled"}
	}
	c, secret, err := s.config(ctx)
	if err != nil {
		return err
	}
	if !c.Enabled {
		return driveError{"drive_off"}
	}
	tok, err := s.oauth(c, secret).Exchange(context.WithValue(ctx, oauth2.HTTPClient, s.client), q.Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		return err
	}
	// Google lets people untick Drive on its consent screen.
	if granted, _ := tok.Extra("scope").(string); granted != "" && !strings.Contains(" "+granted+" ", " "+Scope+" ") {
		return driveError{"drive_scope"}
	}
	if tok.RefreshToken == "" {
		return errors.New("google sent no refresh token")
	}
	email, err := api{hc: oauth2.NewClient(context.WithValue(ctx, oauth2.HTTPClient, s.client), oauth2.StaticTokenSource(tok)), base: s.ep.API}.about(ctx)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var version int
		_ = tx.QueryRow(ctx, `SELECT token_version FROM drive.connections WHERE teacher_id=$1`, u.ID).Scan(&version)
		version++
		sealed, err := s.vault.Seal([]byte(tok.RefreshToken), "drive:"+u.ID, "refresh_token", version)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO drive.connections(teacher_id, email, token_ciphertext, token_nonce, token_wrapped_dek, token_dek_nonce, token_kek_id, token_version, status, connected_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'ok',$9,$9)
			ON CONFLICT (teacher_id) DO UPDATE SET email=EXCLUDED.email, token_ciphertext=EXCLUDED.token_ciphertext, token_nonce=EXCLUDED.token_nonce,
				token_wrapped_dek=EXCLUDED.token_wrapped_dek, token_dek_nonce=EXCLUDED.token_dek_nonce, token_kek_id=EXCLUDED.token_kek_id,
				token_version=EXCLUDED.token_version, status='ok', connected_at=EXCLUDED.connected_at, updated_at=EXCLUDED.updated_at`,
			u.ID, email, sealed.Ciphertext, sealed.Nonce, sealed.WrappedDEK, sealed.DEKNonce, sealed.KEKID, version, s.now()); err != nil {
			return err
		}
		s.mu.Lock()
		s.tokens[u.ID] = tok
		s.mu.Unlock()
		return audit.Log(ctx, tx, u.ID, "drive_connected", "user", u.ID, map[string]string{"email": email})
	})
}

// refreshToken opens the teacher's stored refresh token.
func (s *Service) refreshToken(ctx context.Context, teacherID string) (string, error) {
	var sealed llm.Sealed
	var version int
	var status string
	err := s.pool.QueryRow(ctx, `SELECT token_ciphertext, token_nonce, token_wrapped_dek, token_dek_nonce, coalesce(token_kek_id, ''), token_version, status
		FROM drive.connections WHERE teacher_id=$1`, teacherID).Scan(&sealed.Ciphertext, &sealed.Nonce, &sealed.WrappedDEK, &sealed.DEKNonce, &sealed.KEKID, &version, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errNotLinked
	}
	if err != nil {
		return "", err
	}
	if status == "reconnect" {
		return "", errReconnect
	}
	raw, err := s.vault.Open(sealed, "drive:"+teacherID, "refresh_token", version)
	if err != nil {
		return "", err
	}
	defer clear(raw)
	return string(raw), nil
}

// token returns a current access token, refreshing it when it expires.
func (s *Service) token(ctx context.Context, teacherID string) (*oauth2.Token, error) {
	s.mu.Lock()
	t := s.tokens[teacherID]
	s.mu.Unlock()
	if t != nil && t.Expiry.After(s.now().Add(time.Minute)) {
		return t, nil
	}
	c, secret, err := s.config(ctx)
	if err != nil {
		return nil, err
	}
	if !c.Enabled {
		return nil, errOff
	}
	refresh, err := s.refreshToken(ctx, teacherID)
	if err != nil {
		return nil, err
	}
	t, err = s.oauth(c, secret).TokenSource(context.WithValue(ctx, oauth2.HTTPClient, s.client), &oauth2.Token{RefreshToken: refresh}).Token()
	var re *oauth2.RetrieveError
	if errors.As(err, &re) && re.ErrorCode == "invalid_grant" {
		// Revoked by the teacher in their Google account, or expired: they connect again.
		_, _ = s.pool.Exec(ctx, `UPDATE drive.connections SET status='reconnect', updated_at=now() WHERE teacher_id=$1`, teacherID)
		return nil, errReconnect
	}
	if err != nil {
		s.log.Warn("refreshing a Drive token", "err", err)
		return nil, errDriveDown
	}
	s.mu.Lock()
	s.tokens[teacherID] = t
	s.mu.Unlock()
	return t, nil
}

// api is the Drive API as the teacher.
func (s *Service) api(ctx context.Context, teacherID string) (api, error) {
	if _, err := s.token(ctx, teacherID); err != nil {
		return api{}, err
	}
	return api{hc: &http.Client{Transport: &bearer{s: s, teacherID: teacherID, base: s.client.Transport}, Timeout: s.client.Timeout}, base: s.ep.API}, nil
}

// bearer adds the teacher's access token. When Google refuses one before
// its expiry (revoked, or the server's clock is off), it gets a fresh one
// and tries once more.
type bearer struct {
	s         *Service
	teacherID string
	base      http.RoundTripper
}

func (b *bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	base := b.base
	if base == nil {
		base = http.DefaultTransport
	}
	send := func() (*http.Response, error) {
		t, err := b.s.token(req.Context(), b.teacherID)
		if err != nil {
			return nil, err
		}
		r := req.Clone(req.Context())
		if req.GetBody != nil {
			if r.Body, err = req.GetBody(); err != nil {
				return nil, err
			}
		}
		r.Header.Set("Authorization", "Bearer "+t.AccessToken)
		return base.RoundTrip(r)
	}
	resp, err := send()
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	resp.Body.Close()
	b.s.mu.Lock()
	delete(b.s.tokens, b.teacherID)
	b.s.mu.Unlock()
	return send()
}

// googleErr turns Google's refusals into something a teacher can act on.
func googleErr(err error) error {
	var ae *apiError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ae) && (ae.Status == http.StatusNotFound || ae.Status == http.StatusForbidden):
		return errNoAccess
	case errors.As(err, &ae) && ae.Status == http.StatusUnauthorized:
		return errReconnect
	case errors.Is(err, errTooBig):
		return errCSVTooBig
	}
	var he *httpx.Error
	if errors.As(err, &he) {
		return err
	}
	return errDriveDown
}

// Disconnect forgets the teacher's connection and tells Google so.
func (s *Service) Disconnect(ctx context.Context, teacherID string) error {
	refresh, err := s.refreshToken(ctx, teacherID)
	if err != nil && !errors.Is(err, errReconnect) {
		return err
	}
	if refresh != "" {
		if err := revoke(ctx, s.client, s.ep.RevokeURL, refresh); err != nil {
			s.log.Warn("revoking a Drive token", "err", err)
		}
	}
	s.mu.Lock()
	delete(s.tokens, teacherID)
	s.mu.Unlock()
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM drive.connections WHERE teacher_id=$1`, teacherID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "drive_disconnected", "user", teacherID, nil)
	})
}

// PickerToken is what Google's file picker needs in the browser: a
// short-lived access token limited to drive.file, and the app's ids.
type PickerToken struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	APIKey      string    `json:"api_key"`
	AppID       string    `json:"app_id"`
}

func (s *Service) Picker(ctx context.Context, teacherID string) (PickerToken, error) {
	t, err := s.token(ctx, teacherID)
	if err != nil {
		return PickerToken{}, err
	}
	c, _, err := s.config(ctx)
	if err != nil {
		return PickerToken{}, err
	}
	return PickerToken{AccessToken: t.AccessToken, ExpiresAt: t.Expiry, APIKey: c.APIKey, AppID: c.AppID}, nil
}

// SetAutoBackup turns saving each session's results on or off.
func (s *Service) SetAutoBackup(ctx context.Context, teacherID string, on bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE drive.connections SET auto_backup=$2, updated_at=now() WHERE teacher_id=$1`, teacherID, on)
	if err == nil && tag.RowsAffected() == 0 {
		return errNotLinked
	}
	return err
}
