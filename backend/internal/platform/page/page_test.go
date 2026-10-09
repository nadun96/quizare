package page

import (
	"net/http/httptest"
	"testing"
)

var sorts = Sorts{"created": "created_at", "name": "lower(name)"}

func req(q string) Request {
	return Parse(httptest.NewRequest("GET", "/x?"+q, nil), sorts, "created", true)
}

func TestParseDefaultsAndLimits(t *testing.T) {
	p := req("")
	if p.Page != 1 || p.Size != DefaultSize || p.Sort != "created" || !p.Desc || p.Q != "" {
		t.Fatalf("defaults: %+v", p)
	}
	p = req("page=3&size=500&q=%20ann%20")
	if p.Page != 3 || p.Size != MaxSize || p.Q != "ann" || p.Offset() != 200 {
		t.Fatalf("clamped: %+v offset %d", p, p.Offset())
	}
	for _, bad := range []string{"page=0", "page=-2", "page=x", "size=0", "size=x"} {
		if p := req(bad); p.Page != 1 || p.Size != DefaultSize {
			t.Errorf("%s → %+v", bad, p)
		}
	}
}

func TestSortIsWhitelisted(t *testing.T) {
	if p := req("sort=name"); p.Sort != "name" || p.Desc {
		t.Fatalf("another sort starts ascending: %+v", p)
	}
	if p := req("sort=name&dir=desc"); !p.Desc {
		t.Fatal("dir=desc")
	}
	p := req("sort=password_hash%3BDROP%20TABLE%20x")
	if p.Sort != "created" {
		t.Fatalf("unknown sort kept: %+v", p)
	}
	if got := p.OrderBy(sorts, "id"); got != " ORDER BY created_at DESC NULLS LAST, id DESC" {
		t.Fatalf("order: %q", got)
	}
	if got := req("sort=name").OrderBy(sorts, "id"); got != " ORDER BY lower(name) ASC NULLS LAST, id ASC" {
		t.Fatalf("order: %q", got)
	}
}

func TestLikeEscapesWildcards(t *testing.T) {
	if got := req("q=50%25_off").Like(); got != `%50\%\_off%` {
		t.Fatalf("like: %q", got)
	}
	if req("").Like() != "" {
		t.Fatal("no search, no pattern")
	}
	if got := req("page=2&size=10").Limit(); got != " LIMIT 10 OFFSET 10" {
		t.Fatalf("limit: %q", got)
	}
}

func TestSliceAndMatches(t *testing.T) {
	all := []int{1, 2, 3, 4, 5, 6, 7}
	if got, n := Slice(all, req("page=2&size=3")); n != 7 || len(got) != 3 || got[0] != 4 {
		t.Fatalf("page 2: %v %d", got, n)
	}
	if got, _ := Slice(all, req("page=3&size=3")); len(got) != 1 || got[0] != 7 {
		t.Fatalf("last page: %v", got)
	}
	if got, n := Slice(all, req("page=9&size=3")); n != 7 || len(got) != 0 {
		t.Fatalf("past the end: %v", got)
	}
	if !req("q=ANN").Matches("x", "Joanna") || req("q=bob").Matches("Ann") || !req("").Matches() {
		t.Fatal("matches")
	}
}
