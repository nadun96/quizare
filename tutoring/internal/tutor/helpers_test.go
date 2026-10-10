package tutor_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/testenv"
)

func mustSign(t *testing.T, key []byte, iss, aud, sub string) string {
	t.Helper()
	now := time.Now()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": iss, "aud": aud, "sub": sub, "name": "x", "role": "teacher",
		"iat": now.Unix(), "exp": now.Add(time.Minute).Unix()}).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func rawGet(t *testing.T, e *testenv.Env, path, token string) int {
	t.Helper()
	req, _ := http.NewRequest("GET", e.Server.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// badSocket checks a socket with a bad first message or origin is refused.
func badSocket(t *testing.T, e *testenv.Env, sessionID, first, origin string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	u := "ws" + strings.TrimPrefix(e.Server.URL, "http") + "/ws/sessions/" + sessionID
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}})
	if err != nil {
		return nil // refused at the handshake (another origin)
	}
	defer c.CloseNow()
	_ = c.Write(ctx, websocket.MessageText, []byte(first))
	if _, _, err := c.Read(ctx); err == nil {
		return fmt.Errorf("socket with %q from %s got a message", first, origin)
	}
	return nil
}
