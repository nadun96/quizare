package poll_test

import (
	"testing"

	"github.com/nadun96/quizplatform/internal/poll"
)

// A whole class behind one school router joins at once; only far more joins
// than a class are slowed down, and only for that network (D-51).
func TestJoinLimitFitsAClassBehindOneRouter(t *testing.T) {
	f := setup(t, settings("anonymous", "self", "live"), singleQ)
	// The test server is on loopback, which is a trusted proxy, so
	// X-Forwarded-For stands in for the school's public address.
	school := f.e.Client()
	school.Headers = map[string]string{"X-Forwarded-For": "198.51.100.7"}
	path := "/api/polls/" + f.poll.JoinCode + "/join"
	for i := 0; i < 120; i++ {
		if code, body := school.Do("POST", path, nil); code != 200 {
			t.Fatalf("student %d of the class: %d %s", i+1, code, body)
		}
	}
	limited := false
	for i := 0; i < poll.JoinBurst && !limited; i++ {
		code, _ := school.Do("POST", path, nil)
		limited = code == 429
	}
	if !limited {
		t.Fatalf("no limit after %d joins from one network", 120+poll.JoinBurst)
	}
	home := f.e.Client()
	home.Headers = map[string]string{"X-Forwarded-For": "203.0.113.9"}
	if code, body := home.Do("POST", path, nil); code != 200 {
		t.Fatalf("another network must not be slowed: %d %s", code, body)
	}
}
