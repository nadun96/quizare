// Package app is the composition root: it wires modules together and builds
// the HTTP router. Modules depend only on interfaces; this is the one place
// that knows every concrete type.
package app

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/nadun96/quizplatform/internal/platform/config"
	"github.com/nadun96/quizplatform/internal/platform/db"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
	"github.com/nadun96/quizplatform/migrations"
)

type App struct {
	cfg     config.Config
	log     *slog.Logger
	pool    *pgxpool.Pool
	river   *river.Client[pgx.Tx]
	workers *river.Workers
	router  chi.Router
	ownPool bool
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
	a, err := Build(cfg, log, pool, true)
	if err != nil {
		pool.Close()
		return nil, err
	}
	a.ownPool = true
	return a, nil
}

// Build wires modules onto an existing pool. runJobs=false gives an
// insert-only River client (tests drive workers explicitly).
func Build(cfg config.Config, log *slog.Logger, pool *pgxpool.Pool, runJobs bool) (*App, error) {
	a := &App{cfg: cfg, log: log, pool: pool, workers: river.NewWorkers()}
	r := chi.NewRouter()
	r.Use(httpx.Recover, httpx.SecurityHeaders)
	r.Get("/healthz", a.health)
	a.router = r

	var workers *river.Workers
	if runJobs {
		workers = a.workers
	}
	rc, err := jobs.NewClient(pool, workers, log)
	if err != nil {
		return nil, err
	}
	a.river = rc
	return a, nil
}

func (a *App) Handler() http.Handler { return a.router }

// Start runs background workers.
func (a *App) Start(ctx context.Context) error { return a.river.Start(ctx) }

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
