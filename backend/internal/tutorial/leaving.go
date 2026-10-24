package tutorial

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Time on pages and leaving the app (D-75, PO-32, TU-FR-22 to TU-FR-24,
// TU-FR-34). The browser counts the seconds a page is on screen and in use,
// and says when the student left the app and came back. Like proctoring,
// these are evidence, not proof: a page can't stop anyone leaving.

const (
	maxReport = 60        // seconds one time report may add
	maxAway   = 24 * 3600 // seconds one leave may count
)

// current finds the run and page a report is about, when the student is on
// that page and can still answer; otherwise it isn't recorded.
func (s *Service) current(ctx context.Context, userID, id string, page int) (*run, bool, error) {
	t, err := s.student(ctx, userID, id)
	if err != nil {
		return nil, false, err
	}
	r, err := s.loadRun(ctx, t, userID, false)
	if errors.Is(err, httpx.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return r, page == r.current && page < len(r.pages) && !r.readOnly(s.now()), nil
}

// Time adds the seconds a student spent on the page they're on. A report
// adds at most a minute, and no more than has passed since the last one, so
// a page left open doesn't pile up time.
func (s *Service) Time(ctx context.Context, userID, id string, page, seconds int) error {
	r, ok, err := s.current(ctx, userID, id, page)
	if err != nil || !ok {
		return err
	}
	seconds = min(max(seconds, 0), maxReport)
	now := s.now()
	_, err = s.pool.Exec(ctx, `INSERT INTO tutorial.visits(run_id, page_id, first_at, last_at) VALUES ($1,$2,$4,$4)
		ON CONFLICT (run_id, page_id) DO UPDATE SET
			seconds = tutorial.visits.seconds + LEAST($3, GREATEST(0, ceil(extract(epoch FROM $4 - tutorial.visits.last_at))::int + 2)),
			last_at = $4`, r.id, r.pages[r.current].ID, seconds, now)
	return err
}

// LeaveInput reports one time the student left the app and came back.
type LeaveInput struct {
	Page        int  `json:"page"`
	AwaySeconds int  `json:"away_seconds"`
	ViaLink     bool `json:"via_link"` // through one of the page's own links
}

// Leave records that the student left the app from the page they're on.
// Leaving through the page's own links, or from a page that allows it,
// counts as allowed. On a page that doesn't allow it, enough leaves lock the
// page until the teacher unlocks it.
func (s *Service) Leave(ctx context.Context, userID, id string, in LeaveInput) (RunView, error) {
	r, ok, err := s.current(ctx, userID, id, in.Page)
	if err != nil {
		return RunView{}, err
	}
	if r == nil {
		return RunView{}, httpx.ErrNotFound
	}
	if !ok {
		return s.view(r, r.current), nil
	}
	p := r.pages[r.current]
	viaLink := in.ViaLink && slices.ContainsFunc(p.Blocks, func(b Block) bool { return b.Type == "link" })
	away := min(max(in.AwaySeconds, 0), maxAway)
	now := s.now()
	if p.Leaving == LeaveAllowed || viaLink {
		_, err = s.pool.Exec(ctx, `INSERT INTO tutorial.visits(run_id, page_id, allowed_leaves, away_seconds, first_at, last_at) VALUES ($1,$2,1,$3,$4,$4)
			ON CONFLICT (run_id, page_id) DO UPDATE SET allowed_leaves = tutorial.visits.allowed_leaves + 1,
				away_seconds = tutorial.visits.away_seconds + $3, last_at = $4`, r.id, p.ID, away, now)
	} else {
		lockAfter := 0
		if p.Leaving == LeaveBlocked {
			lockAfter = p.LockAfter
		}
		_, err = s.pool.Exec(ctx, `INSERT INTO tutorial.visits(run_id, page_id, leaves, away_seconds, locked_at, first_at, last_at)
			VALUES ($1,$2,1,$3, CASE WHEN $5 = 1 THEN $4::timestamptz END, $4, $4)
			ON CONFLICT (run_id, page_id) DO UPDATE SET leaves = tutorial.visits.leaves + 1,
				away_seconds = tutorial.visits.away_seconds + $3, last_at = $4,
				locked_at = CASE WHEN $5 > 0 AND tutorial.visits.leaves + 1 - tutorial.visits.forgiven >= $5
					THEN coalesce(tutorial.visits.locked_at, $4) ELSE tutorial.visits.locked_at END`, r.id, p.ID, away, now, lockAfter)
	}
	if err != nil {
		return RunView{}, err
	}
	if err := s.loadVisits(ctx, r); err != nil {
		return RunView{}, err
	}
	return s.view(r, r.current), nil
}

// Unlock lets a student carry on with a page locked for leaving the app.
// Their earlier leaves stay on record, but no longer count toward the lock.
func (s *Service) Unlock(ctx context.Context, teacherID, id, runID, pageID string) (RunDetail, error) {
	if _, _, _, err := s.ownedRun(ctx, teacherID, id, runID); err != nil {
		return RunDetail{}, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE tutorial.visits SET locked_at=NULL, forgiven=leaves WHERE run_id=$1 AND page_id=$2`, runID, pageID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit.Log(ctx, tx, teacherID, "tutorial_page_unlocked", "tutorial_run", runID, map[string]string{"page": pageID})
	})
	if err != nil {
		return RunDetail{}, err
	}
	return s.Run(ctx, teacherID, id, runID)
}
