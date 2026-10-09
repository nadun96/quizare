// Package page is server-side pagination for every list whose data can grow
// (PL-FR-01, ADR-25): ?page=&size=&q=&sort=&dir=. A list keeps its own array
// key in the response and adds total, page and size beside it.
package page

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

const (
	DefaultSize = 25
	MaxSize     = 100
	maxQuery    = 200
)

// Request is one page of a list, as asked for in the query string.
type Request struct {
	Page int    // 1-based
	Size int    // rows per page, 1..MaxSize
	Q    string // search text, trimmed; "" = no search
	Sort string // a key of the endpoint's sort whitelist
	Desc bool
}

// Sorts maps the sort keys a list accepts to the SQL that orders by them.
// Only these keys reach SQL, so ?sort= can't inject anything.
type Sorts map[string]string

// Parse reads page, size, q, sort and dir. Bad or missing values fall back to
// the defaults rather than failing, so an old link still opens.
func Parse(r *http.Request, sorts Sorts, defSort string, defDesc bool) Request {
	q := r.URL.Query()
	p := Request{Page: 1, Size: DefaultSize, Sort: defSort, Desc: defDesc}
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 0 {
		p.Page = n
	}
	if n, err := strconv.Atoi(q.Get("size")); err == nil && n > 0 {
		p.Size = min(n, MaxSize)
	}
	p.Q = strings.TrimSpace(q.Get("q"))
	if len(p.Q) > maxQuery {
		p.Q = p.Q[:maxQuery]
	}
	if s := q.Get("sort"); s != "" {
		if _, ok := sorts[s]; ok {
			p.Sort = s
			p.Desc = defDesc
			if s != defSort {
				p.Desc = false
			}
		}
	}
	switch q.Get("dir") {
	case "asc":
		p.Desc = false
	case "desc":
		p.Desc = true
	}
	return p
}

// Offset is the number of rows before this page.
func (p Request) Offset() int { return (p.Page - 1) * p.Size }

// Like is the search text as an ILIKE pattern ("%text%", with %, _ and \
// escaped), or "" when there is no search.
func (p Request) Like() string {
	if p.Q == "" {
		return ""
	}
	return "%" + strings.NewReplacer(`\`, `\`, `%`, `\%`, `_`, `\_`).Replace(p.Q) + "%"
}

// OrderBy is "ORDER BY <sort> ASC|DESC, <tie>": tie (a unique column) keeps
// the order stable, so no row shows on two pages or none.
func (p Request) OrderBy(sorts Sorts, tie string) string {
	col, ok := sorts[p.Sort]
	if !ok {
		for _, c := range sorts { // the caller's default is always in the map; this is only a guard
			col = c
			break
		}
	}
	dir := " ASC"
	if p.Desc {
		dir = " DESC"
	}
	return " ORDER BY " + col + dir + " NULLS LAST, " + tie + dir
}

// Limit is the SQL for this page.
func (p Request) Limit() string {
	return " LIMIT " + strconv.Itoa(p.Size) + " OFFSET " + strconv.Itoa(p.Offset())
}

// Write sends {key: items, total, page, size}.
func Write(w http.ResponseWriter, key string, items any, total int, p Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{key: items, "total": total, "page": p.Page, "size": p.Size})
}

// Slice cuts this page out of a list already filtered and sorted in Go, for
// lists bounded by something else (a classroom's enrolments are bounded by
// its size) that need data from another module to search or sort (ADR-25).
// It returns the page and the total.
func Slice[T any](all []T, p Request) ([]T, int) {
	total := len(all)
	from := min(p.Offset(), total)
	to := min(from+p.Size, total)
	return all[from:to], total
}

// Matches reports whether any of the fields contains the search text,
// ignoring case; true when there is no search.
func (p Request) Matches(fields ...string) bool {
	if p.Q == "" {
		return true
	}
	q := strings.ToLower(p.Q)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}
