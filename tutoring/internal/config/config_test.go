package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingSettingsAreNamed(t *testing.T) {
	for _, k := range []string{"TUTOR_DATABASE_URL", "TUTOR_PLATFORM_URL", "TUTOR_PLATFORM_ORIGIN", "LIVEKIT_PUBLIC_URL", "LIVEKIT_API_KEY", "TUTOR_SECRET_FILE", "LIVEKIT_API_SECRET_FILE"} {
		t.Setenv(k, "")
	}
	_, err := FromEnv()
	if err == nil || !strings.Contains(err.Error(), "LIVEKIT_API_KEY") || !strings.Contains(err.Error(), "TUTOR_SECRET_FILE") {
		t.Fatalf("err = %v", err)
	}
}

func TestKeysAndGeneratedLiveKitKeys(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "tutoring.secret")
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	if err := os.WriteFile(secret, []byte(hex.EncodeToString(key)+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadKey(secret); err != nil || string(got) != string(key) {
		t.Fatalf("hex key: %v", err)
	}
	short := filepath.Join(dir, "short")
	_ = os.WriteFile(short, []byte("abc"), 0o400)
	if _, err := LoadKey(short); err == nil {
		t.Fatal("short key accepted")
	}

	lk := filepath.Join(dir, "lk", "keys.yaml")
	t.Setenv("TUTOR_DATABASE_URL", "postgres://x")
	t.Setenv("TUTOR_PLATFORM_URL", "http://app:8080/")
	t.Setenv("TUTOR_PLATFORM_ORIGIN", "https://quiz.example.edu")
	t.Setenv("LIVEKIT_PUBLIC_URL", "wss://tutor.example.edu")
	t.Setenv("LIVEKIT_API_KEY", "tutoring")
	t.Setenv("TUTOR_SECRET_FILE", secret)
	t.Setenv("LIVEKIT_API_SECRET_FILE", lk)
	t.Setenv("TUTOR_GENERATE_LIVEKIT_KEYS", "1")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(lk)
	if !strings.HasPrefix(string(raw), "tutoring: ") || len(c.LiveKitSecret) < 32 || strings.Contains(string(c.LiveKitSecret), ":") {
		t.Fatalf("key file %q, secret %q", raw, c.LiveKitSecret)
	}
	if c.PlatformURL != "http://app:8080" || c.Listen != "127.0.0.1:8090" {
		t.Fatalf("config = %+v", c)
	}
	// Never overwritten.
	again, err := FromEnv()
	if err != nil || string(again.LiveKitSecret) != string(c.LiveKitSecret) {
		t.Fatalf("second start: %v", err)
	}
	// A plain secret file works too.
	plain := filepath.Join(dir, "plain")
	_ = os.WriteFile(plain, []byte(strings.Repeat("s", 40)), 0o400)
	if got, err := loadText(plain, "tutoring"); err != nil || len(got) != 40 {
		t.Fatalf("plain secret: %v", err)
	}
}
