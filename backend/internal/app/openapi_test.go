package app_test

import (
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.yaml.in/yaml/v3"

	"github.com/nadun96/quizplatform/internal/apidocs"
	"github.com/nadun96/quizplatform/internal/app/apptest"
)

type spec struct {
	OpenAPI    string                    `yaml:"openapi"`
	Paths      map[string]map[string]any `yaml:"paths"`
	Components struct {
		Schemas       map[string]any `yaml:"schemas"`
		Responses     map[string]any `yaml:"responses"`
		Parameters    map[string]any `yaml:"parameters"`
		RequestBodies map[string]any `yaml:"requestBodies"`
	} `yaml:"components"`
}

var methods = map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}

// The spec and the router must agree in both directions, so the docs cannot drift.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	e := apptest.New(t)
	var s spec
	if err := yaml.Unmarshal(apidocs.Spec(), &s); err != nil {
		t.Fatalf("spec is not valid YAML: %v", err)
	}
	if !strings.HasPrefix(s.OpenAPI, "3.1") {
		t.Fatalf("openapi = %q", s.OpenAPI)
	}
	documented := map[string]bool{}
	ids := map[string]string{}
	for path, ops := range s.Paths {
		for m, op := range ops {
			if !methods[m] {
				continue
			}
			key := strings.ToUpper(m) + " " + path
			documented[key] = true
			id, _ := op.(map[string]any)["operationId"].(string)
			if id == "" {
				t.Errorf("%s has no operationId", key)
			} else if prev, dup := ids[id]; dup {
				t.Errorf("operationId %s used by %s and %s", id, prev, key)
			}
			ids[id] = key
		}
	}
	routed := map[string]bool{}
	err := chi.Walk(e.App.Handler().(chi.Router), func(m, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(strings.TrimSuffix(route, "/*"), "/")
		if route == "" || strings.HasPrefix(route, "/api/docs") {
			return nil
		}
		routed[m+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing, stale []string
	for r := range routed {
		if !documented[r] {
			missing = append(missing, r)
		}
	}
	for d := range documented {
		if !routed[d] {
			stale = append(stale, d)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 {
		t.Errorf("routes missing from openapi.yaml:\n  %s", strings.Join(missing, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("openapi.yaml documents routes that do not exist:\n  %s", strings.Join(stale, "\n  "))
	}
}

// Every $ref must point at a defined component.
func TestOpenAPIRefsResolve(t *testing.T) {
	var s spec
	if err := yaml.Unmarshal(apidocs.Spec(), &s); err != nil {
		t.Fatal(err)
	}
	var doc any
	yaml.Unmarshal(apidocs.Spec(), &doc)
	sections := map[string]map[string]any{
		"schemas": s.Components.Schemas, "responses": s.Components.Responses,
		"parameters": s.Components.Parameters, "requestBodies": s.Components.RequestBodies,
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if ref, ok := x["$ref"].(string); ok {
				parts := strings.Split(strings.TrimPrefix(ref, "#/components/"), "/")
				if len(parts) != 2 || sections[parts[0]][parts[1]] == nil {
					t.Errorf("unresolved $ref %s", ref)
				}
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(doc)
}

func TestDocsServed(t *testing.T) {
	e := apptest.New(t)
	c := e.Server.Client()
	for path, want := range map[string]string{
		"/api/docs":                         "swagger-ui-bundle.js",
		"/api/docs/openapi.yaml":            "openapi: 3.1.0",
		"/api/docs/openapi.json":            `"openapi":"3.1.0"`,
		"/api/docs/ui/init.js":              "SwaggerUIBundle",
		"/api/docs/ui/swagger-ui-bundle.js": "SwaggerUIBundle",
	} {
		resp, err := c.Get(e.Server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(body), want) {
			t.Errorf("%s: %d, missing %q", path, resp.StatusCode, want)
		}
	}
	// The page uses no inline script, so it works under the strict CSP.
	resp, _ := c.Get(e.Server.URL + "/api/docs")
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "<script>") {
		t.Error("docs page contains an inline script")
	}
}
