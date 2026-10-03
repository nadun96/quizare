package config

import "testing"

func TestFromEnvRequiresDatabaseAndKEKFile(t *testing.T) {
	t.Setenv("QP_DATABASE_URL", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error without QP_DATABASE_URL")
	}
	t.Setenv("QP_DATABASE_URL", "postgres://x")
	t.Setenv("QP_KEK_FILE", "")
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error without a KEK file: keys must not come from env vars")
	}
}

func TestFromEnvUsesSystemdCredentials(t *testing.T) {
	t.Setenv("QP_DATABASE_URL", "postgres://x")
	t.Setenv("QP_KEK_FILE", "")
	t.Setenv("CREDENTIALS_DIRECTORY", "/run/credentials/quiz.service")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.KEKFile != "/run/credentials/quiz.service/kek" {
		t.Fatalf("KEKFile = %q", c.KEKFile)
	}
	if c.DBMaxConns != 15 || c.Argon2Workers != 2 {
		t.Fatalf("defaults must match the memory budget, got pool=%d argon=%d", c.DBMaxConns, c.Argon2Workers)
	}
}
