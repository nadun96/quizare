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
