package tutor

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/media"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Breakout groups (D-79, TS-FR-46, TS-FR-50 to TS-FR-54). Each group meets
// in its own media room: its members publish what they may and see and hear
// each other there, which the tokens enforce (TS-FR-37 allows it within a
// group). Everyone stays in the main broadcast as well, so the teacher can
// speak to all groups at once. Teachers visit a group by joining its room.

// Group is one breakout group.
type Group struct {
	ID      int64      `json:"id"`
	Name    string     `json:"name"`
	Members []string   `json:"members"` // user ids; for students, only their own group's
	Size    int        `json:"size"`
	HelpAt  *time.Time `json:"help_at,omitempty"` // asking for help (TS-FR-54); teachers only
}

func groupRoom(sessionID string, groupID int64) string {
	return sessionID + "-g" + strconv.FormatInt(groupID, 10)
}

const maxGroups = 50

var (
	errGroupsOpen   = web.NewError(http.StatusConflict, "groups_open", "close the groups first")
	errGroupsClosed = web.NewError(http.StatusConflict, "groups_closed", "the groups aren't open")
	errNoGroups     = web.NewError(http.StatusConflict, "no_groups", "make the groups first")
	errNotInGroup   = web.NewError(http.StatusConflict, "not_in_group", "you aren't in a group")
)

// groupSources is what p may publish in their group: their own permissions
// and what the teacher allows everyone in groups (TS-FR-53); a teacher
// visiting may use everything.
func groupSources(sess Session, p Participant) []string {
	if p.Staff() {
		return allSources
	}
	set := map[string]bool{}
	for _, s := range p.allowed() {
		set[s] = true
	}
	if sess.GroupMic {
		set[media.Microphone] = true
	}
	if sess.GroupCamera {
		set[media.Camera] = true
	}
	if sess.GroupScreen {
		set[media.Screen], set[media.ScreenAudio] = true, true
	}
	out := []string{}
	for _, s := range allSources {
		if set[s] {
			out = append(out, s)
		}
	}
	return out
}

// groupRoomOf is the group room p belongs in now, or "".
func groupRoomOf(sess Session, p Participant) string {
	switch {
	case !sess.GroupsOpen || sess.Status != "live":
		return ""
	case p.Staff() && p.Visiting != nil:
		return groupRoom(sess.ID, *p.Visiting)
	case !p.Staff() && p.Group != nil && p.State == "admitted":
		return groupRoom(sess.ID, *p.Group)
	}
	return ""
}

func (s *Service) groups(ctx context.Context, q interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}, sessionID string) ([]Group, error) {
	rows, err := q.Query(ctx, `SELECT g.id, g.name, g.help_at, coalesce(array_agg(m.user_id::text ORDER BY m.user_id) FILTER (WHERE m.user_id IS NOT NULL), '{}')
		FROM tutoring.groups g LEFT JOIN tutoring.group_members m ON m.group_id = g.id
		WHERE g.session_id=$1 GROUP BY g.id ORDER BY g.position, g.id`, sessionID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Group, error) {
		var g Group
		err := r.Scan(&g.ID, &g.Name, &g.HelpAt, &g.Members)
		g.Size = len(g.Members)
		return g, err
	})
	if out == nil {
		out = []Group{}
	}
	return out, err
}

// FormInput makes the groups (TS-FR-50).
type FormInput struct {
	// Mode: random (Count groups, or Size students each), categories (the
	// classroom's), choose (Count empty groups students pick from) or
	// manual (Groups as given).
	Mode   string `json:"mode"`
	Count  int    `json:"count,omitempty"`
	Size   int    `json:"size,omitempty"`
	Groups []struct {
		Name    string   `json:"name"`
		UserIDs []string `json:"user_ids"`
	} `json:"groups,omitempty"`
}

type planned struct {
	name    string
	members []string
}

// Form makes the session's groups, replacing any before. Only admitted
// students are placed; anyone joining later can be moved in.
func (s *Service) Form(ctx context.Context, p authn.Person, id string, in FormInput) ([]Group, error) {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if sess.GroupsOpen {
		return nil, errGroupsOpen
	}
	if sess.Status == "ended" {
		return nil, errEnded
	}
	rows, _ := s.pool.Query(ctx, `SELECT user_id::text FROM tutoring.participants WHERE session_id=$1 AND role='student' AND state='admitted' ORDER BY lower(name)`, id)
	students, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	var plan []planned
	switch in.Mode {
	case "random", "choose":
		n := in.Count
		if in.Mode == "random" && in.Size > 0 {
			n = (len(students) + in.Size - 1) / in.Size
		}
		if n < 1 || n > maxGroups {
			return nil, web.Invalid(map[string]string{"count": fmt.Sprintf("1 to %d groups", maxGroups)})
		}
		plan = make([]planned, n)
		for i := range plan {
			plan[i].name = fmt.Sprintf("Group %d", i+1)
		}
		if in.Mode == "random" {
			order := append([]string(nil), students...)
			rand.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
			for i, u := range order {
				plan[i%n].members = append(plan[i%n].members, u)
			}
		}
	case "categories":
		cats, err := s.platform.Categories(ctx, sess.ClassroomID, sess.TeacherID)
		if err != nil {
			return nil, err
		}
		here := map[string]bool{}
		for _, u := range students {
			here[u] = true
		}
		// A student in several categories goes to the first; the rest to "Others".
		placed := map[string]bool{}
		for _, c := range cats {
			g := planned{name: c.Name}
			for _, u := range c.UserIDs {
				if here[u] && !placed[u] {
					g.members = append(g.members, u)
					placed[u] = true
				}
			}
			if len(g.members) > 0 {
				plan = append(plan, g)
			}
		}
		var others []string
		for _, u := range students {
			if !placed[u] {
				others = append(others, u)
			}
		}
		if len(others) > 0 {
			plan = append(plan, planned{name: "Others", members: others})
		}
		if len(plan) == 0 {
			return nil, web.Invalid(map[string]string{"mode": "nobody here is in a category: give the classroom's students categories first"})
		}
	case "manual":
		known := map[string]bool{}
		for _, u := range students {
			known[u] = true
		}
		seen := map[string]bool{}
		for i, g := range in.Groups {
			name := strings.TrimSpace(g.Name)
			if name == "" {
				name = fmt.Sprintf("Group %d", i+1)
			}
			if len([]rune(name)) > 60 {
				return nil, web.Invalid(map[string]string{"groups": "a group name has at most 60 characters"})
			}
			pg := planned{name: name}
			for _, u := range g.UserIDs {
				if !known[u] || seen[u] {
					return nil, web.Invalid(map[string]string{"groups": "put each student who's here in one group at most"})
				}
				seen[u] = true
				pg.members = append(pg.members, u)
			}
			plan = append(plan, pg)
		}
		if len(plan) < 1 || len(plan) > maxGroups {
			return nil, web.Invalid(map[string]string{"groups": fmt.Sprintf("1 to %d groups", maxGroups)})
		}
	default:
		return nil, web.Invalid(map[string]string{"mode": "random, categories, choose or manual"})
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM tutoring.groups WHERE session_id=$1`, id); err != nil {
			return err
		}
		for i, g := range plan {
			var gid int64
			if err := tx.QueryRow(ctx, `INSERT INTO tutoring.groups (session_id, name, position) VALUES ($1,$2,$3) RETURNING id`, id, g.name, i).Scan(&gid); err != nil {
				return err
			}
			for _, u := range g.members {
				if _, err := tx.Exec(ctx, `INSERT INTO tutoring.group_members (session_id, user_id, group_id) VALUES ($1,$2,$3)`, id, u, gid); err != nil {
					return err
				}
			}
		}
		_, err := tx.Exec(ctx, `UPDATE tutoring.sessions SET groups_choose=$2 WHERE id=$1`, id, in.Mode == "choose")
		return err
	})
	if err != nil {
		return nil, err
	}
	s.logAction(ctx, id, p.ID, "groups_formed", nil, map[string]any{"mode": in.Mode, "groups": len(plan)})
	s.hub.changed(id)
	return s.groups(ctx, s.pool, id)
}

// Rename renames one group.
func (s *Service) RenameGroup(ctx context.Context, p authn.Person, id string, groupID int64, name string) error {
	if _, err := s.staffed(ctx, p, id); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if n := len([]rune(name)); n < 1 || n > 60 {
		return web.Invalid(map[string]string{"name": "1 to 60 characters"})
	}
	tag, err := s.pool.Exec(ctx, `UPDATE tutoring.groups SET name=$3 WHERE session_id=$1 AND id=$2`, id, groupID, name)
	if err == nil && tag.RowsAffected() == 0 {
		return web.ErrNotFound
	}
	s.hub.changed(id)
	return err
}

// place puts a student in a group (or takes them out, groupID 0). While the
// groups are open they leave their old group's room at once; the page then
// joins the new one.
func (s *Service) place(ctx context.Context, sess Session, userID string, groupID int64) error {
	old, err := s.participant(ctx, sess.ID, userID)
	if err != nil {
		return err
	}
	if groupID == 0 {
		_, err = s.pool.Exec(ctx, `DELETE FROM tutoring.group_members WHERE session_id=$1 AND user_id=$2`, sess.ID, userID)
	} else {
		var ok bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tutoring.groups WHERE session_id=$1 AND id=$2)`, sess.ID, groupID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return web.ErrNotFound
		}
		_, err = s.pool.Exec(ctx, `INSERT INTO tutoring.group_members (session_id, user_id, group_id) VALUES ($1,$2,$3)
			ON CONFLICT (session_id, user_id) DO UPDATE SET group_id=EXCLUDED.group_id`, sess.ID, userID, groupID)
	}
	if err != nil {
		return err
	}
	if room := groupRoomOf(sess, old); room != "" && (old.Group == nil || *old.Group != groupID) {
		if err := s.media.Remove(ctx, room, userID); err != nil && !errors.Is(err, media.ErrNotInRoom) {
			s.log.Warn("leaving a group room", "session", sess.ID, "err", err)
		}
	}
	s.hub.changed(sess.ID)
	return nil
}

// Move puts a student in another group, or none (TS-FR-52).
func (s *Service) Move(ctx context.Context, p authn.Person, id, userID string, groupID int64) error {
	sess, err := s.staffed(ctx, p, id)
	if err != nil {
		return err
	}
	target, err := s.participant(ctx, id, userID)
	if err != nil {
		return err
	}
	if target.Role != "student" {
		return web.Invalid(map[string]string{"user_id": "only students are in groups"})
	}
	if err := s.place(ctx, sess, userID, groupID); err != nil {
		return err
	}
	s.logAction(ctx, id, p.ID, "group_moved", &userID, map[string]int64{"group": groupID})
	return nil
}

// Choose is a student picking their group, when the teacher lets them.
func (s *Service) Choose(ctx context.Context, p authn.Person, id string, groupID int64) error {
	sess, err := s.session(ctx, id)
	if err != nil {
		return err
	}
	me, err := s.participant(ctx, id, p.ID)
	if err != nil {
		return err
	}
	if me.Role != "student" || me.State != "admitted" {
		return web.ErrForbidden
	}
	if !sess.GroupsChoose {
		return web.NewError(http.StatusForbidden, "groups_assigned", "your teacher puts students in groups")
	}
	return s.place(ctx, sess, p.ID, groupID)
}

// OpenGroups sends every student to their group (TS-FR-51).
func (s *Service) OpenGroups(ctx context.Context, p authn.Person, id string) error {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return err
	}
	if sess.Status != "live" {
		return errNotLive
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tutoring.groups WHERE session_id=$1`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return errNoGroups
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET groups_open=true, groups_close_at=NULL WHERE id=$1`, id); err != nil {
		return err
	}
	s.logAction(ctx, id, p.ID, "groups_opened", nil, map[string]int{"groups": n})
	s.hub.broadcast(id, event{Type: "groups_opened"})
	s.hub.changed(id)
	return nil
}

// CloseGroups brings everyone back, now or after a countdown of up to 5
// minutes that students see (TS-FR-51).
func (s *Service) CloseGroups(ctx context.Context, p authn.Person, id string, countdown int) error {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return err
	}
	if !sess.GroupsOpen {
		return errGroupsClosed
	}
	if countdown < 0 || countdown > 300 {
		return web.Invalid(map[string]string{"countdown": "0 to 300 seconds"})
	}
	if countdown == 0 {
		return s.closeGroups(ctx, id, p.ID)
	}
	at := s.now().Add(time.Duration(countdown) * time.Second)
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET groups_close_at=$2 WHERE id=$1`, id, at); err != nil {
		return err
	}
	s.hub.changed(id)
	time.AfterFunc(time.Until(at), func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Only if this countdown is still the one set: groups may have been closed or reopened since.
		var still bool
		if err := s.pool.QueryRow(ctx, `SELECT groups_open AND groups_close_at=$2 FROM tutoring.sessions WHERE id=$1`, id, at).Scan(&still); err == nil && still {
			if err := s.closeGroups(ctx, id, p.ID); err != nil {
				s.log.Warn("closing groups", "session", id, "err", err)
			}
		}
	})
	return nil
}

// closeGroups closes them now: the group rooms end, teachers stop visiting.
func (s *Service) closeGroups(ctx context.Context, id, actorID string) error {
	gs, err := s.groups(ctx, s.pool, id)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET groups_open=false, groups_close_at=NULL WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.participants SET visiting=NULL WHERE session_id=$1`, id); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.groups SET help_at=NULL WHERE session_id=$1`, id); err != nil {
		return err
	}
	s.logAction(ctx, id, actorID, "groups_closed", nil, nil)
	s.hub.broadcast(id, event{Type: "groups_closed"})
	s.hub.changed(id)
	s.endGroupRooms(ctx, id, gs)
	return nil
}

func (s *Service) endGroupRooms(ctx context.Context, id string, gs []Group) {
	for _, g := range gs {
		if err := s.media.EndRoom(ctx, groupRoom(id, g.ID)); err != nil && !errors.Is(err, media.ErrNotInRoom) {
			s.log.Warn("ending a group room", "session", id, "group", g.ID, "err", err)
		}
	}
}

// CloseDueGroups closes groups whose countdown ran out while the service
// was down; run at start and now and then.
func (s *Service) CloseDueGroups(ctx context.Context) error {
	rows, _ := s.pool.Query(ctx, `SELECT id::text FROM tutoring.sessions WHERE groups_open AND groups_close_at <= $1`, s.now())
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, id := range ids {
		var teacher string
		if err := s.pool.QueryRow(ctx, `SELECT teacher_id::text FROM tutoring.sessions WHERE id=$1`, id).Scan(&teacher); err == nil {
			if err := s.closeGroups(ctx, id, teacher); err != nil {
				return err
			}
		}
	}
	return nil
}

// GroupSettingsInput changes group chat and what students may do in groups.
type GroupSettingsInput struct {
	Chat   *bool `json:"group_chat,omitempty"`
	Mic    *bool `json:"group_mic,omitempty"`
	Camera *bool `json:"group_camera,omitempty"`
	Screen *bool `json:"group_screen,omitempty"`
	Choose *bool `json:"groups_choose,omitempty"`
}

// GroupSettings changes them (the lead teacher's, PO-14); a change applies
// at once in the open groups.
func (s *Service) GroupSettings(ctx context.Context, p authn.Person, id string, in GroupSettingsInput) error {
	if _, err := s.owned(ctx, p, id); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET group_chat=coalesce($2, group_chat), group_mic=coalesce($3, group_mic),
		group_camera=coalesce($4, group_camera), group_screen=coalesce($5, group_screen), groups_choose=coalesce($6, groups_choose) WHERE id=$1`,
		id, in.Chat, in.Mic, in.Camera, in.Screen, in.Choose); err != nil {
		return err
	}
	s.logAction(ctx, id, p.ID, "group_settings_changed", nil, in)
	sess, err := s.session(ctx, id)
	if err != nil {
		return err
	}
	if sess.GroupsOpen {
		rows, _ := s.pool.Query(ctx, `SELECT `+participantCols+` FROM tutoring.participants WHERE session_id=$1 AND role='student' AND state='admitted'`, id)
		ps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Participant, error) { return scanParticipant(r) })
		if err != nil {
			return err
		}
		for _, sp := range ps {
			s.applyGroup(ctx, sess, sp)
		}
	}
	s.hub.changed(id)
	return nil
}

// applyGroup tells LiveKit what a student may now publish in their group.
func (s *Service) applyGroup(ctx context.Context, sess Session, p Participant) {
	room := groupRoomOf(sess, p)
	if room == "" {
		return
	}
	if err := s.media.SetPermission(ctx, room, p.UserID, true, groupSources(sess, p)); err != nil && !errors.Is(err, media.ErrNotInRoom) {
		s.log.Warn("group media permission", "session", sess.ID, "user", p.UserID, "err", err)
	}
}

// Visit has a teacher join a group, where they're seen and heard (TS-FR-52),
// or leave it (groupID 0). Visiting answers the group's call for help.
func (s *Service) Visit(ctx context.Context, p authn.Person, id string, groupID int64) error {
	sess, err := s.staffed(ctx, p, id)
	if err != nil {
		return err
	}
	if groupID != 0 && !sess.GroupsOpen {
		return errGroupsClosed
	}
	me, err := s.participant(ctx, id, p.ID)
	if err != nil {
		return err
	}
	var to *int64
	if groupID != 0 {
		tag, err := s.pool.Exec(ctx, `UPDATE tutoring.groups SET help_at=NULL WHERE session_id=$1 AND id=$2`, id, groupID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return web.ErrNotFound
		}
		to = &groupID
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.participants SET visiting=$3 WHERE session_id=$1 AND user_id=$2`, id, p.ID, to); err != nil {
		return err
	}
	if room := groupRoomOf(sess, me); room != "" && (to == nil || *me.Visiting != groupID) {
		if err := s.media.Remove(ctx, room, p.ID); err != nil && !errors.Is(err, media.ErrNotInRoom) {
			s.log.Warn("leaving a group room", "session", id, "err", err)
		}
	}
	if to != nil {
		s.logAction(ctx, id, p.ID, "group_visited", nil, map[string]int64{"group": groupID})
	}
	s.hub.changed(id)
	return nil
}

// Help is a student in a group calling the teacher, or calling off (TS-FR-54).
func (s *Service) Help(ctx context.Context, p authn.Person, id string, on bool) error {
	sess, err := s.session(ctx, id)
	if err != nil {
		return err
	}
	me, err := s.participant(ctx, id, p.ID)
	if err != nil {
		return err
	}
	if !sess.GroupsOpen {
		return errGroupsClosed
	}
	if me.Group == nil || me.State != "admitted" {
		return errNotInGroup
	}
	var at *time.Time
	if on {
		now := s.now()
		at = &now
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.groups SET help_at=$3 WHERE session_id=$1 AND id=$2`, id, *me.Group, at); err != nil {
		return err
	}
	if on {
		s.toStaff(id, event{Type: "group_help", Data: map[string]any{"group": *me.Group, "name": me.Name}})
	}
	s.hub.changed(id)
	return nil
}

// DismissHelp clears a group's call for help without visiting.
func (s *Service) DismissHelp(ctx context.Context, p authn.Person, id string, groupID int64) error {
	if _, err := s.staffed(ctx, p, id); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.groups SET help_at=NULL WHERE session_id=$1 AND id=$2`, id, groupID); err != nil {
		return err
	}
	s.hub.changed(id)
	return nil
}

// Announce sends a message to every group at once (TS-FR-52); it also
// stays in the main chat.
func (s *Service) Announce(ctx context.Context, p authn.Person, id, text string) (Message, error) {
	sess, err := s.staffed(ctx, p, id)
	if err != nil {
		return Message{}, err
	}
	if !sess.GroupsOpen {
		return Message{}, errGroupsClosed
	}
	m, err := s.Post(ctx, p, id, PostInput{Text: text})
	if err != nil {
		return m, err
	}
	s.hub.broadcast(id, event{Type: "announcement", Data: m})
	return m, nil
}
