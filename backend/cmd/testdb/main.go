// Command testdb runs one embedded PostgreSQL 16 on port 54329 for local
// development: test packages share it instead of each starting their own,
// and it also provides a qp_dev database for running the app.
//
//	go run ./cmd/testdb            # leave running in a terminal
//	QP_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:54329/postgres?sslmode=disable go test ./...
//	QP_DATABASE_URL=postgres://postgres:postgres@localhost:54329/qp_dev?sslmode=disable go run ./cmd/server
//
// It runs with fsync off: fast, but a crash can lose recent dev data.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5"
)

const (
	adminURL = "postgres://postgres:postgres@localhost:54329/postgres?sslmode=disable"
	devDB    = "qp_dev"
)

var identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// ensureDatabase creates the database if it doesn't exist yet.
func ensureDatabase(ctx context.Context, url, name string) error {
	if !identRe.MatchString(name) {
		return fmt.Errorf("invalid database name %q", name)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&exists); err != nil || exists {
		return err
	}
	_, err = conn.Exec(ctx, "CREATE DATABASE "+name)
	return err
}

func main() {
	cache, _ := os.UserCacheDir()
	base := filepath.Join(cache, "quizplatform-pg")
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).Port(54329).
		CachePath(base).
		RuntimePath(filepath.Join(base, "runtime")).
		DataPath(filepath.Join(base, "data")).
		StartParameters(map[string]string{"fsync": "off", "max_connections": "300"}))
	if err := pg.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "start:", err)
		os.Exit(1)
	}
	if err := ensureDatabase(context.Background(), adminURL, devDB); err != nil {
		fmt.Fprintln(os.Stderr, "create "+devDB+":", err)
	}
	fmt.Println("QP_TEST_DATABASE_URL=" + adminURL)
	fmt.Println("QP_DATABASE_URL=postgres://postgres:postgres@localhost:54329/" + devDB + "?sslmode=disable")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	_ = pg.Stop()
}
