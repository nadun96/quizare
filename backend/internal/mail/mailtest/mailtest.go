// Package mailtest is a tiny SMTP server for tests: no TLS, AUTH PLAIN with
// one username and password, and every message it takes kept in Got.
package mailtest

import (
	"bufio"
	"encoding/base64"
	"net"
	"strings"
	"sync"
	"testing"
)

type Server struct {
	Addr     string
	User     string
	Password string
	mu       sync.Mutex
	got      []string
}

// Start listens on 127.0.0.1 until the test ends.
func Start(t testing.TB, user, password string) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Addr: ln.Addr().String(), User: user, Password: password}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

// Got returns the messages taken so far, headers and body.
func (s *Server) Got() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.got...)
}

func (s *Server) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	say := func(line string) { _, _ = c.Write([]byte(line + "\r\n")) }
	say("220 fake ESMTP")
	authed := s.User == ""
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimRight(line, "\r\n")
		up := strings.ToUpper(cmd)
		switch {
		case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
			say("250-fake")
			say("250 AUTH PLAIN")
		case strings.HasPrefix(up, "AUTH PLAIN"):
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(cmd[len("AUTH PLAIN"):]))
			parts := strings.Split(string(raw), "\x00")
			if len(parts) == 3 && parts[1] == s.User && parts[2] == s.Password {
				authed = true
				say("235 2.7.0 Authentication successful")
			} else {
				say("535 5.7.8 Authentication credentials invalid")
			}
		case strings.HasPrefix(up, "MAIL FROM"):
			if !authed {
				say("530 5.7.0 Authentication required")
				continue
			}
			say("250 OK")
		case strings.HasPrefix(up, "RCPT TO"):
			if strings.Contains(up, "REJECT@") {
				say("550 5.1.1 No such user")
				continue
			}
			say("250 OK")
		case up == "DATA":
			say("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			s.mu.Lock()
			s.got = append(s.got, b.String())
			s.mu.Unlock()
			say("250 OK queued")
		case up == "QUIT":
			say("221 bye")
			return
		default:
			say("250 OK")
		}
	}
}
