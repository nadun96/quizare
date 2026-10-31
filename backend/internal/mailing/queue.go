package mailing

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river/rivertype"

	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/page"
)

// The email queue (D-104): every email the platform sends, as the email
// worker sees it. "Sent" means the mail server took it; whether the
// recipient's provider then accepted it is in the mail server's own log.
// Only who and what about: bodies hold sign-in links and stay out of view.

// Statuses, from River's job states.
const (
	StatusWaiting  = "waiting"  // queued, or being sent
	StatusSent     = "sent"     // the mail server took it
	StatusRetrying = "retrying" // failed, will try again
	StatusFailed   = "failed"   // gave up after its attempts
)

// statusSQL turns river_job.state into a status.
const statusSQL = `CASE j.state WHEN 'completed' THEN 'sent' WHEN 'retryable' THEN 'retrying' WHEN 'scheduled' THEN 'retrying'
	WHEN 'discarded' THEN 'failed' WHEN 'cancelled' THEN 'failed' ELSE 'waiting' END`

// Retrier puts a failed job back in the queue; River's client is one.
type Retrier interface {
	JobRetry(ctx context.Context, id int64) (*rivertype.JobRow, error)
}

// SetRetrier lets the queue view retry failed emails.
func (s *Service) SetRetrier(r Retrier) { s.retry = r }

// Delivery is one email in the queue.
type Delivery struct {
	ID          int64      `json:"id"`
	To          string     `json:"to"`
	Subject     string     `json:"subject"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"max_attempts"`
	Error       string     `json:"error,omitempty"` // the last attempt's, from the mail server
	SendID      string     `json:"send_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	At          *time.Time `json:"at,omitempty"` // when it was sent or failed, or its next try
}

// Progress counts a send's or the queue's emails by status.
type Progress map[string]int

// QueueSorts are the queue's columns.
var QueueSorts = page.Sorts{"created": "j.created_at", "to": "j.args->>'to'"}

// Queue lists emails, newest first, of one status and one send if given.
func (s *Service) Queue(ctx context.Context, status, sendID string, p page.Request) ([]Delivery, int, Progress, error) {
	where := ` FROM river_job j WHERE j.kind = 'email' AND ($2 = '' OR j.args->>'send_id' = $2)
		AND ($3 = '' OR j.args->>'to' ILIKE $3 OR j.args->>'subject' ILIKE $3)`
	counts := Progress{StatusWaiting: 0, StatusSent: 0, StatusRetrying: 0, StatusFailed: 0}
	rows, err := s.pool.Query(ctx, `SELECT `+statusSQL+`, count(*) FROM river_job j WHERE j.kind = 'email' AND ($1 = '' OR j.args->>'send_id' = $1)
		AND ($2 = '' OR j.args->>'to' ILIKE $2 OR j.args->>'subject' ILIKE $2) GROUP BY 1`, sendID, p.Like())
	if err != nil {
		return nil, 0, nil, err
	}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			rows.Close()
			return nil, 0, nil, err
		}
		counts[st] += n
	}
	rows.Close()
	where += ` AND ($1 = '' OR ` + statusSQL + ` = $1)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, status, sendID, p.Like()).Scan(&total); err != nil {
		return nil, 0, nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT j.id, coalesce(j.args->>'to', ''), coalesce(j.args->>'subject', ''), `+statusSQL+`, j.attempt, j.max_attempts,
		coalesce(j.errors[array_length(j.errors, 1)]->>'error', ''), coalesce(j.args->>'send_id', ''), j.created_at,
		CASE WHEN j.state IN ('retryable', 'scheduled') THEN j.scheduled_at ELSE j.finalized_at END`+where+p.OrderBy(QueueSorts, "j.id")+p.Limit(),
		status, sendID, p.Like())
	if err != nil {
		return nil, 0, nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Delivery, error) {
		var d Delivery
		return d, r.Scan(&d.ID, &d.To, &d.Subject, &d.Status, &d.Attempts, &d.MaxAttempts, &d.Error, &d.SendID, &d.CreatedAt, &d.At)
	})
	if out == nil {
		out = []Delivery{}
	}
	return out, total, counts, err
}

// progress counts each send's emails still in the queue.
func (s *Service) progress(ctx context.Context, ids []string) (map[string]Progress, error) {
	out := map[string]Progress{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT j.args->>'send_id', `+statusSQL+`, count(*) FROM river_job j
		WHERE j.kind = 'email' AND j.args->>'send_id' = ANY($1) GROUP BY 1, 2`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, st string
		var n int
		if err := rows.Scan(&id, &st, &n); err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = Progress{}
		}
		out[id][st] = n
	}
	return out, rows.Err()
}

// Retry puts failed emails back in the queue: one, or all of a send's (or
// all) when id is 0.
func (s *Service) Retry(ctx context.Context, id int64, sendID string) (int, error) {
	if s.retry == nil {
		return 0, httpx.Conflict("retrying is not available here")
	}
	ids := []int64{id}
	if id == 0 {
		rows, err := s.pool.Query(ctx, `SELECT j.id FROM river_job j WHERE j.kind = 'email' AND j.state IN ('discarded', 'cancelled', 'retryable')
			AND ($1 = '' OR j.args->>'send_id' = $1) ORDER BY j.id LIMIT 5000`, sendID)
		if err != nil {
			return 0, err
		}
		if ids, err = pgx.CollectRows(rows, pgx.RowTo[int64]); err != nil {
			return 0, err
		}
	} else {
		var kind string
		if err := s.pool.QueryRow(ctx, `SELECT kind FROM river_job WHERE id = $1`, id).Scan(&kind); err != nil || kind != "email" {
			return 0, httpx.ErrNotFound
		}
	}
	n := 0
	for _, j := range ids {
		if _, err := s.retry.JobRetry(ctx, j); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
