// Package dbtest provides a real PostgreSQL for integration tests.
//
// If QP_TEST_DATABASE_URL is set it is used (e.g. CI with a service
// container). Otherwise an embedded PostgreSQL 16 is started once per test
// binary on a free port. Each test gets a fresh database cloned from a
// migrated template, so tests are isolated and fast.
package dbtest

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/internal/platform/db"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
	"github.com/nadun96/quizplatform/migrations"
)

var (
	once     sync.Once
	baseURL  string // server URL pointing at the maintenance database
	setupErr error
	stop     func()
	counter  atomic.Int64
)

const template = "qp_template"

// Main wraps testing.M so the embedded server is stopped after the run.
// Use from TestMain: func TestMain(m *testing.M) { dbtest.Main(m) }
func Main(m *testing.M) {
	code := m.Run()
	if stop != nil {
		stop()
	}
	os.Exit(code)
}

func setup() {
	if u := os.Getenv("QP_TEST_DATABASE_URL"); u != "" {
		baseURL = u
	} else {
		port, err := freePort()
		if err != nil {
			setupErr = err
			return
		}
		cache, _ := os.UserCacheDir()
		rt, err := os.MkdirTemp("", "qp-pg-")
		if err != nil {
			setupErr = err
			return
		}
		pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
			Version(embeddedpostgres.V16).Port(uint32(port)).
			CachePath(filepath.Join(cache, "quizplatform-pg")).
			RuntimePath(rt).Logger(io.Discard).
			StartParameters(map[string]string{"fsync": "off", "max_connections": "200"}))
		if err := pg.Start(); err != nil {
			setupErr = fmt.Errorf("start embedded postgres: %w", err)
			return
		}
		stop = func() { _ = pg.Stop(); _ = os.RemoveAll(rt) }
		baseURL = fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", port)
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		setupErr = err
		return
	}
	defer conn.Close(ctx)
	tmpl := fmt.Sprintf("%s_%d", template, os.Getpid())
	_, _ = conn.Exec(ctx, "DROP DATABASE IF EXISTS "+tmpl)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+tmpl); err != nil {
		setupErr = err
		return
	}
	pool, err := db.Open(ctx, withDB(baseURL, tmpl), 2)
	if err != nil {
		setupErr = err
		return
	}
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		setupErr = err
	} else {
		setupErr = jobs.Migrate(ctx, pool)
	}
	pool.Close()
	templateName = tmpl
}

var templateName string

// New returns a pool connected to a fresh, fully migrated database.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	once.Do(setup)
	if setupErr != nil {
		t.Fatalf("dbtest setup: %v", setupErr)
	}
	ctx := context.Background()
	name := fmt.Sprintf("qp_test_%d_%d", os.Getpid(), counter.Add(1))
	conn, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, templateName)); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)
	pool, err := db.Open(ctx, withDB(baseURL, name), 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if c, err := pgx.Connect(ctx, baseURL); err == nil {
			_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			c.Close(ctx)
		}
	})
	return pool
}

func withDB(raw, name string) string {
	u, _ := url.Parse(raw)
	u.Path = "/" + name
	return u.String()
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
