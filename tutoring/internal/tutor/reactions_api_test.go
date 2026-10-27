package tutor_test

import (
	"sync"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/tutoring/internal/testenv"
	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// Quick reactions reach the teachers as counts of the last minute; a person
// counts once per kind, and students can't see them (TS-FR-26).
func TestQuickReactions(t *testing.T) {
	f := setup(t, "auto")
	var mu sync.Mutex
	now := time.Now()
	f.e.Service.SetClock(func() time.Time { mu.Lock(); defer mu.Unlock(); return now })
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	ann, bob := f.student("Ann"), f.student("Bob")
	for _, p := range []*testenv.Person{ann, bob, f.teacher} {
		f.join(p, 200)
	}
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	as, ts := ann.Dial(f.sess.ID), f.teacher.Dial(f.sess.ID)
	as.Must("state")
	ts.Must("state")

	ann.Call("POST", f.path("/react"), map[string]string{"kind": "wave"}, 422, nil)
	f.teacher.Call("POST", f.path("/react"), map[string]string{"kind": "clap"}, 403, nil)
	ann.Call("POST", f.path("/react"), map[string]string{"kind": "confused"}, 204, nil)
	counts := func(m map[string]any) map[string]any { return m["data"].(map[string]any)["counts"].(map[string]any) }
	if c := counts(ts.Must("reactions")); c["confused"] != 1.0 || c["clap"] != 0.0 {
		t.Fatalf("teacher got %v", c)
	}
	as.Quiet("reactions", 200*time.Millisecond)

	// Tapping again soon counts once; another person adds one.
	ann.Call("POST", f.path("/react"), map[string]string{"kind": "confused"}, 204, nil)
	ts.Quiet("reactions", 200*time.Millisecond)
	advance(5 * time.Second)
	ann.Call("POST", f.path("/react"), map[string]string{"kind": "confused"}, 204, nil)
	bob.Call("POST", f.path("/react"), map[string]string{"kind": "confused"}, 204, nil)
	ts.Must("reactions")
	if c := counts(ts.Must("reactions")); c["confused"] != 2.0 {
		t.Fatalf("two people confused: %v", c)
	}

	// A minute later they're gone.
	var r tutor.Reactions
	ann.Call("GET", f.path("/reactions"), nil, 404, nil) // staff only
	f.teacher.Call("GET", f.path("/reactions"), nil, 200, &r)
	if r.Counts["confused"] != 2 || r.WindowSec != 60 {
		t.Fatalf("reactions = %+v", r)
	}
	advance(61 * time.Second)
	f.teacher.Call("GET", f.path("/reactions"), nil, 200, &r)
	if r.Counts["confused"] != 0 {
		t.Fatalf("after a minute = %+v", r)
	}
}
