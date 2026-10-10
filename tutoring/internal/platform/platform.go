// Package platform calls the quiz platform's internal API (TS-NFR-51,
// D-59): tutoring never reads the platform's tables.
package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Access is how a person may take part in a classroom: Role is "teacher"
// or "student".
type Access struct {
	ClassroomID   string `json:"classroom_id"`
	ClassroomName string `json:"classroom_name"`
	TeacherID     string `json:"teacher_id"`
	Archived      bool   `json:"archived"`
	Role          string `json:"role"`
}

type Client struct {
	base string // the platform's address on the server's network
	keys authn.Keys
	http *http.Client
}

func New(base string, keys authn.Keys) *Client {
	return &Client{base: strings.TrimRight(base, "/"), keys: keys, http: &http.Client{Timeout: 5 * time.Second}}
}

// ErrUnavailable: the platform didn't answer; the person should retry.
var ErrUnavailable = web.NewError(http.StatusServiceUnavailable, "platform_unavailable", "the quiz platform isn't answering; try again in a moment")

// Access asks the platform whether userID teaches or studies in the
// classroom. Joining may enrol a student, as joining a quiz does (BR-02).
// The platform's refusals ("not enrolled", "pending") come back as errors
// with its codes and messages, for the page to show.
func (c *Client) Access(ctx context.Context, classroomID, userID string) (Access, error) {
	var a Access
	token, err := c.keys.ServiceToken()
	if err != nil {
		return a, err
	}
	body, _ := json.Marshal(map[string]string{"classroom_id": classroomID, "user_id": userID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/internal/tutoring/access", bytes.NewReader(body))
	if err != nil {
		return a, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return a, ErrUnavailable
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return a, json.NewDecoder(resp.Body).Decode(&a)
	case resp.StatusCode >= 500:
		return a, ErrUnavailable
	}
	e := &web.Error{Status: resp.StatusCode}
	if json.NewDecoder(resp.Body).Decode(e) != nil || e.Code == "" {
		return a, fmt.Errorf("platform access: status %d", resp.StatusCode)
	}
	if e.Status == http.StatusUnprocessableEntity { // the classroom needs a student ID first
		return a, web.NewError(http.StatusForbidden, "student_number_required", "this classroom needs your student ID: join it from your classes page first")
	}
	return a, e
}
