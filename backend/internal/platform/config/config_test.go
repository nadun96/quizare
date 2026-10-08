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

func TestFromEnvTrustedProxies(t *testing.T) {
	t.Setenv("QP_DATABASE_URL", "postgres://x")
	t.Setenv("QP_KEK_FILE", "/k")
	t.Setenv("QP_TRUSTED_PROXIES", " 172.30.0.10, ,10.0.0.0/8 ")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.TrustedProxies) != 2 || c.TrustedProxies[0] != "172.30.0.10" || c.TrustedProxies[1] != "10.0.0.0/8" {
		t.Fatalf("TrustedProxies = %q", c.TrustedProxies)
	}
	t.Setenv("QP_TRUSTED_PROXIES", "caddy")
	if _, err := FromEnv(); err == nil {
		t.Fatal("a name instead of an IP must fail at startup, not silently trust nothing")
	}
}
