// Package mail sends transactional email through a River job on the
// "email" queue, so a slow or failing mail server never blocks a request.
package mail

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	netmail "net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/riverqueue/river"

	"github.com/nadun96/quizplatform/internal/platform/jobs"
)

type Message struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Sender delivers a message.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// LogSender writes messages to the log; used in development.
type LogSender struct{ Log *slog.Logger }

func (s LogSender) Send(ctx context.Context, m Message) error {
	s.Log.InfoContext(ctx, "email (dev)", "to", m.To, "subject", m.Subject, "body", m.Body)
	return nil
}

// Security is how the connection to the mail server is protected.
const (
	SecurityAuto     = "auto"     // STARTTLS when the server offers it (the default)
	SecurityStartTLS = "starttls" // STARTTLS, or no sending at all (port 587)
	SecurityTLS      = "tls"      // TLS from the first byte (port 465)
	SecurityNone     = "none"     // plain; a password is then only sent to this machine
)

// SMTPSender delivers through an SMTP server.
type SMTPSender struct {
	Addr               string // host:port
	Security           string // one of the Security constants; "" = auto
	From               string // the address
	FromName           string // optional, shown before the address
	Username, Password string
	Timeout            time.Duration // for the whole delivery; 30 s when zero
}

// Step names where a delivery failed, for the admin's test (D-104).
type StepError struct {
	Step string // connect, tls, login, send
	Err  error
}

func (e *StepError) Error() string { return e.Step + ": " + e.Err.Error() }
func (e *StepError) Unwrap() error { return e.Err }

func (s SMTPSender) Send(ctx context.Context, m Message) error {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	host, _, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return &StepError{"connect", fmt.Errorf("the server address must be host:port: %w", err)}
	}
	tlsConf := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	dialer := &net.Dialer{}
	if s.Security == SecurityTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConf}).DialContext(ctx, "tcp", s.Addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", s.Addr)
	}
	if err != nil {
		return &StepError{"connect", err}
	}
	if d, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(d)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return &StepError{"connect", err}
	}
	defer c.Close()
	if err := c.Hello(helloName(s.From)); err != nil {
		return &StepError{"connect", err}
	}
	if s.Security != SecurityTLS && s.Security != SecurityNone {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsConf); err != nil {
				return &StepError{"tls", err}
			}
		} else if s.Security == SecurityStartTLS {
			return &StepError{"tls", errors.New("the server does not offer STARTTLS")}
		}
	}
	if s.Username != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return &StepError{"login", errors.New("the server does not accept a login here; leave the username empty or use port 587")}
		}
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, host)); err != nil {
			return &StepError{"login", err}
		}
	}
	if err := c.Mail(s.From); err != nil {
		return &StepError{"send", err}
	}
	if err := c.Rcpt(m.To); err != nil {
		return &StepError{"send", err}
	}
	w, err := c.Data()
	if err != nil {
		return &StepError{"send", err}
	}
	if _, err := w.Write(s.compose(m)); err != nil {
		return &StepError{"send", err}
	}
	if err := w.Close(); err != nil {
		return &StepError{"send", err}
	}
	_ = c.Quit()
	return nil
}

// compose writes the message with the headers mail providers expect: Date,
// Message-ID and MIME, and the subject and name encoded when not ASCII.
func (s SMTPSender) compose(m Message) []byte {
	from := s.From
	if s.FromName != "" {
		from = (&netmail.Address{Name: s.FromName, Address: s.From}).String()
	}
	id := make([]byte, 12)
	_, _ = rand.Read(id)
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n",
		from, m.To, mime.QEncoding.Encode("utf-8", strings.ReplaceAll(m.Subject, "\n", " ")), time.Now().Format(time.RFC1123Z), hex.EncodeToString(id), helloName(s.From))
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n", "\r\n"))
	return []byte(b.String())
}

// helloName is the sending domain, from the From address.
func helloName(from string) string {
	if i := strings.LastIndexByte(from, '@'); i >= 0 && i < len(from)-1 {
		return from[i+1:]
	}
	return "localhost"
}

// SplitAddr reads "host:port" ("host" alone is port 25).
func SplitAddr(addr string) (string, int) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 25
	}
	n, _ := strconv.Atoi(port)
	return host, n
}

// Args is the River job payload.
type Args struct {
	Message
	// SendID ties the email to a send to a group (D-102), for its progress.
	SendID string `json:"send_id,omitempty"`
}

func (Args) Kind() string { return "email" }

func (Args) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueEmail, MaxAttempts: 5}
}

// Worker sends queued email.
type Worker struct {
	river.WorkerDefaults[Args]
	Sender Sender
}

func (w *Worker) Work(ctx context.Context, job *river.Job[Args]) error {
	return w.Sender.Send(ctx, job.Args.Message)
}
