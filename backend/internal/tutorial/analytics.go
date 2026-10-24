package tutorial

import (
	"context"
	"math"
	"slices"
)

// Analytics for the teacher (D-75, TU-FR-40 to TU-FR-42): per page, how many
// students reached it, how long they stayed, where they stopped and how its
// questions went; and for reading pages, whether time spent reading went with
// better scores on the questions that follow.

// QuestionStats is how one question went.
type QuestionStats struct {
	ID       string   `json:"id"`
	Code     string   `json:"code"`
	Text     string   `json:"text"`
	Type     string   `json:"type"`
	Marks    float64  `json:"marks"`
	Answered int      `json:"answered"`
	Correct  int      `json:"correct"`
	Pending  int      `json:"pending"`
	ScorePct *float64 `json:"score_pct,omitempty"` // of marked answers
	AvgTries float64  `json:"avg_tries"`
}

// Split compares the students who spent longer on a reading page than the
// median with those who spent less, by their score on the questions after it.
type Split struct {
	MedianSeconds int      `json:"median_seconds"`
	LongerN       int      `json:"longer_n"`
	Longer        *float64 `json:"longer_pct,omitempty"`
	ShorterN      int      `json:"shorter_n"`
	Shorter       *float64 `json:"shorter_pct,omitempty"`
	Questions     int      `json:"questions"` // how many questions follow
}

// PageStats is one page's figures.
type PageStats struct {
	Index         int             `json:"index"`
	Title         string          `json:"title"`
	Reading       bool            `json:"reading"` // content and no questions
	Checkpoint    string          `json:"checkpoint"`
	Leaving       string          `json:"leaving"`
	Reached       int             `json:"reached"`
	StoppedHere   int             `json:"stopped_here"` // not finished, and on this page
	MedianSeconds *int            `json:"median_seconds,omitempty"`
	Leaves        int             `json:"leaves"`
	LeftStudents  int             `json:"left_students"`
	Locked        int             `json:"locked"`
	OutOfTries    int             `json:"out_of_tries"` // used every try without getting it right
	Questions     []QuestionStats `json:"questions"`
	Then          *Split          `json:"then,omitempty"`
}

// TimePoint is one student's time against their score.
type TimePoint struct {
	RunID          string   `json:"run_id"`
	Seconds        int      `json:"seconds"`
	ReadingSeconds int      `json:"reading_seconds"`
	ScorePct       *float64 `json:"score_pct,omitempty"`
	Finished       bool     `json:"finished"`
}

// Analytics is the whole tutorial's.
type Analytics struct {
	Started  int         `json:"started"`
	Finished int         `json:"finished"`
	Pages    []PageStats `json:"pages"`
	Students []TimePoint `json:"students"`
}

func median(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	if n := len(s); n%2 == 1 {
		return s[n/2]
	} else {
		return (s[n/2-1] + s[n/2]) / 2
	}
}

func pct(score, max float64) *float64 {
	if max <= 0 {
		return nil
	}
	v := math.Round(score/max*1000) / 10
	return &v
}

type statAnswer struct {
	score, max float64
	correct    bool
	pending    bool
	tries      int
}

// Analytics works out a tutorial's figures from its runs.
func (s *Service) Analytics(ctx context.Context, teacherID, id string) (Analytics, error) {
	t, err := s.owned(ctx, teacherID, id)
	if err != nil {
		return Analytics{}, err
	}
	pages, err := s.pages(ctx, id)
	if err != nil {
		return Analytics{}, err
	}
	qs, _, err := s.questions.TutorialQuestions(ctx, t.QuizID)
	if err != nil {
		return Analytics{}, err
	}
	type runRow struct {
		id       string
		current  int
		finished bool
	}
	var runs []runRow
	rows, err := s.pool.Query(ctx, `SELECT id, current, finished_at IS NOT NULL FROM tutorial.runs WHERE tutorial_id=$1`, id)
	if err != nil {
		return Analytics{}, err
	}
	for rows.Next() {
		var r runRow
		if err := rows.Scan(&r.id, &r.current, &r.finished); err != nil {
			rows.Close()
			return Analytics{}, err
		}
		runs = append(runs, r)
	}
	rows.Close()
	visits := map[string]map[string]visit{} // page → run → visit
	rows, err = s.pool.Query(ctx, `SELECT v.page_id, v.run_id, v.seconds, v.leaves, v.locked_at FROM tutorial.visits v
		JOIN tutorial.runs r ON r.id = v.run_id WHERE r.tutorial_id=$1`, id)
	if err != nil {
		return Analytics{}, err
	}
	for rows.Next() {
		var pid, rid string
		var v visit
		if err := rows.Scan(&pid, &rid, &v.Seconds, &v.Leaves, &v.LockedAt); err != nil {
			rows.Close()
			return Analytics{}, err
		}
		if visits[pid] == nil {
			visits[pid] = map[string]visit{}
		}
		visits[pid][rid] = v
	}
	rows.Close()
	answers := map[string]map[string]statAnswer{} // question → run → answer
	rows, err = s.pool.Query(ctx, `SELECT a.question_id, a.run_id, a.score, a.max_score, a.correct, a.status, a.tries FROM tutorial.answers a
		JOIN tutorial.runs r ON r.id = a.run_id WHERE r.tutorial_id=$1`, id)
	if err != nil {
		return Analytics{}, err
	}
	for rows.Next() {
		var qid, rid, status string
		var a statAnswer
		if err := rows.Scan(&qid, &rid, &a.score, &a.max, &a.correct, &status, &a.tries); err != nil {
			rows.Close()
			return Analytics{}, err
		}
		a.pending = status == "pending"
		if answers[qid] == nil {
			answers[qid] = map[string]statAnswer{}
		}
		answers[qid][rid] = a
	}
	rows.Close()

	byID := map[string]int{}
	for i, q := range qs {
		byID[q.ID] = i
	}
	placed := func(p Page) []string {
		out := []string{}
		for _, qid := range p.QuestionIDs {
			if _, ok := byID[qid]; ok {
				out = append(out, qid)
			}
		}
		return out
	}
	// scoreOn is a run's score on some questions, over the ones marked.
	scoreOn := func(runID string, qids []string) *float64 {
		var sc, mx float64
		for _, qid := range qids {
			if a, ok := answers[qid][runID]; ok && !a.pending {
				sc += a.score
				mx += a.max
			}
		}
		return pct(sc, mx)
	}

	out := Analytics{Pages: []PageStats{}, Students: []TimePoint{}}
	reading := make([]bool, len(pages))
	for i, p := range pages {
		reading[i] = len(placed(p)) == 0 && len(p.Blocks) > 0
	}
	for i, p := range pages {
		ps := PageStats{Index: i, Title: p.Title, Reading: reading[i], Checkpoint: p.Checkpoint, Leaving: p.Leaving, Questions: []QuestionStats{}}
		var secs []int
		for _, v := range visits[p.ID] {
			ps.Reached++
			if v.Seconds > 0 {
				secs = append(secs, v.Seconds)
			}
			ps.Leaves += v.Leaves
			if v.Leaves > 0 {
				ps.LeftStudents++
			}
			if v.LockedAt != nil {
				ps.Locked++
			}
		}
		if len(secs) > 0 {
			m := median(secs)
			ps.MedianSeconds = &m
		}
		for _, r := range runs {
			if !r.finished && r.current == i {
				ps.StoppedHere++
			}
		}
		outOfTries := map[string]bool{}
		for _, qid := range placed(p) {
			q := qs[byID[qid]]
			st := QuestionStats{ID: q.ID, Code: q.Code, Text: q.Text, Type: string(q.Type), Marks: q.Marks}
			var sc, mx float64
			tries := 0
			for rid, a := range answers[qid] {
				st.Answered++
				tries += a.tries
				if a.pending {
					st.Pending++
					continue
				}
				sc += a.score
				mx += a.max
				if a.correct {
					st.Correct++
				} else if p.Checkpoint == CheckCorrect && p.MaxTries > 0 && a.tries >= p.MaxTries {
					outOfTries[rid] = true
				}
			}
			st.ScorePct = pct(sc, mx)
			if st.Answered > 0 {
				st.AvgTries = math.Round(float64(tries)/float64(st.Answered)*10) / 10
			}
			ps.Questions = append(ps.Questions, st)
		}
		ps.OutOfTries = len(outOfTries)
		// A reading page: the questions after it, up to the next reading page.
		if reading[i] {
			var then []string
			for j := i + 1; j < len(pages) && !reading[j]; j++ {
				then = append(then, placed(pages[j])...)
			}
			if len(then) > 0 {
				sp := &Split{Questions: len(then)}
				type pt struct {
					secs  int
					score float64
				}
				var pts []pt
				for rid, v := range visits[p.ID] {
					if sc := scoreOn(rid, then); sc != nil && v.Seconds > 0 {
						pts = append(pts, pt{v.Seconds, *sc})
					}
				}
				all := make([]int, len(pts))
				for k, x := range pts {
					all[k] = x.secs
				}
				sp.MedianSeconds = median(all)
				var longer, shorter float64
				for _, x := range pts {
					if x.secs >= sp.MedianSeconds {
						sp.LongerN++
						longer += x.score
					} else {
						sp.ShorterN++
						shorter += x.score
					}
				}
				if sp.LongerN > 0 {
					v := math.Round(longer/float64(sp.LongerN)*10) / 10
					sp.Longer = &v
				}
				if sp.ShorterN > 0 {
					v := math.Round(shorter/float64(sp.ShorterN)*10) / 10
					sp.Shorter = &v
				}
				ps.Then = sp
			}
		}
		out.Pages = append(out.Pages, ps)
	}
	var every []string
	for _, p := range pages {
		every = append(every, placed(p)...)
	}
	for _, r := range runs {
		out.Started++
		if r.finished {
			out.Finished++
		}
		tp := TimePoint{RunID: r.id, Finished: r.finished, ScorePct: scoreOn(r.id, every)}
		for i, p := range pages {
			v := visits[p.ID][r.id]
			tp.Seconds += v.Seconds
			if reading[i] {
				tp.ReadingSeconds += v.Seconds
			}
		}
		out.Students = append(out.Students, tp)
	}
	return out, nil
}
