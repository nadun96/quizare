// Package tutorial runs interactive tutorials (D-74, TU-FR): lessons in
// pages, each with content blocks (reading, pictures, video, documents,
// links), questions about them, or both. Students work through them at their
// own pace; the teacher sets the order, whether students may go back, the
// checkpoints between pages and when feedback shows. The questions are
// ordinary quiz questions in a quiz of kind "tutorial", marked as quizzes are.
// It owns the "tutorial" schema.
package tutorial

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nadun96/quizplatform/internal/imageurl"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

const (
	StatusDraft     = "draft"
	StatusPublished = "published"

	// When students see marks and feedback (PO-33).
	FeedbackPage    = "page"    // as they go: once a page's answers are checked
	FeedbackEnd     = "end"     // when they finish
	FeedbackRelease = "release" // when the teacher releases them

	CheckNone     = "none"
	CheckAnswered = "answered"
	CheckCorrect  = "correct"

	maxPages        = 100
	maxBlocks       = 30
	maxPageQuestion = 20
	maxText         = 20000
)

// Tutorial is a lesson in pages, under a classroom topic.
type Tutorial struct {
	ID          string     `json:"id"`
	TopicID     string     `json:"topic_id"`
	QuizID      string     `json:"quiz_id"` // where its questions live
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	AllowBack   bool       `json:"allow_back"`
	LockAnswers bool       `json:"lock_answers"`
	Feedback    string     `json:"feedback"`
	ReleasedAt  *time.Time `json:"released_at,omitempty"`
	OpensAt     *time.Time `json:"opens_at,omitempty"`
	DueAt       *time.Time `json:"due_at,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Pages       int        `json:"page_count"`
}

// Block is one piece of a page's content.
type Block struct {
	// Type: text (Markdown, as the question editor writes it), image, video
	// (YouTube or a Drive video), document (a Drive file, or a Google Doc,
	// Slides or Sheet, shown in Google's viewer) or link.
	Type string `json:"type"`
	Text string `json:"text,omitempty"` // the text, or a caption
	URL  string `json:"url,omitempty"`  // as given
	// Embed is what the page shows: the image's embeddable URL, or the
	// player or viewer address. Worked out on save.
	Embed string `json:"embed,omitempty"`
	Alt   string `json:"alt,omitempty"` // an image's description
}

// Page is one screen of a tutorial.
type Page struct {
	ID          string   `json:"id"`
	Position    int      `json:"position"`
	Title       string   `json:"title"`
	Blocks      []Block  `json:"blocks"`
	QuestionIDs []string `json:"question_ids"`
	Checkpoint  string   `json:"checkpoint"`
	MaxTries    int      `json:"max_tries"`
}

var (
	youtubeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{6,20}$`)
	googleDocRe = regexp.MustCompile(`^/(document|presentation|spreadsheets)/d/([A-Za-z0-9_-]{10,})`)
)

func httpsURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("enter a web address starting with https://")
	}
	return u, nil
}

// youtubeEmbed turns a YouTube link into its privacy-enhanced player.
func youtubeEmbed(u *url.URL) string {
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	host = strings.TrimPrefix(host, "m.")
	var id string
	switch {
	case host == "youtu.be":
		id = strings.Trim(u.Path, "/")
	case host == "youtube.com" || host == "youtube-nocookie.com":
		switch {
		case u.Path == "/watch":
			id = u.Query().Get("v")
		case strings.HasPrefix(u.Path, "/embed/"), strings.HasPrefix(u.Path, "/shorts/"), strings.HasPrefix(u.Path, "/live/"):
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) > 1 {
				id = parts[1]
			}
		}
	}
	if !youtubeIDRe.MatchString(id) {
		return ""
	}
	return "https://www.youtube-nocookie.com/embed/" + id
}

// googleViewer turns a Drive file or a Google Doc, Slides or Sheet into the
// address of Google's viewer for it.
func googleViewer(u *url.URL) string {
	if id := imageurl.DriveFileID(u); id != "" {
		return "https://drive.google.com/file/d/" + id + "/preview"
	}
	if strings.ToLower(u.Hostname()) == "docs.google.com" {
		if m := googleDocRe.FindStringSubmatch(u.Path); m != nil {
			return "https://docs.google.com/" + m[1] + "/d/" + m[2] + "/preview"
		}
	}
	return ""
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// clean checks one block and works out what it shows.
func (b *Block) clean() string {
	b.Type = strings.TrimSpace(b.Type)
	b.Text = strings.TrimSpace(b.Text)
	b.Alt = strings.TrimSpace(b.Alt)
	b.URL = strings.TrimSpace(b.URL)
	b.Embed = ""
	switch b.Type {
	case "text":
		b.URL, b.Alt = "", ""
		if n := runes(b.Text); n < 1 || n > maxText {
			return fmt.Sprintf("write some text (at most %d characters)", maxText)
		}
		return ""
	case "image":
		norm, err := imageurl.Normalise(b.URL)
		if err != nil {
			return err.Error()
		}
		b.Embed = norm
		if runes(b.Alt) > 500 {
			return "describe the image in at most 500 characters"
		}
	case "video":
		u, err := httpsURL(b.URL)
		if err != nil {
			return err.Error()
		}
		if b.Embed = youtubeEmbed(u); b.Embed == "" {
			if id := imageurl.DriveFileID(u); id != "" {
				b.Embed = "https://drive.google.com/file/d/" + id + "/preview"
			}
		}
		if b.Embed == "" {
			return "use a YouTube link, or a video's Google Drive link"
		}
	case "document":
		u, err := httpsURL(b.URL)
		if err != nil {
			return err.Error()
		}
		if b.Embed = googleViewer(u); b.Embed == "" {
			return "use a Google Drive file's link, or a Google Doc, Slides or Sheet; for other sites, add a link"
		}
	case "link":
		u, err := httpsURL(b.URL)
		if err != nil {
			return err.Error()
		}
		b.URL = u.String()
		if b.Text == "" {
			b.Text = u.Host
		}
	default:
		return "unknown kind of content"
	}
	if runes(b.Text) > 500 {
		return "a caption has at most 500 characters"
	}
	return ""
}

// PageInput creates or changes a page.
type PageInput struct {
	Title       string   `json:"title"`
	Blocks      []Block  `json:"blocks"`
	QuestionIDs []string `json:"question_ids"`
	Checkpoint  string   `json:"checkpoint"`
	MaxTries    int      `json:"max_tries"`
}

func (in *PageInput) clean(feedback string) error {
	f := map[string]string{}
	in.Title = strings.Join(strings.Fields(in.Title), " ")
	if runes(in.Title) > 200 {
		f["title"] = "at most 200 characters"
	}
	if len(in.Blocks) > maxBlocks {
		f["blocks"] = fmt.Sprintf("at most %d pieces of content on a page", maxBlocks)
	}
	for i := range in.Blocks {
		if msg := in.Blocks[i].clean(); msg != "" {
			f[fmt.Sprintf("blocks.%d", i)] = msg
		}
	}
	if len(in.QuestionIDs) > maxPageQuestion {
		f["question_ids"] = fmt.Sprintf("at most %d questions on a page", maxPageQuestion)
	}
	seen := map[string]bool{}
	for _, id := range in.QuestionIDs {
		if seen[id] {
			f["question_ids"] = "a question is listed twice"
		}
		seen[id] = true
	}
	if len(in.Blocks) == 0 && len(in.QuestionIDs) == 0 && in.Title == "" {
		f["blocks"] = "add some content or a question"
	}
	if in.Checkpoint == "" {
		in.Checkpoint = CheckNone
	}
	switch {
	case in.Checkpoint != CheckNone && in.Checkpoint != CheckAnswered && in.Checkpoint != CheckCorrect:
		f["checkpoint"] = "choose none, answered or correct"
	case in.Checkpoint != CheckNone && len(in.QuestionIDs) == 0:
		in.Checkpoint = CheckNone // nothing to check
	case in.Checkpoint == CheckCorrect && feedback != FeedbackPage:
		f["checkpoint"] = "needing correct answers shows whether they're right, so it needs feedback as students go"
	}
	if in.MaxTries < 0 || in.MaxTries > 20 {
		f["max_tries"] = "0 (unlimited) to 20 tries"
	}
	if in.Checkpoint != CheckCorrect {
		in.MaxTries = 0
	}
	if in.Blocks == nil {
		in.Blocks = []Block{}
	}
	if in.QuestionIDs == nil {
		in.QuestionIDs = []string{}
	}
	if len(f) > 0 {
		return httpx.Invalid(f)
	}
	return nil
}

// TutorialInput creates or changes a tutorial's details and settings.
type TutorialInput struct {
	Title       *string    `json:"title"`
	Description *string    `json:"description"`
	AllowBack   *bool      `json:"allow_back"`
	LockAnswers *bool      `json:"lock_answers"`
	Feedback    *string    `json:"feedback"`
	OpensAt     *time.Time `json:"opens_at"`
	DueAt       *time.Time `json:"due_at"`
	// ClearDates removes both dates.
	ClearDates bool `json:"clear_dates"`
}
