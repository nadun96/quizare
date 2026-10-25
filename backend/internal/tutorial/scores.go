package tutorial

import "context"

// Scores gives each student's marks on some tutorials, for setting beside
// how long they spent on a presentation (D-78): their answers marked so far,
// added up, as [score, out of]. Answers waiting for the teacher don't count yet.
func (s *Service) Scores(ctx context.Context, tutorialIDs []string) (map[string][2]float64, error) {
	out := map[string][2]float64{}
	if len(tutorialIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT r.user_id, sum(a.score), sum(a.max_score)
		FROM tutorial.runs r JOIN tutorial.answers a ON a.run_id = r.id
		WHERE r.tutorial_id = ANY($1::uuid[]) AND a.status = 'marked'
		GROUP BY r.user_id HAVING sum(a.max_score) > 0`, tutorialIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var user string
		var v [2]float64
		if err := rows.Scan(&user, &v[0], &v[1]); err != nil {
			return nil, err
		}
		out[user] = v
	}
	return out, rows.Err()
}
