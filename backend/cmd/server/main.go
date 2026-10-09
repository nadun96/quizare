// Command server is the single Go binary of the QR Classroom Quiz Platform
// (ADR-01: modular monolith).
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nadun96/quizplatform/internal/app"
	"github.com/nadun96/quizplatform/internal/platform/config"
	"github.com/nadun96/quizplatform/internal/platform/db"
	"github.com/nadun96/quizplatform/internal/storage"
	"github.com/nadun96/quizplatform/migrations"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck()) // needs no config or database
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if len(os.Args) > 1 && (os.Args[1] == "check-backup" || os.Args[1] == "restore-backup") {
		if err := backupCommand(os.Args[1], os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
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

// backupCommand checks or restores a backup made from the admin console
// (PL-FR-07, D-57). The passphrase is read from stdin:
//
//	server check-backup qp-….qpbackup.age < passphrase.txt
//	QP_DATABASE_URL=… QP_KEK_FILE=… server restore-backup qp-….qpbackup.age < passphrase.txt
//
// Restoring needs a new, empty database; the next normal start applies any
// newer migrations.
func backupCommand(cmd string, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: server %s <backup file>  (passphrase on stdin)", cmd)
	}
	pass, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && pass == "" {
		return errors.New("enter the backup's passphrase on stdin")
	}
	f, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer f.Close()
	r, err := storage.Open(f, strings.TrimRight(pass, "\r\n"))
	if err != nil {
		return err
	}
	var m storage.Manifest
	var counts map[string]int64
	if cmd == "check-backup" {
		m, counts, err = storage.Check(r)
	} else {
		url := os.Getenv("QP_DATABASE_URL")
		if url == "" {
			return errors.New("set QP_DATABASE_URL to the new, empty database")
		}
		ctx := context.Background()
		pool, err2 := db.Open(ctx, url, 2)
		if err2 != nil {
			return err2
		}
		defer pool.Close()
		m, counts, err = storage.Restore(ctx, pool, r, migrations.FS)
	}
	if err != nil {
		return err
	}
	var rows int64
	for _, n := range counts {
		rows += n
	}
	fmt.Printf("ok: backup of %s, %d tables, %d rows, schema %s\n", m.CreatedAt.Format(time.RFC3339), len(m.Tables), rows, m.Migrations[len(m.Migrations)-1])
	if kf := os.Getenv("QP_KEK_FILE"); kf != "" && m.KEKID != "" {
		if kek, err := os.ReadFile(kf); err == nil && storage.KEKID(kek) != m.KEKID {
			fmt.Println("warning: QP_KEK_FILE is not the master key this backup was made with; stored AI keys won't decrypt until you use the original key file")
		}
	} else if m.KEKID != "" {
		fmt.Println("reminder: start the server with the master key file this backup was made with, or stored AI keys won't decrypt")
	}
	return nil
}
