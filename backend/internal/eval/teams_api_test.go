package eval_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nadun96/quizplatform/internal/app/apptest"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/quiz"
)

const teamQuiz = `question_code,type,question_text,options,correct_answer,marks,evaluation
Q1,SINGLE,Capital of France?,Paris|Rome,Paris,2,KEY
Q2,SINGLE,Capital of Norway?,Oslo|Rome,Oslo,1,KEY
`

func teamSetup(t *testing.T, st map[string]any) *fx {
	t.Helper()
	e := apptest.New(t)
	teacher := e.NewUser(auth.RoleTeacher)
	var c content.Classroom
	teacher.Call("POST", "/api/teacher/classrooms", map[string]any{"name": "C"}, 201, &c)
	var m content.Module
	teacher.Call("POST", "/api/teacher/classrooms/"+c.ID+"/modules", map[string]any{"name": "M"}, 201, &m)
	var topic content.Topic
	teacher.Call("POST", "/api/teacher/modules/"+m.ID+"/topics", map[string]any{"name": "T"}, 201, &topic)
	var q quiz.Quiz
	teacher.Call("POST", "/api/teacher/topics/"+topic.ID+"/quizzes", map[string]any{"title": "Q"}, 201, &q)
	teacher.Raw("POST", "/api/teacher/quizzes/"+q.ID+"/questions/import", "text/csv", []byte(teamQuiz))
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/status", map[string]any{"status": "ready"}, 200, nil)
	var sess live.Session
	teacher.Call("POST", "/api/teacher/quizzes/"+q.ID+"/sessions", map[string]any{"settings": st}, 201, &sess)
	return &fx{e: e, teacher: teacher, sess: sess}
}

// takeIn joins team, answers each question with the given option text, and submits.
func (f *fx) takeIn(t *testing.T, team string, picks ...string) (*apptest.Client, string) {
	t.Helper()
	s := f.e.NewUser(auth.RoleStudent)
	var st live.StudentState
	s.Call("POST", "/api/join/sessions/"+f.sess.JoinCode, map[string]string{"team_id": team}, 200, &st)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/admit", map[string]any{"attempt_ids": []string{st.AttemptID}}, 200, nil)
	s.Call("POST", "/api/attempts/"+st.AttemptID+"/start", nil, 200, &st)
	for _, pick := range picks {
		id := ""
		for _, o := range st.Question.Body.Options {
			if o.Text == pick {
				id = o.ID
			}
		}
		s.Call("PUT", "/api/attempts/"+st.AttemptID+"/answers/"+st.Question.ID, map[string]any{"response": quiz.Response{Selected: []string{id}}, "seq": 1}, 200, nil)
		s.Call("POST", "/api/attempts/"+st.AttemptID+"/advance", map[string]string{"question_id": st.Question.ID}, 200, &st)
	}
	return s, st.AttemptID
}

func (f *fx) standings(t *testing.T, st map[string]any) string {
	t.Helper()
	if st != nil {
		f.teacher.Call("PUT", "/api/teacher/sessions/"+f.sess.ID+"/settings", st, 200, nil)
	}
	var out struct{ Teams []eval.TeamStanding }
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID+"/teams/standings", nil, 200, &out)
	var b []string
	for _, x := range out.Teams {
		b = append(b, fmt.Sprintf("%d:%s=%g/%g(%d/%d)", x.Rank, x.Name, x.Score, x.MaxScore, x.Finished, x.Members))
	}
	return strings.Join(b, " ")
}

func rules(acceptance, calc string) map[string]any {
	return map[string]any{"team_mode": "self", "team_acceptance": acceptance, "team_calc": calc, "results_release": "immediate"}
}

func TestTeamStandings(t *testing.T) {
	f := teamSetup(t, rules("all", "average"))
	var red, blue live.TeamInfo
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/teams", map[string]any{"name": "Red"}, 201, &red)
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/teams", map[string]any{"name": "Blue"}, 201, &blue)
	// Max per student: 3. Ann (Red) 3, Ben (Red) 1, Cat (Blue) 3.
	ann, annAttempt := f.takeIn(t, red.ID, "Paris", "Oslo")
	f.takeIn(t, red.ID, "Rome", "Oslo")
	if got := f.standings(t, nil); got != "1:Red=0/3(0/2) 1:Blue=0/3(0/0)" {
		t.Fatalf("before marking: %s", got)
	}
	f.takeIn(t, blue.ID, "Paris", "Oslo")
	f.evaluate(t)

	cases := []struct{ acc, calc, want string }{
		{"all", "average", "1:Blue=3/3(1/1) 2:Red=2/3(2/2)"},
		{"all", "sum", "1:Blue=3/3(1/1) 2:Red=4/6(2/2)"}, // ranked by percentage
		{"all", "max", "1:Red=3/3(2/2) 1:Blue=3/3(1/1)"},
		{"all", "min", "1:Blue=3/3(1/1) 2:Red=1/3(2/2)"},
		{"best", "sum", "1:Red=3/3(2/2) 1:Blue=3/3(1/1)"},
		{"first", "sum", "1:Red=3/3(2/2) 1:Blue=3/3(1/1)"},   // Ann answered first
		{"captain", "sum", "1:Red=3/3(2/2) 1:Blue=3/3(1/1)"}, // Ann joined first
	}
	for _, c := range cases {
		if got := f.standings(t, rules(c.acc, c.calc)); got != c.want {
			t.Errorf("%s/%s: got %s, want %s", c.acc, c.calc, got, c.want)
		}
	}
	// Ben becomes Red's captain: now his marks count.
	var tv live.TeamsView
	f.teacher.Call("GET", "/api/teacher/sessions/"+f.sess.ID+"/teams", nil, 200, &tv)
	ben := ""
	for _, m := range tv.Teams[0].List {
		if m.AttemptID != annAttempt {
			ben = m.AttemptID
		}
	}
	f.teacher.Call("POST", "/api/teacher/sessions/"+f.sess.ID+"/teams/members", map[string]any{"attempt_ids": []string{ben}, "captain": true}, 204, nil)
	if got := f.standings(t, nil); got != "1:Blue=3/3(1/1) 2:Red=1/3(2/2)" {
		t.Fatalf("new captain: %s", got)
	}

	// The student's released result carries their team's place.
	var res eval.StudentResult
	ann.Call("GET", "/api/my/attempts/"+annAttempt+"/result", nil, 200, &res)
	if res.Team == nil || res.Team.Name != "Red" || res.Team.Rank != 2 || res.Team.Of != 2 {
		t.Fatalf("student team result: %+v", res.Team)
	}
	// Teams off: no standings.
	if got := f.standings(t, map[string]any{"team_mode": "off"}); got != "" {
		t.Fatalf("teams off: %s", got)
	}
}
