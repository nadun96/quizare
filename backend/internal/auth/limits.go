package auth

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// SignInLimits are the admin's sign-in settings (D-126).
type SignInLimits struct {
	// One internet address: this many sign-ins at once, then this many a second.
	NetworkBurst  int     `json:"network_burst"`
	NetworkPerSec float64 `json:"network_per_sec"`
	// Passwords checked at a time; nil: the server's own setting (QP_ARGON2_WORKERS).
	PasswordWorkers *int `json:"password_workers"`
	// Read only: what that comes to now, and the server's own setting.
	Workers        int `json:"workers"`
	DefaultWorkers int `json:"default_workers"`
	CPUs           int `json:"cpus"`
}

// DefaultSignInLimits lets 500 students on one network sign in within 30 s.
var DefaultSignInLimits = SignInLimits{NetworkBurst: 600, NetworkPerSec: 20}

func (l *SignInLimits) check() error {
	f := map[string]string{}
	if l.NetworkBurst < 50 || l.NetworkBurst > 5000 {
		f["network_burst"] = "50 to 5,000"
	}
	if l.NetworkPerSec < 1 || l.NetworkPerSec > 200 {
		f["network_per_sec"] = "1 to 200 a second"
	}
	if l.PasswordWorkers != nil && (*l.PasswordWorkers < 1 || *l.PasswordWorkers > 8) {
		f["password_workers"] = "1 to 8"
	}
	if len(f) > 0 {
		return httpx.Invalid(f)
	}
	return nil
}

func (s *Service) apply(l SignInLimits) {
	s.ipLimiter.SetRate(l.NetworkBurst, time.Duration(float64(time.Second)/l.NetworkPerSec))
	if l.PasswordWorkers != nil {
		s.hasher.SetWorkers(*l.PasswordWorkers)
	} else {
		s.hasher.SetWorkers(s.defaultWorkers)
	}
}

// LoadSignInLimits applies the saved limits; the server calls it at start-up.
func (s *Service) LoadSignInLimits(ctx context.Context) error {
	l, err := s.SignInLimits(ctx)
	if err != nil {
		return err
	}
	s.apply(l)
	return nil
}

// SignInLimits are the saved limits, with what they come to.
func (s *Service) SignInLimits(ctx context.Context) (SignInLimits, error) {
	var l SignInLimits
	err := s.pool.QueryRow(ctx, `SELECT network_burst, network_per_sec::float8, password_workers FROM auth.policy`).Scan(&l.NetworkBurst, &l.NetworkPerSec, &l.PasswordWorkers)
	if err != nil {
		return l, err
	}
	l.DefaultWorkers, l.CPUs = s.defaultWorkers, numCPU()
	l.Workers = s.defaultWorkers
	if l.PasswordWorkers != nil {
		l.Workers = *l.PasswordWorkers
	}
	return l, nil
}

// SetSignInLimits saves and applies new limits at once.
func (s *Service) SetSignInLimits(ctx context.Context, actorID string, l SignInLimits) (SignInLimits, error) {
	if err := l.check(); err != nil {
		return l, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE auth.policy SET network_burst=$1, network_per_sec=$2, password_workers=$3`, l.NetworkBurst, l.NetworkPerSec, l.PasswordWorkers); err != nil {
			return err
		}
		return audit.Log(ctx, tx, actorID, "sign_in_limits_changed", "policy", "auth", map[string]any{
			"network_burst": l.NetworkBurst, "network_per_sec": l.NetworkPerSec, "password_workers": l.PasswordWorkers})
	})
	if err != nil {
		return l, err
	}
	saved, err := s.SignInLimits(ctx)
	if err != nil {
		return saved, err
	}
	s.apply(saved)
	return saved, nil
}

func (s *Service) handleGetSignInLimits(w http.ResponseWriter, r *http.Request) error {
	l, err := s.SignInLimits(r.Context())
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, l)
	return nil
}

func (s *Service) handleSetSignInLimits(w http.ResponseWriter, r *http.Request) error {
	var l SignInLimits
	if err := httpx.Decode(w, r, &l); err != nil {
		return err
	}
	saved, err := s.SetSignInLimits(r.Context(), MustUser(r.Context()).ID, l)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, saved)
	return nil
}

func numCPU() int { return runtime.NumCPU() }
