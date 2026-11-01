package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Local models with Ollama (D-115). The admin points the platform at an
// Ollama server (usually this machine, http://127.0.0.1:11434) and chooses
// which of its models teachers may use. Teachers then add "This school's
// server" as a provider, with no API key, and answers never leave the
// school. The admin can also make one model the default for every teacher
// without a key of their own, so AI marking works with no set-up. Only the
// admin sets the address: a teacher-supplied address could reach anything
// on the school's network.

// LocalProvider is the provider name stored on keys that use the school's server.
const LocalProvider = "ollama"

// LocalKeyID stands for "the school's default model" where a key id goes.
const LocalKeyID = "local"

// Local is the admin's Ollama settings.
type Local struct {
	Enabled      bool       `json:"enabled"`
	BaseURL      string     `json:"base_url"`
	Models       []string   `json:"models"`        // allowed; empty = every installed model
	DefaultModel string     `json:"default_model"` // for teachers with no key of their own, when ForEveryone
	ForEveryone  bool       `json:"for_everyone"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
}

// Installed is one model the Ollama server has.
type Installed struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Family   string    `json:"family,omitempty"`
	Params   string    `json:"parameters,omitempty"`
	Modified time.Time `json:"modified_at"`
}

// LocalState is the settings with what the server says now.
type LocalState struct {
	Local
	Reachable bool        `json:"reachable"`
	Error     string      `json:"error,omitempty"`
	Installed []Installed `json:"installed"`
}

// localCache keeps the settings for 30 seconds, so every marking call
// doesn't read them again.
type localCache struct {
	sync.Mutex
	l  *Local
	at time.Time
}

func (s *Service) loadLocal(ctx context.Context) (Local, error) {
	s.local.Lock()
	defer s.local.Unlock()
	if s.local.l != nil && time.Since(s.local.at) < 30*time.Second {
		return *s.local.l, nil
	}
	var l Local
	var at time.Time
	err := s.pool.QueryRow(ctx, `SELECT enabled, base_url, models, default_model, for_everyone, updated_at FROM llm.local`).
		Scan(&l.Enabled, &l.BaseURL, &l.Models, &l.DefaultModel, &l.ForEveryone, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return Local{Models: []string{}}, nil
	}
	if err != nil {
		return l, err
	}
	l.UpdatedAt = &at
	if l.Models == nil {
		l.Models = []string{}
	}
	s.local.l, s.local.at = &l, time.Now()
	return l, nil
}

func (s *Service) forgetLocal() {
	s.local.Lock()
	s.local.l = nil
	s.local.Unlock()
}

// allowed says whether teachers may use a model.
func (l Local) allowed(model string) bool {
	return model != "" && (len(l.Models) == 0 || slices.Contains(l.Models, model))
}

// localBase is the Ollama address, or a reason it can't be used.
func (s *Service) localBase(ctx context.Context) (string, error) {
	l, err := s.loadLocal(ctx)
	if err != nil {
		return "", err
	}
	if !l.Enabled || l.BaseURL == "" {
		return "", &PermanentError{"the school's local AI models are switched off"}
	}
	return l.BaseURL, nil
}

func checkBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil {
		return "", httpx.Invalid(map[string]string{"base_url": "the Ollama server's address, like http://127.0.0.1:11434"})
	}
	return raw, nil
}

// LocalState reads the settings and asks the server what it has.
func (s *Service) LocalState(ctx context.Context) (LocalState, error) {
	s.forgetLocal()
	l, err := s.loadLocal(ctx)
	if err != nil {
		return LocalState{}, err
	}
	st := LocalState{Local: l, Installed: []Installed{}}
	if l.BaseURL == "" {
		return st, nil
	}
	st.Installed, err = s.installed(ctx, l.BaseURL)
	if err != nil {
		st.Error = err.Error()
	} else {
		st.Reachable = true
	}
	return st, nil
}

// installed lists the models an Ollama server has (GET /api/tags).
func (s *Service) installed(ctx context.Context, base string) ([]Installed, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	res, err := s.localHTTP().Do(req)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, errors.New("Ollama didn't answer in 5 seconds")
		}
		return nil, fmt.Errorf("can't reach Ollama at %s: is it running? (%v)", base, rootCause(err))
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama answered %d", res.StatusCode)
	}
	var out struct {
		Models []struct {
			Name       string    `json:"name"`
			Size       int64     `json:"size"`
			ModifiedAt time.Time `json:"modified_at"`
			Details    struct {
				Family        string `json:"family"`
				ParameterSize string `json:"parameter_size"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, errors.New("that address doesn't answer like Ollama")
	}
	list := []Installed{}
	for _, m := range out.Models {
		list = append(list, Installed{Name: m.Name, Size: m.Size, Family: m.Details.Family, Params: m.Details.ParameterSize, Modified: m.ModifiedAt})
	}
	slices.SortFunc(list, func(a, b Installed) int { return strings.Compare(a.Name, b.Name) })
	return list, nil
}

func rootCause(err error) error {
	for {
		u := errors.Unwrap(err)
		if u == nil {
			return err
		}
		err = u
	}
}

// SaveLocal stores the admin's settings.
func (s *Service) SaveLocal(ctx context.Context, actorID string, in Local) (LocalState, error) {
	bad := map[string]string{}
	if in.BaseURL != "" || in.Enabled {
		u, err := checkBaseURL(in.BaseURL)
		if err != nil {
			return LocalState{}, err
		}
		in.BaseURL = u
	}
	models := []string{}
	for _, m := range in.Models {
		if m = strings.TrimSpace(m); m != "" && !slices.Contains(models, m) && len(m) <= 100 {
			models = append(models, m)
		}
	}
	in.Models = models
	in.DefaultModel = strings.TrimSpace(in.DefaultModel)
	if in.ForEveryone && in.DefaultModel == "" {
		bad["default_model"] = "choose the model teachers without a key use"
	}
	if in.DefaultModel != "" && !in.allowed(in.DefaultModel) {
		bad["default_model"] = "the default must be one of the models teachers may use"
	}
	if len(bad) > 0 {
		return LocalState{}, httpx.Invalid(bad)
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO llm.local (id, enabled, base_url, models, default_model, for_everyone, updated_by, updated_at)
			VALUES (true, $1, $2, $3, $4, $5, $6, now())
			ON CONFLICT (id) DO UPDATE SET enabled = $1, base_url = $2, models = $3, default_model = $4, for_everyone = $5, updated_by = $6, updated_at = now()`,
			in.Enabled, in.BaseURL, in.Models, in.DefaultModel, in.ForEveryone, actorID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "local_ai_changed", "llm_local", "local", map[string]any{"enabled": in.Enabled, "base_url": in.BaseURL, "for_everyone": in.ForEveryone, "default_model": in.DefaultModel})
	})
	if err != nil {
		return LocalState{}, err
	}
	s.forgetLocal()
	return s.LocalState(ctx)
}

// TeacherLocal is what a teacher sees of the school's models.
type TeacherLocal struct {
	Enabled      bool     `json:"enabled"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"default_model"`
	ForEveryone  bool     `json:"for_everyone"`
}

// ForTeachers lists the models teachers may pick: the allowed list, or
// everything installed when the admin left it open.
func (s *Service) ForTeachers(ctx context.Context) (TeacherLocal, error) {
	l, err := s.loadLocal(ctx)
	if err != nil || !l.Enabled {
		return TeacherLocal{Models: []string{}}, err
	}
	out := TeacherLocal{Enabled: true, Models: l.Models, DefaultModel: l.DefaultModel, ForEveryone: l.ForEveryone}
	if len(out.Models) == 0 {
		inst, err := s.installed(ctx, l.BaseURL)
		if err == nil {
			for _, m := range inst {
				out.Models = append(out.Models, m.Name)
			}
		}
	}
	if out.Models == nil {
		out.Models = []string{}
	}
	return out, nil
}

// schoolKey is "the school's default model" for a teacher with no key of
// their own, when the admin offers one to everyone; nil otherwise.
func (s *Service) schoolKey(ctx context.Context, teacherID string) (*storedKey, error) {
	l, err := s.loadLocal(ctx)
	if err != nil || !l.Enabled || !l.ForEveryone || l.DefaultModel == "" {
		return nil, err
	}
	return &storedKey{Key: Key{ID: LocalKeyID, Provider: LocalProvider, Label: "School's server", Model: l.DefaultModel, IsDefault: true}, teacherID: teacherID}, nil
}

func (s *Service) localHTTP() *http.Client {
	if s.localClient != nil {
		return s.localClient
	}
	return localDefaultHTTP
}

// Local models are slower than the cloud on school hardware: give them time.
var localDefaultHTTP = &http.Client{Timeout: 5 * time.Minute}

// ---------- Ollama (POST /api/chat with a JSON schema) ----------

// Ollama marks with a model on the school's own server.
type Ollama struct {
	Base   func(ctx context.Context) (string, error)
	Client *http.Client
}

func (p Ollama) Grade(ctx context.Context, _ string, model string, r GradeRequest) (GradeResult, error) {
	base, err := p.Base(ctx)
	if err != nil {
		return GradeResult{}, err
	}
	body := map[string]any{
		"model":  model,
		"stream": false,
		"format": schema(),
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt(r)},
		},
		// Marking should be the same each time it's asked.
		"options": map[string]any{"temperature": 0},
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Error string `json:"error"`
	}
	c := p.Client
	if c == nil {
		c = localDefaultHTTP
	}
	if err := postJSON(ctx, c, base+"/api/chat", nil, body, &out); err != nil {
		var perm *PermanentError
		if errors.As(err, &perm) && strings.Contains(perm.Reason, "not found") {
			return GradeResult{}, &PermanentError{fmt.Sprintf("the school's server doesn't have the model %q (ask the admin to pull it)", model)}
		}
		return GradeResult{}, err
	}
	if out.Error != "" {
		return GradeResult{}, &PermanentError{"Ollama: " + out.Error}
	}
	return decodeResult(out.Message.Content)
}

// localModelAllowed checks a teacher's choice: on the admin's list, or
// installed on the server when the list is open.
func (s *Service) localModelAllowed(ctx context.Context, l Local, model string) (bool, error) {
	if len(l.Models) > 0 {
		return l.allowed(model), nil
	}
	inst, err := s.installed(ctx, l.BaseURL)
	if err != nil {
		return false, httpx.Invalid(map[string]string{"model": err.Error()})
	}
	return slices.ContainsFunc(inst, func(m Installed) bool { return m.Name == model }), nil
}
