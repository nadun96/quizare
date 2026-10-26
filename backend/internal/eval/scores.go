package eval

import "context"

// QuizScores gives each student's marks on some quizzes, for setting beside
// how long they spent on a presentation (D-78): per quiz their best attempt
// that wasn't invalidated (D-27), added up across the quizzes, as
// [score, out of].
func (s *Service) QuizScores(ctx context.Context, quizIDs []string) (map[string][2]float64, error) {
	out := map[string][2]float64{}
	if len(quizIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (r.user_id, se.quiz_id) r.user_id, r.score::float8, r.max_score::float8
		FROM eval.results r JOIN live.sessions se ON se.id = r.session_id
		WHERE se.quiz_id = ANY($1::uuid[]) AND NOT r.invalidated AND r.max_score > 0
		ORDER BY r.user_id, se.quiz_id, r.pct DESC`, quizIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var user string
		var score, max float64
		if err := rows.Scan(&user, &score, &max); err != nil {
			return nil, err
		}
		v := out[user]
		out[user] = [2]float64{v[0] + score, v[1] + max}
	}
	return out, rows.Err()
}

// ClassroomQuiz is a quiz with results in a classroom, for its results table.
type ClassroomQuiz struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ClassroomScores gives, for a classroom's results table (D-87, TS-FR-82),
// the quizzes run in it (by when first run) and each student's best
// percentage on each, from attempts that weren't invalidated (D-27). Bank
// and tutorial quizzes aren't run as sessions, so they never appear.
func (s *Service) ClassroomScores(ctx context.Context, classroomID string) ([]ClassroomQuiz, map[string]map[string]float64, error) {
	rows, err := s.pool.Query(ctx, `SELECT q.id, q.title FROM quiz.quizzes q
		JOIN (SELECT quiz_id, min(created_at) AS first FROM live.sessions WHERE classroom_id=$1 GROUP BY quiz_id) se ON se.quiz_id = q.id
		ORDER BY se.first, q.id`, classroomID)
	if err != nil {
		return nil, nil, err
	}
	var quizzes []ClassroomQuiz
	for rows.Next() {
		var q ClassroomQuiz
		if err := rows.Scan(&q.ID, &q.Title); err != nil {
			rows.Close()
			return nil, nil, err
		}
		quizzes = append(quizzes, q)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	best := map[string]map[string]float64{}
	rows, err = s.pool.Query(ctx, `SELECT r.user_id::text, se.quiz_id::text, max(r.pct)::float8
		FROM eval.results r JOIN live.sessions se ON se.id = r.session_id
		WHERE se.classroom_id=$1 AND NOT r.invalidated GROUP BY r.user_id, se.quiz_id`, classroomID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var user, quiz string
		var pct float64
		if err := rows.Scan(&user, &quiz, &pct); err != nil {
			return nil, nil, err
		}
		if best[user] == nil {
			best[user] = map[string]float64{}
		}
		best[user][quiz] = pct
	}
	if quizzes == nil {
		quizzes = []ClassroomQuiz{}
	}
	return quizzes, best, rows.Err()
}
