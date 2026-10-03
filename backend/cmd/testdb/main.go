// Command testdb runs one embedded PostgreSQL 16 for local test runs so test
// packages share a server instead of each starting their own:
//
//	go run ./cmd/testdb            # leave running in a terminal
//	QP_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:54329/postgres?sslmode=disable go test ./...
package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

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
	fmt.Println("QP_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:54329/postgres?sslmode=disable")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	_ = pg.Stop()
}
