// Package config reads the tutoring service's settings from the environment.
// Secrets come from files, never from environment variables (ADR-09).
package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
)

type Config struct {
	Listen      string // TUTOR_LISTEN, default 127.0.0.1:8090
	DatabaseURL string // TUTOR_DATABASE_URL
	// Secret is shared with the platform (its QP_TUTORING_SECRET_FILE): it
	// signs people's tokens and this service's calls (TUTOR_SECRET_FILE).
	Secret []byte
	// PlatformURL is the platform on the server's network, for its internal
	// API (TUTOR_PLATFORM_URL); PlatformOrigin is where its pages come from,
	// the only origin allowed to call this service (TUTOR_PLATFORM_ORIGIN).
	PlatformURL    string
	PlatformOrigin string
	// LiveKit (D-58): its server API on the server's network (LIVEKIT_URL),
	// the address browsers connect to (LIVEKIT_PUBLIC_URL), and its API key
	// and secret (LIVEKIT_API_KEY, LIVEKIT_API_SECRET_FILE).
	LiveKitURL       string
	LiveKitPublicURL string
	LiveKitKey       string
	LiveKitSecret    []byte
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func FromEnv() (Config, error) {
	c := Config{
		Listen:           env("TUTOR_LISTEN", "127.0.0.1:8090"),
		DatabaseURL:      os.Getenv("TUTOR_DATABASE_URL"),
		PlatformURL:      strings.TrimRight(os.Getenv("TUTOR_PLATFORM_URL"), "/"),
		PlatformOrigin:   strings.TrimRight(os.Getenv("TUTOR_PLATFORM_ORIGIN"), "/"),
		LiveKitURL:       env("LIVEKIT_URL", "http://127.0.0.1:7880"),
		LiveKitPublicURL: os.Getenv("LIVEKIT_PUBLIC_URL"),
		LiveKitKey:       os.Getenv("LIVEKIT_API_KEY"),
	}
	var missing []string
	for k, v := range map[string]string{"TUTOR_DATABASE_URL": c.DatabaseURL, "TUTOR_PLATFORM_URL": c.PlatformURL, "TUTOR_PLATFORM_ORIGIN": c.PlatformOrigin,
		"LIVEKIT_PUBLIC_URL": c.LiveKitPublicURL, "LIVEKIT_API_KEY": c.LiveKitKey, "TUTOR_SECRET_FILE": os.Getenv("TUTOR_SECRET_FILE"),
		"LIVEKIT_API_SECRET_FILE": os.Getenv("LIVEKIT_API_SECRET_FILE")} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return c, fmt.Errorf("set %s", strings.Join(missing, ", "))
	}
	var err error
	if c.Secret, err = LoadKey(os.Getenv("TUTOR_SECRET_FILE")); err != nil {
		return c, fmt.Errorf("TUTOR_SECRET_FILE: %w", err)
	}
	if c.LiveKitSecret, err = loadText(os.Getenv("LIVEKIT_API_SECRET_FILE")); err != nil {
		return c, fmt.Errorf("LIVEKIT_API_SECRET_FILE: %w", err)
	}
	return c, nil
}

func checkPerm(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s must not be readable by group or others (chmod 0400)", path)
	}
	return nil
}

// LoadKey reads a 32-byte key: raw, 64 hex characters or base64, as the
// platform's key files are.
func LoadKey(path string) ([]byte, error) {
	if err := checkPerm(path); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) == 32 {
		return raw, nil
	}
	s := strings.TrimSpace(string(raw))
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, errors.New("the file must hold 32 raw bytes, 64 hex characters or base64 of 32 bytes")
}

// loadText reads a secret as text (LiveKit's API secret is a string).
func loadText(path string) ([]byte, error) {
	if err := checkPerm(path); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := strings.TrimSpace(string(raw))
	if len(s) < 32 {
		return nil, errors.New("use a secret of at least 32 characters")
	}
	return []byte(s), nil
}
