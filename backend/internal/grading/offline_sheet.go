package grading

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/xlsx"
)

// The mark sheet a teacher downloads, fills in and uploads (D-159). Rows are
// matched by the Key column (ids the platform wrote), else by student ID or
// group name, else by the student's name when only one has it. An empty
// mark leaves that mark as it is.

// Sheet names and the columns that matter when reading.
const (
	sheetStudents = "Students"
	sheetGroups   = "Groups"

	colKey     = "key"
	colNumber  = "student id"
	colName    = "name"
	colGroup   = "group"
	colMarks   = "marks"
	colOwn     = "own mark"
	colComment = "comment"
)

// File is a download.
type File struct {
	Name        string
	ContentType string
	Data        []byte
}

func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func fileName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return -1
		}
		return r
	}, s)
	if r := []rune(strings.TrimSpace(s)); len(r) > 80 {
		s = string(r[:80])
	}
	if strings.TrimSpace(s) == "" {
		s = "marks"
	}
	return s
}

func num(f *float64) string {
	if f == nil {
		return ""
	}
	return strconv.FormatFloat(*f, 'f', -1, 64)
}

// tables are the sheets to download: students, and groups in a group assignment.
func (sh OfflineSheet) tables() (students, groups [][]string) {
	out := num(&sh.MaxMarks)
	if sh.Mode == ModeGroup {
		students = [][]string{{"No.", "Student ID", "Name", "Group", "Group's mark", "Own mark (out of " + out + "; empty: the group's)", "Comment", "Key"}}
		for i, st := range sh.Students {
			students = append(students, []string{strconv.Itoa(i + 1), deref(st.StudentNumber), st.Name, st.GroupName, num(st.GroupScore), num(st.Own), st.OwnComment, st.UserID})
		}
		groups = [][]string{{"No.", "Group", "Members", "Marks (out of " + out + ")", "Comment", "Key"}}
		for i, g := range sh.Groups {
			groups = append(groups, []string{strconv.Itoa(i + 1), g.Name, strings.Join(g.Members, ", "), num(g.Score), g.Comment, g.ID})
		}
		return students, groups
	}
	students = [][]string{{"No.", "Student ID", "Name", "Groups", "Marks (out of " + out + ")", "Comment", "Key"}}
	for i, st := range sh.Students {
		students = append(students, []string{strconv.Itoa(i + 1), deref(st.StudentNumber), st.Name, strings.Join(st.Groups, ", "), num(st.Own), st.OwnComment, st.UserID})
	}
	return students, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Download is the mark sheet: an Excel workbook (a Groups sheet first in a
// group assignment), or one table as CSV - which: "students" or "groups".
func (s *Service) Download(ctx context.Context, teacherID, id, format, which string) (File, error) {
	sh, err := s.Sheet(ctx, teacherID, id)
	if err != nil {
		return File{}, err
	}
	students, groups := sh.tables()
	base := fileName(sh.Classroom + " - " + sh.Title)
	if format == "csv" {
		table, suffix := students, ""
		if which == "groups" {
			if groups == nil {
				return File{}, httpx.Invalid(map[string]string{"sheet": "this assignment is marked student by student"})
			}
			table, suffix = groups, " (groups)"
		}
		var buf bytes.Buffer
		buf.WriteString("\xef\xbb\xbf") // so Excel reads Sinhala and Tamil names as UTF-8
		w := csv.NewWriter(&buf)
		for _, row := range table {
			rec := make([]string, len(row))
			for i, c := range row {
				rec[i] = safeCell(c)
			}
			if err := w.Write(rec); err != nil {
				return File{}, err
			}
		}
		w.Flush()
		return File{Name: base + suffix + ".csv", ContentType: "text/csv; charset=utf-8", Data: buf.Bytes()}, w.Error()
	}
	toSheet := func(name string, table [][]string, widths []float64, numeric map[int]bool) xlsx.Sheet {
		x := xlsx.Sheet{Name: name, Widths: widths}
		for r, row := range table {
			cells := make([]xlsx.Cell, len(row))
			for c, v := range row {
				if r > 0 && numeric[c] && v != "" {
					if f, err := strconv.ParseFloat(v, 64); err == nil {
						cells[c] = xlsx.Number(f)
						continue
					}
				}
				cells[c] = xlsx.Text(v)
			}
			x.Rows = append(x.Rows, cells)
		}
		return x
	}
	var sheets []xlsx.Sheet
	if groups != nil {
		sheets = append(sheets,
			toSheet(sheetGroups, groups, []float64{6, 24, 50, 20, 30, 38}, map[int]bool{0: true, 3: true}),
			toSheet(sheetStudents, students, []float64{6, 14, 28, 20, 14, 26, 30, 38}, map[int]bool{0: true, 4: true, 5: true}))
	} else {
		sheets = append(sheets, toSheet(sheetStudents, students, []float64{6, 14, 28, 24, 18, 30, 38}, map[int]bool{0: true, 4: true}))
	}
	data, err := xlsx.Write(sheets)
	if err != nil {
		return File{}, err
	}
	return File{Name: base + ".xlsx", ContentType: xlsx.ContentType, Data: data}, nil
}

// Upload is what a filled-in sheet would change, for the teacher to check
// before saving it as marks.
type Upload struct {
	Students []UploadMark  `json:"students"`
	Groups   []UploadGroup `json:"groups"`
	Problems []Problem     `json:"problems"`
	Same     int           `json:"same"` // rows that change nothing
}

// UploadMark is a student's new mark.
type UploadMark struct {
	UserID  string   `json:"user_id"`
	Name    string   `json:"name"`
	Score   float64  `json:"score"`
	Comment string   `json:"comment"`
	Was     *float64 `json:"was"`
}

// UploadGroup is a group's new mark.
type UploadGroup struct {
	GroupID string   `json:"group_id"`
	Name    string   `json:"name"`
	Score   float64  `json:"score"`
	Comment string   `json:"comment"`
	Was     *float64 `json:"was"`
}

// Problem is a row that can't be used.
type Problem struct {
	Sheet   string `json:"sheet"`
	Row     int    `json:"row"`
	Message string `json:"message"`
}

const maxUpload = 5 << 20

// ReadUpload reads a filled-in sheet: Excel, or CSV (comma or semicolon).
func (s *Service) ReadUpload(ctx context.Context, teacherID, id string, data []byte) (Upload, error) {
	sh, err := s.Sheet(ctx, teacherID, id)
	if err != nil {
		return Upload{}, err
	}
	var tables []namedTable
	if bytes.HasPrefix(data, []byte("PK")) {
		sheets, err := xlsx.Read(data)
		if err != nil {
			return Upload{}, httpx.Invalid(map[string]string{"file": err.Error()})
		}
		for _, x := range sheets {
			tables = append(tables, namedTable{x.Name, x.Rows})
		}
	} else {
		rows, err := readCSV(data)
		if err != nil {
			return Upload{}, httpx.Invalid(map[string]string{"file": err.Error()})
		}
		tables = []namedTable{{"CSV", rows}}
	}
	return sh.match(tables), nil
}

type namedTable struct {
	name string
	rows [][]string
}

func readCSV(data []byte) ([][]string, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("save the CSV as UTF-8, or upload the Excel file")
	}
	first, _, _ := bytes.Cut(data, []byte("\n"))
	r := csv.NewReader(bytes.NewReader(data))
	if bytes.Count(first, []byte(";")) > bytes.Count(first, []byte(",")) {
		r.Comma = ';' // Excel in many countries saves with semicolons
	}
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("this CSV can't be read: %v", err)
	}
	if len(rows) > 20000 {
		return nil, fmt.Errorf("this file has too many rows")
	}
	return rows, nil
}

// columns finds the columns by their headings.
func columns(head []string) map[string]int {
	at := map[string]int{}
	for i, h := range head {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "'")))
		for _, name := range []string{colKey, colNumber, colOwn, colMarks, colComment, colName, colGroup} {
			if _, ok := at[name]; ok {
				continue
			}
			if strings.HasPrefix(h, name) && !(name == colGroup && strings.HasPrefix(h, "group's")) {
				at[name] = i
				break
			}
		}
		if h == "members" {
			at["members"] = i
		}
	}
	return at
}

func cell(row []string, at map[string]int, name string) string {
	i, ok := at[name]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(row[i], "'"))
}

// parseMark reads a mark: empty is none; a comma may be the decimal point.
func parseMark(s string) (*float64, bool) {
	if s == "" {
		return nil, true
	}
	f, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
	if err != nil {
		return nil, false
	}
	f = round(f, 2) // Excel's 7.499999999 is 7.5
	return &f, true
}

func same(a *float64, b float64) bool { return a != nil && *a == b }

func (sh OfflineSheet) match(tables []namedTable) Upload {
	up := Upload{Students: []UploadMark{}, Groups: []UploadGroup{}, Problems: []Problem{}}
	problem := func(t string, row int, msg string) {
		if len(up.Problems) < 200 {
			up.Problems = append(up.Problems, Problem{Sheet: t, Row: row, Message: msg})
		}
	}
	// Ways to find a student or group.
	byKey, byNumber, byName := map[string]int{}, map[string]int{}, map[string][]int{}
	for i, st := range sh.Students {
		byKey[st.UserID] = i
		if n := strings.ToLower(deref(st.StudentNumber)); n != "" {
			byNumber[n] = i
		}
		byName[strings.ToLower(st.Name)] = append(byName[strings.ToLower(st.Name)], i)
	}
	groupKey, groupName := map[string]int{}, map[string]int{}
	for i, g := range sh.Groups {
		groupKey[g.ID] = i
		groupName[strings.ToLower(g.Name)] = i
	}
	doneStudent, doneGroup := map[int]bool{}, map[int]bool{}
	used := false

	for _, t := range tables {
		if len(t.rows) == 0 {
			continue
		}
		at := columns(t.rows[0])
		_, hasNumber := at[colNumber]
		_, hasName := at[colName]
		_, hasMembers := at["members"]
		_, hasGroup := at[colGroup]
		isGroups := hasGroup && hasMembers && !hasNumber
		marksCol := colMarks
		if _, ok := at[colOwn]; ok && !isGroups {
			marksCol = colOwn
		}
		if _, ok := at[marksCol]; !ok || (!isGroups && !hasNumber && !hasName && !hasKey(at)) {
			continue // not a mark sheet
		}
		used = true
		if isGroups && sh.Mode != ModeGroup {
			problem(t.name, 1, "this assignment is marked student by student, so group marks were left out")
			continue
		}
		for r, row := range t.rows[1:] {
			line := r + 2
			if allEmpty(row) {
				continue
			}
			score, ok := parseMark(cell(row, at, marksCol))
			if !ok {
				problem(t.name, line, fmt.Sprintf("“%s” isn't a mark", cell(row, at, marksCol)))
				continue
			}
			if score == nil {
				continue // left empty: no change
			}
			if msg := scoreProblem(score, sh.MaxMarks); msg != "" {
				problem(t.name, line, fmt.Sprintf("%s is outside 0 to %s", num(score), num(&sh.MaxMarks)))
				continue
			}
			comment := cell(row, at, colComment)
			if utf8.RuneCountInString(comment) > 1000 {
				problem(t.name, line, "the comment is longer than 1000 characters")
				continue
			}
			if isGroups {
				i, ok := groupKey[cell(row, at, colKey)]
				if !ok {
					i, ok = groupName[strings.ToLower(cell(row, at, colGroup))]
				}
				if !ok {
					problem(t.name, line, fmt.Sprintf("no group called “%s”", cell(row, at, colGroup)))
					continue
				}
				if doneGroup[i] {
					problem(t.name, line, sh.Groups[i].Name+" is on the sheet twice; the first row was used")
					continue
				}
				doneGroup[i] = true
				g := sh.Groups[i]
				if same(g.Score, *score) && g.Comment == comment {
					up.Same++
					continue
				}
				up.Groups = append(up.Groups, UploadGroup{GroupID: g.ID, Name: g.Name, Score: *score, Comment: comment, Was: g.Score})
				continue
			}
			i, ok := byKey[cell(row, at, colKey)]
			if !ok {
				i, ok = byNumber[strings.ToLower(cell(row, at, colNumber))]
			}
			if !ok {
				if list := byName[strings.ToLower(cell(row, at, colName))]; len(list) == 1 {
					i, ok = list[0], true
				} else if len(list) > 1 {
					problem(t.name, line, "two students are called "+cell(row, at, colName)+": add their student IDs")
					continue
				}
			}
			if !ok {
				problem(t.name, line, fmt.Sprintf("no student %s in this classroom", strings.TrimSpace(cell(row, at, colName)+" "+cell(row, at, colNumber))))
				continue
			}
			if doneStudent[i] {
				problem(t.name, line, sh.Students[i].Name+" is on the sheet twice; the first row was used")
				continue
			}
			doneStudent[i] = true
			st := sh.Students[i]
			if same(st.Own, *score) && st.OwnComment == comment {
				up.Same++
				continue
			}
			up.Students = append(up.Students, UploadMark{UserID: st.UserID, Name: st.Name, Score: *score, Comment: comment, Was: st.Own})
		}
	}
	if !used {
		problem("", 0, "no marks column was found: use the sheet you downloaded, keeping its headings")
	}
	return up
}

func hasKey(at map[string]int) bool {
	_, ok := at[colKey]
	return ok
}

func allEmpty(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}
