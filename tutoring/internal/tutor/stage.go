package tutor

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/media"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Two media rooms per session (D-60):
//
//   - the main room is the broadcast: only the broadcaster publishes there
//     (and the lead teacher's microphone while someone else broadcasts),
//     and everyone watches;
//   - the backstage room holds what students and co-teachers share: they
//     publish there, and only teachers may watch (canSubscribe is false in
//     students' tokens), so no student sees another's camera or screen
//     unless that student broadcasts (TS-FR-25, TS-FR-37, TS-NFR-22).
//
// Both are enforced by LiveKit through the tokens and live permission
// updates, whatever a browser does.

// Person is someone named in a view.
type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
}

func stageRoom(sessionID string) string { return sessionID + "-stage" }

var allSources = []string{media.Camera, media.Microphone, media.Screen, media.ScreenAudio}

// broadcasting says whether p is the session's broadcaster.
func broadcasting(sess Session, p Participant) bool {
	if sess.BroadcasterID == nil {
		return p.Role == "teacher"
	}
	return *sess.BroadcasterID == p.UserID
}

// mainSources is what p may publish to everyone. While someone else
// broadcasts, the lead teacher keeps only the microphone (TS-FR-17).
func mainSources(sess Session, p Participant) []string {
	switch {
	case broadcasting(sess, p):
		return allSources
	case p.Role == "teacher":
		return []string{media.Microphone}
	}
	return []string{}
}

// stageSources is what p may publish for the teachers to see: everything
// for co-teachers (TS-FR-72), what was allowed for students.
func stageSources(sess Session, p Participant) []string {
	switch {
	case broadcasting(sess, p), p.Role == "teacher":
		return []string{}
	case p.Role == "coteacher":
		return allSources
	}
	return p.allowed()
}

// applyMedia tells LiveKit what p may now do in both rooms; someone not
// connected gets it in their next tokens.
func (s *Service) applyMedia(ctx context.Context, sess Session, p Participant) {
	if err := s.media.SetPermission(ctx, sess.ID, p.UserID, true, mainSources(sess, p)); err != nil && !errors.Is(err, media.ErrNotInRoom) {
		s.log.Warn("media permission", "session", sess.ID, "user", p.UserID, "err", err)
	}
	if err := s.media.SetPermission(ctx, stageRoom(sess.ID), p.UserID, p.Staff(), stageSources(sess, p)); err != nil && !errors.Is(err, media.ErrNotInRoom) {
		s.log.Warn("media permission", "session", sess.ID, "room", "stage", "user", p.UserID, "err", err)
	}
}

// applyAll re-applies media permissions for the given people.
func (s *Service) applyAll(ctx context.Context, id string, userIDs ...string) error {
	sess, err := s.session(ctx, id)
	if err != nil {
		return err
	}
	for _, u := range userIDs {
		if p, err := s.participant(ctx, id, u); err == nil {
			s.applyMedia(ctx, sess, p)
		}
	}
	return nil
}

// ---------- the broadcaster (TS-FR-17) ----------

// SetBroadcaster asks someone to broadcast instead of the lead teacher, or
// takes the broadcast back (userID "" or the lead teacher's own id).
// The person asked starts sharing only after accepting (PO-3).
func (s *Service) SetBroadcaster(ctx context.Context, p authn.Person, id, userID string) error {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return err
	}
	if userID == "" || userID == sess.TeacherID {
		prev := sess.BroadcasterID
		if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET broadcaster_id=NULL, broadcast_offer=NULL WHERE id=$1`, id); err != nil {
			return err
		}
		targets := []string{sess.TeacherID}
		if prev != nil {
			targets = append(targets, *prev)
			s.hub.send(id, *prev, event{Type: "broadcast_ended"})
		}
		s.logAction(ctx, id, p.ID, "broadcast_taken_back", prev, nil)
		s.hub.changed(id)
		return s.applyAll(ctx, id, targets...)
	}
	target, err := s.participant(ctx, id, userID)
	if err != nil {
		return err
	}
	if target.State != "admitted" || (target.Role != "student" && target.Role != "coteacher") {
		return web.Invalid(map[string]string{"user_id": "choose someone in the session"})
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET broadcast_offer=$2 WHERE id=$1`, id, userID); err != nil {
		return err
	}
	s.hub.send(id, userID, event{Type: "broadcast_offer"})
	s.hub.changed(id)
	return nil
}

// AnswerBroadcast is the asked person's answer. Accepting makes them the
// broadcaster at once: the previous broadcaster stops, and the lead
// teacher keeps only their microphone in the broadcast.
func (s *Service) AnswerBroadcast(ctx context.Context, p authn.Person, id string, accept bool) error {
	sess, err := s.session(ctx, id)
	if err != nil {
		return err
	}
	if sess.BroadcastOffer == nil || *sess.BroadcastOffer != p.ID {
		return web.Conflict("nobody asked you to broadcast")
	}
	me, err := s.participant(ctx, id, p.ID)
	if err != nil {
		return err
	}
	if !accept {
		if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET broadcast_offer=NULL WHERE id=$1`, id); err != nil {
			return err
		}
		s.toStaff(id, event{Type: "broadcast_answer", Data: map[string]any{"user_id": p.ID, "name": me.Name, "accepted": false}})
		s.hub.changed(id)
		return nil
	}
	prev := sess.BroadcasterID
	tag, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET broadcaster_id=$2, broadcast_offer=NULL WHERE id=$1 AND broadcast_offer=$2`, id, p.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return web.Conflict("nobody asked you to broadcast")
	}
	targets := []string{sess.TeacherID, p.ID}
	if prev != nil && *prev != p.ID {
		targets = append(targets, *prev)
		s.hub.send(id, *prev, event{Type: "broadcast_ended"})
	}
	s.logAction(ctx, id, p.ID, "broadcast_started", &p.ID, nil)
	s.toStaff(id, event{Type: "broadcast_answer", Data: map[string]any{"user_id": p.ID, "name": me.Name, "accepted": true}})
	s.hub.changed(id)
	return s.applyAll(ctx, id, targets...)
}

// toStaff sends an event to the lead teacher and co-teachers.
func (s *Service) toStaff(id string, e event) {
	staff := map[string]bool{}
	rows, err := s.pool.Query(context.Background(), `SELECT user_id FROM tutoring.participants WHERE session_id=$1 AND role IN ('teacher', 'coteacher')`, id)
	if err == nil {
		ids, _ := pgx.CollectRows(rows, pgx.RowTo[string])
		for _, u := range ids {
			staff[u] = true
		}
	}
	for u := range staff {
		s.hub.send(id, u, e)
	}
}

// AskAnswer tells the teachers whether a student accepted or declined a
// request to turn something on (TS-FR-22).
func (s *Service) AskAnswer(ctx context.Context, p authn.Person, id string, accepted bool) error {
	me, err := s.participant(ctx, id, p.ID)
	if err != nil {
		return err
	}
	if me.State != "admitted" || me.Staff() {
		return web.ErrForbidden
	}
	s.toStaff(id, event{Type: "ask_answer", Data: map[string]any{"user_id": p.ID, "name": me.Name, "accepted": accepted}})
	return nil
}

// ---------- co-teachers (TS-FR-70 to TS-FR-73) ----------

// Coteachers lists a session's co-teachers.
func (s *Service) Coteachers(ctx context.Context, p authn.Person, id string) ([]Person, error) {
	if _, err := s.staffed(ctx, p, id); err != nil {
		return nil, err
	}
	rows, _ := s.pool.Query(ctx, `SELECT user_id, name FROM tutoring.coteachers WHERE session_id=$1 ORDER BY lower(name)`, id)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Person, error) {
		x := Person{Role: "coteacher"}
		err := r.Scan(&x.ID, &x.Name)
		return x, err
	})
}

// AddCoteacher adds another teacher by their email; the platform confirms
// it is an active teacher account.
func (s *Service) AddCoteacher(ctx context.Context, p authn.Person, id, email string) (Person, error) {
	sess, err := s.owned(ctx, p, id)
	if err != nil {
		return Person{}, err
	}
	if strings.TrimSpace(email) == "" {
		return Person{}, web.Invalid(map[string]string{"email": "enter the teacher's email"})
	}
	t, err := s.platform.Teacher(ctx, email)
	if err != nil {
		var we *web.Error
		if errors.As(err, &we) && we.Status == http.StatusNotFound {
			return Person{}, web.Invalid(map[string]string{"email": "no active teacher account has this email"})
		}
		return Person{}, err
	}
	if t.ID == sess.TeacherID {
		return Person{}, web.Invalid(map[string]string{"email": "you lead this session already"})
	}
	if existing, err := s.participant(ctx, id, t.ID); err == nil && existing.Role == "student" {
		return Person{}, web.Invalid(map[string]string{"email": "this person joined as a student"})
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO tutoring.coteachers (session_id, user_id, name) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, id, t.ID, t.Name); err != nil {
		return Person{}, err
	}
	s.logAction(ctx, id, p.ID, "coteacher_added", &t.ID, nil)
	s.hub.changed(id)
	return Person{ID: t.ID, Name: t.Name, Role: "coteacher"}, nil
}

// RemoveCoteacher takes a co-teacher's role away; they leave the session.
func (s *Service) RemoveCoteacher(ctx context.Context, p authn.Person, id, userID string) error {
	if _, err := s.owned(ctx, p, id); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM tutoring.coteachers WHERE session_id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return web.ErrNotFound
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM tutoring.participants WHERE session_id=$1 AND user_id=$2 AND role='coteacher'`, id, userID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tutoring.sessions SET broadcaster_id = NULLIF(broadcaster_id, $2), broadcast_offer = NULLIF(broadcast_offer, $2) WHERE id=$1`, id, userID); err != nil {
		return err
	}
	for _, room := range []string{id, stageRoom(id)} {
		if err := s.media.Remove(ctx, room, userID); err != nil && !errors.Is(err, media.ErrNotInRoom) {
			return err
		}
	}
	s.logAction(ctx, id, p.ID, "coteacher_removed", &userID, nil)
	s.hub.kick(id, userID, "removed")
	s.hub.changed(id)
	return s.applyAll(ctx, id, p.ID) // the lead may have been on the microphone only
}

// ---------- HTTP ----------

func (s *Service) hCoteachers(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Coteachers(r.Context(), me(r), id(r))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"coteachers": list})
	return nil
}

func (s *Service) hAddCoteacher(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Email string `json:"email"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	t, err := s.AddCoteacher(r.Context(), me(r), id(r), in.Email)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, t)
	return nil
}

func (s *Service) hRemoveCoteacher(w http.ResponseWriter, r *http.Request) error {
	if err := s.RemoveCoteacher(r.Context(), me(r), id(r), chi.URLParam(r, "user")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) hBroadcaster(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		UserID string `json:"user_id"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.SetBroadcaster(r.Context(), me(r), id(r), in.UserID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) hBroadcastAnswer(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Accept bool `json:"accept"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.AnswerBroadcast(r.Context(), me(r), id(r), in.Accept); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) hAskAnswer(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Accepted bool `json:"accepted"`
	}
	if err := web.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.AskAnswer(r.Context(), me(r), id(r), in.Accepted); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
