package poll

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Entry is one participant's saved answer to a question.
type Entry struct {
	ParticipantID string
	Name          string // identified participants only; never sent to participants
	Value         Answer
	Hidden        bool
	At            time.Time
}

type WordCount struct {
	Word   string `json:"word"`
	Count  int    `json:"count"`
	Hidden bool   `json:"hidden,omitempty"` // teacher view: removed from the shared cloud
}

type TextItem struct {
	ParticipantID string `json:"participant_id,omitempty"` // teacher view only
	Name          string `json:"name,omitempty"`           // teacher view, identified participants
	Cell          string `json:"cell,omitempty"`           // MATRIX text: "row|column"
	Text          string `json:"text"`
	Hidden        bool   `json:"hidden,omitempty"`
	At            int64  `json:"at"`
}

type Bucket struct {
	Label string  `json:"label"`
	From  float64 `json:"from"`
	To    float64 `json:"to"`
	Count int     `json:"count"`
}

type Stats struct {
	Count   int      `json:"count"`
	Mean    float64  `json:"mean"`
	Median  float64  `json:"median"`
	Min     float64  `json:"min"`
	Max     float64  `json:"max"`
	Buckets []Bucket `json:"buckets"`
}

type ValueCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type RankItem struct {
	ID      string  `json:"id"`
	AvgRank float64 `json:"avg_rank"` // 1 = top
	First   int     `json:"first"`    // times ranked first
}

type FileItem struct {
	ParticipantID string  `json:"participant_id"`
	Name          string  `json:"name,omitempty"`
	File          FileRef `json:"file"`
	Hidden        bool    `json:"hidden,omitempty"`
	At            int64   `json:"at"`
}

// Result is the live summary of one question.
type Result struct {
	QuestionID string                    `json:"question_id"`
	Type       Type                      `json:"type"`
	Responses  int                       `json:"responses"`
	Counts     map[string]int            `json:"counts,omitempty"`      // SINGLE, MULTI: option → people
	Grid       map[string]map[string]int `json:"grid,omitempty"`        // MATCH, BLANK_OPT, DRAG boxes, LIKERT, MATRIX
	RowMeans   map[string]float64        `json:"row_means,omitempty"`   // LIKERT: mean point per statement
	Ranks      []RankItem                `json:"ranks,omitempty"`       // DRAG ranking, best first
	Words      []WordCount               `json:"words,omitempty"`       // WORD_CLOUD, SHORT_TEXT
	BlankWords map[string][]WordCount    `json:"blank_words,omitempty"` // BLANK_TEXT: per blank
	Texts      []TextItem                `json:"texts,omitempty"`       // ESSAY, SHORT_TEXT, CODE, MATRIX text; newest first
	Stats      *Stats                    `json:"stats,omitempty"`       // NUMBER, SLIDER, RATING
	Values     []ValueCount              `json:"values,omitempty"`      // DATE, TIME: in order
	Files      []FileItem                `json:"files,omitempty"`       // teacher view only
}

const (
	maxTexts = 300
	maxWords = 150
)

func bump(m map[string]map[string]int, a, b string) {
	if m[a] == nil {
		m[a] = map[string]int{}
	}
	m[a][b]++
}

func wordList(counts map[string]int, hidden map[string]bool, teacher bool) []WordCount {
	out := make([]WordCount, 0, len(counts))
	for w, n := range counts {
		h := hidden[w]
		if h && !teacher {
			continue
		}
		out = append(out, WordCount{Word: w, Count: n, Hidden: h})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Word < out[j].Word
	})
	if len(out) > maxWords {
		out = out[:maxWords]
	}
	return out
}

// Aggregate summarises entries for q. The teacher view keeps moderated items
// (flagged), names and files; the shared view drops all of them. Hidden
// answers never count in either.
func Aggregate(q Question, entries []Entry, hiddenWords map[string]bool, teacher bool) Result {
	r := Result{QuestionID: q.ID, Type: q.Type}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].At.After(entries[j].At) })
	words := map[string]int{}
	blankWords := map[string]map[string]int{}
	var nums []float64
	values := map[string]int{}
	rankSum := map[string]float64{}
	first := map[string]int{}
	ranked := 0
	addText := func(e Entry, cell, text string) {
		if e.Hidden && !teacher {
			return
		}
		if len(r.Texts) >= maxTexts {
			return
		}
		t := TextItem{Cell: cell, Text: text, At: e.At.UnixMilli()}
		if teacher {
			t.ParticipantID, t.Name, t.Hidden = e.ParticipantID, e.Name, e.Hidden
		}
		r.Texts = append(r.Texts, t)
	}

	for _, e := range entries {
		v := e.Value
		if q.Type.IsMedia() {
			if v.File == nil || (e.Hidden && !teacher) {
				continue
			}
			if !e.Hidden {
				r.Responses++
			}
			if teacher {
				r.Files = append(r.Files, FileItem{ParticipantID: e.ParticipantID, Name: e.Name, File: *v.File, Hidden: e.Hidden, At: e.At.UnixMilli()})
			}
			continue
		}
		// Free text stays visible to the teacher when hidden; nothing else counts.
		switch q.Type {
		case Essay, Code:
			if v.Text != nil {
				addText(e, "", *v.Text)
			}
		case ShortText:
			if v.Text != nil {
				addText(e, "", *v.Text)
			}
		case Matrix:
			if q.Body.Mode == "text" {
				keys := make([]string, 0, len(v.Cells))
				for k := range v.Cells {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					addText(e, k, v.Cells[k])
				}
			}
		}
		if e.Hidden {
			continue
		}
		r.Responses++
		switch q.Type {
		case Single, Multi:
			if r.Counts == nil {
				r.Counts = map[string]int{}
			}
			for _, id := range v.Selected {
				r.Counts[id]++
			}
		case Match, BlankOpt:
			if r.Grid == nil {
				r.Grid = map[string]map[string]int{}
			}
			m := v.Pairs
			if q.Type == BlankOpt {
				m = v.Blanks
			}
			for a, b := range m {
				bump(r.Grid, a, b)
			}
		case BlankText:
			for bl, t := range v.Blanks {
				if w := NormaliseWord(t); w != "" {
					if blankWords[bl] == nil {
						blankWords[bl] = map[string]int{}
					}
					blankWords[bl][w]++
				}
			}
		case Drag:
			if len(q.Body.Zones) == 0 {
				if len(v.Order) > 0 {
					ranked++
					for i, id := range v.Order {
						rankSum[id] += float64(i + 1)
					}
					first[v.Order[0]]++
				}
			} else {
				if r.Grid == nil {
					r.Grid = map[string]map[string]int{}
				}
				for item, zone := range v.Pairs {
					bump(r.Grid, item, zone)
				}
			}
		case ShortText:
			if v.Text != nil {
				if w := NormaliseWord(*v.Text); w != "" {
					words[w]++
				}
			}
		case WordCloud:
			for _, w := range v.Words {
				words[w]++
			}
		case Number, Slider, Rating:
			if v.Number != nil {
				nums = append(nums, *v.Number)
			}
		case Date:
			values[v.Date]++
		case Time:
			values[v.Time]++
		case Likert:
			if r.Grid == nil {
				r.Grid = map[string]map[string]int{}
			}
			for row, p := range v.Rows {
				bump(r.Grid, row, p)
			}
		case Matrix:
			if r.Grid == nil && q.Body.Mode != "text" {
				r.Grid = map[string]map[string]int{}
			}
			switch q.Body.Mode {
			case "single":
				for row, col := range v.Rows {
					bump(r.Grid, row, col)
				}
			case "multi":
				for row, cols := range v.Multi {
					for _, col := range cols {
						bump(r.Grid, row, col)
					}
				}
			}
		}
	}

	switch q.Type {
	case ShortText, WordCloud:
		r.Words = wordList(words, hiddenWords, teacher)
	case BlankText:
		r.BlankWords = map[string][]WordCount{}
		for bl, m := range blankWords {
			r.BlankWords[bl] = wordList(m, hiddenWords, teacher)
		}
	case Drag:
		if len(q.Body.Zones) == 0 && ranked > 0 {
			for _, o := range q.Body.Options {
				r.Ranks = append(r.Ranks, RankItem{ID: o.ID, AvgRank: math.Round(rankSum[o.ID]/float64(ranked)*100) / 100, First: first[o.ID]})
			}
			sort.SliceStable(r.Ranks, func(i, j int) bool { return r.Ranks[i].AvgRank < r.Ranks[j].AvgRank })
		}
	case Number, Slider, Rating:
		r.Stats = stats(q, nums)
	case Date, Time:
		for v, n := range values {
			r.Values = append(r.Values, ValueCount{Value: v, Count: n})
		}
		sort.Slice(r.Values, func(i, j int) bool { return r.Values[i].Value < r.Values[j].Value })
	case Likert:
		r.RowMeans = map[string]float64{}
		for row, pts := range r.Grid {
			sum, n := 0, 0
			for p, c := range pts {
				v, _ := strconv.Atoi(p)
				sum += v * c
				n += c
			}
			if n > 0 {
				r.RowMeans[row] = math.Round(float64(sum)/float64(n)*100) / 100
			}
		}
	}
	return r
}

func label(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func stats(q Question, nums []float64) *Stats {
	s := &Stats{Count: len(nums), Buckets: []Bucket{}}
	b := q.Body
	if q.Type == Rating {
		for i := 1; i <= b.Points; i++ {
			s.Buckets = append(s.Buckets, Bucket{Label: strconv.Itoa(i), From: float64(i), To: float64(i)})
		}
	}
	if len(nums) == 0 {
		return s
	}
	sorted := append([]float64(nil), nums...)
	sort.Float64s(sorted)
	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	s.Mean = math.Round(sum/float64(len(sorted))*100) / 100
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		s.Median = sorted[mid]
	} else {
		s.Median = (sorted[mid-1] + sorted[mid]) / 2
	}
	s.Min, s.Max = sorted[0], sorted[len(sorted)-1]
	if q.Type == Rating {
		for _, v := range sorted {
			s.Buckets[int(v)-1].Count++
		}
		return s
	}
	lo, hi := s.Min, s.Max
	if b.Min != nil {
		lo = math.Min(lo, *b.Min)
	}
	if b.Max != nil {
		hi = math.Max(hi, *b.Max)
	}
	integers := true
	for _, v := range sorted {
		integers = integers && v == math.Trunc(v)
	}
	if integers && hi-lo <= 20 {
		// One bar per whole number when the range is small (e.g. 0-10 sliders).
		for v := lo; v <= hi; v++ {
			s.Buckets = append(s.Buckets, Bucket{Label: label(v), From: v, To: v})
		}
		for _, v := range sorted {
			s.Buckets[int(v-lo)].Count++
		}
		return s
	}
	n := 10
	if hi == lo {
		hi = lo + 1
	}
	w := (hi - lo) / float64(n)
	for i := 0; i < n; i++ {
		from, to := lo+float64(i)*w, lo+float64(i+1)*w
		s.Buckets = append(s.Buckets, Bucket{Label: fmt.Sprintf("%s–%s", short(from), short(to)), From: from, To: to})
	}
	for _, v := range sorted {
		i := int((v - lo) / w)
		if i >= n {
			i = n - 1
		}
		s.Buckets[i].Count++
	}
	return s
}

func short(v float64) string {
	if math.Abs(v) >= 100 || v == math.Trunc(v) {
		return strconv.FormatFloat(math.Round(v), 'f', -1, 64)
	}
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 1, 64), "0"), ".")
}
