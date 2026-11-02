// Package insight is multi-dimensional analysis (D-120): a small warehouse
// in schema olap, kept by a loader, and a query engine that answers "this
// measure, by these dimensions, filtered so" for any combination the
// catalog allows, within what the asker may see.
//
// The loader is the only code that reads other modules' tables, and only
// to copy; reports read olap alone, so they never load the tables live
// quizzes write to, and the warehouse could move to its own database.
package insight

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/nadun96/quizplatform/internal/platform/jobs"
)

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: time.Now}
}

// ---------- the job ----------

type LoadArgs struct{}

func (LoadArgs) Kind() string { return "insight_load" }
func (LoadArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueAnalytics, MaxAttempts: 3}
}

type LoadWorker struct {
	river.WorkerDefaults[LoadArgs]
	Service *Service
}

func (w *LoadWorker) Work(ctx context.Context, _ *river.Job[LoadArgs]) error {
	_, err := w.Service.Load(ctx)
	return err
}

// Periodic loads every 5 minutes, and once at start.
func Periodic() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(5*time.Minute), func() (river.JobArgs, *river.InsertOpts) { return LoadArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// LoadStats says what one run did.
type LoadStats struct {
	Results, Tutorials, Answers int64
	Reconciled                  bool
}

// Reconciling (finding facts whose source was deleted) reads every fact, so it runs once a day.
const reconcileEvery = 24 * time.Hour

// Load copies what changed since the last run: dimensions in full (they're
// small), facts by their sources' updated_at with a minute of overlap. The
// first run copies everything.
func (s *Service) Load(ctx context.Context) (LoadStats, error) {
	var st LoadStats
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return st, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO olap.load_state(last_at, reconciled) VALUES ('1970-01-01Z', '1970-01-01Z') ON CONFLICT DO NOTHING`); err != nil {
		return st, err
	}
	var since, reconciled, started time.Time
	// One run at a time: a second waits here, then finds little to do.
	if err := tx.QueryRow(ctx, `SELECT last_at, reconciled, now() FROM olap.load_state FOR UPDATE`).Scan(&since, &reconciled, &started); err != nil {
		return st, err
	}
	from := since.Add(-time.Minute)

	for _, q := range dimensionLoads {
		if _, err := tx.Exec(ctx, q); err != nil {
			return st, err
		}
	}
	exec := func(n *int64, q string, args ...any) error {
		tag, err := tx.Exec(ctx, q, args...)
		if n != nil {
			*n += tag.RowsAffected()
		}
		return err
	}
	steps := []struct {
		n *int64
		q string
	}{
		// Quiz results: complete and valid ones in, the others out.
		{&st.Results, `INSERT INTO olap.activity (kind, source_id, at, classroom_id, topic_id, item_id, session_id, user_id, pct, passed, minutes)
			SELECT 'quiz', r.attempt_id, coalesce(a.submitted_at, s.ended_at, a.created_at), s.classroom_id, q.topic_id, s.quiz_id, s.id, r.user_id,
				r.pct::float8, r.passed,
				CASE WHEN a.started_at IS NOT NULL AND a.submitted_at > a.started_at THEN extract(epoch FROM a.submitted_at - a.started_at) / 60 END
			FROM eval.results r JOIN live.attempts a ON a.id = r.attempt_id JOIN live.sessions s ON s.id = r.session_id
			LEFT JOIN quiz.quizzes q ON q.id = s.quiz_id
			WHERE r.updated_at >= $1 AND r.complete AND NOT r.invalidated
			ON CONFLICT (kind, source_id) DO UPDATE SET at = EXCLUDED.at, classroom_id = EXCLUDED.classroom_id, topic_id = EXCLUDED.topic_id,
				item_id = EXCLUDED.item_id, session_id = EXCLUDED.session_id, user_id = EXCLUDED.user_id, pct = EXCLUDED.pct,
				passed = EXCLUDED.passed, minutes = EXCLUDED.minutes`},
		{nil, `DELETE FROM olap.activity f USING eval.results r
			WHERE f.kind = 'quiz' AND f.source_id = r.attempt_id AND r.updated_at >= $1 AND (NOT r.complete OR r.invalidated)`},
		// Tutorials finished; a run restarted since leaves.
		{&st.Tutorials, `INSERT INTO olap.activity (kind, source_id, at, classroom_id, topic_id, item_id, session_id, user_id, pct, passed, minutes)
			SELECT 'tutorial', ru.id, ru.finished_at, m.classroom_id, tu.topic_id, tu.id, NULL, ru.user_id, NULL, NULL,
				(SELECT sum(v.seconds) / 60.0 FROM tutorial.visits v WHERE v.run_id = ru.id)
			FROM tutorial.runs ru JOIN tutorial.tutorials tu ON tu.id = ru.tutorial_id
			JOIN content.topics tp ON tp.id = tu.topic_id JOIN content.modules m ON m.id = tp.module_id
			WHERE ru.updated_at >= $1 AND ru.finished_at IS NOT NULL
			ON CONFLICT (kind, source_id) DO UPDATE SET at = EXCLUDED.at, classroom_id = EXCLUDED.classroom_id, topic_id = EXCLUDED.topic_id,
				item_id = EXCLUDED.item_id, user_id = EXCLUDED.user_id, minutes = EXCLUDED.minutes`},
		{nil, `DELETE FROM olap.activity f USING tutorial.runs ru
			WHERE f.kind = 'tutorial' AND f.source_id = ru.id AND ru.updated_at >= $1 AND ru.finished_at IS NULL`},
		// Each marked question of a changed result, again.
		{nil, `DELETE FROM olap.answers WHERE attempt_id IN (SELECT attempt_id FROM eval.results WHERE updated_at >= $1)`},
		{&st.Answers, `INSERT INTO olap.answers (attempt_id, question_id, at, classroom_id, topic_id, item_id, user_id, qtype, fraction, correct)
			SELECT mk.attempt_id, mk.question_id, f.at, f.classroom_id, f.topic_id, f.item_id, f.user_id, coalesce(qq.type, 'OTHER'),
				least(1, greatest(0, coalesce(mk.fraction::float8, mk.score::float8 / nullif(mk.max_score::float8, 0), 0))), coalesce(mk.correct, false)
			FROM eval.marks mk
			JOIN olap.activity f ON f.kind = 'quiz' AND f.source_id = mk.attempt_id
			LEFT JOIN quiz.questions qq ON qq.id = mk.question_id
			WHERE mk.attempt_id IN (SELECT attempt_id FROM eval.results WHERE updated_at >= $1) AND mk.status IN ('marked', 'overridden')`},
	}
	for _, x := range steps {
		if err := exec(x.n, x.q, from); err != nil {
			return st, err
		}
	}
	if started.Sub(reconciled) >= reconcileEvery {
		for _, q := range reconcile {
			if err := exec(nil, q); err != nil {
				return st, err
			}
		}
		st.Reconciled = true
		reconciled = started
	}
	if _, err := tx.Exec(ctx, `UPDATE olap.load_state SET last_at = $1, reconciled = $2`, started, reconciled); err != nil {
		return st, err
	}
	return st, tx.Commit(ctx)
}

// dimensionLoads refresh the names and trees, writing only rows that changed.
var dimensionLoads = []string{
	`INSERT INTO olap.classrooms (id, name, teacher_id, teacher_name, archived)
		SELECT c.id, c.name, c.teacher_id, u.name, c.archived_at IS NOT NULL FROM content.classrooms c JOIN auth.users u ON u.id = c.teacher_id
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, teacher_id = EXCLUDED.teacher_id, teacher_name = EXCLUDED.teacher_name, archived = EXCLUDED.archived
		WHERE (olap.classrooms.name, olap.classrooms.teacher_id, olap.classrooms.teacher_name, olap.classrooms.archived)
			IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.teacher_id, EXCLUDED.teacher_name, EXCLUDED.archived)`,
	`INSERT INTO olap.topics (id, name, module_id, module_name, classroom_id)
		SELECT t.id, t.name, m.id, m.name, m.classroom_id FROM content.topics t JOIN content.modules m ON m.id = t.module_id
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, module_id = EXCLUDED.module_id, module_name = EXCLUDED.module_name, classroom_id = EXCLUDED.classroom_id
		WHERE (olap.topics.name, olap.topics.module_id, olap.topics.module_name, olap.topics.classroom_id)
			IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.module_id, EXCLUDED.module_name, EXCLUDED.classroom_id)`,
	`INSERT INTO olap.items (id, kind, title, topic_id)
		SELECT id, 'quiz', title, topic_id FROM quiz.quizzes WHERE kind = 'quiz'
		UNION ALL SELECT id, 'tutorial', title, topic_id FROM tutorial.tutorials
		ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, topic_id = EXCLUDED.topic_id
		WHERE (olap.items.title, olap.items.topic_id) IS DISTINCT FROM (EXCLUDED.title, EXCLUDED.topic_id)`,
	`DELETE FROM olap.coteachers c WHERE NOT EXISTS (SELECT 1 FROM content.classroom_teachers t
		WHERE t.classroom_id = c.classroom_id AND t.user_id = c.user_id AND t.status = 'active')`,
	`INSERT INTO olap.coteachers (classroom_id, user_id) SELECT classroom_id, user_id FROM content.classroom_teachers WHERE status = 'active'
		ON CONFLICT DO NOTHING`,
	`INSERT INTO olap.people (id, name) SELECT id, name FROM auth.users
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name WHERE olap.people.name IS DISTINCT FROM EXCLUDED.name`,
}

// reconcile removes what no longer exists at its source.
var reconcile = []string{
	`DELETE FROM olap.activity f WHERE f.kind = 'quiz' AND NOT EXISTS
		(SELECT 1 FROM eval.results r WHERE r.attempt_id = f.source_id AND r.complete AND NOT r.invalidated)`,
	`DELETE FROM olap.activity f WHERE f.kind = 'tutorial' AND NOT EXISTS
		(SELECT 1 FROM tutorial.runs ru WHERE ru.id = f.source_id AND ru.finished_at IS NOT NULL)`,
	`DELETE FROM olap.answers a WHERE NOT EXISTS (SELECT 1 FROM olap.activity f WHERE f.kind = 'quiz' AND f.source_id = a.attempt_id)`,
	`DELETE FROM olap.classrooms c WHERE NOT EXISTS (SELECT 1 FROM content.classrooms x WHERE x.id = c.id)`,
	`DELETE FROM olap.topics t WHERE NOT EXISTS (SELECT 1 FROM content.topics x WHERE x.id = t.id)`,
	`DELETE FROM olap.items i WHERE NOT EXISTS (SELECT 1 FROM quiz.quizzes x WHERE x.id = i.id) AND NOT EXISTS (SELECT 1 FROM tutorial.tutorials x WHERE x.id = i.id)`,
}
