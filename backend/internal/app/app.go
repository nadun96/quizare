// Package app is the composition root: it wires modules together and builds
// the HTTP router. Modules depend only on interfaces; this is the one place
// that knows every concrete type.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/nadun96/quizplatform/internal/admin"
	"github.com/nadun96/quizplatform/internal/analytics"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/imageurl"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/llm"
	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/platform/config"
	"github.com/nadun96/quizplatform/internal/platform/db"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/settings"
	"github.com/nadun96/quizplatform/migrations"
)

type App struct {
	cfg     config.Config
	log     *slog.Logger
	pool    *pgxpool.Pool
	river   *river.Client[pgx.Tx]
	router  chi.Router
	ownPool bool

	Auth      *auth.Service
	Settings  *settings.Store
	Content   *content.Service
	Quiz      *quiz.Service
	Live      *live.Service
	Eval      *eval.Service
	LLM       *llm.Service
	Analytics *analytics.Service
	Admin     *admin.Service
}

// Options let tests swap infrastructure.
type Options struct {
	RunJobs bool        // false: insert-only River client
	Mailer  mail.Sender // nil: chosen from config
	// Checker validates resource URLs; nil uses the SSRF-safe public checker.
	Checker *imageurl.Checker
	// KEK is the 32-byte master key; New loads it from cfg.KEKFile.
	KEK []byte
	// Providers overrides the LLM adapters (tests point them at fakes).
	Providers map[string]llm.Provider
}

// New opens the database, applies migrations and builds the app.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	pool, err := db.Open(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		pool.Close()
		return nil, err
	}
	if err := jobs.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	kek, err := llm.LoadKEK(cfg.KEKFile)
	if err != nil {
		pool.Close()
		return nil, err
	}
	a, err := Build(cfg, log, pool, Options{RunJobs: true, KEK: kek})
	if err != nil {
		pool.Close()
		return nil, err
	}
	a.ownPool = true
	return a, nil
}

// Build wires modules onto an existing pool.
func Build(cfg config.Config, log *slog.Logger, pool *pgxpool.Pool, opt Options) (*App, error) {
	a := &App{cfg: cfg, log: log, pool: pool}

	mailer := opt.Mailer
	if mailer == nil {
		var err error
		if mailer, err = newMailer(cfg, log); err != nil {
			return nil, err
		}
	}
	checker := opt.Checker
	if checker == nil {
		checker = imageurl.NewChecker(false)
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &mail.Worker{Sender: mailer})
	quizWorker := &quiz.CheckResourcesWorker{}
	river.AddWorker(workers, quizWorker)
	evalWorker := &eval.EvaluateWorker{}
	river.AddWorker(workers, evalWorker)
	llmWorker := &llm.Worker{}
	river.AddWorker(workers, llmWorker)
	analyticsWorker := &analytics.Worker{}
	river.AddWorker(workers, analyticsWorker)

	var w *river.Workers
	if opt.RunJobs {
		w = workers
	}
	rc, err := jobs.NewClient(pool, w, log)
	if err != nil {
		return nil, err
	}
	a.river = rc

	a.Auth = auth.NewService(pool, auth.NewHasher(cfg.Argon2Workers), rc, cfg.BaseURL)
	a.Settings = settings.NewStore(pool)
	a.Content = content.NewService(pool, a.Settings, a.Auth)
	a.Quiz = quiz.NewService(pool, a.Content, a.Settings, rc, checker)
	quizWorker.Service = a.Quiz
	a.Live = live.NewService(pool, a.Quiz, a.Content, a.Auth, cfg.BaseURL, log)
	a.Quiz.SetSessionGuard(a.Live.HasSessions) // BR-16: run quizzes are archived, not deleted
	a.Content.SetDeleteGuard(a.Live.ClassroomHasSessions)
	a.Eval = eval.NewService(pool, a.Live, rc)
	evalWorker.Service = a.Eval
	vault, err := llm.NewVault(opt.KEK)
	if err != nil {
		return nil, err
	}
	providers := opt.Providers
	if providers == nil {
		providers = llm.DefaultProviders()
	}
	a.LLM = llm.NewService(pool, vault, providers, rc, a.Live, a.Eval, log)
	llmWorker.Service = a.LLM
	a.Eval.SetLLM(a.LLM)
	a.Analytics = analytics.NewService(pool, a.Live, a.Eval, a.Auth, rc)
	analyticsWorker.Service = a.Analytics
	a.Admin = admin.NewService(pool, a.Auth)
	a.Eval.SetResultsHook(a.Analytics.OnResults)
	a.Live.SetHooks(live.Hooks{
		AttemptFinished: a.Eval.OnAttemptFinished,
		SessionEnded: func(ctx context.Context, tx pgx.Tx, sessionID string) error {
			if err := a.Eval.OnSessionEnded(ctx, tx, sessionID); err != nil {
				return err
			}
			return a.Analytics.OnResults(ctx, tx, sessionID) // completion and invalidation rates change at the end
		},
	})

	r := chi.NewRouter()
	r.Use(httpx.Recover, httpx.SecurityHeaders)
	r.Get("/healthz", a.health)
	r.Route("/api", func(api chi.Router) {
		api.Use(httpx.SameOrigin(cfg.BaseURL), a.Auth.Middleware)
		api.Route("/auth", a.Auth.Routes)
		api.Route("/public", a.Analytics.PublicRoutes)
		api.Route("/admin", func(ad chi.Router) {
			ad.Use(auth.RequireRole(auth.RoleAdmin))
			a.Auth.AdminRoutes(ad)
			ad.Route("/settings", a.Settings.AdminRoutes)
			a.Admin.AdminRoutes(ad)
		})
		api.Route("/teacher", func(t chi.Router) {
			t.Use(auth.RequireRole(auth.RoleTeacher))
			t.Route("/settings", a.Settings.TeacherRoutes)
			a.Content.TeacherRoutes(t)
			a.Quiz.TeacherRoutes(t)
			a.Live.TeacherRoutes(t)
			a.Eval.TeacherRoutes(t)
			a.LLM.TeacherRoutes(t)
			a.Analytics.TeacherRoutes(t)
		})
		api.Group(func(s chi.Router) {
			s.Use(auth.RequireRole())
			a.Content.StudentRoutes(s)
			a.Admin.UserRoutes(s)
		})
		api.Group(func(s chi.Router) {
			s.Use(auth.RequireRole(auth.RoleStudent))
			a.Live.StudentRoutes(s)
			a.Eval.StudentRoutes(s)
		})
	})
	// WebSockets: the upgrade checks Origin itself; no CSRF header is possible.
	r.Route("/ws", func(ws chi.Router) {
		ws.Use(a.Auth.Middleware)
		a.Live.WSRoutes(ws)
	})
	r.Route("/beacon", func(b chi.Router) {
		b.Use(a.Auth.Middleware)
		a.Live.BeaconRoutes(b)
	})
	if cfg.StaticDir != "" {
		r.NotFound(httpx.SPA(cfg.StaticDir).ServeHTTP)
	}
	a.router = r
	return a, nil
}

func newMailer(cfg config.Config, log *slog.Logger) (mail.Sender, error) {
	if cfg.SMTPAddr == "" {
		return mail.LogSender{Log: log}, nil
	}
	s := mail.SMTPSender{Addr: cfg.SMTPAddr, From: cfg.SMTPFrom, Username: cfg.SMTPUser}
	if cfg.SMTPPasswordFile != "" {
		b, err := os.ReadFile(cfg.SMTPPasswordFile)
		if err != nil {
			return nil, err
		}
		s.Password = strings.TrimSpace(string(b))
	}
	return s, nil
}

func (a *App) Handler() http.Handler { return a.router }

// Start runs background workers and the live-session ticker.
func (a *App) Start(ctx context.Context) error {
	go a.Live.Run(ctx)
	return a.river.Start(ctx)
}

func (a *App) Close() {
	_ = a.river.Stop(context.Background())
	if a.ownPool {
		a.pool.Close()
	}
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	if err := a.pool.Ping(r.Context()); err != nil {
		httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db_unavailable"})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
