// Package config loads process configuration from the environment.
//
// Secrets are never read from environment variables (ADR-09): the master key
// (KEK) is read from a permission-restricted file whose path is given here.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddr    string // e.g. "127.0.0.1:8080" (Caddy proxies to it)
	DatabaseURL   string
	BaseURL       string // public origin, e.g. "https://quiz.example.edu"; used for QR links and Origin checks
	KEKFile       string // path to the 32-byte master key file (mode 0400)
	DBMaxConns    int32  // pgx pool size; architecture budget is 15
	Argon2Workers int    // bounded hashing pool; architecture budget is 2
	DevMode       bool   // relaxes nothing security-critical; enables verbose logs and console email
	ShutdownWait  time.Duration
	StaticDir     string // optional: serve the SPA build from Go when Caddy is not in front (dev only)
}

func FromEnv() (Config, error) {
	c := Config{
		ListenAddr:    env("QP_LISTEN", "127.0.0.1:8080"),
		DatabaseURL:   os.Getenv("QP_DATABASE_URL"),
		BaseURL:       env("QP_BASE_URL", "http://localhost:8080"),
		KEKFile:       os.Getenv("QP_KEK_FILE"),
		DBMaxConns:    int32(envInt("QP_DB_MAX_CONNS", 15)),
		Argon2Workers: envInt("QP_ARGON2_WORKERS", 2),
		DevMode:       os.Getenv("QP_DEV") == "1",
		ShutdownWait:  10 * time.Second,
		StaticDir:     os.Getenv("QP_STATIC_DIR"),
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
