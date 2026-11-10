package poll

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/nadun96/quizplatform/internal/platform/audit"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// Presentations in polls (D-165). The teacher asks for them; each student,
// poll group or classroom group submits one. The browser turns the PDF (or
// a PowerPoint file, converted through the teacher's Google Drive) into
// slide pictures. During the poll the teacher lets one present: its
// presenter controls the slides on their own device and everyone in the poll
// follows; the teacher can take over or stop at any time. The teacher marks
// each one; the audience may rate them.

const (
	PresOff        = "off"
	PresStudent    = "student"
	PresPollGroup  = "poll_group"
	PresClassGroup = "class_group"

	MaxSubmissionSlides = 100
	maxSlideBytes       = 1 << 20
)

var (
	errNoPres      = httpx.NewError(http.StatusConflict, "no_presentations", "this poll doesn't ask for presentations")
	errNotUploader = httpx.NewError(http.StatusForbidden, "not_uploader", "only your group's leader can upload its presentation")
	errNotPresent  = httpx.NewError(http.StatusForbidden, "not_presenting", "your teacher hasn't let you present now")
)

// SlideConverter turns a PowerPoint file into a PDF through the teacher's
// Google Drive (D-78).
type SlideConverter func(ctx context.Context, teacherID, name string, data []byte) ([]byte, error)

// SetSlideConverter gives the service the Drive conversion.
func (s *Service) SetSlideConverter(f SlideConverter) { s.convert = f }

// PresSettings is what the teacher asks for.
type PresSettings struct {
	Mode         string  `json:"mode"`      // off, student, poll_group or class_group
	UploadBy     string  `json:"upload_by"` // any member, or the leader
	OutOf        float64 `json:"out_of"`    // the mark is out of this; 0: not marked
	Rating       bool    `json:"rating"`    // the audience rates them 1-5
	Instructions string  `json:"instructions"`
}

func (s *Service) presSettings(ctx context.Context, pollID string) (PresSettings, error) {
	st := PresSettings{Mode: PresOff, UploadBy: "any"}
	err := s.pool.QueryRow(ctx, `SELECT mode, upload_by, out_of::float8, rating, instructions FROM poll.pres_settings WHERE poll_id = $1`, pollID).
		Scan(&st.Mode, &st.UploadBy, &st.OutOf, &st.Rating, &st.Instructions)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	return st, err
}

// PresSettingsOf is the poll's settings, for its teacher.
func (s *Service) PresSettingsOf(ctx context.Context, teacherID, pollID string) (PresSettings, error) {
	if _, err := s.ownedPoll(ctx, teacherID, pollID); err != nil {
		return PresSettings{}, err
	}
	return s.presSettings(ctx, pollID)
}

// SetPresSettings saves them.
func (s *Service) SetPresSettings(ctx context.Context, teacherID, pollID string, in PresSettings) (PresSettings, error) {
	p, err := s.ownedPoll(ctx, teacherID, pollID)
	if err != nil {
		return in, err
	}
	in.Instructions = strings.TrimSpace(in.Instructions)
	if in.UploadBy == "" {
		in.UploadBy = "any"
	}
	bad := map[string]string{}
	switch in.Mode {
	case PresOff, PresStudent, PresPollGroup:
	case PresClassGroup:
		if p.ClassroomID == nil {
			bad["mode"] = "link the poll to a classroom to use its groups"
		}
	default:
		bad["mode"] = "off, student, poll_group or class_group"
	}
	if in.UploadBy != "any" && in.UploadBy != "leader" {
		bad["upload_by"] = "any or leader"
	}
	if in.OutOf < 0 || in.OutOf > 10000 || in.OutOf != float64(int64(in.OutOf*100))/100 {
		bad["out_of"] = "0 to 10000, to 2 decimal places"
	}
	if utf8.RuneCountInString(in.Instructions) > 2000 {
		bad["instructions"] = "at most 2000 characters"
	}
	if in.Mode == PresClassGroup && p.Identity == "anonymous" {
		bad["mode"] = "classroom groups need participants who sign in"
	}
	if len(bad) > 0 {
		return in, httpx.Invalid(bad)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO poll.pres_settings (poll_id, mode, upload_by, out_of, rating, instructions) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (poll_id) DO UPDATE SET mode = $2, upload_by = $3, out_of = $4, rating = $5, instructions = $6, updated_at = now()`,
			pollID, in.Mode, in.UploadBy, in.OutOf, in.Rating, in.Instructions); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "poll_presentations_set", "poll", pollID, in)
	})
	if err != nil {
		return in, err
	}
	s.hub.markAll(pollID)
	return s.presSettings(ctx, pollID)
}

// Owner is who a submission is from: a student, a poll group or a classroom group.
type Owner struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Submission is one presentation.
type Submission struct {
	ID          string     `json:"id"`
	Owner       Owner      `json:"owner"`
	Title       string     `json:"title"`
	FileName    string     `json:"file_name"`
	Slides      int        `json:"slides"`
	Ready       bool       `json:"ready"`
	UploadedBy  string     `json:"uploaded_by"` // a name
	PresentedAt *time.Time `json:"presented_at"`
	Mark        *float64   `json:"mark"`
	Comment     string     `json:"comment"`
	Rating      *float64   `json:"rating"`  // the audience's average
	Ratings     int        `json:"ratings"` // how many rated it
	UpdatedAt   time.Time  `json:"updated_at"`
	uploader    *string
}

const submissionCols = `s.id::text, s.owner_kind, s.owner_id::text, s.title, s.file_name, s.slides, s.ready, s.uploaded_by::text, s.presented_at,
	s.mark::float8, s.comment, s.updated_at,
	(SELECT avg(stars)::float8 FROM poll.submission_ratings r WHERE r.submission_id = s.id),
	(SELECT count(*)::int FROM poll.submission_ratings r WHERE r.submission_id = s.id)`

func scanSubmission(r pgx.Row) (Submission, error) {
	var x Submission
	err := r.Scan(&x.ID, &x.Owner.Kind, &x.Owner.ID, &x.Title, &x.FileName, &x.Slides, &x.Ready, &x.uploader, &x.PresentedAt,
		&x.Mark, &x.Comment, &x.UpdatedAt, &x.Rating, &x.Ratings)
	return x, err
}

// names fills in owners' and uploaders' names.
func (s *Service) names(ctx context.Context, p *Poll, list []Submission, public bool) error {
	people, err := s.teacherNames(ctx, p, public)
	if err != nil {
		return err
	}
	groups := map[string]string{}
	rows, err := s.pool.Query(ctx, `SELECT id::text, name FROM poll.groups WHERE poll_id = $1
		UNION ALL SELECT c.id::text, coalesce(c.public_name, c.name) FROM content.categories c WHERE c.classroom_id::text = $2`, p.ID, deref(p.ClassroomID))
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return err
		}
		groups[id] = name
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range list {
		x := &list[i]
		if x.Owner.Kind == PresStudent {
			x.Owner.Name = people[x.Owner.ID]
		} else {
			x.Owner.Name = groups[x.Owner.ID]
		}
		if x.uploader != nil {
			x.UploadedBy = people[*x.uploader]
		}
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// teacherNames maps participant ids to the names the teacher sees.
func (s *Service) teacherNames(ctx context.Context, p *Poll, public bool) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT pa.id::text, coalesce(u.name, ''), coalesce(pa.nickname, ''),
			row_number() OVER (ORDER BY pa.created_at, pa.id)::int
		FROM poll.participants pa LEFT JOIN auth.users u ON u.id = pa.user_id WHERE pa.poll_id = $1`, p.ID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for rows.Next() {
		var id, real, nick string
		var n int
		if err := rows.Scan(&id, &real, &nick, &n); err != nil {
			rows.Close()
			return nil, err
		}
		// The teacher sees account names; everyone else what the poll shows.
		name := real
		if public || name == "" {
			name = displayName(p, real, nick, n)
		}
		out[id] = name
	}
	rows.Close()
	return out, rows.Err()
}

// Submissions lists the poll's presentations, for its teacher.
func (s *Service) Submissions(ctx context.Context, teacherID, pollID string) ([]Submission, error) {
	p, err := s.ownedPoll(ctx, teacherID, pollID)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+submissionCols+` FROM poll.submissions s WHERE s.poll_id = $1 ORDER BY s.created_at`, pollID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Submission, error) { return scanSubmission(r) })
	if err != nil {
		return nil, err
	}
	return list, s.names(ctx, p, list, false)
}

// teacherSubmission is a submission of a poll the teacher owns.
func (s *Service) teacherSubmission(ctx context.Context, teacherID, id string) (*Poll, Submission, error) {
	var pollID string
	err := s.pool.QueryRow(ctx, `SELECT poll_id::text FROM poll.submissions WHERE id = $1`, id).Scan(&pollID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, Submission{}, httpx.ErrNotFound
	}
	if err != nil {
		return nil, Submission{}, err
	}
	p, err := s.ownedPoll(ctx, teacherID, pollID)
	if err != nil {
		return nil, Submission{}, err
	}
	x, err := scanSubmission(s.pool.QueryRow(ctx, `SELECT `+submissionCols+` FROM poll.submissions s WHERE s.id = $1`, id))
	return p, x, err
}

// MarkSubmission gives a presentation its mark; no mark takes it away.
func (s *Service) MarkSubmission(ctx context.Context, teacherID, id string, mark *float64, comment string) error {
	p, _, err := s.teacherSubmission(ctx, teacherID, id)
	if err != nil {
		return err
	}
	st, err := s.presSettings(ctx, p.ID)
	if err != nil {
		return err
	}
	comment = strings.TrimSpace(comment)
	switch {
	case mark != nil && st.OutOf == 0:
		return httpx.Invalid(map[string]string{"mark": "set what presentations are marked out of first"})
	case mark != nil && (*mark < 0 || *mark > st.OutOf || *mark != float64(int64(*mark*100))/100):
		return httpx.Invalid(map[string]string{"mark": fmt.Sprintf("0 to %g, to 2 decimal places", st.OutOf)})
	case utf8.RuneCountInString(comment) > 1000:
		return httpx.Invalid(map[string]string{"comment": "at most 1000 characters"})
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE poll.submissions SET mark = $2, comment = $3, updated_at = now() WHERE id = $1`, id, mark, comment); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "poll_submission_marked", "poll_submission", id, map[string]any{"mark": mark})
	})
}

// DeleteSubmission removes one, with its slides and ratings.
func (s *Service) DeleteSubmission(ctx context.Context, teacherID, id string) error {
	p, _, err := s.teacherSubmission(ctx, teacherID, id)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM poll.submissions WHERE id = $1`, id); err != nil {
		return err
	}
	s.hub.markAll(p.ID)
	return nil
}

// ---------- participants ----------

// participantPres finds the participant and the poll's settings, which must be on.
func (s *Service) participantPres(ctx context.Context, code string, c Caller) (*Poll, string, PresSettings, error) {
	p, err := s.pollByCode(ctx, code)
	if err != nil {
		return nil, "", PresSettings{}, err
	}
	st, err := s.presSettings(ctx, p.ID)
	if err != nil {
		return nil, "", st, err
	}
	pid, _, err := s.findParticipant(ctx, p, c)
	if err != nil {
		return nil, "", st, err
	}
	if pid == "" {
		return nil, "", st, httpx.NewError(http.StatusForbidden, "not_joined", "join the poll first")
	}
	return p, pid, st, nil
}

// owners are the owners a participant may submit for, and whether they may upload for each.
func (s *Service) owners(ctx context.Context, p *Poll, pid string, st PresSettings) ([]Owner, map[string]bool, error) {
	may := map[string]bool{}
	switch st.Mode {
	case PresStudent:
		may[pid] = true
		return []Owner{{Kind: PresStudent, ID: pid}}, may, nil
	case PresPollGroup:
		var group *string
		var captain bool
		var name string
		err := s.pool.QueryRow(ctx, `SELECT pa.group_id::text, pa.captain, coalesce(g.name, '') FROM poll.participants pa
			LEFT JOIN poll.groups g ON g.id = pa.group_id WHERE pa.id = $1`, pid).Scan(&group, &captain, &name)
		if err != nil || group == nil {
			return nil, may, err
		}
		may[*group] = st.UploadBy == "any" || captain
		return []Owner{{Kind: PresPollGroup, ID: *group, Name: name}}, may, nil
	case PresClassGroup:
		if p.ClassroomID == nil {
			return nil, may, nil
		}
		rows, err := s.pool.Query(ctx, `SELECT c.id::text, coalesce(c.public_name, c.name), ec.role
			FROM poll.participants pa
			JOIN content.enrolments e ON e.user_id = pa.user_id AND e.classroom_id = $2 AND e.status = 'active'
			JOIN content.enrolment_categories ec ON ec.enrolment_id = e.id
			JOIN content.categories c ON c.id = ec.category_id
			WHERE pa.id = $1 ORDER BY c.position, lower(c.name)`, pid, *p.ClassroomID)
		if err != nil {
			return nil, may, err
		}
		var out []Owner
		for rows.Next() {
			var o Owner
			var role string
			if err := rows.Scan(&o.ID, &o.Name, &role); err != nil {
				rows.Close()
				return nil, may, err
			}
			o.Kind = PresClassGroup
			out = append(out, o)
			may[o.ID] = st.UploadBy == "any" || role == "leader" || role == "assistant"
		}
		rows.Close()
		return out, may, rows.Err()
	}
	return nil, may, nil
}

// MyPresentations is what a participant sees: what's asked, who they submit
// for, and what's been submitted.
type MyPresentations struct {
	PresSettings
	Owners      []Owner               `json:"owners"`      // who they submit for (a classroom group each, maybe several)
	CanUpload   map[string]bool       `json:"can_upload"`  // by owner id
	Submissions map[string]Submission `json:"submissions"` // by owner id
	CanConvert  bool                  `json:"can_convert"` // PowerPoint can be turned into slides
}

// MyPres is the participant's view.
func (s *Service) MyPres(ctx context.Context, code string, c Caller) (MyPresentations, error) {
	p, pid, st, err := s.participantPres(ctx, code, c)
	if err != nil {
		return MyPresentations{}, err
	}
	v := MyPresentations{PresSettings: st, Owners: []Owner{}, CanUpload: map[string]bool{}, Submissions: map[string]Submission{}, CanConvert: s.convert != nil}
	if st.Mode == PresOff {
		return v, nil
	}
	owners, may, err := s.owners(ctx, p, pid, st)
	if err != nil {
		return v, err
	}
	if owners != nil {
		v.Owners, v.CanUpload = owners, may
	}
	ids := make([]string, 0, len(owners))
	for _, o := range owners {
		ids = append(ids, o.ID)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+submissionCols+` FROM poll.submissions s WHERE s.poll_id = $1 AND s.owner_id::text = ANY($2)`, p.ID, ids)
	if err != nil {
		return v, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Submission, error) { return scanSubmission(r) })
	if err != nil {
		return v, err
	}
	if err := s.names(ctx, p, list, true); err != nil {
		return v, err
	}
	for _, x := range list {
		v.Submissions[x.Owner.ID] = x
	}
	return v, nil
}

// SubmitInput starts (or replaces) a submission; slides come next.
type SubmitInput struct {
	OwnerID  string `json:"owner_id"` // a classroom group, when there's a choice
	Title    string `json:"title"`
	FileName string `json:"file_name"`
	Slides   int    `json:"slides"`
}

// StartSubmission readies a submission for its slides: the old slides go.
func (s *Service) StartSubmission(ctx context.Context, code string, c Caller, in SubmitInput) (Submission, error) {
	p, pid, st, err := s.participantPres(ctx, code, c)
	if err != nil {
		return Submission{}, err
	}
	if st.Mode == PresOff {
		return Submission{}, errNoPres
	}
	if p.Status == "closed" {
		return Submission{}, httpx.NewError(http.StatusConflict, "poll_closed", "this poll has closed")
	}
	owners, may, err := s.owners(ctx, p, pid, st)
	if err != nil {
		return Submission{}, err
	}
	var owner *Owner
	for i := range owners {
		if in.OwnerID == "" || owners[i].ID == in.OwnerID {
			owner = &owners[i]
			break
		}
	}
	if owner == nil {
		return Submission{}, httpx.NewError(http.StatusForbidden, "no_group", "you aren't in a group that presents")
	}
	if !may[owner.ID] {
		return Submission{}, errNotUploader
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		in.Title = strings.TrimSuffix(in.FileName, "."+lastExt(in.FileName))
	}
	bad := map[string]string{}
	if in.Title == "" || utf8.RuneCountInString(in.Title) > 200 {
		bad["title"] = "a title of at most 200 characters"
	}
	if in.Slides < 1 || in.Slides > MaxSubmissionSlides {
		bad["slides"] = fmt.Sprintf("1 to %d slides", MaxSubmissionSlides)
	}
	if utf8.RuneCountInString(in.FileName) > 200 {
		in.FileName = string([]rune(in.FileName)[:200])
	}
	if len(bad) > 0 {
		return Submission{}, httpx.Invalid(bad)
	}
	var id string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Not while it's on the screen: the audience would lose its slides.
		var onStage bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM poll.stage st JOIN poll.submissions x ON x.id = st.submission_id
			WHERE x.poll_id = $1 AND x.owner_kind = $2 AND x.owner_id = $3)`, p.ID, owner.Kind, owner.ID).Scan(&onStage); err != nil {
			return err
		}
		if onStage {
			return httpx.Conflict("this presentation is on the screen now; replace it afterwards")
		}
		if err := tx.QueryRow(ctx, `INSERT INTO poll.submissions (poll_id, owner_kind, owner_id, uploaded_by, title, file_name, slides)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (poll_id, owner_kind, owner_id) DO UPDATE SET uploaded_by = $4, title = $5, file_name = $6, slides = $7, ready = false, updated_at = now()
			RETURNING id::text`, p.ID, owner.Kind, owner.ID, pid, in.Title, in.FileName, in.Slides).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM poll.submission_slides WHERE submission_id = $1`, id)
		return err
	})
	if err != nil {
		return Submission{}, err
	}
	x, err := scanSubmission(s.pool.QueryRow(ctx, `SELECT `+submissionCols+` FROM poll.submissions s WHERE s.id = $1`, id))
	return x, err
}

func lastExt(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return ""
}

// ownSubmission is a submission the participant may upload for.
func (s *Service) ownSubmission(ctx context.Context, code string, c Caller, id string) (*Poll, Submission, error) {
	p, pid, st, err := s.participantPres(ctx, code, c)
	if err != nil {
		return nil, Submission{}, err
	}
	x, err := scanSubmission(s.pool.QueryRow(ctx, `SELECT `+submissionCols+` FROM poll.submissions s WHERE s.id = $1 AND s.poll_id = $2`, id, p.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, x, httpx.ErrNotFound
	}
	if err != nil {
		return nil, x, err
	}
	_, may, err := s.owners(ctx, p, pid, st)
	if err != nil {
		return nil, x, err
	}
	if !may[x.Owner.ID] {
		return nil, x, errNotUploader
	}
	return p, x, nil
}

// PutSlide stores one slide picture, a JPEG.
func (s *Service) PutSlide(ctx context.Context, code string, c Caller, id string, n int, image []byte, text string) error {
	p, x, err := s.ownSubmission(ctx, code, c, id)
	if err != nil {
		return err
	}
	if n < 1 || n > x.Slides {
		return httpx.Invalid(map[string]string{"n": fmt.Sprintf("slide 1 to %d", x.Slides)})
	}
	if len(image) == 0 || len(image) > maxSlideBytes || !bytes.HasPrefix(image, []byte{0xFF, 0xD8, 0xFF}) {
		return httpx.Invalid(map[string]string{"image": "a JPEG picture of at most 1 MB"})
	}
	if r := []rune(text); len(r) > 5000 {
		text = string(r[:5000])
	}
	var used int64
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(length(sl.image)), 0) FROM poll.submission_slides sl JOIN poll.submissions x ON x.id = sl.submission_id
		WHERE x.poll_id = $1 AND NOT (sl.submission_id = $2 AND sl.n = $3)`, p.ID, id, n).Scan(&used); err != nil {
		return err
	}
	if used+int64(len(image)) > PollStorageLimit {
		return httpx.NewError(http.StatusInsufficientStorage, "poll_full", "this poll's presentations have used all their space")
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO poll.submission_slides (submission_id, n, image, text) VALUES ($1, $2, $3, $4)
		ON CONFLICT (submission_id, n) DO UPDATE SET image = $3, text = $4`, id, n, image, text)
	return err
}

// FinishSubmission marks it ready once every slide is in.
func (s *Service) FinishSubmission(ctx context.Context, code string, c Caller, id string) (Submission, error) {
	p, x, err := s.ownSubmission(ctx, code, c, id)
	if err != nil {
		return x, err
	}
	var have int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)::int FROM poll.submission_slides WHERE submission_id = $1 AND n <= $2`, id, x.Slides).Scan(&have); err != nil {
		return x, err
	}
	if have != x.Slides {
		return x, httpx.Invalid(map[string]string{"slides": fmt.Sprintf("%d of %d slides are in", have, x.Slides)})
	}
	if _, err := s.pool.Exec(ctx, `UPDATE poll.submissions SET ready = true, updated_at = now() WHERE id = $1`, id); err != nil {
		return x, err
	}
	s.hub.markAll(p.ID)
	x.Ready = true
	return x, nil
}

// Convert turns a participant's PowerPoint file into a PDF through the
// teacher's Google Drive.
func (s *Service) Convert(ctx context.Context, code string, c Caller, name string, data []byte) ([]byte, error) {
	p, pid, st, err := s.participantPres(ctx, code, c)
	if err != nil {
		return nil, err
	}
	if st.Mode == PresOff {
		return nil, errNoPres
	}
	if s.convert == nil {
		return nil, httpx.NewError(http.StatusConflict, "no_drive", "PowerPoint can't be turned into slides here: save it as a PDF and upload that")
	}
	if !s.uploads.Allow(pid) {
		return nil, httpx.NewError(http.StatusTooManyRequests, "slow_down", "one moment, then try again")
	}
	return s.convert(ctx, p.TeacherID, name, data)
}

// ---------- the stage ----------

// Stage is what's on the poll's screen.
type Stage struct {
	SubmissionID  string `json:"submission_id"`
	Title         string `json:"title"`
	Owner         string `json:"owner"`
	Slide         int    `json:"slide"`
	Slides        int    `json:"slides"`
	PresenterKey  string `json:"presenter_key,omitempty"` // the participant presenting, as their rank key; empty: the teacher
	PresenterName string `json:"presenter_name,omitempty"`
	Rating        bool   `json:"rating"` // the audience may rate it
}

// stage is the poll's stage, or nil.
func (s *Service) stage(ctx context.Context, p *Poll) (*Stage, error) {
	var st Stage
	var presenter *string
	var ownerKind, ownerID string
	err := s.pool.QueryRow(ctx, `SELECT x.id::text, x.title, x.owner_kind, x.owner_id::text, st.slide, x.slides, st.presenter::text,
			coalesce((SELECT rating FROM poll.pres_settings ps WHERE ps.poll_id = st.poll_id), false)
		FROM poll.stage st JOIN poll.submissions x ON x.id = st.submission_id WHERE st.poll_id = $1`, p.ID).
		Scan(&st.SubmissionID, &st.Title, &ownerKind, &ownerID, &st.Slide, &st.Slides, &presenter, &st.Rating)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	list := []Submission{{Owner: Owner{Kind: ownerKind, ID: ownerID}, uploader: presenter}}
	if err := s.names(ctx, p, list, true); err != nil {
		return nil, err
	}
	st.Owner = list[0].Owner.Name
	if presenter != nil {
		st.PresenterKey = rankKey(p.ID, *presenter)
		st.PresenterName = list[0].UploadedBy
	}
	return &st, nil
}

// StageInput puts a presentation on the screen.
type StageInput struct {
	SubmissionID string `json:"submission_id"`
	Presenter    string `json:"presenter"` // a participant id; "teacher" keeps control; empty: who uploaded it
}

// SetStage lets a presentation be shown, controlled by its presenter.
func (s *Service) SetStage(ctx context.Context, teacherID, pollID string, in StageInput) error {
	p, err := s.ownedPoll(ctx, teacherID, pollID)
	if err != nil {
		return err
	}
	var ready bool
	var uploader *string
	err = s.pool.QueryRow(ctx, `SELECT ready, uploaded_by::text FROM poll.submissions WHERE id = $1 AND poll_id = $2`, in.SubmissionID, p.ID).Scan(&ready, &uploader)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if !ready {
		return httpx.Conflict("its slides aren't all in yet")
	}
	var presenter any
	switch in.Presenter {
	case "teacher":
	case "":
		if uploader != nil {
			presenter = *uploader
		}
	default:
		var ok bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM poll.participants WHERE id::text = $1 AND poll_id = $2)`, in.Presenter, p.ID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return httpx.Invalid(map[string]string{"presenter": "not a participant of this poll"})
		}
		presenter = in.Presenter
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO poll.stage (poll_id, submission_id, slide, presenter) VALUES ($1, $2, 1, $3)
			ON CONFLICT (poll_id) DO UPDATE SET submission_id = $2, slide = 1, presenter = $3, started_at = now()`, p.ID, in.SubmissionID, presenter); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE poll.submissions SET presented_at = coalesce(presented_at, now()) WHERE id = $1`, in.SubmissionID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, teacherID, "poll_stage_set", "poll", p.ID, in)
	})
	if err != nil {
		return err
	}
	s.hub.markAll(p.ID)
	return nil
}

// ClearStage takes it off the screen.
func (s *Service) ClearStage(ctx context.Context, teacherID, pollID string) error {
	if _, err := s.ownedPoll(ctx, teacherID, pollID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM poll.stage WHERE poll_id = $1`, pollID); err != nil {
		return err
	}
	s.hub.markAll(pollID)
	return nil
}

// moveSlide goes to slide n of what's on the stage.
func (s *Service) moveSlide(ctx context.Context, pollID string, n int) error {
	tag, err := s.pool.Exec(ctx, `UPDATE poll.stage st SET slide = $2 FROM poll.submissions x
		WHERE st.poll_id = $1 AND x.id = st.submission_id AND $2 BETWEEN 1 AND x.slides`, pollID, n)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.Invalid(map[string]string{"n": "no such slide, or nothing is being presented"})
	}
	s.hub.markAll(pollID)
	return nil
}

// TeacherSlide moves the stage, for the teacher, who can always take over.
func (s *Service) TeacherSlide(ctx context.Context, teacherID, pollID string, n int) error {
	if _, err := s.ownedPoll(ctx, teacherID, pollID); err != nil {
		return err
	}
	return s.moveSlide(ctx, pollID, n)
}

// PresenterSlide moves the stage, for the participant presenting.
func (s *Service) PresenterSlide(ctx context.Context, code string, c Caller, n int) error {
	p, err := s.pollByCode(ctx, code)
	if err != nil {
		return err
	}
	pid, _, err := s.findParticipant(ctx, p, c)
	if err != nil {
		return err
	}
	var presenting bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM poll.stage WHERE poll_id = $1 AND presenter::text = $2)`, p.ID, pid).Scan(&presenting); err != nil {
		return err
	}
	if pid == "" || !presenting {
		return errNotPresent
	}
	return s.moveSlide(ctx, p.ID, n)
}

// slide reads one slide picture.
func (s *Service) slide(ctx context.Context, id string, n int) ([]byte, error) {
	var img []byte
	err := s.pool.QueryRow(ctx, `SELECT image FROM poll.submission_slides WHERE submission_id = $1 AND n = $2`, id, n).Scan(&img)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	return img, err
}

// PublicSlide is a slide for a participant: of what's on the stage, or of
// their own submission.
func (s *Service) PublicSlide(ctx context.Context, code string, c Caller, id string, n int) ([]byte, error) {
	p, err := s.pollByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	var onStage bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM poll.stage WHERE poll_id = $1 AND submission_id::text = $2)`, p.ID, id).Scan(&onStage); err != nil {
		return nil, err
	}
	if !onStage {
		pid, _, err := s.findParticipant(ctx, p, c)
		if err != nil {
			return nil, err
		}
		if pid == "" {
			return nil, httpx.ErrNotFound
		}
		st, err := s.presSettings(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		owners, _, err := s.owners(ctx, p, pid, st)
		if err != nil {
			return nil, err
		}
		var mine bool
		for _, o := range owners {
			var ok bool
			if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM poll.submissions WHERE id::text = $1 AND poll_id = $2 AND owner_id::text = $3)`, id, p.ID, o.ID).Scan(&ok); err != nil {
				return nil, err
			}
			mine = mine || ok
		}
		if !mine {
			return nil, httpx.ErrNotFound
		}
	}
	return s.slide(ctx, id, n)
}

// TeacherSlideImage is a slide of a submission in the teacher's poll.
func (s *Service) TeacherSlideImage(ctx context.Context, teacherID, id string, n int) ([]byte, error) {
	if _, _, err := s.teacherSubmission(ctx, teacherID, id); err != nil {
		return nil, err
	}
	return s.slide(ctx, id, n)
}

// Rate is the audience's 1-5 for a presentation they've seen; not their own.
func (s *Service) Rate(ctx context.Context, code string, c Caller, id string, stars int) error {
	p, pid, st, err := s.participantPres(ctx, code, c)
	if err != nil {
		return err
	}
	if !st.Rating {
		return httpx.NewError(http.StatusConflict, "no_rating", "this poll doesn't ask for ratings")
	}
	if stars < 1 || stars > 5 {
		return httpx.Invalid(map[string]string{"stars": "1 to 5"})
	}
	var ownerID string
	var presented bool
	err = s.pool.QueryRow(ctx, `SELECT owner_id::text, presented_at IS NOT NULL FROM poll.submissions WHERE id = $1 AND poll_id = $2`, id, p.ID).Scan(&ownerID, &presented)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if !presented {
		return httpx.Conflict("it hasn't been presented yet")
	}
	owners, _, err := s.owners(ctx, p, pid, st)
	if err != nil {
		return err
	}
	for _, o := range owners {
		if o.ID == ownerID {
			return httpx.NewError(http.StatusForbidden, "own_presentation", "you can't rate your own presentation")
		}
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO poll.submission_ratings (submission_id, participant_id, stars) VALUES ($1, $2, $3)
		ON CONFLICT (submission_id, participant_id) DO UPDATE SET stars = $3`, id, pid, stars)
	return err
}
