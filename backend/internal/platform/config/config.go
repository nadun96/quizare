// Package config loads process configuration from the environment.
//
// Secrets are never read from environment variables (ADR-09): the master key
// (KEK) is read from a permission-restricted file whose path is given here.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

type Config struct {
	ListenAddr  string // e.g. "127.0.0.1:8080" (Caddy proxies to it)
	DatabaseURL string
	BaseURL     string // public origin, e.g. "https://quiz.example.edu"; used for QR links and Origin checks
	KEKFile     string // path to the 32-byte master key file (mode 0400)
	// BackupDir holds backups made from the admin console (QP_BACKUP_DIR,
	// default data/backups); NightlyBackupDir is where deploy/backup.sh
	// writes (QP_NIGHTLY_BACKUP_DIR; empty = no nightly job to report on).
	BackupDir        string
	NightlyBackupDir string
	// Tutoring (ADR-18, D-59): the tutoring service's public address
	// (QP_TUTORING_URL) and the secret file shared with it
	// (QP_TUTORING_SECRET_FILE). Without both, tutoring is off.
	TutoringURL        string
	TutoringSecretFile string
	// TutoringSecretGenerate creates the shared secret file on first start
	// (containers; QP_TUTORING_SECRET_GENERATE=1), as QP_KEK_GENERATE does.
	TutoringSecretGenerate bool
	DBMaxConns             int32 // pgx pool size; architecture budget is 15
	Argon2Workers          int   // bounded hashing pool; architecture budget is 2
	DevMode                bool  // relaxes nothing security-critical; enables verbose logs and console email
	ShutdownWait           time.Duration
	StaticDir              string // optional: serve the SPA build from Go when Caddy is not in front (dev only)

	// TrustedProxies are proxy IPs or CIDRs, besides loopback, whose
	// X-Forwarded-For is believed (QP_TRUSTED_PROXIES, comma-separated), e.g.
	// Caddy's container address in compose (D-51).
	TrustedProxies []string

	APIDocs bool // serve the OpenAPI spec and Swagger UI at /api/docs (QP_API_DOCS=0 disables)

	KEKGenerate bool          // create the KEK file on first start if missing (containers; QP_KEK_GENERATE=1)
	DBWait      time.Duration // keep retrying the database for this long at startup (QP_DB_WAIT_SECONDS, default 60)

	SMTPAddr, SMTPFrom, SMTPUser string
	SMTPPasswordFile             string // secret read from a file, like the KEK
}

func FromEnv() (Config, error) {
	c := Config{
		ListenAddr:             env("QP_LISTEN", "127.0.0.1:8080"),
		DatabaseURL:            os.Getenv("QP_DATABASE_URL"),
		BaseURL:                env("QP_BASE_URL", "http://localhost:8080"),
		KEKFile:                os.Getenv("QP_KEK_FILE"),
		BackupDir:              env("QP_BACKUP_DIR", "data/backups"),
		NightlyBackupDir:       os.Getenv("QP_NIGHTLY_BACKUP_DIR"),
		TutoringURL:            os.Getenv("QP_TUTORING_URL"),
		TutoringSecretFile:     os.Getenv("QP_TUTORING_SECRET_FILE"),
		TutoringSecretGenerate: os.Getenv("QP_TUTORING_SECRET_GENERATE") == "1",
		DBMaxConns:             int32(envInt("QP_DB_MAX_CONNS", 15)),
		Argon2Workers:          envInt("QP_ARGON2_WORKERS", 2),
		DevMode:                os.Getenv("QP_DEV") == "1",
		ShutdownWait:           10 * time.Second,
		StaticDir:              os.Getenv("QP_STATIC_DIR"),

		TrustedProxies: list(os.Getenv("QP_TRUSTED_PROXIES")),

		APIDocs:          os.Getenv("QP_API_DOCS") != "0",
		KEKGenerate:      os.Getenv("QP_KEK_GENERATE") == "1",
		DBWait:           time.Duration(envInt("QP_DB_WAIT_SECONDS", 60)) * time.Second,
		SMTPAddr:         os.Getenv("QP_SMTP_ADDR"),
		SMTPFrom:         env("QP_SMTP_FROM", "no-reply@localhost"),
		SMTPUser:         os.Getenv("QP_SMTP_USER"),
		SMTPPasswordFile: os.Getenv("QP_SMTP_PASSWORD_FILE"),
	}
	if _, err := httpx.ParseProxies(c.TrustedProxies); err != nil {
		return c, fmt.Errorf("QP_TRUSTED_PROXIES: %w", err)
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("QP_DATABASE_URL is required")
	}
	if c.KEKFile == "" {
		// systemd LoadCredential= exposes credentials under $CREDENTIALS_DIRECTORY.
		if dir := os.Getenv("CREDENTIALS_DIRECTORY"); dir != "" {
			c.KEKFile = dir + "/kek"
		} else {
			return c, fmt.Errorf("QP_KEK_FILE is required (path to the master key file)")
		}
	}
	return c, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil && v > 0 {
		return v
	}
	return def
}

func list(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
