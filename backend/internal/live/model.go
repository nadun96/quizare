// Package live runs sessions: QR join, waiting room, admission, countdown,
// server-authoritative timers, pause/extend, answer saving, proctoring and
// the real-time hub (FR-SS, FR-PR; ADR-04, ADR-05, ADR-07, ADR-08).
package live

import (
	"time"

	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/settings"
)

// Session statuses (BR-04: the QR works while open or live).
const (
	SessionOpen  = "open"
	SessionLive  = "live"
	SessionEnded = "ended"
)

// Attempt states (FR-SS-10 dashboard columns).
const (
	StateWaiting     = "waiting"
	StateAdmitted    = "admitted" // countdown running, or waiting for the teacher to start (no deadline, D-53)
	StateInProgress  = "in_progress"
	StatePaused      = "paused"
	StateSubmitted   = "submitted"
	StateInvalidated = "invalidated"
	StateNotStarted  = "not_started" // session ended before the student started
)

// Finished reports whether an attempt can no longer change.
func Finished(state string) bool {
	return state == StateSubmitted || state == StateInvalidated || state == StateNotStarted
}

// Snapshot freezes the quiz at session creation so later edits never change
// a running session or its results (ADR-14, D-16).
type Snapshot struct {
	QuizTitle string               `json:"quiz_title"`
	Base      settings.Effective   `json:"base"`   // defaults → platform → teacher
	Layers    []settings.Overrides `json:"layers"` // classroom, module, topic, quiz
	Questions []quiz.Question      `json:"questions"`
}

type Session struct {
	ID           string             `json:"id"`
	QuizID       string             `json:"quiz_id"`
	TeacherID    string             `json:"teacher_id"`
	ClassroomID  string             `json:"classroom_id"`
	Title        string             `json:"title"`
	JoinCode     string             `json:"join_code"`
	JoinURL      string             `json:"join_url"`
	Status       string             `json:"status"`
	Settings     settings.Overrides `json:"settings"`
	ExtensionSec int                `json:"extension_sec"` // session-wide extension, added to everyone (BA §7)
	CreatedAt    time.Time          `json:"created_at"`
	EndedAt      *time.Time         `json:"ended_at,omitempty"`
	ReleasedAt   *time.Time         `json:"results_released_at,omitempty"`
	// StartedAt is when the teacher started the quiz for everyone (D-53).
	// From then on, students admitted later count down by themselves.
	StartedAt *time.Time `json:"started_at,omitempty"`
	Snapshot  *Snapshot  `json:"-"`
}

// Effective resolves configuration for this session, optionally for one
// question and one student (BA §7 resolution order: … Quiz < Question <
// Session < Student).
func (s *Session) Effective(q *quiz.Question, student settings.Overrides) settings.Effective {
	layers := append([]settings.Overrides{}, s.Snapshot.Layers...)
	if q != nil {
		layers = append(layers, q.Settings)
	}
	layers = append(layers, s.Settings, student)
	return settings.Resolve(s.Snapshot.Base, layers...)
}

type Attempt struct {
	ID            string             `json:"id"`
	SessionID     string             `json:"session_id"`
	UserID        string             `json:"user_id"`
	StudentNumber *string            `json:"student_number"`
	State         string             `json:"state"`
	Overrides     settings.Overrides `json:"overrides"` // student-level settings
	ExtensionSec  int                `json:"extension_sec"`

	CountdownDeadline   *time.Time `json:"countdown_deadline,omitempty"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	QuizDeadline        *time.Time `json:"quiz_deadline,omitempty"`
	QuizRemainingMs     *int64     `json:"quiz_remaining_ms,omitempty"` // set while paused (ADR-05 rule)
	Current             int        `json:"current"`
	QuestionDeadline    *time.Time `json:"question_deadline,omitempty"` // uncapped; effective = min(this, quiz deadline)
	QuestionRemainingMs *int64     `json:"question_remaining_ms,omitempty"`
	PausedAt            *time.Time `json:"paused_at,omitempty"`

	Order        []int               `json:"-"` // question indexes into the snapshot
	OptionOrders map[string][]string `json:"-"` // per question id, shuffled choice ids

	Warnings       int        `json:"warnings"`
	Violations     int        `json:"violations"`
	DisconnectedAt *time.Time `json:"-"`
	SubmittedAt    *time.Time `json:"submitted_at,omitempty"`
	InvalidatedAt  *time.Time `json:"invalidated_at,omitempty"`
	InvalidReason  string     `json:"invalid_reason,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	TeamID         *string    `json:"team_id,omitempty"` // D-44
	Captain        bool       `json:"captain,omitempty"`
}
