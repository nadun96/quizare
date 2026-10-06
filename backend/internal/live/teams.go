package live

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/settings"
)

// Teams in live sessions (V2-06, V2-07, D-44). Every student still sits
// their own proctored attempt; a team's mark is combined from its members'
// marks by the session's team_acceptance and team_calc settings. Teams are
// formed by the teacher, at random, from classroom categories, or chosen by
// students when they join (team_mode).

const MaxTeams = 50

// TeamInfo is what students and the dashboard see of a team.
type TeamInfo struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Color    int     `json:"color"`
	Position int     `json:"position"`
	Category *string `json:"category_id,omitempty"`
	Members  int     `json:"members"`
}

// TeamMember is a student in the teacher's team view.
type TeamMember struct {
	AttemptID     string  `json:"attempt_id"`
	Name          string  `json:"name"`
	StudentNumber *string `json:"student_number"`
	State         string  `json:"state"`
	Captain       bool    `json:"captain"`
	TeamID        string  `json:"team_id,omitempty"`
}

type Team struct {
	TeamInfo
	List []TeamMember `json:"member_list"`
}

type TeamsView struct {
	Mode       string       `json:"mode"`
	Acceptance string       `json:"acceptance"`
	Calc       string       `json:"calc"`
	Teams      []Team       `json:"teams"`
	Unassigned []TeamMember `json:"unassigned"`
}

var errChooseTeam = httpx.Invalid(map[string]string{"team_id": "choose a team to join"})

func cleanTeamName(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > 40 {
		s = string([]rune(s)[:40])
	}
	return s
}

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func (s *Service) teamInfos(ctx context.Context, sessionID string) ([]TeamInfo, error) {
	rows, err := s.pool.Query(ctx, `SELECT t.id, t.name, t.color, t.position, t.category_id, count(a.id) FROM live.teams t
		LEFT JOIN live.attempts a ON a.team_id=t.id WHERE t.session_id=$1
		GROUP BY t.id ORDER BY t.position, t.created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (TeamInfo, error) {
		var t TeamInfo
		err := r.Scan(&t.ID, &t.Name, &t.Color, &t.Position, &t.Category, &t.Members)
		return t, err
	})
}

// TeamInfos lists a session's teams with member counts (for other modules).
func (s *Service) TeamInfos(ctx context.Context, sessionID string) ([]TeamInfo, error) {
	return s.teamInfos(ctx, sessionID)
}

// TeamRules returns a session's team settings for its owner.
func (s *Service) TeamRules(ctx context.Context, teacherID, sessionID string) (settings.Effective, error) {
	sess, err := s.ownedSession(ctx, teacherID, sessionID)
	if err != nil {
		return settings.Effective{}, err
	}
	v := s.sessionView(sess)
	return v.Effective(nil, settings.Overrides{}), nil
}

// SessionTeamRules is TeamRules without the ownership check (student results).
func (s *Service) SessionTeamRules(ctx context.Context, sessionID string) (settings.Effective, error) {
	sess, err := s.session(ctx, sessionID)
	if err != nil {
		return settings.Effective{}, err
	}
	v := s.sessionView(sess)
	return v.Effective(nil, settings.Overrides{}), nil
}

func (s *Service) Teams(ctx context.Context, teacherID, sessionID string) (TeamsView, error) {
	sess, err := s.ownedSession(ctx, teacherID, sessionID)
	if err != nil {
		return TeamsView{}, err
	}
	sv := s.sessionView(sess)
	eff := sv.Effective(nil, settings.Overrides{})
	infos, err := s.teamInfos(ctx, sessionID)
	if err != nil {
		return TeamsView{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id, user_id, student_number, state, captain, team_id FROM live.attempts WHERE session_id=$1 ORDER BY created_at`, sessionID)
	if err != nil {
		return TeamsView{}, err
	}
	type row struct {
		m    TeamMember
		user string
		team *string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.m.AttemptID, &x.user, &x.m.StudentNumber, &x.m.State, &x.m.Captain, &x.team)
		return x, err
	})
	if err != nil {
		return TeamsView{}, err
	}
	ids := make([]string, len(list))
	for i, x := range list {
		ids[i] = x.user
	}
	users, err := s.users.UsersByID(ctx, ids)
	if err != nil {
		return TeamsView{}, err
	}
	v := TeamsView{Mode: eff.TeamMode, Acceptance: eff.TeamAcceptance, Calc: eff.TeamCalc, Teams: make([]Team, len(infos)), Unassigned: []TeamMember{}}
	idx := map[string]int{}
	for i, t := range infos {
		v.Teams[i] = Team{TeamInfo: t, List: []TeamMember{}}
		idx[t.ID] = i
	}
	for _, x := range list {
		x.m.Name = users[x.user].Name
		if x.team != nil {
			if i, ok := idx[*x.team]; ok {
				x.m.TeamID = *x.team
				v.Teams[i].List = append(v.Teams[i].List, x.m)
				continue
			}
		}
		v.Unassigned = append(v.Unassigned, x.m)
	}
	return v, nil
}

type TeamInput struct {
	Name  *string `json:"name"`
	Color *int    `json:"color"`
}

func (s *Service) CreateTeam(ctx context.Context, teacherID, sessionID string, in TeamInput) (TeamInfo, error) {
	sess, err := s.ownedSession(ctx, teacherID, sessionID)
	if err != nil {
		return TeamInfo{}, err
	}
	name := ""
	if in.Name != nil {
		name = cleanTeamName(*in.Name)
	}
	t, err := s.insertTeam(ctx, sess.ID, name, in.Color, nil)
	if err == nil {
		s.teamsChanged(ctx, sess.ID)
	}
	return t, err
}

func (s *Service) insertTeam(ctx context.Context, sessionID, name string, color *int, category *string) (TeamInfo, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM live.teams WHERE session_id=$1`, sessionID).Scan(&n); err != nil {
		return TeamInfo{}, err
	}
	if n >= MaxTeams {
		return TeamInfo{}, httpx.Conflict(fmt.Sprintf("a session can have at most %d teams", MaxTeams))
	}
	c := n%8 + 1
	if color != nil {
		if *color < 1 || *color > 8 {
			return TeamInfo{}, httpx.Invalid(map[string]string{"color": "color must be 1-8"})
		}
		c = *color
	}
	auto := name == ""
	if auto {
		name = fmt.Sprintf("Team %d", n+1)
	}
	base := name
	for try := 2; ; try++ {
		var t TeamInfo
		err := s.pool.QueryRow(ctx, `INSERT INTO live.teams (session_id, name, color, position, category_id) VALUES ($1,$2,$3,$4,$5)
			RETURNING id, name, color, position, category_id`, sessionID, name, c, n, category).Scan(&t.ID, &t.Name, &t.Color, &t.Position, &t.Category)
		if isUnique(err) && (auto || category != nil) && try < 100 {
			if auto {
				name = fmt.Sprintf("Team %d", n+try)
			} else {
				name = fmt.Sprintf("%s (%d)", base, try)
			}
			continue
		}
		if isUnique(err) {
			return t, httpx.Invalid(map[string]string{"name": "another team already has this name"})
		}
		return t, err
	}
}

func (s *Service) ownedTeam(ctx context.Context, teacherID, teamID string) (*Session, TeamInfo, error) {
	var t TeamInfo
	var sessionID string
	err := s.pool.QueryRow(ctx, `SELECT id, session_id, name, color, position, category_id FROM live.teams WHERE id=$1`, teamID).
		Scan(&t.ID, &sessionID, &t.Name, &t.Color, &t.Position, &t.Category)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, t, httpx.ErrNotFound
	}
	if err != nil {
		return nil, t, err
	}
	sess, err := s.ownedSession(ctx, teacherID, sessionID)
	return sess, t, err
}

func (s *Service) UpdateTeam(ctx context.Context, teacherID, teamID string, in TeamInput) (TeamInfo, error) {
	sess, t, err := s.ownedTeam(ctx, teacherID, teamID)
	if err != nil {
		return t, err
	}
	if in.Name != nil {
		if t.Name = cleanTeamName(*in.Name); t.Name == "" {
			return t, httpx.Invalid(map[string]string{"name": "name the team"})
		}
	}
	if in.Color != nil {
		if *in.Color < 1 || *in.Color > 8 {
			return t, httpx.Invalid(map[string]string{"color": "color must be 1-8"})
		}
		t.Color = *in.Color
	}
	_, err = s.pool.Exec(ctx, `UPDATE live.teams SET name=$2, color=$3 WHERE id=$1`, t.ID, t.Name, t.Color)
	if isUnique(err) {
		return t, httpx.Invalid(map[string]string{"name": "another team already has this name"})
	}
	s.teamsChanged(ctx, sess.ID)
	return t, err
}

// DeleteTeam removes a team; its members carry on without one.
func (s *Service) DeleteTeam(ctx context.Context, teacherID, teamID string) error {
	sess, t, err := s.ownedTeam(ctx, teacherID, teamID)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE live.attempts SET captain=false WHERE team_id=$1`, t.ID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM live.teams WHERE id=$1`, t.ID); err != nil {
		return err
	}
	s.teamsChanged(ctx, sess.ID)
	return nil
}

// TeamAssign moves attempts to a team ("" removes them), or with captain
// set, makes one student their team's captain.
type TeamAssign struct {
	AttemptIDs []string `json:"attempt_ids"`
	TeamID     *string  `json:"team_id"`
	Captain    *bool    `json:"captain"`
}

func (s *Service) AssignTeam(ctx context.Context, teacherID, sessionID string, in TeamAssign) error {
	sess, err := s.ownedSession(ctx, teacherID, sessionID)
	if err != nil {
		return err
	}
	if len(in.AttemptIDs) == 0 || len(in.AttemptIDs) > 2000 {
		return httpx.Invalid(map[string]string{"attempt_ids": "choose students"})
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM live.attempts WHERE session_id=$1 AND id = ANY($2)`, sess.ID, in.AttemptIDs).Scan(&n); err != nil {
		return err
	}
	if n != len(in.AttemptIDs) {
		return httpx.Invalid(map[string]string{"attempt_ids": "unknown attempt"})
	}
	if in.TeamID != nil {
		var team any
		if *in.TeamID != "" {
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM live.teams WHERE session_id=$1 AND id=$2`, sess.ID, *in.TeamID).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return httpx.Invalid(map[string]string{"team_id": "unknown team"})
			}
			team = *in.TeamID
		}
		if _, err := s.pool.Exec(ctx, `UPDATE live.attempts SET team_id=$2, captain=false WHERE id = ANY($1) AND team_id IS DISTINCT FROM $2::uuid`, in.AttemptIDs, team); err != nil {
			return err
		}
		if err := s.fillCaptains(ctx, sess.ID); err != nil {
			return err
		}
	}
	if in.Captain != nil {
		if len(in.AttemptIDs) != 1 {
			return httpx.Invalid(map[string]string{"attempt_ids": "a team has one captain"})
		}
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			id := in.AttemptIDs[0]
			if *in.Captain {
				var t *string
				if err := tx.QueryRow(ctx, `SELECT team_id FROM live.attempts WHERE id=$1`, id).Scan(&t); err != nil {
					return err
				}
				if t == nil {
					return httpx.Invalid(map[string]string{"captain": "put the student in a team first"})
				}
				if _, err := tx.Exec(ctx, `UPDATE live.attempts SET captain=false WHERE team_id=$1 AND captain`, *t); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `UPDATE live.attempts SET captain=$2 WHERE id=$1`, id, *in.Captain)
			return err
		})
		if err != nil {
			return err
		}
	}
	s.teamsChanged(ctx, sess.ID)
	return nil
}

// fillCaptains makes the earliest member of every captain-less team its captain.
func (s *Service) fillCaptains(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE live.attempts a SET captain=true FROM (
			SELECT DISTINCT ON (team_id) id FROM live.attempts
			WHERE session_id=$1 AND team_id IS NOT NULL
			  AND team_id NOT IN (SELECT team_id FROM live.attempts WHERE session_id=$1 AND captain AND team_id IS NOT NULL)
			ORDER BY team_id, created_at, id) first
		WHERE a.id = first.id`, sessionID)
	return err
}

type TeamGenerate struct {
	From     string `json:"from"`  // random | categories
	Count    int    `json:"count"` // random
	Reassign bool   `json:"reassign"`
}

func (s *Service) GenerateTeams(ctx context.Context, teacherID, sessionID string, in TeamGenerate) (TeamsView, error) {
	sess, err := s.ownedSession(ctx, teacherID, sessionID)
	if err != nil {
		return TeamsView{}, err
	}
	switch in.From {
	case "random":
		if in.Count < 2 || in.Count > MaxTeams {
			return TeamsView{}, httpx.Invalid(map[string]string{"count": fmt.Sprintf("make 2-%d teams", MaxTeams)})
		}
		err = s.randomTeams(ctx, sess.ID, in.Count, in.Reassign)
	case "categories":
		err = s.categoryTeams(ctx, sess, in.Reassign)
	default:
		return TeamsView{}, httpx.Invalid(map[string]string{"from": "from must be random or categories"})
	}
	if err == nil {
		err = s.fillCaptains(ctx, sess.ID)
	}
	if err != nil {
		return TeamsView{}, err
	}
	s.teamsChanged(ctx, sess.ID)
	return s.Teams(ctx, teacherID, sessionID)
}

func (s *Service) randomTeams(ctx context.Context, sessionID string, count int, reassign bool) error {
	ts, err := s.teamInfos(ctx, sessionID)
	if err != nil {
		return err
	}
	for len(ts) < count {
		t, err := s.insertTeam(ctx, sessionID, "", nil, nil)
		if err != nil {
			return err
		}
		ts = append(ts, t)
	}
	ts = ts[:count]
	if reassign {
		if _, err := s.pool.Exec(ctx, `UPDATE live.attempts SET team_id=NULL, captain=false WHERE session_id=$1`, sessionID); err != nil {
			return err
		}
	}
	pos := map[string]int{}
	for i, t := range ts {
		pos[t.ID] = i
	}
	sizes := make([]int, len(ts))
	rows, err := s.pool.Query(ctx, `SELECT id, team_id FROM live.attempts WHERE session_id=$1`, sessionID)
	if err != nil {
		return err
	}
	var free []string
	for rows.Next() {
		var id string
		var t *string
		if err := rows.Scan(&id, &t); err != nil {
			rows.Close()
			return err
		}
		if t == nil {
			free = append(free, id)
		} else if i, ok := pos[*t]; ok {
			sizes[i]++
		}
	}
	rows.Close()
	rand.Shuffle(len(free), func(i, j int) { free[i], free[j] = free[j], free[i] })
	for _, id := range free {
		best := 0
		for i := range sizes {
			if sizes[i] < sizes[best] {
				best = i
			}
		}
		sizes[best]++
		if _, err := s.pool.Exec(ctx, `UPDATE live.attempts SET team_id=$2 WHERE id=$1`, id, ts[best].ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) categoryTeams(ctx context.Context, sess *Session, reassign bool) error {
	cats, err := s.classrooms.ListCategories(ctx, sess.TeacherID, sess.ClassroomID)
	if err != nil {
		return err
	}
	if len(cats) == 0 {
		return httpx.Invalid(map[string]string{"from": "this classroom has no categories yet"})
	}
	ts, err := s.teamInfos(ctx, sess.ID)
	if err != nil {
		return err
	}
	byCat := map[string]string{}
	for _, t := range ts {
		if t.Category != nil {
			byCat[*t.Category] = t.ID
		}
	}
	for _, c := range cats {
		if _, ok := byCat[c.ID]; ok {
			continue
		}
		id, color := c.ID, c.Color
		t, err := s.insertTeam(ctx, sess.ID, c.Name, &color, &id)
		if err != nil {
			return err
		}
		byCat[c.ID] = t.ID
	}
	first, err := s.firstCategoryTeam(ctx, sess, byCat)
	if err != nil {
		return err
	}
	cond := ` AND team_id IS NULL`
	if reassign {
		cond = ``
	}
	rows, err := s.pool.Query(ctx, `SELECT id, user_id FROM live.attempts WHERE session_id=$1`+cond, sess.ID)
	if err != nil {
		return err
	}
	type au struct{ id, user string }
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (au, error) {
		var x au
		err := r.Scan(&x.id, &x.user)
		return x, err
	})
	if err != nil {
		return err
	}
	for _, x := range list {
		var team any
		if id, ok := first[x.user]; ok {
			team = id
		} else if !reassign {
			continue
		}
		if _, err := s.pool.Exec(ctx, `UPDATE live.attempts SET team_id=$2, captain=false WHERE id=$1 AND team_id IS DISTINCT FROM $2::uuid`, x.id, team); err != nil {
			return err
		}
	}
	return nil
}

// firstCategoryTeam maps each student to the team of their first category.
func (s *Service) firstCategoryTeam(ctx context.Context, sess *Session, byCat map[string]string) (map[string]string, error) {
	members, err := s.classrooms.CategoryMembers(ctx, sess.TeacherID, sess.ClassroomID)
	if err != nil {
		return nil, err
	}
	first := map[string]string{}
	for _, m := range members {
		if t, ok := byCat[m.CategoryID]; ok {
			if _, seen := first[m.UserID]; !seen {
				first[m.UserID] = t
			}
		}
	}
	return first, nil
}

// placeTeam puts a new attempt in a team according to team_mode; chosen is
// the team picked on the join screen.
func (s *Service) placeTeam(ctx context.Context, tx pgx.Tx, sess *Session, eff settings.Effective, attemptID, userID, chosen string) error {
	var team string
	switch eff.TeamMode {
	case "self":
		if chosen == "" {
			return nil
		}
		team = chosen
	case "random":
		err := tx.QueryRow(ctx, `SELECT t.id FROM live.teams t LEFT JOIN live.attempts a ON a.team_id=t.id
			WHERE t.session_id=$1 GROUP BY t.id ORDER BY count(a.id), random() LIMIT 1`, sess.ID).Scan(&team)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
	case "categories":
		ts, err := s.teamInfos(ctx, sess.ID)
		if err != nil {
			return err
		}
		byCat := map[string]string{}
		for _, t := range ts {
			if t.Category != nil {
				byCat[*t.Category] = t.ID
			}
		}
		if len(byCat) == 0 {
			return nil
		}
		first, err := s.firstCategoryTeam(ctx, sess, byCat)
		if err != nil {
			return err
		}
		if team = first[userID]; team == "" {
			return nil
		}
	default:
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE live.attempts SET team_id=$2,
		captain = NOT EXISTS (SELECT 1 FROM live.attempts WHERE team_id=$2 AND captain) WHERE id=$1`, attemptID, team)
	return err
}

// checkTeamChoice validates a self-selected team before the attempt exists.
func (s *Service) checkTeamChoice(ctx context.Context, sessionID string, eff settings.Effective, chosen string) error {
	if eff.TeamMode != "self" {
		return nil
	}
	var all, ok int
	if err := s.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE id::text=$2) FROM live.teams WHERE session_id=$1`, sessionID, chosen).Scan(&all, &ok); err != nil {
		return err
	}
	if all > 0 && ok == 0 {
		return errChooseTeam
	}
	return nil
}

// teamsChanged refreshes the dashboard and every student's view of their team.
func (s *Service) teamsChanged(ctx context.Context, sessionID string) {
	sess, err := s.session(ctx, sessionID)
	if err != nil {
		return
	}
	rows, err := s.pool.Query(ctx, `SELECT `+attemptCols+` FROM live.attempts a WHERE a.session_id=$1`, sessionID)
	if err != nil {
		return
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Attempt, error) { return scanAttempt(r) })
	if err != nil {
		return
	}
	for _, a := range list {
		s.hub.publishAttempt(sess, a)
	}
	s.hub.markDirty(sessionID)
}

// SessionMaxScore is the most one student can score in the session.
func (s *Service) SessionMaxScore(ctx context.Context, sessionID string) (float64, error) {
	sess, err := s.session(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	total := 0.0
	for _, q := range s.sessionView(sess).Snapshot.Questions {
		total += q.Marks
	}
	return total, nil
}
