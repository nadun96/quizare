// Package mail sends transactional email through a River job on the
// "email" queue, so a slow or failing mail server never blocks a request.
package mail

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"

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

// SMTPSender delivers through an SMTP relay.
type SMTPSender struct {
	Addr, From, Username, Password string
}

func (s SMTPSender) Send(_ context.Context, m Message) error {
	host := strings.Split(s.Addr, ":")[0]
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, host)
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		s.From, m.To, strings.ReplaceAll(m.Subject, "\n", " "), m.Body)
	return smtp.SendMail(s.Addr, auth, s.From, []string{m.To}, []byte(msg))
}

// Args is the River job payload.
type Args struct {
	Message
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
