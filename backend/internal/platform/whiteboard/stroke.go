// Package whiteboard is what the boards of polls (D-47) and meetings (D-72)
// share: a stroke on the fixed 1600×900 board, and the checks every stroke
// passes before it's kept or sent on.
package whiteboard

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	W, H         = 1600, 900
	MaxPointsPer = 4000 // numbers, i.e. 2000 points
)

var (
	colorRe   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	gestureRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
)

// Stroke is one mark on a board. By says whose it is, so a browser can
// recognise its own strokes; pieces of one pen line share a gesture id so
// undo removes the whole line.
type Stroke struct {
	ID      int64     `json:"id"`
	By      string    `json:"by"`
	Gesture string    `json:"gesture"`
	Tool    string    `json:"tool"` // pen highlighter line rect ellipse arrow text
	Color   string    `json:"color"`
	Size    float64   `json:"size"`
	Points  []float64 `json:"points"`
	Text    string    `json:"text,omitempty"`
}

// Clean tidies a stroke (text without control characters) and lists what's
// wrong with it, by field; empty means it may be kept.
func (st *Stroke) Clean() map[string]string {
	f := map[string]string{}
	switch st.Tool {
	case "pen", "highlighter":
		if len(st.Points) < 2 || len(st.Points)%2 != 0 || len(st.Points) > MaxPointsPer {
			f["points"] = "a line needs 1-2000 points"
		}
	case "line", "rect", "ellipse", "arrow":
		if len(st.Points) != 4 {
			f["points"] = "a shape needs two corners"
		}
	case "text":
		if len(st.Points) != 2 {
			f["points"] = "text needs a position"
		}
		st.Text = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) && r != '\n' {
				return -1
			}
			return r
		}, strings.TrimSpace(st.Text))
		if n := utf8.RuneCountInString(st.Text); n < 1 || n > 200 {
			f["text"] = "text must be 1-200 characters"
		}
	default:
		f["tool"] = "unknown tool"
	}
	if st.Tool != "text" {
		st.Text = ""
	}
	for i, v := range st.Points {
		lim := float64(W)
		if i%2 == 1 {
			lim = H
		}
		if v < -50 || v > lim+50 || v != v {
			f["points"] = "points must be on the board"
			break
		}
	}
	if !colorRe.MatchString(st.Color) {
		f["color"] = "colour must be #rrggbb"
	}
	if st.Size < 1 || st.Size > 80 {
		f["size"] = "size must be 1-80"
	}
	if !GestureOK(st.Gesture) {
		f["gesture"] = "invalid gesture id"
	}
	return f
}

// GestureOK says whether a gesture id is well formed.
func GestureOK(g string) bool { return gestureRe.MatchString(g) }
