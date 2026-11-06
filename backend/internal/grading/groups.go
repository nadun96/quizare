package grading

import (
	"bytes"
	"context"
	"encoding/base64"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Groups of students for mark sheets (D-141): the whole class's, or one
// quiz's, tutorial's or poll's own, which it then has as well. A group's mark
// on an item comes from its members' marks by the item's rule. Live quiz and
// poll teams are separate.

const (
	RuleAverage = "average"
	RuleTotal   = "total"
	RuleHighest = "highest"
	RuleLowest  = "lowest"

	ScopeClass = "class"

	maxGroups = 200
)

// GroupMark is a group's mark on one item.
type GroupMark struct {
	Score  float64  `json:"score"`
	Max    float64  `json:"max"`
	Pct    float64  `json:"pct"`
	Points *float64 `json:"points,omitempty"`
	Marked int      `json:"marked"` // members with a mark
}

// GroupRow is a group with its marks.
type GroupRow struct {
	ID      string               `json:"id"`
	Name    string               `json:"name"`
	Members []string             `json:"members"` // user ids of active students
	Marks   map[string]GroupMark `json:"marks"`
	Total   float64              `json:"total"`
	Final   *float64             `json:"final"`
}

// ItemGroups is one item's own groups.
type ItemGroups struct {
	Kind   string     `json:"kind"`
	ID     string     `json:"id"`
	Groups []GroupRow `json:"groups"`
}

// groupMark applies the rule. A member with no mark counts as 0 of what the
// item is out of, so a group can't lift its mark by members not taking part.
func groupMark(rule string, marks []ReportMark, members int, itemMax float64) GroupMark {
	list := append([]ReportMark(nil), marks...)
	for len(list) < members {
		list = append(list, ReportMark{Max: itemMax})
	}
	out := GroupMark{Marked: len(marks)}
	if len(list) == 0 {
		return out
	}
	switch rule {
	case RuleHighest, RuleLowest:
		best := list[0]
		for _, m := range list[1:] {
			if (rule == RuleHighest && m.Pct > best.Pct) || (rule == RuleLowest && m.Pct < best.Pct) {
				best = m
			}
		}
		out.Score, out.Max, out.Pct = best.Score, best.Max, best.Pct
	default:
		var pct float64
		for _, m := range list {
			out.Score += m.Score
			out.Max += m.Max
			pct += m.Pct
		}
		if rule == RuleTotal {
			if out.Max > 0 {
				out.Pct = round(out.Score/out.Max*100, 1)
			}
		} else {
			n := float64(len(list))
			out.Score, out.Max, out.Pct = out.Score/n, out.Max/n, round(pct/n, 1)
		}
		out.Score, out.Max = round(out.Score, 2), round(out.Max, 2)
	}
	return out
}

// loadGroups fills in the class's groups and each item's own, with their marks.
func (s *Service) loadGroups(ctx context.Context, classroomID string, rep *ClassReport) error {
	rows, err := s.pool.Query(ctx, `SELECT g.id::text, g.kind, coalesce(g.item_id::text, ''), g.name,
			coalesce((SELECT array_agg(m.user_id::text ORDER BY m.user_id) FROM grading.group_members m WHERE m.group_id = g.id), '{}')
		FROM grading.groups g WHERE g.classroom_id = $1 ORDER BY g.kind, g.item_id, g.position, g.created_at`, classroomID)
	if err != nil {
		return err
	}
	type loaded struct {
		row      GroupRow
		kind, id string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (loaded, error) {
		var x loaded
		err := r.Scan(&x.row.ID, &x.kind, &x.id, &x.row.Name, &x.row.Members)
		return x, err
	})
	if err != nil {
		return err
	}
	students := map[string]*ReportStudent{}
	for i := range rep.Students {
		students[rep.Students[i].UserID] = &rep.Students[i]
	}
	items := map[string]*ReportItem{}
	for i := range rep.Items {
		items[Key(rep.Items[i].Kind, rep.Items[i].ID)] = &rep.Items[i]
	}
	mark := func(row *GroupRow, it *ReportItem) {
		k := Key(it.Kind, it.ID)
		if !it.Included || len(row.Members) == 0 {
			return
		}
		var marks []ReportMark
		for _, u := range row.Members {
			if m, ok := students[u].Marks[k]; ok {
				marks = append(marks, m)
			}
		}
		m := groupMark(it.GroupRule, marks, len(row.Members), it.MaxMarks)
		if it.Weight > 0 {
			p := round(m.Pct*it.Weight, 2)
			m.Points = &p
			row.Total += p
		}
		row.Marks[k] = m
	}
	rep.ClassGroups, rep.ItemGroups = []GroupRow{}, []ItemGroups{}
	own := map[string]int{}
	for _, x := range list {
		row := x.row
		active := []string{}
		for _, u := range row.Members {
			if students[u] != nil { // left the class: no longer shown
				active = append(active, u)
			}
		}
		row.Members, row.Marks = active, map[string]GroupMark{}
		if x.kind == ScopeClass {
			for i := range rep.Items {
				mark(&row, &rep.Items[i])
			}
			row.Total = round(row.Total, 2)
			if rep.OutOf > 0 && len(row.Members) > 0 {
				f := round(row.Total/rep.OutOf*100, 1)
				row.Final = &f
			}
			rep.ClassGroups = append(rep.ClassGroups, row)
			continue
		}
		it := items[Key(x.kind, x.id)]
		if it == nil {
			continue // the item has gone
		}
		mark(&row, it)
		k := Key(x.kind, x.id)
		i, ok := own[k]
		if !ok {
			i = len(rep.ItemGroups)
			own[k] = i
			rep.ItemGroups = append(rep.ItemGroups, ItemGroups{Kind: x.kind, ID: x.id, Groups: []GroupRow{}})
		}
		rep.ItemGroups[i].Groups = append(rep.ItemGroups[i].Groups, row)
	}
	return nil
}

// GroupInput is a group to save.
type GroupInput struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

// GroupsInput replaces the groups of one scope; none removes them.
type GroupsInput struct {
	Kind   string       `json:"kind"` // class, quiz, tutorial or poll
	ID     string       `json:"id"`   // the item, unless it's the class's
	Groups []GroupInput `json:"groups"`
}

// SetGroups saves a scope's groups. Any teacher of the classroom may.
func (s *Service) SetGroups(ctx context.Context, teacherID, classroomID string, in GroupsInput) (ClassReport, error) {
	rep, err := s.ClassReport(ctx, teacherID, classroomID)
	if err != nil {
		return rep, err
	}
	var item any
	if in.Kind == ScopeClass {
		in.ID = ""
	} else {
		item = in.ID
		found := false
		for _, it := range rep.Items {
			found = found || (it.Kind == in.Kind && it.ID == in.ID)
		}
		if !found {
			return rep, httpx.Invalid(map[string]string{"kind": "this isn't a quiz, tutorial or poll of this classroom"})
		}
	}
	active := map[string]bool{}
	for _, st := range rep.Students {
		active[st.UserID] = true
	}
	bad := map[string]string{}
	names, placed := map[string]bool{}, map[string]bool{}
	if len(in.Groups) > maxGroups {
		bad["groups"] = "at most 200 groups"
	}
	for i := range in.Groups {
		g := &in.Groups[i]
		g.Name = strings.TrimSpace(g.Name)
		low := strings.ToLower(g.Name)
		switch {
		case g.Name == "" || utf8.RuneCountInString(g.Name) > 100:
			bad["groups"] = "each group needs a name of at most 100 characters"
		case names[low]:
			bad["groups"] = "two groups have the same name: " + g.Name
		}
		names[low] = true
		for _, u := range g.Members {
			switch {
			case !active[u]:
				bad["groups"] = "one of these students isn't in this classroom"
			case placed[u]:
				bad["groups"] = "a student is in two groups"
			}
			placed[u] = true
		}
	}
	if len(bad) > 0 {
		return rep, httpx.Invalid(bad)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM grading.groups WHERE classroom_id = $1 AND kind = $2 AND item_id IS NOT DISTINCT FROM $3::uuid`, classroomID, in.Kind, item); err != nil {
			return err
		}
		for i, g := range in.Groups {
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO grading.groups (classroom_id, kind, item_id, name, position) VALUES ($1, $2, $3::uuid, $4, $5) RETURNING id`,
				classroomID, in.Kind, item, g.Name, i).Scan(&id); err != nil {
				return err
			}
			for _, u := range g.Members {
				if _, err := tx.Exec(ctx, `INSERT INTO grading.group_members (group_id, user_id) VALUES ($1, $2)`, id, u); err != nil {
					return err
				}
			}
		}
		return audit.Log(ctx, tx, teacherID, "report_groups_set", "classroom", classroomID, map[string]any{"kind": in.Kind, "item": in.ID, "groups": len(in.Groups)})
	})
	if err != nil {
		return rep, err
	}
	return s.ClassReport(ctx, teacherID, classroomID)
}

// ReportHeader is what every exported report of the classroom carries.
type ReportHeader struct {
	Classroom   string `json:"classroom"` // read only
	Teacher     string `json:"teacher"`   // read only
	Title       string `json:"title"`     // blank: the report's own name
	Subtitle    string `json:"subtitle"`
	Institution string `json:"institution"`
	DateMode    string `json:"date_mode"`  // today, custom or none
	DateValue   string `json:"date_value"` // YYYY-MM-DD, when custom
	ShowTeacher bool   `json:"show_teacher"`
	Footer      string `json:"footer"`
	PageNumbers bool   `json:"page_numbers"`
	Logo        string `json:"logo"`        // a data: URL, PNG or JPEG; empty: none (D-143)
	Accent      string `json:"accent"`      // the reports' colour, #rrggbb
	ColorMarks  bool   `json:"color_marks"` // tint marks by how high they are
}

const (
	defaultAccent = "#1e4fa3"
	maxLogoBytes  = 300 * 1024
)

var accentPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// decodeLogo checks a logo is what it says: only PNG and JPEG, by their bytes,
// so nothing that runs (an SVG, say) is ever stored or shown.
func decodeLogo(s string) (data []byte, contentType string, problem string) {
	for _, t := range []struct {
		contentType string
		magic       []byte
	}{{"image/png", []byte("\x89PNG\r\n\x1a\n")}, {"image/jpeg", []byte{0xFF, 0xD8, 0xFF}}} {
		rest, ok := strings.CutPrefix(s, "data:"+t.contentType+";base64,")
		if !ok {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(rest)
		switch {
		case err != nil || !bytes.HasPrefix(raw, t.magic):
			return nil, "", "this isn't a picture that can be used"
		case len(raw) > maxLogoBytes:
			return nil, "", "a logo can be at most 300 KB"
		}
		return raw, t.contentType, ""
	}
	return nil, "", "a logo must be a PNG or JPEG picture"
}

// GetReportHeader is the saved header, or the defaults.
func (s *Service) GetReportHeader(ctx context.Context, teacherID, classroomID string) (ReportHeader, error) {
	h := ReportHeader{DateMode: "today", ShowTeacher: true, PageNumbers: true, Accent: defaultAccent, ColorMarks: true}
	var err error
	if h.Classroom, err = s.teaches(ctx, teacherID, classroomID); err != nil {
		return h, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(u.name, '') FROM content.classrooms c LEFT JOIN auth.users u ON u.id = c.teacher_id WHERE c.id = $1`, classroomID).Scan(&h.Teacher); err != nil {
		return h, err
	}
	var logo []byte
	var logoType string
	err = s.pool.QueryRow(ctx, `SELECT title, subtitle, institution, date_mode, coalesce(to_char(date_value, 'YYYY-MM-DD'), ''), show_teacher, footer, page_numbers,
			logo, coalesce(logo_type, ''), accent, color_marks
		FROM grading.report_headers WHERE classroom_id = $1`, classroomID).
		Scan(&h.Title, &h.Subtitle, &h.Institution, &h.DateMode, &h.DateValue, &h.ShowTeacher, &h.Footer, &h.PageNumbers, &logo, &logoType, &h.Accent, &h.ColorMarks)
	if err == pgx.ErrNoRows {
		return h, nil
	}
	if err == nil && len(logo) > 0 {
		h.Logo = "data:" + logoType + ";base64," + base64.StdEncoding.EncodeToString(logo)
	}
	return h, err
}

// SetReportHeader saves the classroom's header and footer.
func (s *Service) SetReportHeader(ctx context.Context, teacherID, classroomID string, in ReportHeader) (ReportHeader, error) {
	if _, err := s.teaches(ctx, teacherID, classroomID); err != nil {
		return in, err
	}
	in.Title, in.Subtitle, in.Institution, in.Footer = strings.TrimSpace(in.Title), strings.TrimSpace(in.Subtitle), strings.TrimSpace(in.Institution), strings.TrimSpace(in.Footer)
	bad := map[string]string{}
	for field, v := range map[string]string{"title": in.Title, "subtitle": in.Subtitle, "institution": in.Institution, "footer": in.Footer} {
		if utf8.RuneCountInString(v) > 300 {
			bad[field] = "at most 300 characters"
		}
	}
	var date any
	switch in.DateMode {
	case "today", "none":
	case "custom":
		if _, err := time.Parse("2006-01-02", in.DateValue); err != nil {
			bad["date_value"] = "choose a date"
		}
		date = in.DateValue
	default:
		bad["date_mode"] = "choose today, a date or none"
	}
	in.Accent = strings.ToLower(strings.TrimSpace(in.Accent))
	if !accentPattern.MatchString(in.Accent) {
		bad["accent"] = "choose a colour"
	}
	var logo []byte
	var logoType *string
	if in.Logo != "" {
		data, contentType, problem := decodeLogo(in.Logo)
		if problem != "" {
			bad["logo"] = problem
		}
		logo, logoType = data, &contentType
	}
	if len(bad) > 0 {
		return in, httpx.Invalid(bad)
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO grading.report_headers (classroom_id, title, subtitle, institution, date_mode, date_value, show_teacher, footer, page_numbers, updated_by, logo, logo_type, accent, color_marks)
			VALUES ($1, $2, $3, $4, $5, $6::date, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (classroom_id) DO UPDATE SET title = $2, subtitle = $3, institution = $4, date_mode = $5, date_value = $6::date,
				show_teacher = $7, footer = $8, page_numbers = $9, updated_by = $10, logo = $11, logo_type = $12, accent = $13, color_marks = $14, updated_at = now()`,
			classroomID, in.Title, in.Subtitle, in.Institution, in.DateMode, date, in.ShowTeacher, in.Footer, in.PageNumbers, teacherID, logo, logoType, in.Accent, in.ColorMarks); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "report_header_set", "classroom", classroomID, nil)
	})
	if err != nil {
		return in, err
	}
	return s.GetReportHeader(ctx, teacherID, classroomID)
}
