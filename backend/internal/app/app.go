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

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/imageurl"
	"github.com/nadun96/quizplatform/internal/live"
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

	Auth     *auth.Service
	Settings *settings.Store
	Content  *content.Service
	Quiz     *quiz.Service
	Live     *live.Service
}

// Options let tests swap infrastructure.
type Options struct {
	RunJobs bool        // false: insert-only River client
	Mailer  mail.Sender // nil: chosen from config
	// Checker validates resource URLs; nil uses the SSRF-safe public checker.
	Checker *imageurl.Checker
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
	a, err := Build(cfg, log, pool, Options{RunJobs: true})
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

	r := chi.NewRouter()
	r.Use(httpx.Recover, httpx.SecurityHeaders)
	r.Get("/healthz", a.health)
	r.Route("/api", func(api chi.Router) {
		api.Use(httpx.SameOrigin(cfg.BaseURL), a.Auth.Middleware)
		api.Route("/auth", a.Auth.Routes)
		api.Route("/admin", func(ad chi.Router) {
			ad.Use(auth.RequireRole(auth.RoleAdmin))
			a.Auth.AdminRoutes(ad)
			ad.Route("/settings", a.Settings.AdminRoutes)
		})
		api.Route("/teacher", func(t chi.Router) {
			t.Use(auth.RequireRole(auth.RoleTeacher))
			t.Route("/settings", a.Settings.TeacherRoutes)
			a.Content.TeacherRoutes(t)
			a.Quiz.TeacherRoutes(t)
			a.Live.TeacherRoutes(t)
		})
		api.Group(func(s chi.Router) {
			s.Use(auth.RequireRole())
			a.Content.StudentRoutes(s)
		})
		api.Group(func(s chi.Router) {
			s.Use(auth.RequireRole(auth.RoleStudent))
			a.Live.StudentRoutes(s)
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
