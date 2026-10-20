package directory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Service is the directory side of sign-in; auth calls it through auth.Directory.
type Service struct {
	store *Store
	auth  *auth.Service
	log   *slog.Logger

	mu       sync.Mutex
	cached   *Config
	password string
	fetched  time.Time
}

// The settings are read at most every 30 s, so a sign-in burst at the start
// of a lesson doesn't read and unseal them for every student.
const cacheTTL = 30 * time.Second

func NewService(pool *pgxpool.Pool, vault Sealer, a *auth.Service, log *slog.Logger) *Service {
	s := &Service{store: &Store{pool: pool, vault: vault}, auth: a, log: log}
	a.SetDirectory(s)
	return s
}

func (s *Service) current(ctx context.Context) (Config, string, error) {
	s.mu.Lock()
	if s.cached != nil && time.Since(s.fetched) < cacheTTL {
		c, pw := *s.cached, s.password
		s.mu.Unlock()
		return c, pw, nil
	}
	s.mu.Unlock()
	c, pw, err := s.store.Load(ctx)
	if err != nil {
		return Config{}, "", err
	}
	s.mu.Lock()
	s.cached, s.password, s.fetched = &c, pw, time.Now()
	s.mu.Unlock()
	return c, pw, nil
}

func (s *Service) forget() {
	s.mu.Lock()
	s.cached, s.password = nil, ""
	s.mu.Unlock()
}

// Enabled implements auth.Directory.
func (s *Service) Enabled(ctx context.Context) bool {
	c, _, err := s.current(ctx)
	return err == nil && c.Enabled
}

// Authenticate implements auth.Directory.
func (s *Service) Authenticate(ctx context.Context, login, password string) (auth.DirectoryUser, error) {
	c, pw, err := s.current(ctx)
	if err != nil {
		return auth.DirectoryUser{}, err
	}
	if !c.Enabled {
		return auth.DirectoryUser{}, auth.ErrDirectoryCredentials
	}
	du, err := authenticate(c, pw, login, password)
	if errors.Is(err, auth.ErrDirectoryDown) {
		s.log.Warn("directory sign-in failed", "err", err)
	}
	return du, err
}

// ---------- the admin's test ----------

// Step is one line of the test's report.
type Step struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// TestResult is what the test found, step by step.
type TestResult struct {
	OK    bool   `json:"ok"`
	Steps []Step `json:"steps"`
	// Account: what signing in as Login would give, when a login was tested.
	Account *struct {
		Email string    `json:"email"`
		Name  string    `json:"name"`
		Role  auth.Role `json:"role"`
		DN    string    `json:"dn"`
	} `json:"account,omitempty"`
}

// Test tries unsaved settings: connect, sign the service account in, and,
// given a login, find it, check its password and work out its role. Errors
// are shown in full here, to the admin, and nowhere else.
func (s *Service) Test(ctx context.Context, c Config, login, password string) (TestResult, error) {
	c.normalise()
	_, stored, err := s.store.Load(ctx)
	if err != nil {
		return TestResult{}, err
	}
	if c.BindPassword == "" {
		c.BindPassword = stored
	}
	if err := c.validate(c.BindPassword != ""); err != nil {
		return TestResult{}, err
	}
	var r TestResult
	step := func(name string, err error, detail string) bool {
		st := Step{Name: name, OK: err == nil, Detail: detail}
		if err != nil {
			st.Detail = err.Error()
		}
		r.Steps = append(r.Steps, st)
		return err == nil
	}
	conn, err := connect(c)
	if !step("Connect securely to "+hostOf(c.URL), err, "") {
		return r, nil
	}
	defer conn.Close()
	if c.BindDN != "" {
		if !step("Sign the service account in", serviceBind(conn, c, c.BindPassword), c.BindDN) {
			return r, nil
		}
	}
	if login == "" {
		r.OK = true
		return r, nil
	}
	f, err := find(conn, c, login)
	if errors.Is(err, auth.ErrDirectoryCredentials) {
		err = fmt.Errorf("no single account matches %q under %s", login, c.BaseDN)
	}
	if !step("Find "+login, err, f.DN) {
		return r, nil
	}
	if password != "" {
		err := conn.Bind(f.DN, password)
		if !step("Check the password", err, "") {
			return r, nil
		}
		if err := serviceBind(conn, c, c.BindPassword); err != nil {
			step("Sign the service account back in", err, "")
			return r, nil
		}
	}
	role, err := roleOf(conn, c, f)
	if errors.Is(err, auth.ErrDirectoryNotAllowed) {
		err = errors.New("not in the teacher group or the student group, so this account can't sign in")
	}
	if !step("Work out the role from the groups", err, string(role)) {
		return r, nil
	}
	if f.Email == "" {
		step("Read the email address", fmt.Errorf("no valid address in %s or userPrincipalName", c.EmailAttr), "")
		return r, nil
	}
	r.Account = &struct {
		Email string    `json:"email"`
		Name  string    `json:"name"`
		Role  auth.Role `json:"role"`
		DN    string    `json:"dn"`
	}{f.Email, f.Name, role, f.DN}
	r.OK = true
	return r, nil
}

func hostOf(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	return strings.TrimSuffix(u, "/")
}

// ---------- the hourly check ----------

// Check signs out directory accounts the directory no longer lets in. If the
// directory can't be reached, nobody is signed out: an outage at the school
// shouldn't end every lesson.
func (s *Service) Check(ctx context.Context) error {
	c, pw, err := s.current(ctx)
	if err != nil || !c.Enabled {
		return err
	}
	accounts, err := s.auth.SignedInDirectoryAccounts(ctx)
	if err != nil || len(accounts) == 0 {
		return err
	}
	conn, err := connect(c)
	if err != nil {
		s.log.Warn("directory check skipped", "err", err)
		return nil
	}
	defer conn.Close()
	if err := serviceBind(conn, c, pw); err != nil {
		s.log.Warn("directory check skipped", "err", err)
		return nil
	}
	for _, a := range accounts {
		ok, why, err := stillAllowed(conn, c, a.DN, a.Role)
		if err != nil {
			s.log.Warn("directory check stopped", "err", err)
			return nil
		}
		if !ok {
			if err := s.auth.SignOutDirectoryAccount(ctx, a.ID, why); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckArgs is the hourly job.
type CheckArgs struct{}

func (CheckArgs) Kind() string { return "directory_check" }

type CheckWorker struct {
	river.WorkerDefaults[CheckArgs]
	Service *Service
}

func (w *CheckWorker) Work(ctx context.Context, _ *river.Job[CheckArgs]) error {
	return w.Service.Check(ctx)
}

// Periodic runs the check every hour.
func Periodic() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(time.Hour), func() (river.JobArgs, *river.InsertOpts) { return CheckArgs{}, nil }, nil)
}

// ---------- HTTP ----------

// AdminRoutes: admins only. The directory decides who becomes a teacher, so
// it isn't a feature a manager can be given (PL-FR-13).
func (s *Service) AdminRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.auth.AdminOnly())
		r.Method("GET", "/directory", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			c, _, err := s.store.Load(r.Context())
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, map[string]any{"config": c, "defaults": Defaults()})
			return nil
		}))
		r.Method("PUT", "/directory", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var c Config
			if err := httpx.Decode(w, r, &c); err != nil {
				return err
			}
			u, _ := auth.CurrentUser(r.Context())
			saved, err := s.store.Save(r.Context(), u.ID, c)
			s.forget()
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, saved)
			return nil
		}))
		r.Method("POST", "/directory/test", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			var in struct {
				Config   Config `json:"config"`
				Login    string `json:"login"`
				Password string `json:"password"`
			}
			if err := httpx.Decode(w, r, &in); err != nil {
				return err
			}
			res, err := s.Test(r.Context(), in.Config, strings.TrimSpace(in.Login), in.Password)
			if err != nil {
				return err
			}
			httpx.JSON(w, http.StatusOK, res)
			return nil
		}))
	})
}
