// Package settings implements the hierarchical configuration of BA §7 and
// ADR-14: every level stores sparse overrides; the most specific level that
// sets a value wins; anything unset inherits from the level above, down to
// platform defaults.
package settings

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type Level string

// Levels from least to most specific (BA §7 resolution order, reversed).
const (
	LevelPlatform  Level = "platform"
	LevelTeacher   Level = "teacher"
	LevelClassroom Level = "classroom"
	LevelModule    Level = "module"
	LevelTopic     Level = "topic"
	LevelQuiz      Level = "quiz"
	LevelQuestion  Level = "question"
	LevelSession   Level = "session"
	LevelStudent   Level = "student"
)

// Order is the merge order: later entries override earlier ones.
var Order = []Level{LevelPlatform, LevelTeacher, LevelClassroom, LevelModule, LevelTopic, LevelQuiz, LevelQuestion, LevelSession, LevelStudent}

// Overrides is the sparse form stored as JSONB at each level. A nil field
// means "not set here; inherit".
//
// Time extensions and pauses are not settings: they accumulate rather than
// override (BA §7 timing rules) and live with the session/attempt.
type Overrides struct {
	CountdownSeconds     *int    `json:"countdown_seconds,omitempty" levels:"teacher,classroom,quiz,session"`
	QuestionTimeLimitSec *int    `json:"question_time_limit_sec,omitempty" levels:"quiz,question"`
	QuizTimeLimitSec     *int    `json:"quiz_time_limit_sec,omitempty" levels:"topic,quiz,session,student"`
	AdmissionMode        *string `json:"admission_mode,omitempty" levels:"classroom,session" enum:"manual,auto"`
	EvaluationMethod     *string `json:"evaluation_method,omitempty" levels:"teacher,quiz,question" enum:"key,llm,manual"`
	LLMKeyID             *string `json:"llm_key_id,omitempty" levels:"teacher,quiz,question"`
	LLMModel             *string `json:"llm_model,omitempty" levels:"teacher,quiz,question"`
	FeedbackMode         *string `json:"feedback_mode,omitempty" levels:"teacher,quiz,question" enum:"none,predefined,ai,both"`
	ViolationPolicy      *string `json:"violation_policy,omitempty" levels:"classroom,quiz,session" enum:"invalidate,warn_then_invalidate,log_only"`
	AllowedWarnings      *int    `json:"allowed_warnings,omitempty" levels:"classroom,quiz,session"`
	BlurGraceMs          *int    `json:"blur_grace_ms,omitempty" levels:"classroom,quiz,session"`
	DisconnectGraceSec   *int    `json:"disconnect_grace_sec,omitempty" levels:"classroom,quiz,session"`
	QuestionOrder        *string `json:"question_order,omitempty" levels:"quiz,session" enum:"fixed,shuffled"`
	OptionOrder          *string `json:"option_order,omitempty" levels:"quiz,question" enum:"fixed,shuffled"`
	OneWayNavigation     *bool   `json:"one_way_navigation,omitempty" levels:"quiz"`
	ResultsVisibility    *string `json:"results_visibility,omitempty" levels:"quiz,session" enum:"private,public"`
	ResultsView          *string `json:"results_view,omitempty" levels:"quiz,session" enum:"individual,question_pct,pass_rate"`
	ResultsRelease       *string `json:"results_release,omitempty" levels:"quiz,session" enum:"immediate,on_session_end,manual"`
	ResultsShowAnswers   *bool   `json:"results_show_answers,omitempty" levels:"quiz,session"`
	ResultsShowCorrect   *bool   `json:"results_show_correct,omitempty" levels:"quiz,session"`
	ResultsShowFeedback  *bool   `json:"results_show_feedback,omitempty" levels:"quiz,session"`
	PassMarkPct          *int    `json:"pass_mark_pct,omitempty" levels:"classroom,quiz"`
	StudentIDRequired    *bool   `json:"student_id_required,omitempty" levels:"classroom"`
	EnrolmentApproval    *bool   `json:"enrolment_approval,omitempty" levels:"classroom"`
	AutoEnrolOnJoin      *bool   `json:"auto_enrol_on_join,omitempty" levels:"classroom"`
}

// Effective is the fully resolved configuration; every field has a value.
// Optional limits use 0 for "none".
type Effective struct {
	CountdownSeconds     int    `json:"countdown_seconds"`
	QuestionTimeLimitSec int    `json:"question_time_limit_sec"`
	QuizTimeLimitSec     int    `json:"quiz_time_limit_sec"`
	AdmissionMode        string `json:"admission_mode"`
	EvaluationMethod     string `json:"evaluation_method"`
	LLMKeyID             string `json:"llm_key_id"`
	LLMModel             string `json:"llm_model"`
	FeedbackMode         string `json:"feedback_mode"`
	ViolationPolicy      string `json:"violation_policy"`
	AllowedWarnings      int    `json:"allowed_warnings"`
	BlurGraceMs          int    `json:"blur_grace_ms"`
	DisconnectGraceSec   int    `json:"disconnect_grace_sec"`
	QuestionOrder        string `json:"question_order"`
	OptionOrder          string `json:"option_order"`
	OneWayNavigation     bool   `json:"one_way_navigation"`
	ResultsVisibility    string `json:"results_visibility"`
	ResultsView          string `json:"results_view"`
	ResultsRelease       string `json:"results_release"`
	ResultsShowAnswers   bool   `json:"results_show_answers"`
	ResultsShowCorrect   bool   `json:"results_show_correct"`
	ResultsShowFeedback  bool   `json:"results_show_feedback"`
	PassMarkPct          int    `json:"pass_mark_pct"`
	StudentIDRequired    bool   `json:"student_id_required"`
	EnrolmentApproval    bool   `json:"enrolment_approval"`
	AutoEnrolOnJoin      bool   `json:"auto_enrol_on_join"`
}

// Defaults are the BA §7 defaults; the admin can override them at platform level.
func Defaults() Effective {
	return Effective{
		CountdownSeconds:   60,
		AdmissionMode:      "manual",
		EvaluationMethod:   "key",
		FeedbackMode:       "predefined",
		ViolationPolicy:    "invalidate",
		AllowedWarnings:    1,    // used only by warn_then_invalidate
		BlurGraceMs:        2000, // BA risk R-01: short blur threshold
		DisconnectGraceSec: 10,   // architecture heartbeat rule: 5-10 s
		QuestionOrder:      "fixed",
		OptionOrder:        "fixed",
		OneWayNavigation:   true, // BR-09
		ResultsVisibility:  "private",
		ResultsView:        "individual",
		ResultsRelease:     "on_session_end",
		// BA §11: score, answers, correct answers and feedback are each toggleable.
		ResultsShowAnswers:  true,
		ResultsShowCorrect:  true,
		ResultsShowFeedback: true,
		PassMarkPct:         50,
		AutoEnrolOnJoin:     true, // BR-02, Q-06; see DECISIONS.md
	}
}

// Validate checks enum values, ranges, and that each set key is allowed at level.
func (o Overrides) Validate(level Level) error {
	errs := map[string]string{}
	v := reflect.ValueOf(o)
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := v.Field(i)
		if f.IsNil() {
			continue
		}
		sf := t.Field(i)
		name := strings.Split(sf.Tag.Get("json"), ",")[0]
		if level != LevelPlatform && !contains(sf.Tag.Get("levels"), string(level)) {
			errs[name] = fmt.Sprintf("cannot be set at %s level (allowed: %s)", level, sf.Tag.Get("levels"))
			continue
		}
		switch x := f.Elem().Interface().(type) {
		case string:
			if enum := sf.Tag.Get("enum"); enum != "" && !contains(enum, x) {
				errs[name] = "must be one of " + enum
			}
			if len(x) > 200 {
				errs[name] = "too long"
			}
		case int:
			if msg := checkInt(name, x); msg != "" {
				errs[name] = msg
			}
		}
	}
	if len(errs) > 0 {
		return &ValidationError{Fields: errs}
	}
	return nil
}

func checkInt(name string, x int) string {
	switch name {
	case "pass_mark_pct":
		if x < 0 || x > 100 {
			return "must be between 0 and 100"
		}
	case "countdown_seconds":
		if x < 0 || x > 3600 {
			return "must be between 0 and 3600"
		}
	default:
		if x < 0 || x > 7*24*3600*1000 {
			return "must be zero or a positive number"
		}
	}
	return ""
}

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return fmt.Sprintf("invalid settings: %v", e.Fields) }

func contains(csv, s string) bool {
	for _, p := range strings.Split(csv, ",") {
		if p == s {
			return true
		}
	}
	return false
}

// Resolve applies layers in order on top of base; later layers win.
func Resolve(base Effective, layers ...Overrides) Effective {
	out := reflect.ValueOf(&base).Elem()
	for _, layer := range layers {
		lv := reflect.ValueOf(layer)
		for i := 0; i < lv.NumField(); i++ {
			if f := lv.Field(i); !f.IsNil() {
				out.FieldByName(lv.Type().Field(i).Name).Set(f.Elem())
			}
		}
	}
	return base
}

// ApplyPlatform resolves platform overrides on top of built-in defaults.
func ApplyPlatform(platform Overrides) Effective { return Resolve(Defaults(), platform) }

// Parse decodes stored JSONB overrides, rejecting unknown keys.
func Parse(raw []byte) (Overrides, error) {
	var o Overrides
	if len(raw) == 0 {
		return o, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	err := dec.Decode(&o)
	return o, err
}

// MustJSON encodes overrides for storage.
func (o Overrides) JSON() []byte {
	b, _ := json.Marshal(o)
	return b
}
