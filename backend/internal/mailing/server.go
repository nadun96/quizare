package mailing

import (
	"context"
	"errors"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/llm"
	qmail "github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// The mail server (D-104): the admin sets it in the console, and it takes
// over from the config file's QP_SMTP_* while it is turned on. The password
// is sealed with the master key and never sent back.

// Server is the mail server's settings as the admin sees them.
type Server struct {
	Enabled     bool       `json:"enabled"`
	Host        string     `json:"host"`
	Port        int        `json:"port"`
	Security    string     `json:"security"`
	Username    string     `json:"username"`
	Password    string     `json:"password,omitempty"` // in only; "" keeps the stored one
	PasswordSet bool       `json:"password_set"`
	From        string     `json:"from"`
	FromName    string     `json:"from_name"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

// Source says what sends email now: "console" (these settings), "config"
// (the config file) or "log" (nothing: emails are only written to the log).
type ServerState struct {
	Server
	Source string `json:"source"`
	Config struct {
		Addr string `json:"addr"`
		From string `json:"from"`
		User string `json:"user"`
	} `json:"config"`
}

// Sealer seals and opens the password with the master key.
type Sealer interface {
	Seal(plaintext []byte, owner, kind string, version int) (llm.Sealed, error)
	Open(s llm.Sealed, owner, kind string, version int) ([]byte, error)
}

// ConfigInfo is what the config file says, shown beside the console's settings.
type ConfigInfo struct{ Addr, From, User string }

type server struct {
	vault    Sealer
	fallback qmail.Sender // from the config file
	config   ConfigInfo
	mu       sync.Mutex
	cached   *qmail.SMTPSender // nil: not loaded; From == "": use the fallback
	at       time.Time
}

// SetServer gives the service the master key and the config file's sender,
// used while the console's settings are off.
func (s *Service) SetServer(vault Sealer, fallback qmail.Sender, cfg ConfigInfo) {
	s.srv = &server{vault: vault, fallback: fallback, config: cfg}
}

// Sender is what the email worker sends with.
func (s *Service) Sender() qmail.Sender { return senderFunc(s.send) }

type senderFunc func(context.Context, qmail.Message) error

func (f senderFunc) Send(ctx context.Context, m qmail.Message) error { return f(ctx, m) }

// send uses the console's settings when on (re-read at most every 30 s, or
// at once after a change), the config file's otherwise.
func (s *Service) send(ctx context.Context, m qmail.Message) error {
	srv := s.srv
	srv.mu.Lock()
	if srv.cached == nil || time.Since(srv.at) > 30*time.Second {
		c, pw, err := s.loadServer(ctx)
		if err != nil {
			srv.mu.Unlock()
			return err
		}
		srv.cached, srv.at = &qmail.SMTPSender{}, time.Now()
		if c.Enabled {
			srv.cached = c.sender(pw)
		}
	}
	use := *srv.cached
	srv.mu.Unlock()
	if use.From == "" {
		return srv.fallback.Send(ctx, m)
	}
	return use.Send(ctx, m)
}

func (c Server) sender(password string) *qmail.SMTPSender {
	return &qmail.SMTPSender{Addr: net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), Security: c.Security, From: c.From, FromName: c.FromName, Username: c.Username, Password: password}
}

func (s *Service) loadServer(ctx context.Context) (Server, string, error) {
	var c Server
	var at time.Time
	var sealed llm.Sealed
	var version int
	err := s.pool.QueryRow(ctx, `SELECT enabled, host, port, security, username, from_addr, from_name, updated_at,
		pw_ciphertext, pw_nonce, pw_wrapped_dek, pw_dek_nonce, coalesce(pw_kek_id, ''), secret_version FROM mailing.server`).
		Scan(&c.Enabled, &c.Host, &c.Port, &c.Security, &c.Username, &c.From, &c.FromName, &at,
			&sealed.Ciphertext, &sealed.Nonce, &sealed.WrappedDEK, &sealed.DEKNonce, &sealed.KEKID, &version)
	if err != nil {
		return c, "", err
	}
	c.UpdatedAt = &at
	c.PasswordSet = len(sealed.Ciphertext) > 0
	if !c.PasswordSet {
		return c, "", nil
	}
	pw, err := s.srv.vault.Open(sealed, "mailing", "smtp", version)
	if err != nil {
		return c, "", err
	}
	defer clear(pw)
	return c, string(pw), nil
}

// ServerState is the settings, with what sends email now.
func (s *Service) ServerState(ctx context.Context) (ServerState, error) {
	c, _, err := s.loadServer(ctx)
	if err != nil {
		return ServerState{}, err
	}
	st := ServerState{Server: c}
	st.Config.Addr, st.Config.From, st.Config.User = s.srv.config.Addr, s.srv.config.From, s.srv.config.User
	switch {
	case c.Enabled:
		st.Source = "console"
	case s.srv.config.Addr != "":
		st.Source = "config"
	default:
		st.Source = "log"
	}
	return st, nil
}

func (c *Server) normalise() {
	c.Host = strings.TrimSpace(c.Host)
	c.Username = strings.TrimSpace(c.Username)
	c.From = strings.ToLower(strings.TrimSpace(c.From))
	c.FromName = strings.TrimSpace(c.FromName)
	if c.Security == "" {
		c.Security = qmail.SecurityStartTLS
	}
}

func (c Server) validate(hasPassword bool) error {
	bad := map[string]string{}
	if c.Host == "" || strings.ContainsAny(c.Host, " /:@") && net.ParseIP(c.Host) == nil {
		bad["host"] = "the mail server's name, like smtp.gmail.com or localhost"
	}
	if c.Port < 1 || c.Port > 65535 {
		bad["port"] = "1 to 65535: usually 587, 465 or 25"
	}
	switch c.Security {
	case qmail.SecurityAuto, qmail.SecurityStartTLS, qmail.SecurityTLS, qmail.SecurityNone:
	default:
		bad["security"] = "auto, starttls, tls or none"
	}
	if a, err := mail.ParseAddress(c.From); err != nil || a.Address != c.From {
		bad["from"] = "an email address, like no-reply@school.lk"
	}
	if c.Username != "" && !hasPassword {
		bad["password"] = "required with a username"
	}
	ip := net.ParseIP(c.Host)
	local := c.Host == "localhost" || ip != nil && ip.IsLoopback()
	if c.Username != "" && c.Security == qmail.SecurityNone && !local {
		bad["security"] = "a password is only sent encrypted: choose STARTTLS or TLS"
	}
	if len(c.FromName) > 100 || strings.ContainsAny(c.FromName, "\r\n") {
		bad["from_name"] = "one line, at most 100 characters"
	}
	if len(bad) > 0 {
		return httpx.Invalid(bad)
	}
	return nil
}

// SaveServer stores the settings; an empty password keeps the stored one,
// and clearing the username clears it.
func (s *Service) SaveServer(ctx context.Context, actorID string, c Server) (ServerState, error) {
	c.normalise()
	_, stored, err := s.loadServer(ctx)
	if err != nil {
		return ServerState{}, err
	}
	if c.Enabled || c.Host != "" {
		if err := c.validate(c.Password != "" || stored != ""); err != nil {
			return ServerState{}, err
		}
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if c.Password != "" || c.Username == "" {
			var version int
			if err := tx.QueryRow(ctx, `UPDATE mailing.server SET secret_version = secret_version + 1 RETURNING secret_version`).Scan(&version); err != nil {
				return err
			}
			var sealed llm.Sealed
			if c.Password != "" && c.Username != "" {
				if sealed, err = s.srv.vault.Seal([]byte(c.Password), "mailing", "smtp", version); err != nil {
					return err
				}
			}
			var kek *string
			if sealed.KEKID != "" {
				kek = &sealed.KEKID
			}
			if _, err := tx.Exec(ctx, `UPDATE mailing.server SET pw_ciphertext=$1, pw_nonce=$2, pw_wrapped_dek=$3, pw_dek_nonce=$4, pw_kek_id=$5`,
				sealed.Ciphertext, sealed.Nonce, sealed.WrappedDEK, sealed.DEKNonce, kek); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE mailing.server SET enabled=$1, host=$2, port=$3, security=$4, username=$5, from_addr=$6, from_name=$7, updated_by=$8, updated_at=now()`,
			c.Enabled, c.Host, c.Port, c.Security, c.Username, c.From, c.FromName, actorID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "mail_server_changed", "mail_server", "server", map[string]any{"enabled": c.Enabled, "host": c.Host, "port": c.Port, "from": c.From})
	})
	if err != nil {
		return ServerState{}, err
	}
	s.forget()
	return s.ServerState(ctx)
}

// forget makes the next email read the settings again.
func (s *Service) forget() {
	s.srv.mu.Lock()
	s.srv.cached = nil
	s.srv.mu.Unlock()
}

// TestResult is how a test email went: on failure, the step and the
// server's own words.
type TestResult struct {
	OK    bool   `json:"ok"`
	Step  string `json:"step,omitempty"` // connect, tls, login, send
	Error string `json:"error,omitempty"`
	Via   string `json:"via"` // what was used: host:port, or "the config file", or "the log"
}

// TestServer sends one email now, without the queue, with the settings
// given (an empty password uses the stored one) or, with none, with what
// sends email now.
func (s *Service) TestServer(ctx context.Context, actorID, to string, c *Server) (TestResult, error) {
	var name string
	if err := s.pool.QueryRow(ctx, `SELECT name FROM auth.users WHERE id = $1`, actorID).Scan(&name); err != nil {
		return TestResult{}, err
	}
	if a, err := mail.ParseAddress(strings.TrimSpace(to)); err != nil {
		return TestResult{}, httpx.Invalid(map[string]string{"to": "an email address"})
	} else {
		to = a.Address
	}
	m := qmail.Message{To: to, Subject: "Test email from " + s.baseURL,
		Body: "Hi,\n\nThis is a test of the mail server settings, sent by " + name + " from " + s.baseURL + ".\nIf you can read it, email works.\n\nIf it went to spam, check the sending domain's SPF, DKIM and DMARC records and the server's reverse DNS (PTR)."}
	var sender qmail.Sender
	res := TestResult{}
	if c == nil {
		st, err := s.ServerState(ctx)
		if err != nil {
			return res, err
		}
		if st.Source != "console" {
			res.Via = map[string]string{"config": "the config file (" + st.Config.Addr + ")", "log": "the log only: no mail server is set"}[st.Source]
			sender = s.srv.fallback
		}
	}
	if sender == nil {
		cur, stored, err := s.loadServer(ctx)
		if err != nil {
			return res, err
		}
		use := cur
		if c != nil {
			use = *c
			use.normalise()
			if use.Password == "" {
				use.Password = stored
			}
			if err := use.validate(use.Password != ""); err != nil {
				return res, err
			}
		} else {
			use.Password = stored
		}
		snd := use.sender(use.Password)
		snd.Timeout = 20 * time.Second
		sender = snd
		res.Via = snd.Addr
	}
	err := sender.Send(ctx, m)
	_ = audit.Log(ctx, s.pool, actorID, "mail_server_tested", "mail_server", "server", map[string]any{"to": to, "ok": err == nil})
	if err != nil {
		var se *qmail.StepError
		if errors.As(err, &se) {
			res.Step = se.Step
		}
		res.Error = err.Error()
		return res, nil
	}
	res.OK = true
	return res, nil
}
