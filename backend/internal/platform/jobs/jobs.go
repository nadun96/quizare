// Package jobs wraps River, the Postgres-backed job queue (ADR-06).
//
// Jobs are inserted in the same transaction as the state change that causes
// them, so they are never lost and never run before that change commits.
package jobs

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
)

// Queue names and worker caps from the architecture memory/CPU budget (§2, §5).
const (
	QueueLLM       = "llm_marking" // network-bound, capped to protect teachers' rate limits
	QueueAnalytics = "analytics"
	QueueEmail     = "email"
	QueueDefault   = river.QueueDefault
)

var queueConfig = map[string]river.QueueConfig{
	QueueLLM:       {MaxWorkers: 4},
	QueueAnalytics: {MaxWorkers: 1},
	QueueEmail:     {MaxWorkers: 2},
	QueueDefault:   {MaxWorkers: 2},
}

// Inserter is what modules depend on to enqueue work transactionally.
// *river.Client[pgx.Tx] satisfies it.
type Inserter interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// Migrate creates or upgrades River's tables.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	_, err = m.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}

// NewClient builds a River client. With workers == nil the client is
// insert-only (useful in tests and for processes that must not run jobs).
func NewClient(pool *pgxpool.Pool, workers *river.Workers, logger *slog.Logger) (*river.Client[pgx.Tx], error) {
	cfg := &river.Config{Logger: logger}
	if workers != nil {
		cfg.Workers = workers
		cfg.Queues = queueConfig
	}
	return river.NewClient(riverpgxv5.New(pool), cfg)
}
