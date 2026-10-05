// Package analytics computes and shares results analytics (BA §11,
// FR-RS-01…05, ADR-15).
package analytics

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/quiz"
)

// StudentRow is the per-student view (BA §11 "Student").
type StudentRow struct {
	AttemptID     string   `json:"attempt_id"`
	SessionID     string   `json:"session_id"`
	Name          string   `json:"name"`
	StudentNumber *string  `json:"student_number"`
	State         string   `json:"state"`
	Score         float64  `json:"score"`
	MaxScore      float64  `json:"max_score"`
	Pct           float64  `json:"pct"`
	Passed        bool     `json:"passed"`
	Complete      bool     `json:"complete"`
	TimeTakenSec  *int     `json:"time_taken_sec"`
	Violations    int      `json:"violations"`
	ExtensionSec  int      `json:"extension_sec"`
	Pauses        int      `json:"pauses"`
	Answers       []Answer `json:"answers,omitempty"` // per-question answers (teacher views only)
}

type Answer struct {
	QuestionID string         `json:"question_id"`
	Code       string         `json:"code"`
	Response   *quiz.Response `json:"response"`
	Score      *float64       `json:"score"`
	Correct    *bool          `json:"correct"`
}

// QuestionRow is the per-question view (BA §11 "Question").
type QuestionRow struct {
	QuestionID     string            `json:"question_id"`
	Code           string            `json:"code"`
	Text           string            `json:"text"`
	Format         string            `json:"format,omitempty"` // text format, as quiz.Body.Format
	Type           quiz.Type         `json:"type"`
	MaxScore       float64           `json:"max_score"`
	Answered       int               `json:"answered"`
	PctCorrect     float64           `json:"pct_correct"`
	AvgScore       float64           `json:"avg_score"`
	OptionCounts   map[string]int    `json:"option_counts,omitempty"` // option id → times chosen
	OptionLabels   map[string]string `json:"option_labels,omitempty"`
	CommonWrong    []WrongAnswer     `json:"common_wrong,omitempty"`
	Discrimination *float64          `json:"discrimination"` // upper 27% minus lower 27% correct rate
}

type WrongAnswer struct {
	Answer string `json:"answer"`
	Count  int    `json:"count"`
}

// ClassStats is the per-class view (BA §11 "Class").
type ClassStats struct {
	Joined           int     `json:"joined"`
	Finished         int     `json:"finished"`
	Marked           int     `json:"marked"` // counted in score statistics (finished, not invalidated)
	Mean             float64 `json:"mean_pct"`
	Median           float64 `json:"median_pct"`
	PassRate         float64 `json:"pass_rate"`
	PassMarkPct      int     `json:"pass_mark_pct"`
	CompletionRate   float64 `json:"completion_rate"`
	InvalidationRate float64 `json:"invalidation_rate"`
	Distribution     []int   `json:"distribution"` // 10 buckets of 10 percentage points
	Pending          int     `json:"pending"`      // attempts still awaiting LLM/manual marks
}

type SessionStats struct {
	SessionID  string        `json:"session_id"`
	Title      string        `json:"title"`
	QuizTitle  string        `json:"quiz_title"`
	CreatedAt  time.Time     `json:"created_at"`
	Class      ClassStats    `json:"class"`
	Questions  []QuestionRow `json:"questions"`
	Students   []StudentRow  `json:"students"`
	ComputedAt time.Time     `json:"computed_at"`
}

func round(x float64) float64 { return math.Round(x*100) / 100 }

// input bundles what compute needs for one session.
type input struct {
	info    live.SessionInfo
	results []eval.AttemptResult
	data    map[string]*live.MarkingData
	names   map[string]string
	counts  map[string]int
	pauses  map[string]int
}

func compute(in input, now time.Time) SessionStats {
	st := SessionStats{SessionID: in.info.ID, Title: in.info.Title, QuizTitle: in.info.QuizTitle, CreatedAt: in.info.CreatedAt, ComputedAt: now}
	joined := 0
	for _, n := range in.counts {
		joined += n
	}
	c := ClassStats{Joined: joined, PassMarkPct: in.info.Effective.PassMarkPct, Distribution: make([]int, 10)}
	var pcts []float64
	passed, invalid := 0, 0
	for _, r := range in.results {
		c.Finished++
		d := in.data[r.AttemptID]
		row := StudentRow{AttemptID: r.AttemptID, SessionID: in.info.ID, Name: in.names[r.UserID], StudentNumber: r.StudentNumber,
			State: r.State, Score: r.Score, MaxScore: r.MaxScore, Pct: r.Pct, Passed: r.Passed, Complete: r.Complete,
			Pauses: in.pauses[r.AttemptID]}
		if d != nil {
			row.Violations, row.ExtensionSec = d.Violations, d.ExtensionSec
			if d.StartedAt != nil && d.SubmittedAt != nil {
				sec := int(d.SubmittedAt.Sub(*d.StartedAt).Seconds())
				row.TimeTakenSec = &sec
			}
		}
		for _, m := range r.Marks {
			row.Answers = append(row.Answers, Answer{QuestionID: m.QuestionID, Code: m.Code, Response: m.Response, Score: m.Score, Correct: m.Correct})
		}
		st.Students = append(st.Students, row)
		if r.Invalidated {
			invalid++
			continue
		}
		if !r.Complete {
			c.Pending++
		}
		c.Marked++
		pcts = append(pcts, r.Pct)
		if r.Passed {
			passed++
		}
		b := int(r.Pct / 10)
		if b > 9 {
			b = 9
		}
		if b < 0 {
			b = 0
		}
		c.Distribution[b]++
	}
	if len(pcts) > 0 {
		sum := 0.0
		for _, p := range pcts {
			sum += p
		}
		c.Mean = round(sum / float64(len(pcts)))
		sort.Float64s(pcts)
		mid := len(pcts) / 2
		if len(pcts)%2 == 1 {
			c.Median = pcts[mid]
		} else {
			c.Median = round((pcts[mid-1] + pcts[mid]) / 2)
		}
		c.PassRate = round(float64(passed) / float64(len(pcts)) * 100)
	}
	if joined > 0 {
		c.CompletionRate = round(float64(in.counts[live.StateSubmitted]) / float64(joined) * 100)
		c.InvalidationRate = round(float64(invalid) / float64(joined) * 100)
	}
	st.Class = c
	st.Questions = questionStats(in.info.Questions, in.results)
	return st
}

func questionStats(questions []quiz.Question, results []eval.AttemptResult) []QuestionRow {
	// Discrimination groups: top and bottom 27% of non-invalidated attempts by score.
	var ranked []eval.AttemptResult
	for _, r := range results {
		if !r.Invalidated {
			ranked = append(ranked, r)
		}
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Pct > ranked[j].Pct })
	k := int(math.Round(float64(len(ranked)) * 0.27))
	upper, lower := map[string]bool{}, map[string]bool{}
	if k > 0 && len(ranked) >= 4 {
		for i := 0; i < k; i++ {
			upper[ranked[i].AttemptID] = true
			lower[ranked[len(ranked)-1-i].AttemptID] = true
		}
	}

	out := make([]QuestionRow, 0, len(questions))
	for _, q := range questions {
		row := QuestionRow{QuestionID: q.ID, Code: q.Code, Text: q.Text, Format: q.Body.Format, Type: q.Type, MaxScore: q.Marks}
		correct, scored := 0, 0
		var sum float64
		wrong := map[string]int{}
		upRight, upN, loRight, loN := 0, 0, 0, 0
		if q.Type == quiz.Single || q.Type == quiz.Multi {
			row.OptionCounts, row.OptionLabels = map[string]int{}, map[string]string{}
			for _, o := range q.Body.Options {
				row.OptionCounts[o.ID] = 0
				row.OptionLabels[o.ID] = o.Text
			}
		}
		for _, r := range ranked {
			for _, m := range r.Marks {
				if m.QuestionID != q.ID || !eval.Answered(m.Response) {
					continue
				}
				row.Answered++
				right := m.Correct != nil && *m.Correct
				if m.Score != nil {
					scored++
					sum += *m.Score
				}
				if right {
					correct++
				}
				if upper[r.AttemptID] {
					upN++
					if right {
						upRight++
					}
				}
				if lower[r.AttemptID] {
					loN++
					if right {
						loRight++
					}
				}
				if row.OptionCounts != nil {
					for _, id := range m.Response.Selected {
						row.OptionCounts[id]++
					}
				}
				if !right {
					if w := wrongLabel(q, m.Response); w != "" {
						wrong[w]++
					}
				}
			}
		}
		if row.Answered > 0 {
			row.PctCorrect = round(float64(correct) / float64(row.Answered) * 100)
		}
		if scored > 0 {
			row.AvgScore = round(sum / float64(scored))
		}
		if upN > 0 && loN > 0 {
			d := round(float64(upRight)/float64(upN) - float64(loRight)/float64(loN))
			row.Discrimination = &d
		}
		for a, n := range wrong {
			row.CommonWrong = append(row.CommonWrong, WrongAnswer{a, n})
		}
		sort.Slice(row.CommonWrong, func(i, j int) bool {
			if row.CommonWrong[i].Count != row.CommonWrong[j].Count {
				return row.CommonWrong[i].Count > row.CommonWrong[j].Count
			}
			return row.CommonWrong[i].Answer < row.CommonWrong[j].Answer
		})
		if len(row.CommonWrong) > 5 {
			row.CommonWrong = row.CommonWrong[:5]
		}
		out = append(out, row)
	}
	return out
}

// wrongLabel turns a wrong response into a short, comparable label.
func wrongLabel(q quiz.Question, r *quiz.Response) string {
	names := map[string]string{}
	for _, c := range q.Body.Options {
		names[c.ID] = c.Text
	}
	switch q.Type {
	case quiz.Single, quiz.Multi:
		var parts []string
		for _, id := range r.Selected {
			parts = append(parts, names[id])
		}
		sort.Strings(parts)
		return strings.Join(parts, ", ")
	case quiz.BlankText, quiz.BlankOpt:
		var parts []string
		for _, b := range q.Body.Blanks {
			v := strings.ToLower(strings.Join(strings.Fields(r.Blanks[b]), " "))
			if n, ok := names[r.Blanks[b]]; ok {
				v = n
			}
			parts = append(parts, v)
		}
		return strings.Join(parts, " | ")
	}
	return "" // essays, matching and drag answers are too varied to group
}
