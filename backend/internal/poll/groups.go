package poll

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Groups and group competitions (V2-06, V2-07, D-43). A poll's groups are
// formed by the teacher (manual), at random, from classroom categories, or by
// participants choosing one when they join. Acceptance decides which answers
// count for the group: everyone's (combined by sum, average, highest or
// lowest), the group's first, the captain's, or the group's best.

const MaxGroups = 50

type Group struct {
	ID         string   `json:"id"`
	PollID     string   `json:"poll_id"`
	Name       string   `json:"name"`
	Color      int      `json:"color"`
	Position   int      `json:"position"`
	CategoryID *string  `json:"category_id,omitempty"`
	Members    []Member `json:"members,omitempty"` // teacher view
}

// Member is a participant in the teacher's group view.
type Member struct {
	ParticipantID string `json:"participant_id"`
	Name          string `json:"name"`
	Nickname      string `json:"nickname,omitempty"`
	RealName      string `json:"real_name,omitempty"`
	Captain       bool   `json:"captain"`
	GroupID       string `json:"group_id,omitempty"`
}

// GroupInfo is what participants see of a group.
type GroupInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Color   int    `json:"color"`
	Members int    `json:"members"`
}

// GroupRank is one group on the group leaderboard.
type GroupRank struct {
	Rank     int     `json:"rank"`
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Color    int     `json:"color"`
	Members  int     `json:"members"`
	Score    float64 `json:"score"`
	Answered int     `json:"answered"` // questions with a counted answer
}

// GroupAnswer is a teammate's answer, shown to the group in first-answer and
// captain polls so everyone sees what was submitted for them.
type GroupAnswer struct {
	By    string `json:"by"`
	Value Answer `json:"value"`
}

var (
	errCaptainOnly   = httpx.NewError(http.StatusConflict, "captain_only", "only your group's captain answers in this poll")
	errGroupAnswered = httpx.NewError(http.StatusConflict, "group_answered", "a teammate has already answered for your group")
	errChooseGroup   = httpx.Invalid(map[string]string{"group_id": "choose a group to join"})
)

// ---------- names ----------

type pname struct {
	n        int
	userID   *string
	nickname string
	real     string
	groupID  *string
	captain  bool
	joined   time.Time
}

func displayName(p *Poll, real, nickname string, n int) string {
	switch {
	case p.Names == "name" && real != "":
		return real
	case nickname != "":
		return nickname
	}
	return fmt.Sprintf("Participant %d", n)
}

// participantNames numbers participants by join order (for "Participant N")
// and, when wanted, resolves account names.
func (s *Service) participantNames(ctx context.Context, p *Poll, withReal bool) (map[string]*pname, []string, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, user_id, coalesce(nickname, ''), group_id, captain, created_at,
		row_number() OVER (ORDER BY created_at, id) FROM poll.participants WHERE poll_id=$1 ORDER BY created_at, id`, p.ID)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]*pname{}
	var order, users []string
	for rows.Next() {
		var id string
		x := &pname{}
		if err := rows.Scan(&id, &x.userID, &x.nickname, &x.groupID, &x.captain, &x.joined, &x.n); err != nil {
			rows.Close()
			return nil, nil, err
		}
		out[id] = x
		order = append(order, id)
		if x.userID != nil {
			users = append(users, *x.userID)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if withReal && len(users) > 0 {
		us, err := s.users.UsersByID(ctx, users)
		if err != nil {
			return nil, nil, err
		}
		for _, x := range out {
			if x.userID != nil {
				x.real = us[*x.userID].Name
			}
		}
	}
	return out, order, nil
}

// ---------- teacher: managing groups ----------

func cleanGroupName(s string) string {
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

func (s *Service) listGroups(ctx context.Context, pollID string) ([]Group, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, poll_id, name, color, position, category_id FROM poll.groups WHERE poll_id=$1 ORDER BY position, created_at`, pollID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Group, error) {
		var g Group
		err := r.Scan(&g.ID, &g.PollID, &g.Name, &g.Color, &g.Position, &g.CategoryID)
		return g, err
	})
}

// GroupsView is the teacher's picture of a poll's groups.
type GroupsView struct {
	Groups    []Group  `json:"groups"`
	Ungrouped []Member `json:"ungrouped"`
}

func (s *Service) Groups(ctx context.Context, teacherID, pollID string) (GroupsView, error) {
	p, err := s.ownedPoll(ctx, teacherID, pollID)
	if err != nil {
		return GroupsView{}, err
	}
	gs, err := s.listGroups(ctx, p.ID)
	if err != nil {
		return GroupsView{}, err
	}
	names, order, err := s.participantNames(ctx, p, true)
	if err != nil {
		return GroupsView{}, err
	}
	idx := map[string]int{}
	for i := range gs {
		idx[gs[i].ID] = i
		gs[i].Members = []Member{}
	}
	v := GroupsView{Groups: gs, Ungrouped: []Member{}}
	for _, id := range order {
		x := names[id]
		m := Member{ParticipantID: id, Name: displayName(p, x.real, x.nickname, x.n), Nickname: x.nickname, RealName: x.real, Captain: x.captain}
		if x.groupID != nil {
			if i, ok := idx[*x.groupID]; ok {
				m.GroupID = *x.groupID
				gs[i].Members = append(gs[i].Members, m)
				continue
			}
		}
		v.Ungrouped = append(v.Ungrouped, m)
	}
	return v, nil
}

type GroupInput struct {
	Name  *string `json:"name"`
	Color *int    `json:"color"`
}

func (s *Service) CreateGroup(ctx context.Context, teacherID, pollID string, in GroupInput) (Group, error) {
	p, err := s.ownedPoll(ctx, teacherID, pollID)
	if err != nil {
		return Group{}, err
	}
	name := ""
	if in.Name != nil {
		name = cleanGroupName(*in.Name)
	}
	g, err := s.insertGroup(ctx, p.ID, name, in.Color, nil)
	if err == nil {
		s.hub.markAll(p.ID)
	}
	return g, err
}

func (s *Service) insertGroup(ctx context.Context, pollID, name string, color *int, category *string) (Group, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM poll.groups WHERE poll_id=$1`, pollID).Scan(&n); err != nil {
		return Group{}, err
	}
	if n >= MaxGroups {
		return Group{}, httpx.Conflict(fmt.Sprintf("a poll can have at most %d groups", MaxGroups))
	}
	c := n%8 + 1
	if color != nil {
		if *color < 1 || *color > 8 {
			return Group{}, httpx.Invalid(map[string]string{"color": "color must be 1-8"})
		}
		c = *color
	}
	auto := name == ""
	if auto {
		name = fmt.Sprintf("Group %d", n+1)
	}
	base := name
	for try := 2; ; try++ {
		var g Group
		err := s.pool.QueryRow(ctx, `INSERT INTO poll.groups (poll_id, name, color, position, category_id) VALUES ($1,$2,$3,$4,$5)
			RETURNING id, poll_id, name, color, position, category_id`, pollID, name, c, n, category).
			Scan(&g.ID, &g.PollID, &g.Name, &g.Color, &g.Position, &g.CategoryID)
		if isUnique(err) && (auto || category != nil) && try < 100 {
			if auto {
				name = fmt.Sprintf("Group %d", n+try)
			} else {
				name = fmt.Sprintf("%s (%d)", base, try)
			}
			continue
		}
		if isUnique(err) {
			return g, httpx.Invalid(map[string]string{"name": "another group already has this name"})
		}
		return g, err
	}
}

func (s *Service) ownedGroup(ctx context.Context, teacherID, groupID string) (*Poll, Group, error) {
	var g Group
	err := s.pool.QueryRow(ctx, `SELECT id, poll_id, name, color, position, category_id FROM poll.groups WHERE id=$1`, groupID).
		Scan(&g.ID, &g.PollID, &g.Name, &g.Color, &g.Position, &g.CategoryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, g, httpx.ErrNotFound
	}
	if err != nil {
		return nil, g, err
	}
	p, err := s.ownedPoll(ctx, teacherID, g.PollID)
	return p, g, err
}

func (s *Service) UpdateGroup(ctx context.Context, teacherID, groupID string, in GroupInput) (Group, error) {
	p, g, err := s.ownedGroup(ctx, teacherID, groupID)
	if err != nil {
		return g, err
	}
	if in.Name != nil {
		g.Name = cleanGroupName(*in.Name)
		if g.Name == "" {
			return g, httpx.Invalid(map[string]string{"name": "name the group"})
		}
	}
	if in.Color != nil {
		if *in.Color < 1 || *in.Color > 8 {
			return g, httpx.Invalid(map[string]string{"color": "color must be 1-8"})
		}
		g.Color = *in.Color
	}
	_, err = s.pool.Exec(ctx, `UPDATE poll.groups SET name=$2, color=$3 WHERE id=$1`, g.ID, g.Name, g.Color)
	if isUnique(err) {
		return g, httpx.Invalid(map[string]string{"name": "another group already has this name"})
	}
	s.hub.markAll(p.ID)
	return g, err
}

// DeleteGroup removes a group; its members become ungrouped.
func (s *Service) DeleteGroup(ctx context.Context, teacherID, groupID string) error {
	p, g, err := s.ownedGroup(ctx, teacherID, groupID)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE poll.participants SET captain=false WHERE group_id=$1`, g.ID); err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM poll.groups WHERE id=$1`, g.ID)
	s.hub.markAll(p.ID)
	return err
}

// AssignInput moves participants to a group ("" or null: no group), or with
// captain set, makes one participant their group's captain.
type AssignInput struct {
	ParticipantIDs []string `json:"participant_ids"`
	GroupID        *string  `json:"group_id"`
	Captain        *bool    `json:"captain"`
}

func (s *Service) AssignMembers(ctx context.Context, teacherID, pollID string, in AssignInput) error {
	p, err := s.ownedPoll(ctx, teacherID, pollID)
	if err != nil {
		return err
	}
	if len(in.ParticipantIDs) == 0 || len(in.ParticipantIDs) > MaxParticipants {
		return httpx.Invalid(map[string]string{"participant_ids": "choose participants"})
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM poll.participants WHERE poll_id=$1 AND id = ANY($2)`, p.ID, in.ParticipantIDs).Scan(&n); err != nil {
		return err
	}
	if n != len(in.ParticipantIDs) {
		return httpx.Invalid(map[string]string{"participant_ids": "unknown participant"})
	}
	if in.GroupID != nil {
		var group any
		if *in.GroupID != "" {
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM poll.groups WHERE poll_id=$1 AND id=$2`, p.ID, *in.GroupID).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return httpx.Invalid(map[string]string{"group_id": "unknown group"})
			}
			group = *in.GroupID
		}
		if _, err := s.pool.Exec(ctx, `UPDATE poll.participants SET group_id=$2, captain=false WHERE id = ANY($1) AND group_id IS DISTINCT FROM $2::uuid`, in.ParticipantIDs, group); err != nil {
			return err
		}
		if group != nil {
			if err := s.fillCaptains(ctx, p.ID); err != nil {
				return err
			}
		}
	}
	if in.Captain != nil {
		if len(in.ParticipantIDs) != 1 {
			return httpx.Invalid(map[string]string{"participant_ids": "a group has one captain"})
		}
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			pid := in.ParticipantIDs[0]
			if *in.Captain {
				var g *string
				if err := tx.QueryRow(ctx, `SELECT group_id FROM poll.participants WHERE id=$1`, pid).Scan(&g); err != nil {
					return err
				}
				if g == nil {
					return httpx.Invalid(map[string]string{"captain": "put the participant in a group first"})
				}
				if _, err := tx.Exec(ctx, `UPDATE poll.participants SET captain=false WHERE group_id=$1 AND captain`, *g); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `UPDATE poll.participants SET captain=$2 WHERE id=$1`, pid, *in.Captain)
			return err
		})
		if err != nil {
			return err
		}
	}
	s.hub.markAll(p.ID)
	return nil
}

// fillCaptains makes the earliest member of every captain-less group its captain.
func (s *Service) fillCaptains(ctx context.Context, pollID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE poll.participants pa SET captain=true FROM (
			SELECT DISTINCT ON (group_id) id FROM poll.participants
			WHERE poll_id=$1 AND group_id IS NOT NULL
			  AND group_id NOT IN (SELECT group_id FROM poll.participants WHERE poll_id=$1 AND captain AND group_id IS NOT NULL)
			ORDER BY group_id, created_at, id) first
		WHERE pa.id = first.id`, pollID)
	return err
}

// GenerateInput forms groups automatically.
type GenerateInput struct {
	From     string `json:"from"`     // random | categories
	Count    int    `json:"count"`    // random: number of groups
	Reassign bool   `json:"reassign"` // also move participants who already have a group
}

func (s *Service) GenerateGroups(ctx context.Context, teacherID, pollID string, in GenerateInput) (GroupsView, error) {
	p, err := s.ownedPoll(ctx, teacherID, pollID)
	if err != nil {
		return GroupsView{}, err
	}
	switch in.From {
	case "random":
		if in.Count < 2 || in.Count > MaxGroups {
			return GroupsView{}, httpx.Invalid(map[string]string{"count": fmt.Sprintf("make 2-%d groups", MaxGroups)})
		}
		err = s.randomGroups(ctx, p, in.Count, in.Reassign)
	case "categories":
		err = s.categoryGroups(ctx, p, in.Reassign)
	default:
		return GroupsView{}, httpx.Invalid(map[string]string{"from": "from must be random or categories"})
	}
	if err != nil {
		return GroupsView{}, err
	}
	if err := s.fillCaptains(ctx, p.ID); err != nil {
		return GroupsView{}, err
	}
	s.hub.markAll(p.ID)
	return s.Groups(ctx, teacherID, pollID)
}

func (s *Service) randomGroups(ctx context.Context, p *Poll, count int, reassign bool) error {
	gs, err := s.listGroups(ctx, p.ID)
	if err != nil {
		return err
	}
	for len(gs) < count {
		g, err := s.insertGroup(ctx, p.ID, "", nil, nil)
		if err != nil {
			return err
		}
		gs = append(gs, g)
	}
	gs = gs[:count]
	if reassign {
		if _, err := s.pool.Exec(ctx, `UPDATE poll.participants SET group_id=NULL, captain=false WHERE poll_id=$1`, p.ID); err != nil {
			return err
		}
	}
	sizes := make([]int, len(gs))
	pos := map[string]int{}
	for i, g := range gs {
		pos[g.ID] = i
	}
	rows, err := s.pool.Query(ctx, `SELECT id, group_id FROM poll.participants WHERE poll_id=$1`, p.ID)
	if err != nil {
		return err
	}
	var free []string
	for rows.Next() {
		var id string
		var g *string
		if err := rows.Scan(&id, &g); err != nil {
			rows.Close()
			return err
		}
		if g == nil {
			free = append(free, id)
		} else if i, ok := pos[*g]; ok {
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
		if _, err := s.pool.Exec(ctx, `UPDATE poll.participants SET group_id=$2 WHERE id=$1`, id, gs[best].ID); err != nil {
			return err
		}
	}
	return nil
}

// categoryGroups makes one group per classroom category and puts each
// identified participant in the group of their first category.
func (s *Service) categoryGroups(ctx context.Context, p *Poll, reassign bool) error {
	if p.ClassroomID == nil {
		return httpx.Invalid(map[string]string{"from": "link the poll to a classroom to use its categories"})
	}
	members, err := s.classes.CategoryMembers(ctx, p.TeacherID, *p.ClassroomID)
	if err != nil {
		return err
	}
	cats, err := s.classes.ListCategories(ctx, p.TeacherID, *p.ClassroomID)
	if err != nil {
		return err
	}
	if len(cats) == 0 {
		return httpx.Invalid(map[string]string{"from": "this classroom has no categories yet"})
	}
	gs, err := s.listGroups(ctx, p.ID)
	if err != nil {
		return err
	}
	byCat := map[string]string{}
	for _, g := range gs {
		if g.CategoryID != nil {
			byCat[*g.CategoryID] = g.ID
		}
	}
	for _, c := range cats {
		if _, ok := byCat[c.ID]; ok {
			continue
		}
		id := c.ID
		color := c.Color
		g, err := s.insertGroup(ctx, p.ID, c.Name, &color, &id)
		if err != nil {
			return err
		}
		byCat[c.ID] = g.ID
	}
	first := map[string]string{} // user → group
	for _, m := range members {
		if _, ok := first[m.UserID]; !ok {
			first[m.UserID] = byCat[m.CategoryID]
		}
	}
	cond := ` AND group_id IS NULL`
	if reassign {
		cond = ``
	}
	rows, err := s.pool.Query(ctx, `SELECT id, user_id FROM poll.participants WHERE poll_id=$1 AND user_id IS NOT NULL`+cond, p.ID)
	if err != nil {
		return err
	}
	type pu struct{ id, user string }
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pu, error) {
		var x pu
		err := r.Scan(&x.id, &x.user)
		return x, err
	})
	if err != nil {
		return err
	}
	for _, x := range list {
		var g any
		if id, ok := first[x.user]; ok {
			g = id
		} else if !reassign {
			continue
		}
		if _, err := s.pool.Exec(ctx, `UPDATE poll.participants SET group_id=$2, captain=false WHERE id=$1 AND group_id IS DISTINCT FROM $2::uuid`, x.id, g); err != nil {
			return err
		}
	}
	return nil
}

// ---------- joining ----------

// placeOnJoin puts a participant in a group according to the poll's mode.
// chosen is the group picked on the join screen (self-selection).
func (s *Service) placeOnJoin(ctx context.Context, p *Poll, pid string, userID *string, chosen string, rejoin bool) error {
	var group string
	switch p.Groups {
	case "self":
		if chosen == "" {
			return nil
		}
		if rejoin {
			// Changing group is allowed only before answering anything.
			var n int
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM poll.responses WHERE participant_id=$1`, pid).Scan(&n); err != nil || n > 0 {
				return err
			}
		}
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM poll.groups WHERE poll_id=$1 AND id=$2`, p.ID, chosen).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return errChooseGroup
		}
		group = chosen
	case "random":
		if rejoin {
			return nil
		}
		err := s.pool.QueryRow(ctx, `SELECT g.id FROM poll.groups g LEFT JOIN poll.participants pa ON pa.group_id=g.id
			WHERE g.poll_id=$1 GROUP BY g.id ORDER BY count(pa.id), random() LIMIT 1`, p.ID).Scan(&group)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
	case "categories":
		if rejoin || userID == nil || p.ClassroomID == nil {
			return nil
		}
		members, err := s.classes.CategoryMembers(ctx, p.TeacherID, *p.ClassroomID)
		if err != nil {
			return err
		}
		var cats []string
		for _, m := range members {
			if m.UserID == *userID {
				cats = append(cats, m.CategoryID)
			}
		}
		if len(cats) == 0 {
			return nil
		}
		err = s.pool.QueryRow(ctx, `SELECT id FROM poll.groups WHERE poll_id=$1 AND category_id = ANY($2) ORDER BY position LIMIT 1`, p.ID, cats).Scan(&group)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
	default:
		return nil
	}
	if _, err := s.pool.Exec(ctx, `UPDATE poll.participants SET group_id=$2, captain=false WHERE id=$1 AND group_id IS DISTINCT FROM $2::uuid`, pid, group); err != nil {
		return err
	}
	return s.fillCaptains(ctx, p.ID)
}

// ---------- answering ----------

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// groupGate enforces captain and first-answer acceptance. For first-answer
// groups it returns a transaction holding the group's row lock, so two
// teammates answering at once can't both get in; finish commits or rolls back.
func (s *Service) groupGate(ctx context.Context, p *Poll, pid, questionID string, clearing bool) (execer, func(error) error, error) {
	pass := func(err error) error { return err }
	if p.Groups == "off" || (p.GroupAcceptance != "first" && p.GroupAcceptance != "captain") {
		return s.pool, pass, nil
	}
	var group *string
	var captain bool
	if err := s.pool.QueryRow(ctx, `SELECT group_id, captain FROM poll.participants WHERE id=$1`, pid).Scan(&group, &captain); err != nil {
		return nil, nil, err
	}
	if group == nil {
		return s.pool, pass, nil // answers only for themselves
	}
	if p.GroupAcceptance == "captain" {
		if !captain {
			return nil, nil, errCaptainOnly
		}
		return s.pool, pass, nil
	}
	if clearing {
		return s.pool, pass, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	var n int
	if _, err := tx.Exec(ctx, `SELECT 1 FROM poll.groups WHERE id=$1 FOR UPDATE`, *group); err == nil {
		err = tx.QueryRow(ctx, `SELECT count(*) FROM poll.responses r JOIN poll.participants pa ON pa.id=r.participant_id
			WHERE pa.group_id=$1 AND r.question_id=$2 AND r.participant_id<>$3`, *group, questionID, pid).Scan(&n)
	}
	if err == nil && n > 0 {
		err = errGroupAnswered
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}
	return tx, func(err error) error {
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		return tx.Commit(ctx)
	}, nil
}

// groupAnswers returns teammates' answers (first-answer and captain polls).
func (s *Service) groupAnswers(ctx context.Context, p *Poll, pid string) (map[string]GroupAnswer, error) {
	if p.Groups == "off" || (p.GroupAcceptance != "first" && p.GroupAcceptance != "captain") {
		return nil, nil
	}
	names, _, err := s.participantNames(ctx, p, p.Names == "name")
	if err != nil {
		return nil, err
	}
	me := names[pid]
	if me == nil || me.groupID == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT r.participant_id, r.question_id, r.value FROM poll.responses r
		JOIN poll.participants pa ON pa.id=r.participant_id
		WHERE pa.group_id=$1 AND r.participant_id<>$2 AND NOT r.hidden`, *me.groupID, pid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]GroupAnswer{}
	for rows.Next() {
		var who, qid string
		var raw []byte
		if err := rows.Scan(&who, &qid, &raw); err != nil {
			return nil, err
		}
		var a Answer
		if json.Unmarshal(raw, &a) == nil {
			x := names[who]
			by := ""
			if x != nil {
				by = displayName(p, x.real, x.nickname, x.n)
			}
			out[qid] = GroupAnswer{By: by, Value: a}
		}
	}
	return out, rows.Err()
}

// ---------- scoring ----------

func (s *Service) groupInfos(ctx context.Context, pollID string) ([]GroupInfo, error) {
	rows, err := s.pool.Query(ctx, `SELECT g.id, g.name, g.color, count(pa.id) FROM poll.groups g
		LEFT JOIN poll.participants pa ON pa.group_id=g.id WHERE g.poll_id=$1
		GROUP BY g.id ORDER BY g.position, g.created_at`, pollID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (GroupInfo, error) {
		var g GroupInfo
		err := r.Scan(&g.ID, &g.Name, &g.Color, &g.Members)
		return g, err
	})
}

// combine turns the scores a group's members earned on one question into
// the group's score. Average and lowest count members who didn't answer as
// 0, so a group can't lift its average by letting only its best answer.
func combine(acceptance, calc string, scores []float64, members int) float64 {
	if len(scores) == 0 {
		return 0
	}
	switch acceptance {
	case "first", "captain":
		return scores[0]
	case "best":
		calc = "max"
	}
	sum, hi, lo := 0.0, scores[0], scores[0]
	for _, x := range scores {
		sum += x
		hi = max(hi, x)
		lo = min(lo, x)
	}
	switch calc {
	case "average":
		if members < len(scores) {
			members = len(scores)
		}
		return roundPts(sum / float64(members))
	case "max":
		return hi
	case "min":
		if len(scores) < members {
			return 0
		}
		return lo
	}
	return roundPts(sum)
}

func roundPts(x float64) float64 { return float64(int64(x*100+0.5)) / 100 }

// groupLeaderboard ranks groups by their combined score.
func (s *Service) groupLeaderboard(ctx context.Context, p *Poll) ([]GroupRank, error) {
	if p.Groups == "off" || !p.Scoring {
		return nil, nil
	}
	infos, err := s.groupInfos(ctx, p.ID)
	if err != nil || len(infos) == 0 {
		return nil, err
	}
	// first: the group's earliest answer; captain: the captain's answer.
	rows, err := s.pool.Query(ctx, `SELECT pa.group_id, r.question_id, r.score, pa.captain FROM poll.responses r
		JOIN poll.participants pa ON pa.id=r.participant_id
		WHERE r.poll_id=$1 AND pa.group_id IS NOT NULL AND r.score IS NOT NULL AND NOT r.hidden
		ORDER BY r.created_at, r.participant_id`, p.ID)
	if err != nil {
		return nil, err
	}
	type key struct{ g, q string }
	scores := map[key][]float64{}
	for rows.Next() {
		var g, q string
		var sc float64
		var captain bool
		if err := rows.Scan(&g, &q, &sc, &captain); err != nil {
			rows.Close()
			return nil, err
		}
		if p.GroupAcceptance == "captain" && !captain {
			continue
		}
		scores[key{g, q}] = append(scores[key{g, q}], sc)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]GroupRank, len(infos))
	pos := map[string]int{}
	for i, g := range infos {
		out[i] = GroupRank{ID: g.ID, Name: g.Name, Color: g.Color, Members: g.Members}
		pos[g.ID] = i
	}
	for k, list := range scores {
		i := pos[k.g]
		out[i].Score += combine(p.GroupAcceptance, p.GroupCalc, list, out[i].Members)
		out[i].Answered++
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	for i := range out {
		out[i].Score = roundPts(out[i].Score)
		if i > 0 && out[i].Score == out[i-1].Score {
			out[i].Rank = out[i-1].Rank
		} else {
			out[i].Rank = i + 1
		}
	}
	return out, nil
}
