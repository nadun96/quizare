// Command server is the single Go binary of the QR Classroom Quiz Platform
// (ADR-01: modular monolith).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/nadun96/quizplatform/internal/app"
	"github.com/nadun96/quizplatform/internal/platform/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	a, err := app.New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.Start(ctx); err != nil {
		return err
	}

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: a.Handler(), ReadHeaderTimeout: 10e9}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, c2 := context.WithTimeout(context.Background(), cfg.ShutdownWait)
	defer c2()
	return srv.Shutdown(shutdownCtx)
}
