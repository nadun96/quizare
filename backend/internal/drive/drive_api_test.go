package drive_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/drive"
	"github.com/nadun96/quizplatform/internal/drive/drivetest"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/platform/dbtest"
	"github.com/nadun96/quizplatform/internal/quiz"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

type fixture struct {
	e       *apptest.Env
	g       *drivetest.Google
	admin   *apptest.Client
	teacher *apptest.Client
}

func setup(t *testing.T) *fixture {
	g := drivetest.New(t)
	e := apptest.New(t, apptest.WithHTTPClient(g.Server.Client()), apptest.WithGoogle(g.Endpoints()))
	f := &fixture{e: e, g: g, admin: e.NewUser(auth.RoleAdmin), teacher: e.NewUser(auth.RoleTeacher)}
	f.admin.Call("PUT", "/api/admin/drive", map[string]any{"enabled": true, "client_id": g.ClientID, "client_secret": g.ClientSecret, "api_key": "AIza-test", "app_id": "123456"}, 200, nil)
	return f
}

func (f *fixture) connect(t *testing.T, c *apptest.Client) {
	t.Helper()
	if at := c.Follow("/api/teacher/drive/connect"); at != "/t/settings?drive=connected" {
		t.Fatalf("connect ended at %s", at)
	}
}

func status(c *apptest.Client) drive.Status {
	var st drive.Status
	c.Call("GET", "/api/teacher/drive", nil, 200, &st)
	return st
}

func errCode(t *testing.T, c *apptest.Client, method, path string, body any, want int) string {
	t.Helper()
	code, raw := c.Do(method, path, body)
	if code != want {
		t.Fatalf("%s %s: %d, want %d: %s", method, path, code, want, raw)
	}
	var e struct{ Code string }
	_ = json.Unmarshal(raw, &e)
	return e.Code
}

func TestSettingsAndConnection(t *testing.T) {
	g := drivetest.New(t)
	e := apptest.New(t, apptest.WithHTTPClient(g.Server.Client()), apptest.WithGoogle(g.Endpoints()))
	admin, teacher := e.NewUser(auth.RoleAdmin), e.NewUser(auth.RoleTeacher)

	if st := status(teacher); st.Available || st.Connected {
		t.Fatalf("not set up yet: %+v", st)
	}
	if c := errCode(t, teacher, "GET", "/api/teacher/drive/connect", nil, 404); c != "drive_off" {
		t.Fatalf("connect while off: %s", c)
	}
	// Admins only; everything needed before it's on.
	teacher.Call("GET", "/api/admin/drive", nil, 403, nil)
	code, raw := admin.Do("PUT", "/api/admin/drive", map[string]any{"enabled": true, "client_id": "x"})
	if code != 422 || !strings.Contains(string(raw), "client_secret") || !strings.Contains(string(raw), "api_key") || !strings.Contains(string(raw), "app_id") {
		t.Fatalf("validation: %d %s", code, raw)
	}
	var saved drive.Config
	admin.Call("PUT", "/api/admin/drive", map[string]any{"enabled": true, "client_id": g.ClientID, "client_secret": g.ClientSecret, "api_key": "AIza-test", "app_id": "123456"}, 200, &saved)
	if !saved.ClientSecretSet || saved.ClientSecret != "" {
		t.Fatalf("the secret is write-only: %+v", saved)
	}
	var adm struct {
		RedirectURL string `json:"redirect_url"`
	}
	admin.Call("GET", "/api/admin/drive", nil, 200, &adm)
	if !strings.HasSuffix(adm.RedirectURL, "/api/teacher/drive/callback") {
		t.Fatalf("redirect url %q", adm.RedirectURL)
	}

	// Connecting: Google's consent, then back to the settings page.
	f := &fixture{e: e, g: g, admin: admin, teacher: teacher}
	f.connect(t, teacher)
	st := status(teacher)
	if !st.Available || !st.Connected || st.Email != g.Email || st.AutoBackup {
		t.Fatalf("connected = %+v", st)
	}
	var pick drive.PickerToken
	teacher.Call("POST", "/api/teacher/drive/picker", nil, 200, &pick)
	if !strings.HasPrefix(pick.AccessToken, "access-") || pick.APIKey != "AIza-test" || pick.AppID != "123456" {
		t.Fatalf("picker = %+v", pick)
	}
	// Students never reach a teacher's Drive.
	e.NewUser(auth.RoleStudent).Call("POST", "/api/teacher/drive/picker", nil, 403, nil)

	// A callback that isn't this browser's.
	if at := teacher.Follow("/api/teacher/drive/callback?state=forged&code=x"); at != "/t/settings?drive_error=drive_expired" {
		t.Fatalf("forged: %s", at)
	}
	// Unticking Drive on Google's consent screen.
	g.NoScope = true
	if at := teacher.Follow("/api/teacher/drive/connect?next=/t/bank"); at != "/t/bank?drive_error=drive_scope" {
		t.Fatalf("no scope: %s", at)
	}
	g.NoScope = false

	// An expired access token is refreshed without the teacher noticing.
	g.Expire()
	teacher.Call("POST", "/api/teacher/drive/share", map[string]string{"file_id": g.Add("a.png", "image/png", nil)}, 200, nil)
	// Revoked in the Google account: connect again.
	g.Refuse = true
	g.Expire()
	if c := errCode(t, teacher, "POST", "/api/teacher/drive/share", map[string]string{"file_id": "x"}, 409); c != "drive_reconnect" {
		t.Fatalf("revoked: %s", c)
	}
	if !status(teacher).Reconnect {
		t.Fatal("the page says to reconnect")
	}
	g.Refuse = false
	f.connect(t, teacher)
	if st := status(teacher); st.Reconnect {
		t.Fatalf("reconnected: %+v", st)
	}

	// Disconnecting tells Google.
	teacher.Call("DELETE", "/api/teacher/drive", nil, 204, nil)
	if len(g.Revoked) != 1 || !strings.HasPrefix(g.Revoked[0], "refresh-") {
		t.Fatalf("revoked = %v", g.Revoked)
	}
	if st := status(teacher); st.Connected {
		t.Fatal("still connected")
	}
	if c := errCode(t, teacher, "POST", "/api/teacher/drive/picker", nil, 409); c != "drive_not_connected" {
		t.Fatal(c)
	}
}

func TestShareAndImport(t *testing.T) {
	f := setup(t)
	f.connect(t, f.teacher)

	// A question image: shared by link, as a URL the question editor takes.
	img := f.g.Add("cell.png", "image/png", []byte("png"))
	var sh drive.Shared
	f.teacher.Call("POST", "/api/teacher/drive/share", map[string]string{"file_id": img}, 200, &sh)
	if !sh.Image || sh.ImageURL != "https://drive.google.com/file/d/"+img+"/view" {
		t.Fatalf("shared = %+v", sh)
	}
	if file, _ := f.g.File(img); len(file.Permissions) != 1 || file.Permissions[0].Type != "anyone" {
		t.Fatalf("permissions = %+v", file.Permissions)
	}
	doc := f.g.Add("Worksheet", "application/vnd.google-apps.document", nil)
	f.teacher.Call("POST", "/api/teacher/drive/share", map[string]string{"file_id": doc}, 200, &sh)
	if sh.Image || sh.WebLink == "" {
		t.Fatalf("document = %+v", sh)
	}
	folder := f.g.Add("Stuff", drive.MimeFolder, nil)
	errCode(t, f.teacher, "POST", "/api/teacher/drive/share", map[string]string{"file_id": folder}, 422)
	if c := errCode(t, f.teacher, "POST", "/api/teacher/drive/share", map[string]string{"file_id": "not-picked"}, 403); c != "drive_no_access" {
		t.Fatal(c)
	}

	// Questions from a Google Sheet, then from a CSV file.
	var c content.Classroom
	f.teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var m content.Module
	f.teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	f.teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var q quiz.Quiz
	f.teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Science"}, 201, &q)
	sheet := f.g.Add("Questions", drive.MimeSheet, []byte("question_code,type,question_text,options,correct_answer,marks\nQ1,SINGLE,Capital of France?,Paris|Rome,Paris,1\nQ2,SINGLE,Bad,,,1\n"))
	var rep quiz.ImportReport
	f.teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import-drive", map[string]string{"file_id": sheet}, 200, &rep)
	if rep.Imported != 1 || len(rep.Rejected) != 1 {
		t.Fatalf("sheet import = %+v", rep)
	}
	csvFile := f.g.Add("more.csv", "text/csv", []byte("question_code,type,question_text,options,correct_answer,marks\nQ3,SINGLE,2+2?,4|5,4,1\n"))
	f.teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import-drive", map[string]string{"file_id": csvFile}, 200, &rep)
	if rep.Imported != 1 {
		t.Fatalf("csv import = %+v", rep)
	}
	pdf := f.g.Add("notes.pdf", "application/pdf", []byte("%PDF"))
	errCode(t, f.teacher, "POST", "/api/teacher/quizzes/"+q.ID+"/questions/import-drive", map[string]string{"file_id": pdf}, 422)
	big := f.g.Add("big.csv", "text/csv", []byte(strings.Repeat("x", quiz.MaxCSVBytes+1)))
	errCode(t, f.teacher, "POST", "/api/teacher/quizzes/"+q.ID+"/questions/import-drive", map[string]string{"file_id": big}, 422)
	// Someone else's quiz.
	other := f.e.NewUser(auth.RoleTeacher)
	f.connect(t, other)
	other.Call("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import-drive", map[string]string{"file_id": csvFile}, 404, nil)
}

func TestClassroomFiles(t *testing.T) {
	f := setup(t)
	f.connect(t, f.teacher)
	var c content.Classroom
	f.teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "Grade 9"}, 201, &c)
	student := func(email string) *apptest.Client {
		s := f.e.Client()
		s.Call("POST", "/api/auth/register", map[string]any{"email": email, "name": email, "password": "password123", "role": "student"}, 201, &s.User)
		s.Call("POST", "/api/enrolments", map[string]string{"join_code": c.JoinCode}, 200, nil)
		return s
	}
	ann, ben := student("ann@school.test"), student("ben@school.test")
	outsider := f.e.NewUser(auth.RoleStudent)

	notes := f.g.Add("Lesson notes", "application/vnd.google-apps.document", nil)
	var list struct{ Files []drive.ClassroomFile }
	f.teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/drive-files", map[string]any{"file_ids": []string{notes, notes}}, 200, &list)
	if len(list.Files) != 1 || list.Files[0].Sharing != "students" || list.Files[0].SharedTo != 2 || list.Files[0].SyncError != "" {
		t.Fatalf("attached = %+v", list.Files)
	}
	readers := func(id string) string {
		file, _ := f.g.File(id)
		var out []string
		for _, p := range file.Permissions {
			out = append(out, p.Type+":"+p.Email)
		}
		return strings.Join(out, ",")
	}
	if r := readers(notes); !strings.Contains(r, "user:ann@school.test") || !strings.Contains(r, "user:ben@school.test") || strings.Count(r, "user:") != 2 {
		t.Fatalf("readers = %s", r)
	}

	// Students see the list; others don't.
	var mine struct{ Files []drive.ClassroomFile }
	ann.Call("GET", "/api/my/classrooms/"+c.ID+"/drive-files", nil, 200, &mine)
	if len(mine.Files) != 1 || mine.Files[0].Name != "Lesson notes" || mine.Files[0].FileID != "" || mine.Files[0].Sharing != "" {
		t.Fatalf("student view = %+v", mine.Files)
	}
	outsider.Call("GET", "/api/my/classrooms/"+c.ID+"/drive-files", nil, 404, nil)
	f.e.NewUser(auth.RoleTeacher).Call("GET", "/api/teacher/classrooms/"+c.ID+"/drive-files", nil, 404, nil)

	// A student who joins later gets it; one who leaves loses it.
	cat := student("cat@school.test")
	var enrols struct{ Enrolments []content.Enrolment }
	f.teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/enrolments", nil, 200, &enrols)
	for _, en := range enrols.Enrolments {
		if en.UserID == ben.User.ID {
			f.teacher.Call("PATCH", "/api/teacher/enrolments/"+en.ID, map[string]any{"status": "removed"}, 200, nil)
		}
	}
	if err := f.e.App.Drive.SyncAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := readers(notes); !strings.Contains(r, "cat@school.test") || strings.Contains(r, "ben@school.test") {
		t.Fatalf("after sync = %s", r)
	}
	_ = cat

	// A student without a Google account: the rest still get it, and the teacher is told.
	f.g.NoGoogle["dan@school.test"] = true
	student("dan@school.test")
	_ = f.e.App.Drive.SyncAll(context.Background())
	f.teacher.Call("GET", "/api/teacher/classrooms/"+c.ID+"/drive-files", nil, 200, &list)
	if !strings.Contains(list.Files[0].SyncError, "1 student has no Google account") || list.Files[0].SharedTo != 2 {
		t.Fatalf("no google: %+v", list.Files[0])
	}

	// By link instead: the per-student shares go, one link share comes.
	id := list.Files[0].ID
	var linked struct{ Files []drive.ClassroomFile }
	f.teacher.Call("PATCH", "/api/teacher/classrooms/"+c.ID+"/drive-files/"+id, map[string]string{"sharing": "link"}, 200, &linked)
	if r := readers(notes); r != "anyone:" || linked.Files[0].SharedTo != 0 || linked.Files[0].SyncError != "" {
		t.Fatalf("by link = %s %+v", r, linked.Files[0])
	}
	errCode(t, f.teacher, "PATCH", "/api/teacher/classrooms/"+c.ID+"/drive-files/"+id, map[string]string{"sharing": "everyone"}, 422)

	// Removing it takes back what the platform shared; the file stays.
	f.teacher.Call("DELETE", "/api/teacher/classrooms/"+c.ID+"/drive-files/"+id, nil, 204, nil)
	if r := readers(notes); r != "" {
		t.Fatalf("after removal = %s", r)
	}
	if _, ok := f.g.File(notes); !ok {
		t.Fatal("the file must stay in Drive")
	}
	mine.Files = nil
	ann.Call("GET", "/api/my/classrooms/"+c.ID+"/drive-files", nil, 200, &mine)
	if len(mine.Files) != 0 {
		t.Fatal("removed")
	}
	if c := errCode(t, f.teacher, "POST", "/api/teacher/classrooms/"+c.ID+"/drive-files", map[string]any{"file_ids": []string{"never-picked"}}, 403); c != "drive_no_access" {
		t.Fatal(c)
	}
	errCode(t, f.teacher, "POST", "/api/teacher/classrooms/"+c.ID+"/drive-files", map[string]any{"file_ids": []string{}}, 422)
}

func TestResultsToDrive(t *testing.T) {
	f := setup(t)
	// Not connected yet.
	if c := errCode(t, f.teacher, "POST", "/api/teacher/sessions/00000000-0000-0000-0000-000000000000/drive-export", nil, 404); c != "not_found" {
		t.Fatal(c)
	}
	f.connect(t, f.teacher)

	// A session with one student's results.
	var c content.Classroom
	f.teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var m content.Module
	f.teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	f.teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var q quiz.Quiz
	f.teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Science"}, 201, &q)
	f.teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte("question_code,type,question_text,options,correct_answer,marks\nQ1,SINGLE,Capital of France?,Paris|Rome,Paris,1\n"))
	f.teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	var sess live.Session
	f.teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{}, 201, &sess)
	s := f.e.Client()
	s.Call("POST", "/api/auth/register", map[string]string{"email": "nimal@school.test", "name": "Nimal Perera", "password": "password123", "role": "student"}, 201, nil)
	var st live.StudentState
	s.Call("POST", "/api/join/sessions/"+sess.JoinCode, map[string]string{}, 200, &st)
	f.teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/admit", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
	s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Selected: []string{"o1"}}}, 200, nil)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, &st)
	for _, raw := range f.e.TakeJobs("evaluate_attempt") {
		var a eval.EvaluateAttemptArgs
		_ = json.Unmarshal(raw, &a)
		if err := f.e.App.Eval.EvaluateAttempt(context.Background(), a.AttemptID); err != nil {
			t.Fatal(err)
		}
	}

	// On demand: a Google Sheet in the results folder.
	var ex drive.Export
	f.teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/drive-export", nil, 201, &ex)
	if ex.Status != "done" || ex.WebLink == "" || !strings.HasPrefix(ex.Name, "Science (") || ex.Automatic {
		t.Fatalf("export = %+v", ex)
	}
	st2 := status(f.teacher)
	folder := strings.TrimPrefix(st2.FolderLink, "https://drive.google.com/drive/folders/")
	files := f.g.Files(folder)
	if folder == "" || len(files) != 1 || files[0].MimeType != drive.MimeSheet || !strings.Contains(string(files[0].Data), "Nimal Perera") {
		t.Fatalf("folder %q files = %+v", folder, files)
	}
	// The folder was deleted in Drive: a new one is made.
	f.g.Delete(folder)
	f.teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/drive-export", nil, 201, &ex)
	if newFolder := strings.TrimPrefix(status(f.teacher).FolderLink, "https://drive.google.com/drive/folders/"); newFolder == folder || len(f.g.Files(newFolder)) != 1 {
		t.Fatal("a new results folder")
	}
	// Someone else's session.
	other := f.e.NewUser(auth.RoleTeacher)
	f.connect(t, other)
	other.Call("POST", "/api/teacher/sessions/"+sess.ID+"/drive-export", nil, 404, nil)

	// After each session: the end queues it; the job saves once.
	f.teacher.Call("PUT", "/api/teacher/drive", map[string]bool{"auto_backup": true}, 200, nil)
	f.teacher.Call("POST", "/api/teacher/sessions/"+sess.ID+"/end", map[string]any{}, 204, nil)
	jobs := f.e.TakeJobs("drive_backup")
	if len(jobs) != 1 {
		t.Fatalf("%d backup jobs", len(jobs))
	}
	var args drive.BackupArgs
	_ = json.Unmarshal(jobs[0], &args)
	w := &drive.BackupWorker{Service: f.e.App.Drive}
	for range 2 {
		if err := w.Work(context.Background(), &river.Job[drive.BackupArgs]{JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 6}, Args: args}); err != nil {
			t.Fatal(err)
		}
	}
	var list struct{ Exports []drive.Export }
	f.teacher.Call("GET", "/api/teacher/drive/exports?target_id="+sess.ID, nil, 200, &list)
	autos := 0
	for _, e := range list.Exports {
		if e.Automatic {
			autos++
		}
	}
	if len(list.Exports) != 3 || autos != 1 {
		t.Fatalf("exports = %+v", list.Exports)
	}

	// Polls, on demand.
	var poll struct{ ID string }
	f.teacher.Call("POST", "/api/teacher/polls", map[string]any{"title": "Exit ticket"}, 201, &poll)
	f.teacher.Call("POST", "/api/teacher/polls/"+poll.ID+"/drive-export", nil, 201, &ex)
	if ex.Kind != "poll" || !strings.HasPrefix(ex.Name, "Exit ticket (poll ") {
		t.Fatalf("poll export = %+v", ex)
	}

	// Disconnected: the job does nothing, and saving asks to connect.
	f.teacher.Call("DELETE", "/api/teacher/drive", nil, 204, nil)
	if err := w.Work(context.Background(), &river.Job[drive.BackupArgs]{JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 6}, Args: args}); err != nil {
		t.Fatal(err)
	}
	if c := errCode(t, f.teacher, "POST", "/api/teacher/sessions/"+sess.ID+"/drive-export", nil, 409); c != "drive_not_connected" {
		t.Fatal(c)
	}
}
