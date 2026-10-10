package tutor

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Page is one page of a list, as on the platform (PL-FR-01, TS-FR-91,
// TS-FR-92): page and size (25 by default, at most 100), a search, a sort
// and its direction, all from the address.
type Page struct {
	Page, Size int
	Q, Sort    string
	Desc       bool
}

// ParsePage reads a page; defSort and defDesc are the list's default, and
// any other sort starts ascending.
func ParsePage(r *http.Request, sorts map[string]string, defSort string, defDesc bool) Page {
	q := r.URL.Query()
	p := Page{Page: 1, Size: 25, Sort: defSort, Desc: defDesc, Q: strings.TrimSpace(q.Get("q"))}
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 0 {
		p.Page = n
	}
	if n, err := strconv.Atoi(q.Get("size")); err == nil && n > 0 {
		p.Size = min(n, 100)
	}
	if len(p.Q) > 200 {
		p.Q = p.Q[:200]
	}
	if s := q.Get("sort"); s != "" {
		if _, ok := sorts[s]; ok && s != defSort {
			p.Sort, p.Desc = s, false
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

// Like is the ILIKE pattern for the search ("" for none).
func (p Page) Like() string {
	if p.Q == "" {
		return ""
	}
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(p.Q) + "%"
}

// OrderBy ends with tie, a unique column, so no row is on two pages.
func (p Page) OrderBy(sorts map[string]string, tie string) string {
	dir := " ASC"
	if p.Desc {
		dir = " DESC"
	}
	return " ORDER BY " + sorts[p.Sort] + dir + " NULLS LAST, " + tie + dir
}

func (p Page) Limit() string {
	return " LIMIT " + strconv.Itoa(p.Size) + " OFFSET " + strconv.Itoa((p.Page-1)*p.Size)
}

// WritePage sends {key: items, total, page, size}.
func WritePage(w http.ResponseWriter, key string, items any, total int, p Page) {
	web.JSON(w, http.StatusOK, map[string]any{key: items, "total": total, "page": p.Page, "size": p.Size})
}
