package quiz

import "time"

// Role is where a resource attaches (BA §10.4).
type Role string

const (
	RoleQuestion Role = "Q" // question stem
	RoleOption   Role = "O" // answer option n
	RoleFeedback Role = "F" // feedback, shown with results
)

func (r Role) Valid() bool { return r == RoleQuestion || r == RoleOption || r == RoleFeedback }

// Resource is an image referenced by public URL; the file is never stored (BR-15).
type Resource struct {
	ID         string     `json:"id"`
	QuestionID string     `json:"question_id,omitempty"`
	Role       Role       `json:"role"`
	N          int        `json:"n"`
	SourceURL  string     `json:"source_url,omitempty"` // as entered by the teacher
	URL        string     `json:"url"`                  // normalised, embeddable form
	AltText    string     `json:"alt_text"`
	Status     string     `json:"status,omitempty"` // unchecked, ok, broken
	Message    string     `json:"message,omitempty"`
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
}

// Public is the subset sent to students.
func (r Resource) Public() Resource {
	return Resource{ID: r.ID, Role: r.Role, N: r.N, URL: r.URL, AltText: r.AltText}
}
