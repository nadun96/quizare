// Package oidc signs people in with OpenID Connect providers: Google,
// Microsoft Entra ID, Keycloak, Okta and any other (D-70). Admins add the
// providers; each becomes a "Sign in with …" button on the login page. It
// owns the "oidc" schema: the providers, with their client secrets sealed
// with the master key (ADR-09), and the sign-ins in progress. Which account
// a sign-in belongs to is auth's decision (auth.SignInExternal).
package oidc

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/llm"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Provider is one identity provider and how its users become accounts.
type Provider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Position int    `json:"position"`
	Issuer   string `json:"issuer"`
	ClientID string `json:"client_id"`
	// ClientSecret is write-only: empty on read, and empty on save keeps the stored one.
	ClientSecret    string   `json:"client_secret,omitempty"`
	ClientSecretSet bool     `json:"client_secret_set"`
	Scopes          []string `json:"scopes"`
	EmailClaim      string   `json:"email_claim"`
	TrustEmail      bool     `json:"trust_email"`
	AllowedDomains  []string `json:"allowed_domains"`
	// NewAccounts: the role of accounts made on first sign-in, or "none".
	NewAccounts   string     `json:"new_accounts"`
	TeacherClaim  string     `json:"teacher_claim"`
	TeacherValues []string   `json:"teacher_values"`
	LinkByEmail   bool       `json:"link_by_email"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
}

var claimRe = regexp.MustCompile(`^[A-Za-z0-9_:./-]{1,100}$`)

func clean(list []string, lower bool) []string {
	out := []string{}
	for _, v := range list {
		v = strings.TrimSpace(v)
		if lower {
			v = strings.ToLower(strings.TrimPrefix(v, "@"))
		}
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func (p *Provider) normalise() {
	p.Name = strings.Join(strings.Fields(p.Name), " ")
	p.Issuer = strings.TrimRight(strings.TrimSpace(p.Issuer), "/")
	p.ClientID = strings.TrimSpace(p.ClientID)
	p.ClientSecret = strings.TrimSpace(p.ClientSecret)
	p.Scopes = clean(p.Scopes, false)
	if !slices.Contains(p.Scopes, "openid") {
		p.Scopes = append([]string{"openid"}, p.Scopes...)
	}
	p.EmailClaim = strings.TrimSpace(p.EmailClaim)
	if p.EmailClaim == "" {
		p.EmailClaim = "email"
	}
	p.AllowedDomains = clean(p.AllowedDomains, true)
	p.NewAccounts = strings.ToLower(strings.TrimSpace(p.NewAccounts))
	if p.NewAccounts == "" {
		p.NewAccounts = "none"
	}
	p.TeacherClaim = strings.TrimSpace(p.TeacherClaim)
	p.TeacherValues = clean(p.TeacherValues, false)
}

// localIssuer: plain http only for a provider on this machine (a Keycloak
// for development); anything else must be https.
func localIssuer(u *url.URL) bool {
	h := u.Hostname()
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func (p *Provider) validate(secretKnown bool) error {
	f := map[string]string{}
	if n := len([]rune(p.Name)); n < 1 || n > 60 {
		f["name"] = "enter the name on the button, for example Google"
	}
	u, err := url.Parse(p.Issuer)
	switch {
	case p.Issuer == "" || err != nil || u.Host == "":
		f["issuer"] = "enter the issuer, for example https://accounts.google.com"
	case u.Scheme != "https" && !(u.Scheme == "http" && localIssuer(u)):
		f["issuer"] = "the issuer must start with https://"
	case u.RawQuery != "" || u.Fragment != "":
		f["issuer"] = "the issuer has no ?query or #fragment"
	}
	if p.ClientID == "" || len(p.ClientID) > 500 {
		f["client_id"] = "enter the client ID from the provider"
	}
	if len(p.ClientSecret) > 1000 {
		f["client_secret"] = "this is too long for a client secret"
	}
	_ = secretKnown // an empty secret is a public client, which PKCE protects
	if len(p.Scopes) > 20 {
		f["scopes"] = "at most 20 scopes"
	}
	for _, s := range p.Scopes {
		if !claimRe.MatchString(s) {
			f["scopes"] = "scopes are single words such as openid, email, profile"
		}
	}
	if !claimRe.MatchString(p.EmailClaim) {
		f["email_claim"] = "a claim name such as email or preferred_username"
	}
	for _, d := range p.AllowedDomains {
		if !strings.Contains(d, ".") || strings.ContainsAny(d, " /@") {
			f["allowed_domains"] = "domains look like school.edu, one per line"
		}
	}
	if p.NewAccounts != "none" && p.NewAccounts != "student" && p.NewAccounts != "teacher" {
		f["new_accounts"] = "choose none, student or teacher"
	}
	if p.TeacherClaim != "" && !claimRe.MatchString(p.TeacherClaim) {
		f["teacher_claim"] = "a claim name such as groups or roles"
	}
	if p.TeacherClaim != "" && len(p.TeacherValues) == 0 {
		f["teacher_values"] = "enter the values that mean teacher"
	}
	if len(f) > 0 {
		return httpx.Invalid(f)
	}
	return nil
}

// Sealer seals and opens client secrets with the master key.
type Sealer interface {
	Seal(plaintext []byte, owner, kind string, version int) (llm.Sealed, error)
	Open(s llm.Sealed, owner, kind string, version int) ([]byte, error)
}

// Store reads and writes the providers.
type Store struct {
	pool  *pgxpool.Pool
	vault Sealer
}

const cols = `id, name, enabled, position, issuer, client_id, scopes, email_claim, trust_email, allowed_domains, new_accounts,
	teacher_claim, teacher_values, link_by_email, updated_at, secret_ciphertext IS NOT NULL`

func scan(r pgx.Row) (Provider, error) {
	var p Provider
	var at time.Time
	err := r.Scan(&p.ID, &p.Name, &p.Enabled, &p.Position, &p.Issuer, &p.ClientID, &p.Scopes, &p.EmailClaim, &p.TrustEmail, &p.AllowedDomains,
		&p.NewAccounts, &p.TeacherClaim, &p.TeacherValues, &p.LinkByEmail, &at, &p.ClientSecretSet)
	p.UpdatedAt = &at
	return p, err
}

// List returns every provider, in button order.
func (st *Store) List(ctx context.Context) ([]Provider, error) {
	rows, err := st.pool.Query(ctx, `SELECT `+cols+` FROM oidc.providers ORDER BY position, lower(name)`)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Provider, error) { return scan(r) })
	if out == nil {
		out = []Provider{}
	}
	return out, err
}

// Get returns a provider and its client secret ("" if none).
func (st *Store) Get(ctx context.Context, id string) (Provider, string, error) {
	var sealed llm.Sealed
	var version int
	var p Provider
	var at time.Time
	err := st.pool.QueryRow(ctx, `SELECT `+cols+`, secret_ciphertext, secret_nonce, secret_wrapped_dek, secret_dek_nonce, coalesce(secret_kek_id, ''), secret_version
		FROM oidc.providers WHERE id=$1`, id).Scan(&p.ID, &p.Name, &p.Enabled, &p.Position, &p.Issuer, &p.ClientID, &p.Scopes, &p.EmailClaim, &p.TrustEmail, &p.AllowedDomains,
		&p.NewAccounts, &p.TeacherClaim, &p.TeacherValues, &p.LinkByEmail, &at, &p.ClientSecretSet,
		&sealed.Ciphertext, &sealed.Nonce, &sealed.WrappedDEK, &sealed.DEKNonce, &sealed.KEKID, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Provider{}, "", httpx.ErrNotFound
	}
	if err != nil {
		return Provider{}, "", err
	}
	p.UpdatedAt = &at
	if !p.ClientSecretSet {
		return p, "", nil
	}
	secret, err := st.vault.Open(sealed, "oidc:"+p.ID, "client_secret", version)
	if err != nil {
		return p, "", err
	}
	defer clear(secret)
	return p, string(secret), nil
}

// Save creates a provider (empty ID) or updates one. An empty ClientSecret
// keeps the stored secret; clearSecret removes it (a public client).
func (st *Store) Save(ctx context.Context, actorID string, p Provider, clearSecret bool) (Provider, error) {
	p.normalise()
	if err := p.validate(true); err != nil {
		return Provider{}, err
	}
	err := pgx.BeginFunc(ctx, st.pool, func(tx pgx.Tx) error {
		action := "sso_provider_changed"
		if p.ID == "" {
			action = "sso_provider_added"
			if err := tx.QueryRow(ctx, `INSERT INTO oidc.providers(name, issuer, client_id, position)
				VALUES ($1,$2,$3,(SELECT coalesce(max(position), 0) + 1 FROM oidc.providers)) RETURNING id`, p.Name, p.Issuer, p.ClientID).Scan(&p.ID); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE oidc.providers SET name=$2, enabled=$3, issuer=$4, client_id=$5, scopes=$6, email_claim=$7, trust_email=$8,
			allowed_domains=$9, new_accounts=$10, teacher_claim=$11, teacher_values=$12, link_by_email=$13, updated_at=now(), updated_by=$14 WHERE id=$1`,
			p.ID, p.Name, p.Enabled, p.Issuer, p.ClientID, p.Scopes, p.EmailClaim, p.TrustEmail, p.AllowedDomains, p.NewAccounts,
			p.TeacherClaim, p.TeacherValues, p.LinkByEmail, actorID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		if p.ClientSecret != "" || clearSecret {
			var version int
			if err := tx.QueryRow(ctx, `UPDATE oidc.providers SET secret_version = secret_version + 1 WHERE id=$1 RETURNING secret_version`, p.ID).Scan(&version); err != nil {
				return err
			}
			var s llm.Sealed
			if p.ClientSecret != "" {
				if s, err = st.vault.Seal([]byte(p.ClientSecret), "oidc:"+p.ID, "client_secret", version); err != nil {
					return err
				}
			}
			var kek *string
			if s.KEKID != "" {
				kek = &s.KEKID
			}
			if _, err := tx.Exec(ctx, `UPDATE oidc.providers SET secret_ciphertext=$2, secret_nonce=$3, secret_wrapped_dek=$4, secret_dek_nonce=$5, secret_kek_id=$6 WHERE id=$1`,
				p.ID, s.Ciphertext, s.Nonce, s.WrappedDEK, s.DEKNonce, kek); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, actorID, action, "sso_provider", p.ID, map[string]any{"name": p.Name, "issuer": p.Issuer, "enabled": p.Enabled})
	})
	if err != nil {
		return Provider{}, err
	}
	saved, _, err := st.Get(ctx, p.ID)
	return saved, err
}

// Delete removes a provider and every account's sign-in with it.
func (st *Store) Delete(ctx context.Context, actorID, id string) error {
	return pgx.BeginFunc(ctx, st.pool, func(tx pgx.Tx) error {
		var name string
		if err := tx.QueryRow(ctx, `DELETE FROM oidc.providers WHERE id=$1 RETURNING name`, id).Scan(&name); errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		} else if err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "sso_provider_removed", "sso_provider", id, map[string]string{"name": name})
	})
}

// Reorder sets the button order.
func (st *Store) Reorder(ctx context.Context, ids []string) error {
	return pgx.BeginFunc(ctx, st.pool, func(tx pgx.Tx) error {
		for i, id := range ids {
			if _, err := tx.Exec(ctx, `UPDATE oidc.providers SET position=$2 WHERE id=$1`, id, i+1); err != nil {
				return err
			}
		}
		return nil
	})
}
