package mail_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/mail/mailtest"
)

// The SMTP sender logs in, writes the headers providers expect, and says
// which step failed (D-104).
func TestSMTPSender(t *testing.T) {
	srv := mailtest.Start(t, "app", "secret")
	s := mail.SMTPSender{Addr: srv.Addr, Security: mail.SecurityNone, From: "no-reply@school.lk", FromName: "Classroom Quiz", Username: "app", Password: "secret"}
	if err := s.Send(context.Background(), mail.Message{To: "ann@example.com", Subject: "Résumé day", Body: "Line one\nLine two"}); err != nil {
		t.Fatal(err)
	}
	got := srv.Got()
	if len(got) != 1 {
		t.Fatalf("got %d messages", len(got))
	}
	for _, want := range []string{`From: "Classroom Quiz" <no-reply@school.lk>`, "To: ann@example.com", "Subject: =?utf-8?q?R=C3=A9sum=C3=A9_day?=", "Date: ", "Message-ID: <", "@school.lk>", "Line one\r\nLine two"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("missing %q in\n%s", want, got[0])
		}
	}

	var se *mail.StepError
	s.Password = "wrong"
	if err := s.Send(context.Background(), mail.Message{To: "ann@example.com", Subject: "x", Body: "y"}); !errors.As(err, &se) || se.Step != "login" || !strings.Contains(err.Error(), "535") {
		t.Fatalf("wrong password: %v", err)
	}
	s.Password = "secret"
	if err := s.Send(context.Background(), mail.Message{To: "reject@example.com", Subject: "x", Body: "y"}); !errors.As(err, &se) || se.Step != "send" {
		t.Fatalf("rejected: %v", err)
	}
	s.Security = mail.SecurityStartTLS
	if err := s.Send(context.Background(), mail.Message{To: "ann@example.com", Subject: "x", Body: "y"}); !errors.As(err, &se) || se.Step != "tls" {
		t.Fatalf("no STARTTLS: %v", err)
	}
	s.Addr = "127.0.0.1:1"
	if err := s.Send(context.Background(), mail.Message{To: "ann@example.com", Subject: "x", Body: "y"}); !errors.As(err, &se) || se.Step != "connect" {
		t.Fatalf("nothing there: %v", err)
	}
}
