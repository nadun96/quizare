package tutor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/tutoring/internal/testenv"
	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// Phase 3 (D-80): recording the presenter, kept encrypted for 2 days.

func chunk(t *testing.T, f *fixture, p *testenv.Person, rid string, n int, data []byte) int {
	t.Helper()
	req, _ := http.NewRequest("PUT", f.e.Server.URL+f.path("/recordings/"+rid+"/chunks/"+strconv.Itoa(n)), bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+p.Token())
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// fetch gets a play or download link's content, without any token.
func fetch(t *testing.T, f *fixture, path string) (int, http.Header, []byte) {
	t.Helper()
	resp, err := http.Get(f.e.Server.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

func TestRecording(t *testing.T) {
	f := setup(t, "manual")
	ann := f.student("Ann")
	late := f.student("Late")
	co := f.e.NewPerson("teacher", "Mr Silva")
	f.teacher.Call("POST", f.path("/coteachers"), map[string]string{"email": co.Email}, 201, nil)
	for _, p := range []*testenv.Person{f.teacher, ann, co} {
		f.join(p, 200)
	}
	f.teacher.Call("POST", f.path("/admit"), map[string]any{"user_ids": []string{ann.ID}, "admit": true}, 200, nil)

	// Only the teachers start it, and only while the session is live (TS-FR-75).
	f.teacher.Call("POST", f.path("/recordings"), map[string]string{"mime": "video/webm"}, 409, nil)
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	ann.Call("POST", f.path("/recordings"), map[string]string{"mime": "video/webm"}, 404, nil) // teachers only, like other teacher actions
	f.teacher.Call("POST", f.path("/recordings"), map[string]string{"mime": "text/html"}, 422, nil)
	var r tutor.Recording
	f.teacher.Call("POST", f.path("/recordings"), map[string]string{"mime": "video/webm;codecs=vp8,opus"}, 201, &r)
	co.Call("POST", f.path("/recordings"), map[string]string{"mime": "video/webm"}, 409, nil) // one at a time

	// Everyone is shown it, even a student still waiting to be let in (TS-FR-76).
	f.e.Service.Flush(f.sess.ID)
	if v := f.join(late, 200); v.Recording == nil || v.Recording.Status != "recording" || v.Recording.StartedByName != "Ms Perera" {
		t.Fatalf("a waiting student's view = %+v", v.Recording)
	}

	// Parts come from the recording browser, in order; a retry is fine.
	part0, part1 := []byte("WEBM-HEADER-and-first-seconds"), []byte("the-next-five-seconds")
	if c := chunk(t, f, co, r.ID, 0, part0); c != 403 {
		t.Fatalf("another teacher's part: %d", c)
	}
	if c := chunk(t, f, f.teacher, r.ID, 1, part1); c != 409 {
		t.Fatalf("out of order: %d", c)
	}
	for _, c := range []int{chunk(t, f, f.teacher, r.ID, 0, part0), chunk(t, f, f.teacher, r.ID, 0, part0), chunk(t, f, f.teacher, r.ID, 1, part1)} {
		if c != 204 {
			t.Fatalf("parts: %d", c)
		}
	}
	// Kept encrypted on disk (TS-NFR-24).
	raw, err := os.ReadFile(filepath.Join(f.e.RecordingsDir, r.ID+".rec"))
	if err != nil || bytes.Contains(raw, []byte("WEBM-HEADER")) || len(raw) < len(part0)+len(part1) {
		t.Fatalf("on disk: %d bytes, err %v", len(raw), err)
	}

	// Pause, resume, stop: kept for 2 days (PO-22).
	co.Call("POST", f.path("/recordings/"+r.ID+"/pause"), map[string]bool{"paused": true}, 204, nil)
	f.e.Service.Flush(f.sess.ID)
	var v tutor.View
	ann.Call("GET", f.path("/"), nil, 200, &v)
	if v.Recording == nil || v.Recording.Status != "paused" {
		t.Fatalf("paused = %+v", v.Recording)
	}
	co.Call("POST", f.path("/recordings/"+r.ID+"/pause"), map[string]bool{"paused": false}, 204, nil)
	f.teacher.Call("POST", f.path("/recordings/"+r.ID+"/stop"), nil, 200, &r)
	if r.Status != "stopped" || r.ExpiresAt == nil || r.ExpiresAt.Sub(*r.EndedAt) != 48*time.Hour || r.Chunks != 2 {
		t.Fatalf("stopped = %+v", r)
	}
	if c := chunk(t, f, f.teacher, r.ID, 2, part1); c != 409 {
		t.Fatalf("a part after stopping: %d", c)
	}
	f.e.Service.Flush(f.sess.ID)
	if v := f.view(ann); v.Recording != nil {
		t.Fatal("the indicator should go when it stops")
	}

	// Watching (TS-FR-78): teachers by default; students when the lead teacher allows.
	var list struct{ Recordings []tutor.Recording }
	ann.Call("GET", f.path("/recordings"), nil, 200, &list)
	if len(list.Recordings) != 0 {
		t.Fatal("a student sees a teachers-only recording")
	}
	ann.Call("POST", "/api/recordings/"+r.ID+"/link", map[string]bool{}, 404, nil)
	co.Call("PUT", f.path("/recordings/"+r.ID+"/viewers"), map[string]string{"viewers": "students"}, 403, nil) // lead only
	f.teacher.Call("PUT", f.path("/recordings/"+r.ID+"/viewers"), map[string]string{"viewers": "students"}, 204, nil)
	var link struct{ URL string }
	ann.Call("POST", "/api/recordings/"+r.ID+"/link", map[string]bool{}, 200, &link)
	code, h, body := fetch(t, f, link.URL)
	if code != 200 || string(body) != string(part0)+string(part1) || h.Get("Content-Type") != "video/webm;codecs=vp8,opus" || h.Get("Content-Disposition")[:6] != "inline" {
		t.Fatalf("playing: %d %v %q", code, h, body)
	}
	if code, _, _ := fetch(t, f, link.URL+"x"); code != 403 {
		t.Fatalf("a tampered link: %d", code)
	}
	var my struct{ Recordings []tutor.Recording }
	ann.Call("GET", "/api/my/recordings", nil, 200, &my)
	if len(my.Recordings) != 1 || my.Recordings[0].SessionTitle != "Algebra help" {
		t.Fatalf("my recordings = %+v", my.Recordings)
	}
	// Downloads: the lead teacher (and the admin) only.
	ann.Call("POST", "/api/recordings/"+r.ID+"/link", map[string]bool{"download": true}, 403, nil)
	f.teacher.Call("POST", "/api/recordings/"+r.ID+"/link", map[string]bool{"download": true}, 200, &link)
	if code, h, _ := fetch(t, f, link.URL); code != 200 || h.Get("Content-Disposition")[:10] != "attachment" {
		t.Fatalf("download: %d %v", code, h)
	}

	// The admin sees and deletes every recording, and sets the limit (PO-22, PO-24).
	admin := f.e.NewPerson("admin", "Admin")
	f.teacher.Call("GET", "/api/admin/recordings", nil, 403, nil)
	var all struct {
		Recordings []tutor.Recording
		Total      int
		Space      tutor.RecordingSpace
	}
	admin.Call("GET", "/api/admin/recordings", nil, 200, &all)
	if all.Total != 1 || all.Space.Limit != tutor.DefaultRecordingLimit || all.Space.Used == 0 {
		t.Fatalf("admin list = %+v", all)
	}
	admin.Call("PUT", "/api/admin/recordings/limit", map[string]int64{"limit": 1 << 20}, 422, nil)
	admin.Call("PUT", "/api/admin/recordings/limit", map[string]int64{"limit": 200 << 20}, 200, nil)

	// Full: no new recordings, and a running one stops (TS-FR-79).
	if _, err := f.e.Pool.Exec(context.Background(), `UPDATE tutoring.settings SET value='10' WHERE key='recording_limit'`); err != nil {
		t.Fatal(err)
	}
	if c := codeOf(t, f.teacher, "POST", f.path("/recordings"), map[string]string{"mime": "video/webm"}); c != "recording_full" {
		t.Fatalf("full: %s", c)
	}
	if _, err := f.e.Pool.Exec(context.Background(), `DELETE FROM tutoring.settings`); err != nil {
		t.Fatal(err)
	}

	// Two days on, it goes, file and all; a recorder gone quiet stops.
	var r2 tutor.Recording
	f.teacher.Call("POST", f.path("/recordings"), map[string]string{"mime": "video/webm"}, 201, &r2)
	if _, err := f.e.Pool.Exec(context.Background(), `UPDATE tutoring.recordings SET expires_at = now() - interval '1 minute' WHERE id=$1;`, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.Pool.Exec(context.Background(), `UPDATE tutoring.recordings SET last_chunk_at = now() - interval '5 minutes' WHERE id=$1`, r2.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.e.Service.SweepRecordings(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.e.RecordingsDir, r.ID+".rec")); !os.IsNotExist(err) {
		t.Fatal("the expired recording's file is still there")
	}
	f.teacher.Call("GET", f.path("/recordings"), nil, 200, &list)
	if len(list.Recordings) != 1 || list.Recordings[0].ID != r2.ID || list.Recordings[0].Status != "stopped" {
		t.Fatalf("after the sweep = %+v", list.Recordings)
	}
	admin.Call("DELETE", "/api/admin/recordings/"+r2.ID, nil, 204, nil)
	if _, err := os.Stat(filepath.Join(f.e.RecordingsDir, r2.ID+".rec")); !os.IsNotExist(err) {
		t.Fatal("the deleted recording's file is still there")
	}
}

func codeOf(t *testing.T, p *testenv.Person, method, path string, body any) string {
	t.Helper()
	_, raw := p.Do(method, path, body)
	var e struct{ Code string }
	_ = json.Unmarshal(raw, &e)
	return e.Code
}
