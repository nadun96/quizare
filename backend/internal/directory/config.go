// Package directory signs students and teachers in with the school's
// directory: Windows Active Directory or another LDAP server (D-64). It owns
// the "directory" schema: one row of settings, with the service account's
// password sealed with the master key like the LLM keys (ADR-09).
package directory

import (
	"context"
	"crypto/x509"
	"net/url"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/llm"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Config is the directory connection and how entries become accounts.
type Config struct {
	Enabled bool `json:"enabled"`
	// Kind: "ad" (Windows Active Directory) or "ldap" (OpenLDAP and others);
	// it decides the defaults below.
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	StartTLS bool   `json:"start_tls"`
	CAPEM    string `json:"ca_pem"`
	BindDN   string `json:"bind_dn"`
	// BindPassword is write-only: empty on read, and empty on save keeps the stored one.
	BindPassword    string     `json:"bind_password,omitempty"`
	BindPasswordSet bool       `json:"bind_password_set"`
	BaseDN          string     `json:"base_dn"`
	UserFilter      string     `json:"user_filter"`
	IDAttr          string     `json:"id_attr"`
	EmailAttr       string     `json:"email_attr"`
	NameAttr        string     `json:"name_attr"`
	TeacherGroup    string     `json:"teacher_group"`
	StudentGroup    string     `json:"student_group"`
	NestedGroups    bool       `json:"nested_groups"`
	LinkByEmail     bool       `json:"link_by_email"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

// Defaults per kind. The AD filter leaves out disabled accounts
// (userAccountControl bit 2) and accepts the Windows name, the
// user@domain name or the email.
var defaults = map[string]Config{
	"ad": {
		UserFilter: "(&(objectCategory=person)(objectClass=user)(!(userAccountControl:1.2.840.113556.1.4.803:=2))(|(sAMAccountName={login})(userPrincipalName={login})(mail={login})))",
		IDAttr:     "objectGUID", EmailAttr: "mail", NameAttr: "displayName", NestedGroups: true,
	},
	"ldap": {
		UserFilter: "(&(objectClass=inetOrgPerson)(|(uid={login})(mail={login})))",
		IDAttr:     "entryUUID", EmailAttr: "mail", NameAttr: "cn",
	},
}

// Defaults are what the admin page fills in when the kind changes.
func Defaults() map[string]Config { return defaults }

func (c *Config) normalise() {
	c.Kind = strings.ToLower(strings.TrimSpace(c.Kind))
	if _, ok := defaults[c.Kind]; !ok {
		c.Kind = "ad"
	}
	d := defaults[c.Kind]
	trim := func(p *string, def string) {
		*p = strings.TrimSpace(*p)
		if *p == "" {
			*p = def
		}
	}
	trim(&c.URL, "")
	trim(&c.BindDN, "")
	trim(&c.BaseDN, "")
	trim(&c.UserFilter, d.UserFilter)
	trim(&c.IDAttr, d.IDAttr)
	trim(&c.EmailAttr, d.EmailAttr)
	trim(&c.NameAttr, d.NameAttr)
	trim(&c.TeacherGroup, "")
	trim(&c.StudentGroup, "")
	c.CAPEM = strings.TrimSpace(c.CAPEM)
	if c.Kind != "ad" {
		c.NestedGroups = false // the in-chain rule is Active Directory's
	}
}

// validate checks a configuration that is about to be switched on or tested.
func (c *Config) validate(passwordKnown bool) error {
	f := map[string]string{}
	u, err := url.Parse(c.URL)
	switch {
	case c.URL == "" || err != nil || u.Host == "":
		f["url"] = "enter the server, for example ldaps://dc1.school.local"
	case u.Scheme == "ldaps" && c.StartTLS:
		f["start_tls"] = "ldaps:// is already encrypted; StartTLS is for ldap://"
	case u.Scheme == "ldap" && !c.StartTLS:
		f["url"] = "use ldaps://, or ldap:// with StartTLS: passwords must not cross the network unencrypted"
	case u.Scheme != "ldap" && u.Scheme != "ldaps":
		f["url"] = "the address starts with ldaps:// or ldap://"
	}
	if c.CAPEM != "" && !x509.NewCertPool().AppendCertsFromPEM([]byte(c.CAPEM)) {
		f["ca_pem"] = "this isn't a PEM certificate (-----BEGIN CERTIFICATE-----)"
	}
	if c.BaseDN == "" {
		f["base_dn"] = "enter where to search, for example DC=school,DC=local"
	} else if _, err := ldap.ParseDN(c.BaseDN); err != nil {
		f["base_dn"] = "this isn't a valid DN"
	}
	if c.Kind == "ad" && c.BindDN == "" {
		f["bind_dn"] = "Active Directory needs a service account to search"
	}
	if c.BindDN != "" && !passwordKnown {
		f["bind_password"] = "enter the service account's password"
	}
	if !strings.Contains(c.UserFilter, "{login}") {
		f["user_filter"] = "the filter must contain {login}"
	} else if _, err := ldap.CompileFilter(strings.ReplaceAll(c.UserFilter, "{login}", "x")); err != nil {
		f["user_filter"] = "this isn't a valid LDAP filter"
	}
	for k, dn := range map[string]string{"teacher_group": c.TeacherGroup, "student_group": c.StudentGroup} {
		if dn != "" {
			if _, err := ldap.ParseDN(dn); err != nil {
				f[k] = "this isn't a valid DN"
			}
		}
	}
	if c.TeacherGroup == "" && c.StudentGroup == "" && c.Kind == "ad" {
		// Without either group every account in the base would get in.
		f["student_group"] = "choose at least one group, so only your students and teachers can sign in"
	}
	if len(f) > 0 {
		return httpx.Invalid(f)
	}
	return nil
}

// Sealer seals and opens the service account's password with the master key.
type Sealer interface {
	Seal(plaintext []byte, owner, kind string, version int) (llm.Sealed, error)
	Open(s llm.Sealed, owner, kind string, version int) ([]byte, error)
}

// Store reads and writes the one row of settings.
type Store struct {
	pool  *pgxpool.Pool
	vault Sealer
}

const cols = `enabled, kind, url, start_tls, ca_pem, bind_dn, base_dn, user_filter, id_attr, email_attr, name_attr,
	teacher_group, student_group, nested_groups, link_by_email, updated_at`

// dest lists the scan targets for cols.
func (c *Config) dest(at *time.Time) []any {
	return []any{&c.Enabled, &c.Kind, &c.URL, &c.StartTLS, &c.CAPEM, &c.BindDN, &c.BaseDN, &c.UserFilter, &c.IDAttr, &c.EmailAttr, &c.NameAttr,
		&c.TeacherGroup, &c.StudentGroup, &c.NestedGroups, &c.LinkByEmail, at}
}

// Load returns the settings and the service account's password ("" if none).
func (st *Store) Load(ctx context.Context) (Config, string, error) {
	var c Config
	var at time.Time
	var sealed llm.Sealed
	var version int
	err := st.pool.QueryRow(ctx, `SELECT `+cols+`, bind_ciphertext, bind_nonce, bind_wrapped_dek, bind_dek_nonce, coalesce(bind_kek_id, ''), secret_version FROM directory.config`).
		Scan(append(c.dest(&at), &sealed.Ciphertext, &sealed.Nonce, &sealed.WrappedDEK, &sealed.DEKNonce, &sealed.KEKID, &version)...)
	if err != nil {
		return Config{}, "", err
	}
	c.UpdatedAt = &at
	c.BindPasswordSet = len(sealed.Ciphertext) > 0
	if !c.BindPasswordSet {
		return c, "", nil
	}
	pw, err := st.vault.Open(sealed, "directory", "bind", version)
	if err != nil {
		return c, "", err
	}
	defer clear(pw)
	return c, string(pw), nil
}

// Save stores new settings; an empty BindPassword keeps the stored one.
func (st *Store) Save(ctx context.Context, actorID string, c Config) (Config, error) {
	c.normalise()
	_, stored, err := st.Load(ctx)
	if err != nil {
		return Config{}, err
	}
	if c.Enabled {
		if err := c.validate(c.BindPassword != "" || stored != ""); err != nil {
			return Config{}, err
		}
	}
	err = pgx.BeginFunc(ctx, st.pool, func(tx pgx.Tx) error {
		if c.BindPassword != "" || c.BindDN == "" {
			var version int
			if err := tx.QueryRow(ctx, `UPDATE directory.config SET secret_version = secret_version + 1 RETURNING secret_version`).Scan(&version); err != nil {
				return err
			}
			var s llm.Sealed
			if c.BindPassword != "" {
				if s, err = st.vault.Seal([]byte(c.BindPassword), "directory", "bind", version); err != nil {
					return err
				}
			}
			var kek *string
			if s.KEKID != "" {
				kek = &s.KEKID
			}
			if _, err := tx.Exec(ctx, `UPDATE directory.config SET bind_ciphertext=$1, bind_nonce=$2, bind_wrapped_dek=$3, bind_dek_nonce=$4, bind_kek_id=$5`,
				s.Ciphertext, s.Nonce, s.WrappedDEK, s.DEKNonce, kek); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE directory.config SET enabled=$1, kind=$2, url=$3, start_tls=$4, ca_pem=$5, bind_dn=$6, base_dn=$7, user_filter=$8,
			id_attr=$9, email_attr=$10, name_attr=$11, teacher_group=$12, student_group=$13, nested_groups=$14, link_by_email=$15, updated_at=now(), updated_by=$16`,
			c.Enabled, c.Kind, c.URL, c.StartTLS, c.CAPEM, c.BindDN, c.BaseDN, c.UserFilter, c.IDAttr, c.EmailAttr, c.NameAttr,
			c.TeacherGroup, c.StudentGroup, c.NestedGroups, c.LinkByEmail, actorID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "directory_settings_changed", "directory", "config", map[string]string{"enabled": map[bool]string{true: "on", false: "off"}[c.Enabled], "url": c.URL})
	})
	if err != nil {
		return Config{}, err
	}
	saved, _, err := st.Load(ctx)
	return saved, err
}
