// Command tutor is the tutoring service (ADR-18, D-59): a separate program
// from the quiz platform, with its own schema, that runs tutoring sessions
// with LiveKit as the media server (D-58).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/config"
	"github.com/nadun96/quizplatform/tutoring/internal/db"
	"github.com/nadun96/quizplatform/tutoring/internal/media"
	"github.com/nadun96/quizplatform/tutoring/internal/platform"
	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
	"github.com/nadun96/quizplatform/tutoring/migrations"
)

// Version is set at build time (-ldflags "-X main.Version=...").
var Version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pool, err := db.Open(ctx, cfg.DatabaseURL, 10, 90*time.Second)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		return err
	}
	keys := authn.Keys{Secret: cfg.Secret}
	svc := tutor.New(pool, platform.New(cfg.PlatformURL, keys), media.New(cfg.LiveKitURL, cfg.LiveKitKey, cfg.LiveKitSecret), cfg.LiveKitPublicURL, log)
	// Visits left open by a restart are closed; people reconnect (TS-NFR-11).
	if err := svc.CloseOpenVisits(ctx); err != nil {
		return err
	}
	srv := &http.Server{Addr: cfg.Listen, Handler: svc.Handler(keys, cfg.PlatformOrigin, pool), ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Listen, "version", Version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	sctx, c2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer c2()
	return srv.Shutdown(sctx)
}

// healthcheck probes /healthz, for the container's HEALTHCHECK (no shell in distroless).
func healthcheck() int {
	addr := os.Getenv("TUTOR_LISTEN")
	if addr == "" {
		addr = "127.0.0.1:8090"
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Get("http://" + addr + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
