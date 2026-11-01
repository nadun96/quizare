package llm_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/llm"
)

// fakeOllama answers /api/tags and /api/chat as Ollama does.
type fakeOllama struct {
	mu     sync.Mutex
	models []string
	asked  []string // the model of each chat
}

func (f *fakeOllama) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/api/tags":
		var ms []map[string]any
		for _, m := range f.models {
			ms = append(ms, map[string]any{"name": m, "size": 2_000_000_000, "modified_at": "2026-10-01T00:00:00Z", "details": map[string]string{"family": "llama", "parameter_size": "3B"}})
		}
		json.NewEncoder(w).Encode(map[string]any{"models": ms})
	case "/api/chat":
		var in struct {
			Model  string         `json:"model"`
			Stream bool           `json:"stream"`
			Format map[string]any `json:"format"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		f.asked = append(f.asked, in.Model)
		found := false
		for _, m := range f.models {
			found = found || m == in.Model
		}
		if !found {
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"error": "model '" + in.Model + "' not found"})
			return
		}
		if in.Stream || in.Format == nil {
			w.WriteHeader(400)
			return
		}
		content, _ := json.Marshal(llm.GradeResult{Score: 3, Rationale: "partly right", Feedback: "Name the pigment.", Confidence: 0.8})
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": string(content)}, "done": true})
	default:
		http.NotFound(w, r)
	}
}

// The admin points the platform at Ollama and picks models; teachers use
// them with no key, and teachers without a key of their own get the
// school's default (D-115).
func TestLocalModels(t *testing.T) {
	f := setup(t, nil)
	ollama := &fakeOllama{models: []string{"llama3.2:3b", "qwen2.5:7b", "gemma3:4b"}}
	srv := httptest.NewServer(ollama)
	t.Cleanup(srv.Close)
	admin := f.e.NewUser(auth.RoleAdmin)

	var st llm.LocalState
	admin.Call("GET", "/api/admin/ai/local", nil, 200, &st)
	if st.Enabled || st.Reachable {
		t.Fatalf("at first = %+v", st)
	}
	admin.Call("PUT", "/api/admin/ai/local", map[string]any{"enabled": true, "base_url": "ftp://10.0.0.1"}, 422, nil)
	admin.Call("PUT", "/api/admin/ai/local", map[string]any{"enabled": true, "base_url": srv.URL, "models": []string{"llama3.2:3b", "qwen2.5:7b"}, "for_everyone": true}, 422, nil) // no default
	admin.Call("PUT", "/api/admin/ai/local", map[string]any{"enabled": true, "base_url": srv.URL + "/", "models": []string{"llama3.2:3b", "qwen2.5:7b"}, "default_model": "qwen2.5:7b", "for_everyone": true}, 200, &st)
	if !st.Reachable || len(st.Installed) != 3 || st.BaseURL != srv.URL || st.Installed[0].Params != "3B" {
		t.Fatalf("saved = %+v", st)
	}
	var tr llm.LocalTest
	admin.Call("POST", "/api/admin/ai/local/test", map[string]string{"model": "llama3.2:3b"}, 200, &tr)
	if !tr.OK || tr.Score != 3 {
		t.Fatalf("test = %+v", tr)
	}
	admin.Call("POST", "/api/admin/ai/local/test", map[string]string{"model": "missing:1b"}, 200, &tr)
	if tr.OK || !strings.Contains(tr.Error, "doesn't have the model") {
		t.Fatalf("missing model = %+v", tr)
	}
	f.teacher.Call("GET", "/api/admin/ai/local", nil, 403, nil)

	// A teacher with no key of their own: the school's default marks the essay.
	id := f.take(t, "Plants use light")
	if n := f.runLLM(t); n != 1 {
		t.Fatalf("llm jobs = %d", n)
	}
	r := result(t, f, id)
	if r.Score != 4 || ollama.asked[len(ollama.asked)-1] != "qwen2.5:7b" { // 1 (choice) + 3 (essay)
		t.Fatalf("school default result = %+v, asked %v", r, ollama.asked)
	}

	// The teacher sees the models and adds one as a key, with no API key.
	var tl llm.TeacherLocal
	f.teacher.Call("GET", "/api/teacher/llm-local", nil, 200, &tl)
	if !tl.Enabled || len(tl.Models) != 2 || tl.DefaultModel != "qwen2.5:7b" {
		t.Fatalf("teacher view = %+v", tl)
	}
	f.teacher.Call("POST", "/api/teacher/llm-keys", map[string]any{"provider": "ollama", "model": "gemma3:4b"}, 422, nil) // not allowed
	var k llm.Key
	f.teacher.Call("POST", "/api/teacher/llm-keys", map[string]any{"provider": "ollama", "model": "llama3.2:3b", "label": "School"}, 201, &k)
	if k.Provider != "ollama" || k.Last4 != "" || !k.IsDefault {
		t.Fatalf("key = %+v", k)
	}
	f.teacher.Call("POST", "/api/teacher/llm-keys/"+k.ID+"/test", nil, 200, &k)
	if k.LastTestOK == nil || !*k.LastTestOK {
		t.Fatalf("key test = %+v", k)
	}
	id = f.take(t, "Light makes glucose")
	f.runLLM(t)
	if ollama.asked[len(ollama.asked)-1] != "llama3.2:3b" {
		t.Fatalf("own key asked %v", ollama.asked)
	}

	// Switched off: no new keys, and the school default stops.
	admin.Call("PUT", "/api/admin/ai/local", map[string]any{"enabled": false, "base_url": srv.URL}, 200, &st)
	f.teacher.Call("POST", "/api/teacher/llm-keys", map[string]any{"provider": "ollama", "model": "llama3.2:3b"}, 422, nil)
	f.teacher.Call("GET", "/api/teacher/llm-local", nil, 200, &tl)
	if tl.Enabled {
		t.Fatalf("off = %+v", tl)
	}
}
