package poll

import (
	"strings"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func q(t Type, text string, b Body) Question {
	x := Question{ID: "q", Type: t, Text: text, Body: b}
	x.Normalise()
	return x
}

func opts(texts ...string) []Choice {
	cs := make([]Choice, len(texts))
	for i, t := range texts {
		cs[i] = Choice{Text: t}
	}
	return cs
}

// One valid question per type, with an accepted and a rejected answer each.
func cases() []struct {
	q       Question
	ok, bad Answer
} {
	return []struct {
		q       Question
		ok, bad Answer
	}{
		{q(Single, "Pick", Body{Options: opts("a", "b")}), Answer{Selected: []string{"o1"}}, Answer{Selected: []string{"o1", "o2"}}},
		{q(Multi, "Pick", Body{Options: opts("a", "b", "c"), MaxChoices: 2}), Answer{Selected: []string{"o1", "o3"}}, Answer{Selected: []string{"o1", "o2", "o3"}}},
		{q(Match, "Match", Body{Left: opts("x", "y"), Right: opts("1", "2")}), Answer{Pairs: map[string]string{"l1": "r2"}}, Answer{Pairs: map[string]string{"l1": "r9"}}},
		{q(BlankOpt, "The [[1]] sky", Body{Options: opts("blue", "red")}), Answer{Blanks: map[string]string{"1": "o1"}}, Answer{Blanks: map[string]string{"1": "o7"}}},
		{q(BlankText, "Water is [[1]]", Body{}), Answer{Blanks: map[string]string{"1": "wet"}}, Answer{Blanks: map[string]string{"2": "x"}}},
		{q(Drag, "Rank", Body{Options: opts("a", "b", "c")}), Answer{Order: []string{"o3", "o1", "o2"}}, Answer{Order: []string{"o1", "o1", "o2"}}},
		{q(Drag, "Sort", Body{Options: opts("a", "b"), Zones: opts("X", "Y")}), Answer{Pairs: map[string]string{"o1": "z2"}}, Answer{Pairs: map[string]string{"o1": "z3"}}},
		{q(Essay, "Why", Body{MaxWords: 3}), Answer{Text: ptr("one two three")}, Answer{Text: ptr("one two three four")}},
		{q(ShortText, "Name", Body{MaxLength: 5}), Answer{Text: ptr("hello")}, Answer{Text: ptr("hello world")}},
		{q(WordCloud, "Words", Body{MaxEntries: 2}), Answer{Words: []string{"Fun!", "fast"}}, Answer{Words: []string{"a", "b", "c"}}},
		{q(Number, "Age", Body{Min: ptr(0.0), Max: ptr(120.0), Step: ptr(1.0)}), Answer{Number: ptr(17.0)}, Answer{Number: ptr(17.5)}},
		{q(Date, "When", Body{From: "2026-01-01", To: "2026-12-31"}), Answer{Date: "2026-10-05"}, Answer{Date: "2027-01-01"}},
		{q(Time, "When", Body{From: "08:00", To: "17:00"}), Answer{Time: "09:30"}, Answer{Time: "25:00"}},
		{q(Rating, "Rate", Body{Points: 5}), Answer{Number: ptr(4.0)}, Answer{Number: ptr(6.0)}},
		{q(Slider, "How much", Body{Min: ptr(0.0), Max: ptr(10.0), Step: ptr(0.5)}), Answer{Number: ptr(7.5)}, Answer{Number: ptr(7.3)}},
		{q(Likert, "Agree?", Body{Rows: opts("Fun", "Useful")}), Answer{Rows: map[string]string{"row1": "5", "row2": "1"}}, Answer{Rows: map[string]string{"row1": "6"}}},
		{q(Matrix, "Grid", Body{Rows: opts("A", "B"), Columns: opts("Yes", "No")}), Answer{Rows: map[string]string{"row1": "col2"}}, Answer{Rows: map[string]string{"row9": "col1"}}},
		{q(Matrix, "Grid", Body{Rows: opts("A"), Columns: opts("x", "y"), Mode: "multi"}), Answer{Multi: map[string][]string{"row1": {"col1", "col2"}}}, Answer{Multi: map[string][]string{"row1": {"col1", "col1"}}}},
		{q(Matrix, "Grid", Body{Rows: opts("A"), Columns: opts("x", "y"), Mode: "text"}), Answer{Cells: map[string]string{"row1|col2": "hi"}}, Answer{Cells: map[string]string{"row1col2": "hi"}}},
		{q(Code, "Write a loop", Body{Language: "Go"}), Answer{Text: ptr("for {}\n\tbreak")}, Answer{Text: ptr(strings.Repeat("x", 10001))}},
	}
}

func TestEveryTypeValidatesQuestionsAndAnswers(t *testing.T) {
	seen := map[Type]bool{}
	for _, c := range cases() {
		seen[c.q.Type] = true
		if f := c.q.Validate(); len(f) > 0 {
			t.Fatalf("%s: valid question rejected: %v", c.q.Type, f)
		}
		ok := c.ok
		if empty, err := c.q.CheckAnswer(&ok); err != nil || empty {
			t.Errorf("%s: good answer rejected (empty=%v): %v", c.q.Type, empty, err)
		}
		bad := c.bad
		if _, err := c.q.CheckAnswer(&bad); err == nil {
			t.Errorf("%s: bad answer accepted: %+v", c.q.Type, c.bad)
		}
		if empty, err := c.q.CheckAnswer(&Answer{}); !empty || err != nil {
			t.Errorf("%s: an empty answer should clear (empty=%v, err=%v)", c.q.Type, empty, err)
		}
	}
	for _, typ := range AllTypes {
		if !seen[typ] && !typ.IsMedia() {
			t.Errorf("no test case for %s", typ)
		}
	}
}

func TestAnswersKeepOnlyTheirFields(t *testing.T) {
	x := q(Single, "Pick", Body{Options: opts("a", "b")})
	a := Answer{Selected: []string{"o2"}, Text: ptr("sneaky"), Words: []string{"x"}}
	if _, err := x.CheckAnswer(&a); err != nil {
		t.Fatal(err)
	}
	if a.Text != nil || a.Words != nil {
		t.Fatalf("extra fields kept: %+v", a)
	}
	wc := q(WordCloud, "Words", Body{MaxEntries: 3})
	w := Answer{Words: []string{"  Hello,  World! ", "hello world", "?"}}
	if _, err := wc.CheckAnswer(&w); err != nil {
		t.Fatal(err)
	}
	if strings.Join(w.Words, "|") != "hello world" {
		t.Fatalf("words not normalised and de-duplicated: %q", w.Words)
	}
	// Media answers are set only by the upload endpoint.
	f := q(File, "Upload", Body{})
	if _, err := f.CheckAnswer(&Answer{File: &FileRef{ID: "x"}}); err == nil {
		t.Fatal("a JSON file reference was accepted")
	}
}

func TestQuestionValidation(t *testing.T) {
	for name, c := range map[string]Question{
		"unknown type":       q("POLL", "x", Body{}),
		"one option":         q(Single, "x", Body{Options: opts("a")}),
		"blank without mark": q(BlankText, "no blanks", Body{}),
		"likert 4 points":    q(Likert, "x", Body{Rows: opts("a"), Scale: []string{"1", "2", "3", "4"}}),
		"rating 11":          q(Rating, "x", Body{Points: 11}),
		"slider min>=max":    q(Slider, "x", Body{Min: ptr(5.0), Max: ptr(5.0)}),
		"bad date":           q(Date, "x", Body{From: "05/10/2026"}),
		"matrix mode":        q(Matrix, "x", Body{Rows: opts("a"), Columns: opts("b", "c"), Mode: "stars"}),
		"file 6 MB":          q(File, "x", Body{MaxMB: 6}),
		"video 60 s":         q(Video, "x", Body{MaxSeconds: 60}),
		"accept exe":         q(File, "x", Body{Accept: []string{"exe"}}),
		"empty text":         q(ShortText, "  ", Body{}),
	} {
		if len(c.Validate()) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
	// Defaults fill in what a teacher leaves out.
	l := q(Likert, "x", Body{Rows: opts("a")})
	if len(l.Body.Scale) != 5 || l.Body.Scale[4] != "Strongly agree" {
		t.Fatalf("default Likert scale: %v", l.Body.Scale)
	}
	if s := q(Slider, "x", Body{}); *s.Body.Min != 0 || *s.Body.Max != 100 {
		t.Fatal("slider defaults")
	}
}

func entries(as ...Answer) []Entry {
	out := make([]Entry, len(as))
	for i, a := range as {
		out[i] = Entry{ParticipantID: string(rune('a' + i)), Value: a, At: time.Unix(int64(i), 0)}
	}
	return out
}

func TestAggregate(t *testing.T) {
	single := q(Single, "Pick", Body{Options: opts("a", "b")})
	r := Aggregate(single, entries(Answer{Selected: []string{"o1"}}, Answer{Selected: []string{"o1"}}, Answer{Selected: []string{"o2"}}), nil, false)
	if r.Responses != 3 || r.Counts["o1"] != 2 || r.Counts["o2"] != 1 {
		t.Fatalf("single: %+v", r)
	}

	wc := q(WordCloud, "Words", Body{})
	es := entries(Answer{Words: []string{"fun", "fast"}}, Answer{Words: []string{"fun"}}, Answer{Words: []string{"rude"}})
	es[2].Hidden = true
	pub := Aggregate(wc, es, map[string]bool{"fast": true}, false)
	if pub.Responses != 2 || len(pub.Words) != 1 || pub.Words[0] != (WordCount{Word: "fun", Count: 2}) {
		t.Fatalf("public cloud must drop hidden answers and words: %+v", pub.Words)
	}
	tv := Aggregate(wc, es, map[string]bool{"fast": true}, true)
	if len(tv.Words) != 2 || !tv.Words[1].Hidden {
		t.Fatalf("teacher cloud keeps hidden words flagged: %+v", tv.Words)
	}

	essay := q(Essay, "Why", Body{})
	es = entries(Answer{Text: ptr("first")}, Answer{Text: ptr("second")})
	es[0].Hidden, es[1].Name = true, "Amaya"
	pub = Aggregate(essay, es, nil, false)
	if len(pub.Texts) != 1 || pub.Texts[0].Text != "second" || pub.Texts[0].Name != "" || pub.Texts[0].ParticipantID != "" {
		t.Fatalf("public texts: %+v", pub.Texts)
	}
	tv = Aggregate(essay, es, nil, true)
	if len(tv.Texts) != 2 || tv.Texts[0].Name != "Amaya" || !tv.Texts[1].Hidden {
		t.Fatalf("teacher texts, newest first with names: %+v", tv.Texts)
	}

	rating := q(Rating, "Rate", Body{Points: 5})
	r = Aggregate(rating, entries(Answer{Number: ptr(5.0)}, Answer{Number: ptr(4.0)}, Answer{Number: ptr(5.0)}), nil, false)
	if r.Stats.Mean != 4.67 || r.Stats.Median != 5 || len(r.Stats.Buckets) != 5 || r.Stats.Buckets[4].Count != 2 {
		t.Fatalf("rating stats: %+v", r.Stats)
	}
	num := q(Number, "Guess", Body{})
	r = Aggregate(num, entries(Answer{Number: ptr(1.0)}, Answer{Number: ptr(1000.0)}, Answer{Number: ptr(500.5)}), nil, false)
	total := 0
	for _, b := range r.Stats.Buckets {
		total += b.Count
	}
	if len(r.Stats.Buckets) != 10 || total != 3 {
		t.Fatalf("number buckets: %+v", r.Stats.Buckets)
	}

	rank := q(Drag, "Rank", Body{Options: opts("a", "b", "c")})
	r = Aggregate(rank, entries(Answer{Order: []string{"o2", "o1", "o3"}}, Answer{Order: []string{"o2", "o3", "o1"}}), nil, false)
	if r.Ranks[0].ID != "o2" || r.Ranks[0].AvgRank != 1 || r.Ranks[0].First != 2 {
		t.Fatalf("ranking: %+v", r.Ranks)
	}

	lik := q(Likert, "Agree?", Body{Rows: opts("Fun")})
	r = Aggregate(lik, entries(Answer{Rows: map[string]string{"row1": "5"}}, Answer{Rows: map[string]string{"row1": "4"}}), nil, false)
	if r.Grid["row1"]["5"] != 1 || r.RowMeans["row1"] != 4.5 {
		t.Fatalf("likert: %+v", r)
	}

	date := q(Date, "When", Body{})
	r = Aggregate(date, entries(Answer{Date: "2026-10-06"}, Answer{Date: "2026-10-05"}, Answer{Date: "2026-10-06"}), nil, false)
	if len(r.Values) != 2 || r.Values[0].Value != "2026-10-05" || r.Values[1].Count != 2 {
		t.Fatalf("dates in order: %+v", r.Values)
	}

	file := q(Audio, "Say hi", Body{})
	es = entries(Answer{File: &FileRef{ID: "f1", Name: "a.webm"}})
	if Aggregate(file, es, nil, false).Files != nil {
		t.Fatal("participants must never see the file list")
	}
	if len(Aggregate(file, es, nil, true).Files) != 1 {
		t.Fatal("teacher sees files")
	}
}

func TestAllowedFile(t *testing.T) {
	img := q(File, "x", Body{Accept: []string{"image"}})
	if _, ok := allowedFile(img, "a.png", "image/png", "image/png"); !ok {
		t.Fatal("png rejected")
	}
	if _, ok := allowedFile(img, "a.png", "text/html; charset=utf-8", "image/png"); ok {
		t.Fatal("HTML disguised as PNG accepted")
	}
	doc := q(File, "x", Body{Accept: []string{"document"}})
	if ct, ok := allowedFile(doc, "Report.DOCX", "application/zip", ""); !ok || !strings.Contains(ct, "officedocument") {
		t.Fatalf("docx: %q %v", ct, ok)
	}
	if _, ok := allowedFile(doc, "evil.zip", "application/zip", ""); ok {
		t.Fatal("plain zip accepted as a document")
	}
	audio := q(Audio, "x", Body{})
	if ct, ok := allowedFile(audio, "r.webm", "video/webm", "audio/webm;codecs=opus"); !ok || ct != "audio/webm" {
		t.Fatalf("recorded audio: %q %v", ct, ok)
	}
	video := q(Video, "x", Body{})
	if _, ok := allowedFile(video, "x.mp4", "audio/mpeg", "video/mp4"); ok {
		t.Fatal("mp3 accepted as video")
	}
	if cleanName(`..\..\secret".txt`) != "secret.txt" {
		t.Fatalf("clean name: %q", cleanName(`..\..\secret".txt`))
	}
}

func TestSafeCSV(t *testing.T) {
	for in, want := range map[string]string{"=SUM(A1)": "'=SUM(A1)", "+1": "'+1", "@x": "'@x", "fine": "fine", "": ""} {
		if got := safeCSV(in); got != want {
			t.Errorf("safeCSV(%q) = %q", in, got)
		}
	}
}
