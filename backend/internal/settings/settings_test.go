package settings

import (
	"errors"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestDefaultsMatchBA(t *testing.T) {
	d := Defaults()
	if d.CountdownSeconds != 60 || d.AdmissionMode != "manual" || d.EvaluationMethod != "key" ||
		d.FeedbackMode != "predefined" || d.ViolationPolicy != "invalidate" || d.QuestionOrder != "fixed" ||
		d.OptionOrder != "fixed" || d.ResultsVisibility != "private" || d.ResultsView != "individual" ||
		d.ResultsRelease != "on_session_end" || d.PassMarkPct != 50 || d.StudentIDRequired ||
		d.QuizTimeLimitSec != 0 || d.QuestionTimeLimitSec != 0 {
		t.Fatalf("defaults diverge from BA §7: %+v", d)
	}
}

func TestMostSpecificLevelWins(t *testing.T) {
	teacher := Overrides{CountdownSeconds: ptr(30), FeedbackMode: ptr("both")}
	classroom := Overrides{CountdownSeconds: ptr(45)}
	quiz := Overrides{QuizTimeLimitSec: ptr(600)}
	session := Overrides{QuizTimeLimitSec: ptr(900)} // BA: session duration overrides the quiz's
	got := Resolve(Defaults(), teacher, classroom, Overrides{}, Overrides{}, quiz, Overrides{}, session)
	if got.CountdownSeconds != 45 {
		t.Errorf("countdown = %d, want classroom's 45", got.CountdownSeconds)
	}
	if got.FeedbackMode != "both" {
		t.Errorf("feedback mode = %s, want inherited teacher value", got.FeedbackMode)
	}
	if got.QuizTimeLimitSec != 900 {
		t.Errorf("quiz limit = %d, want session's 900", got.QuizTimeLimitSec)
	}
	if got.ViolationPolicy != "invalidate" {
		t.Errorf("unset values must fall back to defaults")
	}
}

func TestExplicitZeroAndFalseOverride(t *testing.T) {
	base := Resolve(Defaults(), Overrides{OneWayNavigation: ptr(true), QuizTimeLimitSec: ptr(600)})
	got := Resolve(base, Overrides{OneWayNavigation: ptr(false), QuizTimeLimitSec: ptr(0)})
	if got.OneWayNavigation || got.QuizTimeLimitSec != 0 {
		t.Fatalf("explicit false/0 must override: %+v", got)
	}
}

func TestValidateLevelsAndEnums(t *testing.T) {
	cases := []struct {
		name  string
		level Level
		o     Overrides
		bad   string
	}{
		{"admission not at quiz", LevelQuiz, Overrides{AdmissionMode: ptr("auto")}, "admission_mode"},
		{"student id only at classroom", LevelSession, Overrides{StudentIDRequired: ptr(true)}, "student_id_required"},
		{"bad enum", LevelQuiz, Overrides{ViolationPolicy: ptr("explode")}, "violation_policy"},
		{"pass mark range", LevelQuiz, Overrides{PassMarkPct: ptr(140)}, "pass_mark_pct"},
		{"negative limit", LevelQuiz, Overrides{QuizTimeLimitSec: ptr(-1)}, "quiz_time_limit_sec"},
	}
	for _, c := range cases {
		err := c.o.Validate(c.level)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Fields[c.bad] == "" {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
	ok := Overrides{CountdownSeconds: ptr(90), AdmissionMode: ptr("auto"), ViolationPolicy: ptr("log_only")}
	if err := ok.Validate(LevelSession); err != nil {
		t.Errorf("valid session overrides rejected: %v", err)
	}
	// Platform defaults may set anything.
	if err := (Overrides{StudentIDRequired: ptr(true)}).Validate(LevelPlatform); err != nil {
		t.Errorf("platform: %v", err)
	}
}

func TestParseRejectsUnknownKeys(t *testing.T) {
	if _, err := Parse([]byte(`{"countdown_seconds":5,"bogus":1}`)); err == nil {
		t.Fatal("unknown key accepted")
	}
	o, err := Parse([]byte(`{"countdown_seconds":5}`))
	if err != nil || *o.CountdownSeconds != 5 {
		t.Fatalf("parse: %+v %v", o, err)
	}
	if string((Overrides{}).JSON()) != "{}" {
		t.Fatal("empty overrides must encode as {}")
	}
}
