// Command server is the single Go binary of the QR Classroom Quiz Platform
// (ADR-01: modular monolith).
package main

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
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

	// Bootstrap: `server create-admin <email> <name>` reads the password from
	// stdin. There is no HTTP path to create an admin.
	if len(os.Args) > 1 && os.Args[1] == "create-admin" {
		if len(os.Args) != 4 {
			return errors.New("usage: server create-admin <email> <name>  (password on stdin)")
		}
		pw, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && pw == "" {
			return err
		}
		u, err := a.Auth.CreateAdmin(ctx, os.Args[2], os.Args[3], strings.TrimRight(pw, "\r\n"))
		if err != nil {
			return err
		}
		logger.Info("admin created", "id", u.ID, "email", u.Email)
		return nil
	}

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
