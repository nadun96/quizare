package content

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// A classroom's groups (D-158) are its categories (D-41) with more to them:
// a leader and assistant leaders, a public name the leader chooses, and, while
// the teacher allows it, students choosing their own. Polls, quiz sessions and
// the class report form their groups from these.

// The roles in a group.
const (
	RoleMember    = "member"
	RoleLeader    = "leader"
	RoleAssistant = "assistant"
)

// GroupMember is a student in a group.
type GroupMember struct {
	EnrolmentID   string  `json:"enrolment_id,omitempty"`
	UserID        string  `json:"user_id"`
	Name          string  `json:"name"`
	StudentNumber *string `json:"student_number,omitempty"`
	Role          string  `json:"role"`
	SelfJoined    bool    `json:"self_joined"`      // the student chose it
	Gender        string  `json:"gender,omitempty"` // teachers only (D-162)
}

// ClassGroup is a group with its members.
type ClassGroup struct {
	Category
	People []GroupMember `json:"people"`
}

// GroupPolicy is whether students choose their own groups, when, and how many.
type GroupPolicy struct {
	SelfJoin      bool       `json:"self_join"`
	OpensAt       *time.Time `json:"opens_at"`
	ClosesAt      *time.Time `json:"closes_at"`
	MaxPerStudent int        `json:"max_per_student"`
	MaxSize       *int       `json:"max_size"`
	Open          bool       `json:"open"` // students may choose now
}

func (p *GroupPolicy) settle(now time.Time) {
	p.Open = p.SelfJoin && (p.OpensAt == nil || !now.Before(*p.OpensAt)) && (p.ClosesAt == nil || now.Before(*p.ClosesAt))
}

func (s *Service) groupPolicy(ctx context.Context, q pgx.Row) (GroupPolicy, error) {
	p := GroupPolicy{MaxPerStudent: 1}
	err := q.Scan(&p.SelfJoin, &p.OpensAt, &p.ClosesAt, &p.MaxPerStudent, &p.MaxSize)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	p.settle(time.Now())
	return p, err
}

const policyCols = `SELECT self_join, opens_at, closes_at, max_per_student, max_size FROM content.group_policy WHERE classroom_id=$1`

// ClassGroups lists a classroom's groups with their members, and the policy.
func (s *Service) ClassGroups(ctx context.Context, teacherID, classroomID string) ([]ClassGroup, GroupPolicy, error) {
	if _, err := s.GetClassroom(ctx, teacherID, classroomID); err != nil {
		return nil, GroupPolicy{}, err
	}
	pol, err := s.groupPolicy(ctx, s.pool.QueryRow(ctx, policyCols, classroomID))
	if err != nil {
		return nil, pol, err
	}
	groups, err := s.groupsWithMembers(ctx, classroomID, "")
	return groups, pol, err
}

// groupsWithMembers lists the groups; with onlyUser, only the members of groups that user is in.
func (s *Service) groupsWithMembers(ctx context.Context, classroomID, onlyUser string) ([]ClassGroup, error) {
	cats, err := s.categories(ctx, classroomID)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT ec.category_id, e.id, e.user_id, coalesce(u.name, ''), e.student_number, ec.role, ec.self_joined
		FROM content.enrolment_categories ec
		JOIN content.categories c ON c.id = ec.category_id
		JOIN content.enrolments e ON e.id = ec.enrolment_id AND e.status = 'active'
		LEFT JOIN auth.users u ON u.id = e.user_id
		WHERE c.classroom_id = $1
		  AND ($2 = '' OR ec.category_id IN (SELECT ec2.category_id FROM content.enrolment_categories ec2 JOIN content.enrolments e2 ON e2.id = ec2.enrolment_id WHERE e2.user_id::text = $2 AND e2.classroom_id = $1))
		ORDER BY CASE ec.role WHEN 'leader' THEN 0 WHEN 'assistant' THEN 1 ELSE 2 END, lower(u.name)`, classroomID, onlyUser)
	if err != nil {
		return nil, err
	}
	by := map[string][]GroupMember{}
	for rows.Next() {
		var cat string
		var m GroupMember
		if err := rows.Scan(&cat, &m.EnrolmentID, &m.UserID, &m.Name, &m.StudentNumber, &m.Role, &m.SelfJoined); err != nil {
			rows.Close()
			return nil, err
		}
		by[cat] = append(by[cat], m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ClassGroup, 0, len(cats))
	for _, c := range cats {
		ms := by[c.ID]
		if ms == nil {
			ms = []GroupMember{}
		}
		out = append(out, ClassGroup{Category: c, People: ms})
	}
	return out, nil
}

// activeStudents is everyone in the classroom, to put in groups.
func (s *Service) activeStudents(ctx context.Context, classroomID string) ([]GroupMember, error) {
	rows, err := s.pool.Query(ctx, `SELECT e.id, e.user_id, coalesce(u.name, ''), e.student_number, coalesce(u.gender, '') FROM content.enrolments e
		LEFT JOIN auth.users u ON u.id = e.user_id WHERE e.classroom_id = $1 AND e.status = 'active' ORDER BY lower(u.name), e.created_at`, classroomID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (GroupMember, error) {
		m := GroupMember{Role: RoleMember}
		err := r.Scan(&m.EnrolmentID, &m.UserID, &m.Name, &m.StudentNumber, &m.Gender)
		return m, err
	})
}

// GroupPolicyInput changes the policy.
type GroupPolicyInput struct {
	SelfJoin      bool       `json:"self_join"`
	OpensAt       *time.Time `json:"opens_at"`
	ClosesAt      *time.Time `json:"closes_at"`
	MaxPerStudent int        `json:"max_per_student"`
	MaxSize       *int       `json:"max_size"`
}

// SetGroupPolicy saves when and how students may choose their own groups.
func (s *Service) SetGroupPolicy(ctx context.Context, teacherID, classroomID string, in GroupPolicyInput) (GroupPolicy, error) {
	if _, err := s.GetClassroom(ctx, teacherID, classroomID); err != nil {
		return GroupPolicy{}, err
	}
	bad := map[string]string{}
	if in.MaxPerStudent < 1 || in.MaxPerStudent > 50 {
		bad["max_per_student"] = "between 1 and 50 groups"
	}
	if in.MaxSize != nil && (*in.MaxSize < 1 || *in.MaxSize > 1000) {
		bad["max_size"] = "between 1 and 1000 students, or no limit"
	}
	if in.OpensAt != nil && in.ClosesAt != nil && !in.ClosesAt.After(*in.OpensAt) {
		bad["closes_at"] = "it must close after it opens"
	}
	if len(bad) > 0 {
		return GroupPolicy{}, httpx.Invalid(bad)
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO content.group_policy (classroom_id, self_join, opens_at, closes_at, max_per_student, max_size)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (classroom_id) DO UPDATE SET self_join=$2, opens_at=$3, closes_at=$4, max_per_student=$5, max_size=$6, updated_at=now()`,
			classroomID, in.SelfJoin, in.OpensAt, in.ClosesAt, in.MaxPerStudent, in.MaxSize); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "group_policy_set", "classroom", classroomID, in)
	})
	if err != nil {
		return GroupPolicy{}, err
	}
	return s.groupPolicy(ctx, s.pool.QueryRow(ctx, policyCols, classroomID))
}

// SetMemberRole makes a member the leader, an assistant leader or a plain
// member. A new leader replaces the old one, who stays as a member.
func (s *Service) SetMemberRole(ctx context.Context, teacherID, categoryID, enrolmentID, role string) error {
	if _, err := s.ownedCategory(ctx, teacherID, categoryID); err != nil {
		return err
	}
	if role != RoleMember && role != RoleLeader && role != RoleAssistant {
		return httpx.Invalid(map[string]string{"role": "a member, the leader or an assistant leader"})
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if role == RoleLeader {
			if _, err := tx.Exec(ctx, `UPDATE content.enrolment_categories SET role='member' WHERE category_id=$1 AND role='leader' AND enrolment_id<>$2`, categoryID, enrolmentID); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE content.enrolment_categories SET role=$3 WHERE category_id=$1 AND enrolment_id=$2`, categoryID, enrolmentID, role)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.Invalid(map[string]string{"enrolment_id": "put the student in the group first"})
		}
		return audit.Log(ctx, tx, teacherID, "group_role_set", "category", categoryID, map[string]string{"enrolment": enrolmentID, "role": role})
	})
}

// ClearPublicName lets the teacher remove a leader's choice of name.
func (s *Service) ClearPublicName(ctx context.Context, teacherID, categoryID string) error {
	if _, err := s.ownedCategory(ctx, teacherID, categoryID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE content.categories SET public_name=NULL WHERE id=$1`, categoryID)
	return err
}

// MyGroup is a group as a student of the classroom sees it.
type MyGroup struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`         // the public name if the leader set one
	Teacher string        `json:"teacher_name"` // the name the teacher gave it
	Public  *string       `json:"public_name"`
	Color   int           `json:"color"`
	Size    int           `json:"size"`
	Mine    bool          `json:"mine"`
	Role    string        `json:"role,omitempty"`        // theirs, if they're in it
	Self    bool          `json:"self_joined,omitempty"` // they chose it, so they may leave
	Members []GroupMember `json:"members,omitempty"`     // only of their own groups
	Full    bool          `json:"full"`
}

// MyGroupsView is a student's view of the classroom's groups.
type MyGroupsView struct {
	Groups []MyGroup   `json:"groups"`
	Policy GroupPolicy `json:"policy"`
	Chosen int         `json:"chosen"` // groups they chose themselves
}

// enrolledActive returns the student's active enrolment in the classroom.
func (s *Service) enrolledActive(ctx context.Context, userID, classroomID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id FROM content.enrolments WHERE classroom_id=$1 AND user_id=$2 AND status='active'`, classroomID, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httpx.ErrNotFound
	}
	return id, err
}

// MyGroups is what a student sees of the classroom's groups.
func (s *Service) MyGroups(ctx context.Context, userID, classroomID string) (MyGroupsView, error) {
	if _, err := s.enrolledActive(ctx, userID, classroomID); err != nil {
		return MyGroupsView{}, err
	}
	pol, err := s.groupPolicy(ctx, s.pool.QueryRow(ctx, policyCols, classroomID))
	if err != nil {
		return MyGroupsView{}, err
	}
	all, err := s.groupsWithMembers(ctx, classroomID, userID)
	if err != nil {
		return MyGroupsView{}, err
	}
	view := MyGroupsView{Groups: []MyGroup{}, Policy: pol}
	for _, g := range all {
		mg := MyGroup{ID: g.ID, Name: g.Name, Teacher: g.Name, Public: g.PublicName, Color: g.Color, Size: g.Category.Members}
		if g.PublicName != nil {
			mg.Name = *g.PublicName
		}
		mg.Full = pol.MaxSize != nil && g.Category.Members >= *pol.MaxSize
		for _, m := range g.People {
			if m.UserID == userID {
				mg.Mine, mg.Role, mg.Self = true, m.Role, m.SelfJoined
				if m.SelfJoined {
					view.Chosen++
				}
			}
		}
		if mg.Mine { // their own group's members, without what only teachers see
			for _, m := range g.People {
				mg.Members = append(mg.Members, GroupMember{UserID: m.UserID, Name: m.Name, Role: m.Role})
			}
		}
		view.Groups = append(view.Groups, mg)
	}
	return view, nil
}

// groupOfStudent returns the group and its classroom, if the student is enrolled there.
func (s *Service) groupOfStudent(ctx context.Context, userID, categoryID string) (Category, string, error) {
	var c Category
	err := s.pool.QueryRow(ctx, `SELECT c.id, c.classroom_id, c.name, c.color, c.position FROM content.categories c WHERE c.id=$1`, categoryID).
		Scan(&c.ID, &c.ClassroomID, &c.Name, &c.Color, &c.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, "", httpx.ErrNotFound
	}
	if err != nil {
		return c, "", err
	}
	enrolment, err := s.enrolledActive(ctx, userID, c.ClassroomID)
	return c, enrolment, err
}

var errGroupsClosed = httpx.NewError(http.StatusConflict, "groups_closed", "your teacher isn't letting students choose groups now")

// JoinGroup puts the student in a group they choose, while the teacher allows it.
func (s *Service) JoinGroup(ctx context.Context, userID, categoryID string) error {
	c, enrolment, err := s.groupOfStudent(ctx, userID, categoryID)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// One student at a time per classroom, so limits hold.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('groups:' || $1))`, c.ClassroomID); err != nil {
			return err
		}
		pol, err := s.groupPolicy(ctx, tx.QueryRow(ctx, policyCols, c.ClassroomID))
		if err != nil {
			return err
		}
		if !pol.Open {
			return errGroupsClosed
		}
		var in, chosen, size int
		if err := tx.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM content.enrolment_categories WHERE enrolment_id=$1 AND category_id=$2),
				(SELECT count(*) FROM content.enrolment_categories WHERE enrolment_id=$1 AND self_joined),
				(SELECT count(*) FROM content.enrolment_categories ec JOIN content.enrolments e ON e.id=ec.enrolment_id AND e.status='active' WHERE ec.category_id=$2)`,
			enrolment, categoryID).Scan(&in, &chosen, &size); err != nil {
			return err
		}
		switch {
		case in > 0:
			return nil
		case chosen >= pol.MaxPerStudent:
			return httpx.NewError(http.StatusConflict, "group_limit", "you've joined as many groups as you may; leave one first")
		case pol.MaxSize != nil && size >= *pol.MaxSize:
			return httpx.NewError(http.StatusConflict, "group_full", "this group is full")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO content.enrolment_categories (enrolment_id, category_id, self_joined) VALUES ($1, $2, true)`, enrolment, categoryID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, userID, "group_joined", "category", categoryID, nil)
	})
}

// LeaveGroup takes the student out of a group they chose, while the teacher
// allows choosing. One the teacher put them in stays.
func (s *Service) LeaveGroup(ctx context.Context, userID, categoryID string) error {
	c, enrolment, err := s.groupOfStudent(ctx, userID, categoryID)
	if err != nil {
		return err
	}
	pol, err := s.groupPolicy(ctx, s.pool.QueryRow(ctx, policyCols, c.ClassroomID))
	if err != nil {
		return err
	}
	if !pol.Open {
		return errGroupsClosed
	}
	var self bool
	err = s.pool.QueryRow(ctx, `SELECT self_joined FROM content.enrolment_categories WHERE enrolment_id=$1 AND category_id=$2`, enrolment, categoryID).Scan(&self)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !self {
		return httpx.NewError(http.StatusConflict, "teacher_placed", "your teacher put you in this group, so only they can move you")
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM content.enrolment_categories WHERE enrolment_id=$1 AND category_id=$2`, enrolment, categoryID)
	return err
}

// SetPublicName is the leader naming their group for everyone to see; ""
// goes back to the teacher's name.
func (s *Service) SetPublicName(ctx context.Context, userID, categoryID, name string) error {
	c, enrolment, err := s.groupOfStudent(ctx, userID, categoryID)
	if err != nil {
		return err
	}
	var role string
	err = s.pool.QueryRow(ctx, `SELECT role FROM content.enrolment_categories WHERE enrolment_id=$1 AND category_id=$2`, enrolment, categoryID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && role != RoleLeader) {
		return httpx.NewError(http.StatusForbidden, "leader_only", "only the group's leader can name it")
	}
	if err != nil {
		return err
	}
	name = strings.Join(strings.Fields(name), " ")
	var value any
	if name != "" {
		if n := utf8.RuneCountInString(name); n > 40 {
			return httpx.Invalid(map[string]string{"public_name": "at most 40 characters"})
		}
		value = name
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var clash bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM content.categories WHERE classroom_id=$1 AND id<>$2 AND (lower(name)=lower($3) OR lower(public_name)=lower($3)))`, c.ClassroomID, categoryID, name).Scan(&clash); err != nil {
			return err
		}
		if clash {
			return httpx.Invalid(map[string]string{"public_name": "another group already has this name"})
		}
		if _, err := tx.Exec(ctx, `UPDATE content.categories SET public_name=$2 WHERE id=$1`, categoryID, value); err != nil {
			return err
		}
		return audit.Log(ctx, tx, userID, "group_named", "category", categoryID, map[string]string{"name": name})
	})
	if isUnique(err, "categories_public_name_key") {
		return httpx.Invalid(map[string]string{"public_name": "another group already has this name"})
	}
	return err
}

func (s *Service) groupRoutes(r chi.Router) {
	r.Method("GET", "/classrooms/{id}/groups", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		gs, pol, err := s.ClassGroups(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		students, err := s.activeStudents(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"groups": gs, "policy": pol, "students": students})
		return nil
	}))
	r.Method("PUT", "/classrooms/{id}/group-policy", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in GroupPolicyInput
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		p, err := s.SetGroupPolicy(r.Context(), uid(r), chi.URLParam(r, "id"), in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, p)
		return nil
	}))
	r.Method("PUT", "/categories/{id}/members/{enrolment}/role", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Role string `json:"role"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		if err := s.SetMemberRole(r.Context(), uid(r), chi.URLParam(r, "id"), chi.URLParam(r, "enrolment"), in.Role); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("DELETE", "/categories/{id}/public-name", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.ClearPublicName(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
}

func (s *Service) myGroupRoutes(r chi.Router) {
	r.Method("GET", "/my/classrooms/{id}/groups", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		v, err := s.MyGroups(r.Context(), uid(r), chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, v)
		return nil
	}))
	r.Method("POST", "/my/groups/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.JoinGroup(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("DELETE", "/my/groups/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.LeaveGroup(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	r.Method("PUT", "/my/groups/{id}/name", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Name string `json:"name"`
		}
		if err := httpx.Decode(w, r, &in); err != nil {
			return err
		}
		if err := s.SetPublicName(r.Context(), uid(r), chi.URLParam(r, "id"), in.Name); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
}
