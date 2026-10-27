package live

import (
	"context"
	"time"
)

// Old quiz results (D-94, D-34, PL-FR-06): sessions that ended before a date
// go with everything recorded in them (attempts, answers, marks, events),
// through the admin's clean-up or the optional retention rule.

const oldSessions = `FROM live.sessions s WHERE s.status = 'ended' AND s.ended_at < $1`

// PreviewOldSessions counts the ended sessions before a date and roughly
// how much they take.
func (s *Service) PreviewOldSessions(ctx context.Context, before time.Time) (int, int64, error) {
	var n int
	var bytes int64
	err := s.pool.QueryRow(ctx, `SELECT count(*),
		coalesce(sum(pg_column_size(s.*)), 0) + coalesce((SELECT sum(pg_column_size(a.*)) FROM live.attempts a WHERE a.session_id IN (SELECT s.id `+oldSessions+`)), 0) `+oldSessions,
		before).Scan(&n, &bytes)
	return n, bytes, err
}

// DeleteOldSessions deletes them; their attempts and everything recorded
// about them go with them.
func (s *Service) DeleteOldSessions(ctx context.Context, before time.Time) (int, int64, error) {
	n, bytes, err := s.PreviewOldSessions(ctx, before)
	if err != nil || n == 0 {
		return 0, 0, err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM live.sessions s WHERE s.status = 'ended' AND s.ended_at < $1`, before)
	if err != nil {
		return 0, 0, err
	}
	return int(tag.RowsAffected()), bytes, nil
}
