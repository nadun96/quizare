// Package app is the composition root: it wires modules together and builds
// the HTTP router. Modules depend only on interfaces; this is the one place
// that knows every concrete type.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/nadun96/quizplatform/internal/admin"
	"github.com/nadun96/quizplatform/internal/analytics"
	"github.com/nadun96/quizplatform/internal/apidocs"
	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/content"
	"github.com/nadun96/quizplatform/internal/dashboard"
	"github.com/nadun96/quizplatform/internal/directory"
	"github.com/nadun96/quizplatform/internal/drive"
	"github.com/nadun96/quizplatform/internal/eval"
	"github.com/nadun96/quizplatform/internal/imageurl"
	"github.com/nadun96/quizplatform/internal/live"
	"github.com/nadun96/quizplatform/internal/llm"
	"github.com/nadun96/quizplatform/internal/mail"
	"github.com/nadun96/quizplatform/internal/meet"
	"github.com/nadun96/quizplatform/internal/oidc"
	"github.com/nadun96/quizplatform/internal/oversight"
	"github.com/nadun96/quizplatform/internal/platform/config"
	"github.com/nadun96/quizplatform/internal/platform/db"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
	"github.com/nadun96/quizplatform/internal/platform/jobs"
	"github.com/nadun96/quizplatform/internal/platform/page"
	"github.com/nadun96/quizplatform/internal/poll"
	"github.com/nadun96/quizplatform/internal/presentation"
	"github.com/nadun96/quizplatform/internal/progress"
	"github.com/nadun96/quizplatform/internal/quiz"
	"github.com/nadun96/quizplatform/internal/settings"
	"github.com/nadun96/quizplatform/internal/storage"
	"github.com/nadun96/quizplatform/internal/tutorial"
	"github.com/nadun96/quizplatform/internal/tutorlink"
	"github.com/nadun96/quizplatform/migrations"
)

type App struct {
	cfg     config.Config
	log     *slog.Logger
	pool    *pgxpool.Pool
	river   *river.Client[pgx.Tx]
	router  chi.Router
	ownPool bool

	Auth      *auth.Service
	Settings  *settings.Store
	Content   *content.Service
	Quiz      *quiz.Service
	Live      *live.Service
	Eval      *eval.Service
	LLM       *llm.Service
	Analytics *analytics.Service
	Admin     *admin.Service
	Poll      *poll.Service
	Storage   *storage.Service
	Directory *directory.Service    // school sign-in (D-64)
	Meet      *meet.Service         // peer-to-peer meetings (D-69)
	OIDC      *oidc.Service         // sign-in with OpenID Connect providers (D-70)
	Drive     *drive.Service        // teachers' Google Drive (D-71)
	Tutorial  *tutorial.Service     // interactive tutorials (D-74)
	Slides    *presentation.Service // presentations (D-76)
	Tutor     *tutorlink.Service    // nil when tutoring is off
	Oversight *oversight.Service    // the admin's live activity (D-90)
	Progress  *progress.Service     // students' own XP, levels and badges (D-97)
	Dashboard *dashboard.Service    // each person's own overview (D-99)
}

// Options let tests swap infrastructure.
type Options struct {
	RunJobs bool        // false: insert-only River client
	Mailer  mail.Sender // nil: chosen from config
	// Checker validates resource URLs; nil uses the SSRF-safe public checker.
	Checker *imageurl.Checker
	// KEK is the 32-byte master key; New loads it from cfg.KEKFile.
	KEK []byte
	// Providers overrides the LLM adapters (tests point them at fakes).
	Providers map[string]llm.Provider
	// TutoringSecret is the key shared with the tutoring service; New loads
	// it from cfg.TutoringSecretFile.
	TutoringSecret []byte
	// HTTPClient talks to outside services: OpenID Connect providers. nil:
	// a default with a timeout (tests pass one that trusts their fakes).
	HTTPClient *http.Client
	// Google is where Google Drive is reached (D-71); zero means Google.
	Google drive.Endpoints
}

// New opens the database, applies migrations and builds the app.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	pool, err := db.OpenWait(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBWait)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		pool.Close()
		return nil, err
	}
	if err := jobs.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	if cfg.KEKGenerate {
		created, err := llm.EnsureKEK(cfg.KEKFile)
		if err != nil {
			pool.Close()
			return nil, err
		}
		if created {
			log.Warn("generated a new master key; back it up, losing it means teachers must re-enter their API keys", "path", cfg.KEKFile)
		}
	}
	kek, err := llm.LoadKEK(cfg.KEKFile)
	if err != nil {
		pool.Close()
		return nil, err
	}
	var tutorSecret []byte
	if cfg.TutoringSecretFile != "" {
		if cfg.TutoringSecretGenerate {
			if _, err := llm.EnsureKEK(cfg.TutoringSecretFile); err != nil {
				pool.Close()
				return nil, fmt.Errorf("tutoring secret: %w", err)
			}
		}
		// Same file format and permission rules as the master key.
		if tutorSecret, err = llm.LoadKEK(cfg.TutoringSecretFile); err != nil {
			pool.Close()
			return nil, fmt.Errorf("tutoring secret: %w", err)
		}
	}
	a, err := Build(cfg, log, pool, Options{RunJobs: true, KEK: kek, TutoringSecret: tutorSecret})
	if err != nil {
		pool.Close()
		return nil, err
	}
	a.ownPool = true
	return a, nil
}

// Build wires modules onto an existing pool.
func Build(cfg config.Config, log *slog.Logger, pool *pgxpool.Pool, opt Options) (*App, error) {
	a := &App{cfg: cfg, log: log, pool: pool}
	if err := httpx.TrustProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}

	mailer := opt.Mailer
	if mailer == nil {
		var err error
		if mailer, err = newMailer(cfg, log); err != nil {
			return nil, err
		}
	}
	checker := opt.Checker
	if checker == nil {
		checker = imageurl.NewChecker(false)
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &mail.Worker{Sender: mailer})
	quizWorker := &quiz.CheckResourcesWorker{}
	river.AddWorker(workers, quizWorker)
	evalWorker := &eval.EvaluateWorker{}
	river.AddWorker(workers, evalWorker)
	llmWorker := &llm.Worker{}
	river.AddWorker(workers, llmWorker)
	llmTutorialWorker := &llm.TutorialWorker{}
	river.AddWorker(workers, llmTutorialWorker)
	analyticsWorker := &analytics.Worker{}
	river.AddWorker(workers, analyticsWorker)

	var w *river.Workers
	if opt.RunJobs {
		w = workers
	}
	snapshotWorker := &storage.SnapshotWorker{}
	river.AddWorker(workers, snapshotWorker)
	directoryWorker := &directory.CheckWorker{}
	river.AddWorker(workers, directoryWorker)
	driveBackup, driveSync := &drive.BackupWorker{}, &drive.SyncWorker{}
	river.AddWorker(workers, driveBackup)
	river.AddWorker(workers, driveSync)
	var periodic []*river.PeriodicJob
	if opt.RunJobs {
		periodic = append(periodic, storage.Periodic())   // hourly storage figures (PL-FR-04)
		periodic = append(periodic, directory.Periodic()) // sign out accounts the directory disabled (D-64)
		periodic = append(periodic, drive.SyncPeriodic()) // classroom Drive files follow enrolments (D-71)
	}
	rc, err := jobs.NewClient(pool, w, log, periodic...)
	if err != nil {
		return nil, err
	}
	a.river = rc

	a.Auth = auth.NewService(pool, auth.NewHasher(cfg.Argon2Workers), rc, cfg.BaseURL)
	a.Settings = settings.NewStore(pool)
	a.Content = content.NewService(pool, a.Settings, a.Auth)
	a.Auth.SetRelations(a.Content)
	a.Quiz = quiz.NewService(pool, a.Content, a.Settings, rc, checker)
	quizWorker.Service = a.Quiz
	a.Live = live.NewService(pool, a.Quiz, a.Content, a.Auth, cfg.BaseURL, log)
	a.Quiz.SetSessionGuard(a.Live.HasSessions) // BR-16: run quizzes are archived, not deleted
	a.Eval = eval.NewService(pool, a.Live, rc)
	evalWorker.Service = a.Eval
	vault, err := llm.NewVault(opt.KEK)
	if err != nil {
		return nil, err
	}
	providers := opt.Providers
	if providers == nil {
		providers = llm.DefaultProviders()
	}
	a.LLM = llm.NewService(pool, vault, providers, rc, a.Live, a.Eval, log)
	a.Directory = directory.NewService(pool, vault, a.Auth, log)
	a.OIDC = oidc.NewService(pool, vault, a.Auth, cfg.BaseURL, opt.HTTPClient, log)
	if a.Meet, err = meet.NewService(pool, a.Content, a.Auth, cfg.BaseURL, meet.Options{STUN: cfg.MeetSTUN, TURNURLs: cfg.MeetTURNURLs, TURNSecretFile: cfg.MeetTURNSecretFile}, log); err != nil {
		return nil, err
	}
	directoryWorker.Service = a.Directory
	llmWorker.Service, llmTutorialWorker.Service = a.LLM, a.LLM
	a.Eval.SetLLM(a.LLM)
	a.Analytics = analytics.NewService(pool, a.Live, a.Eval, a.Auth, rc)
	analyticsWorker.Service = a.Analytics
	a.Admin = admin.NewService(pool, a.Auth)
	a.Poll = poll.NewService(pool, a.Content, a.Auth, cfg.BaseURL, log)
	a.Storage = storage.NewService(pool, a.Auth, log, cfg.BackupDir, cfg.NightlyBackupDir, opt.KEK)
	snapshotWorker.Service = a.Storage
	// Tutoring's reminders and notices go out as the platform's email jobs (D-84).
	tutorMail := func(ctx context.Context, msgs []mail.Message) error {
		return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			for _, m := range msgs {
				if _, err := rc.InsertTx(ctx, tx, mail.Args{Message: m}, nil); err != nil {
					return err
				}
			}
			return nil
		})
	}
	a.Tutor = tutorlink.New(opt.TutoringSecret, cfg.TutoringURL, a.Content, a.Auth, tutorMail)
	// Old quiz results (D-94): on demand, and every hour when a retention
	// period is set.
	a.Storage.AddArea(storage.Area{ID: "quiz_sessions", Label: "Quiz sessions that ended before the date, with their results", RetainDays: cfg.ResultsRetentionDays,
		Preview: func(ctx context.Context, before time.Time) (storage.Freed, error) {
			n, b, err := a.Live.PreviewOldSessions(ctx, before)
			return storage.Freed{Items: n, Bytes: b}, err
		},
		Run: func(ctx context.Context, _ string, before time.Time) (storage.Freed, error) {
			n, b, err := a.Live.DeleteOldSessions(ctx, before)
			return storage.Freed{Items: n, Bytes: b}, err
		}})
	a.Storage.AddArea(storage.Area{ID: "poll_files", Label: "Uploaded files of polls closed before the date",
		Preview: func(ctx context.Context, before time.Time) (storage.Freed, error) {
			n, b, err := a.Poll.PreviewOldFiles(ctx, before)
			return storage.Freed{Items: n, Bytes: b}, err
		},
		Run: func(ctx context.Context, _ string, before time.Time) (storage.Freed, error) {
			n, b, err := a.Poll.DeleteOldFiles(ctx, before)
			return storage.Freed{Items: n, Bytes: b}, err
		}})
	a.Analytics.SetPolls(a.Poll)
	a.Drive = drive.NewService(pool, vault, a.Auth, a.Content, a.Quiz, driveResults{a}, rc, cfg.BaseURL, opt.Google, opt.HTTPClient, log)
	driveBackup.Service, driveSync.Service = a.Drive, a.Drive
	a.Tutorial = tutorial.NewService(pool, a.Content, a.Quiz, a.Auth)
	a.Tutorial.SetLLM(a.LLM) // AI marking of written answers (D-75)
	a.LLM.SetTutorials(a.Tutorial)
	a.Slides = presentation.NewService(pool, a.Content, a.Auth, cfg.BaseURL)
	a.Slides.SetMarks(presentationMarks{a}) // time on slides against marks (D-78)
	// The admin's live activity: ending anything live, and pausing new
	// activities (D-90).
	a.Oversight = oversight.NewService(pool, map[string]oversight.Kind{
		oversight.KindSession:      {End: a.Live.ForceEnd},
		oversight.KindPoll:         {End: a.Poll.ForceClose},
		oversight.KindMeeting:      {End: a.Meet.ForceEnd, Online: a.Meet.Online},
		oversight.KindPresentation: {End: a.Slides.ForceEndLive, Online: a.Slides.Online},
	}, tutorMail)
	a.Progress = progress.NewService(pool)
	a.Dashboard = dashboard.NewService(pool)
	a.Live.SetGate(a.Oversight.Check)
	a.Poll.SetGate(a.Oversight.Check)
	a.Meet.SetGate(a.Oversight.Check)
	a.Slides.SetGate(a.Oversight.Check)
	a.Tutor.SetPauses(a.Oversight.Paused)
	a.Eval.SetResultsHook(a.Analytics.OnResults)
	a.Live.SetHooks(live.Hooks{
		AttemptFinished: a.Eval.OnAttemptFinished,
		SessionEnded: func(ctx context.Context, tx pgx.Tx, sessionID string) error {
			if err := a.Eval.OnSessionEnded(ctx, tx, sessionID); err != nil {
				return err
			}
			if err := a.Analytics.OnResults(ctx, tx, sessionID); err != nil { // completion and invalidation rates change at the end
				return err
			}
			return a.Drive.OnSessionEnded(ctx, tx, sessionID) // save results to Drive, if the teacher asked (D-71)
		},
	})

	r := chi.NewRouter()
	r.Use(httpx.Recover, httpx.SecurityHeaders)
	r.Get("/healthz", a.health)
	r.Route("/api", func(api chi.Router) {
		api.Use(httpx.SameOrigin(cfg.BaseURL), a.Auth.Middleware)
		api.Route("/auth", func(r chi.Router) {
			a.Auth.Routes(r)
			a.OIDC.Routes(r) // sign-in with providers (D-70)
		})
		api.Route("/public", a.Analytics.PublicRoutes)
		// Polls: anyone with the code; the poll decides whether login is needed (D-40).
		api.Route("/polls", a.Poll.PublicRoutes)
		if cfg.APIDocs {
			api.Route("/docs", apidocs.Routes)
		}
		api.Route("/admin", func(ad chi.Router) {
			// Admins and managers; each route checks its own feature (PL-NFR-06, D-56).
			ad.Use(auth.RequireStaff)
			a.Auth.AdminRoutes(ad)
			ad.With(a.Auth.RequireFeature(auth.FeatSettings)).Route("/settings", a.Settings.AdminRoutes)
			a.Admin.AdminRoutes(ad)
			a.Storage.AdminRoutes(ad, a.Auth)
			a.Directory.AdminRoutes(ad)
			a.OIDC.AdminRoutes(ad)
			a.Drive.AdminRoutes(ad)
			a.Meet.AdminRoutes(ad, a.Auth)
			a.Oversight.AdminRoutes(ad, a.Auth)
		})
		api.Route("/teacher", func(t chi.Router) {
			t.Use(auth.RequireRole(auth.RoleTeacher))
			t.Route("/settings", a.Settings.TeacherRoutes)
			a.Content.TeacherRoutes(t)
			a.Quiz.TeacherRoutes(t)
			a.Live.TeacherRoutes(t)
			a.Eval.TeacherRoutes(t)
			a.LLM.TeacherRoutes(t)
			a.Analytics.TeacherRoutes(t)
			a.Poll.TeacherRoutes(t)
			a.Meet.TeacherRoutes(t)
			a.Drive.TeacherRoutes(t)
			a.Tutorial.TeacherRoutes(t)
			a.Slides.TeacherRoutes(t)
			// A classroom's results: each student's best mark on each quiz (D-87, TS-FR-82).
			t.Method("GET", "/classrooms/{id}/results", httpx.Handler(a.classroomResults))
		})
		api.Group(func(s chi.Router) {
			s.Use(auth.RequireRole())
			a.Content.StudentRoutes(s)
			a.Admin.UserRoutes(s)
			a.Progress.StudentRoutes(s)
			a.Dashboard.Routes(s)
			s.Route("/tutoring", tutorlink.UserRoutes(a.Tutor))
			a.Meet.UserRoutes(s)
			a.Drive.UserRoutes(s)
		})
		api.Group(func(s chi.Router) {
			s.Use(auth.RequireRole(auth.RoleStudent))
			a.Live.StudentRoutes(s)
			a.Tutorial.StudentRoutes(s)
			a.Slides.StudentRoutes(s)
			a.Eval.StudentRoutes(s)
		})
	})
	// WebSockets: the upgrade checks Origin itself; no CSRF header is possible.
	r.Route("/ws", func(ws chi.Router) {
		ws.Use(a.Auth.Middleware)
		a.Live.WSRoutes(ws)
		a.Poll.WSRoutes(ws)
		a.Meet.WSRoutes(ws)
		a.Slides.WSRoutes(ws) // presenting live (D-77)
	})
	// The tutoring service's calls (D-59); not under /api, so not public.
	r.Route("/internal/tutoring", tutorlink.InternalRoutes(a.Tutor))
	r.Route("/beacon", func(b chi.Router) {
		b.Use(a.Auth.Middleware)
		a.Live.BeaconRoutes(b)
	})
	if cfg.StaticDir != "" {
		r.NotFound(httpx.SPA(cfg.StaticDir).ServeHTTP)
	}
	a.router = r
	return a, nil
}

func newMailer(cfg config.Config, log *slog.Logger) (mail.Sender, error) {
	if cfg.SMTPAddr == "" {
		return mail.LogSender{Log: log}, nil
	}
	s := mail.SMTPSender{Addr: cfg.SMTPAddr, From: cfg.SMTPFrom, Username: cfg.SMTPUser}
	if cfg.SMTPPasswordFile != "" {
		b, err := os.ReadFile(cfg.SMTPPasswordFile)
		if err != nil {
			return nil, err
		}
		s.Password = strings.TrimSpace(string(b))
	}
	return s, nil
}

func (a *App) Handler() http.Handler { return a.router }

// Start runs background workers and the live-session ticker.
func (a *App) Start(ctx context.Context) error {
	go a.Live.Run(ctx)
	go a.Poll.Run(ctx)
	if err := a.Storage.Recover(ctx); err != nil {
		return err
	}
	return a.river.Start(ctx)
}

func (a *App) Close() {
	a.Storage.Wait() // let a running backup finish its file
	_ = a.river.Stop(context.Background())
	if a.ownPool {
		a.pool.Close()
	}
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	if err := a.pool.Ping(r.Context()); err != nil {
		httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db_unavailable"})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// presentationMarks finds a topic's quizzes and tutorials, and students'
// scores on them, for presentations (D-78).
type presentationMarks struct{ a *App }

func (m presentationMarks) Linkable(ctx context.Context, teacherID, topicID string) ([]presentation.Linkable, error) {
	var out []presentation.Linkable
	for pg := 1; ; pg++ {
		qs, total, err := m.a.Quiz.ListQuizzes(ctx, teacherID, topicID, "", page.Request{Page: pg, Size: page.MaxSize})
		if err != nil {
			return nil, err
		}
		for _, q := range qs {
			out = append(out, presentation.Linkable{Kind: "quiz", ID: q.ID, Title: q.Title})
		}
		if pg*page.MaxSize >= total {
			break
		}
	}
	ts, err := m.a.Tutorial.List(ctx, teacherID, topicID)
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		out = append(out, presentation.Linkable{Kind: "tutorial", ID: t.ID, Title: t.Title})
	}
	return out, nil
}

func (m presentationMarks) Scores(ctx context.Context, quizIDs, tutorialIDs []string) (map[string][2]float64, error) {
	out, err := m.a.Eval.QuizScores(ctx, quizIDs)
	if err != nil {
		return nil, err
	}
	ts, err := m.a.Tutorial.Scores(ctx, tutorialIDs)
	if err != nil {
		return nil, err
	}
	for user, v := range ts {
		w := out[user]
		out[user] = [2]float64{w[0] + v[0], w[1] + v[1]}
	}
	return out, nil
}

// driveResults gives the Drive integration results to save (D-71).
type driveResults struct{ a *App }

func (d driveResults) SessionCSV(ctx context.Context, teacherID, sessionID string) (string, []byte, error) {
	info, err := d.a.Live.SessionInfo(ctx, teacherID, sessionID)
	if err != nil {
		return "", nil, err
	}
	data, err := d.a.Analytics.ExportSessionCSV(ctx, teacherID, sessionID)
	if err != nil {
		return "", nil, err
	}
	name := info.QuizTitle
	if info.Title != "" && info.Title != info.QuizTitle {
		name += " – " + info.Title
	}
	return fmt.Sprintf("%s (%s)", name, info.CreatedAt.Format("2 Jan 2006 15:04")), data, nil
}

func (d driveResults) PresentationCSV(ctx context.Context, teacherID, id string) (string, []byte, error) {
	return d.a.Slides.CSV(ctx, teacherID, id)
}

func (d driveResults) PollCSV(ctx context.Context, teacherID, pollID string) (string, []byte, error) {
	return d.a.Poll.ExportCSV(ctx, teacherID, pollID)
}

func (d driveResults) SessionTeacher(ctx context.Context, sessionID string) (string, error) {
	info, err := d.a.Live.SessionInfo(ctx, "", sessionID)
	return info.TeacherID, err
}

// ClassroomStudentResult is one active student's row in a classroom's results.
type ClassroomStudentResult struct {
	UserID        string             `json:"user_id"`
	Name          string             `json:"name"`
	StudentNumber *string            `json:"student_number,omitempty"`
	Scores        map[string]float64 `json:"scores"`            // quiz id → best %
	Average       *float64           `json:"average,omitempty"` // of the quizzes they took
}

// classroomResults is GET /api/teacher/classrooms/{id}/results (D-87,
// TS-FR-82): the roster from content (which checks the classroom is the
// teacher's) with the marks from eval. Attendance comes from the tutoring
// service; the page sets it beside these.
func (a *App) classroomResults(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	roster, err := a.Content.ListEnrolments(r.Context(), auth.MustUser(r.Context()).ID, id, "active")
	if err != nil {
		return err
	}
	quizzes, best, err := a.Eval.ClassroomScores(r.Context(), id)
	if err != nil {
		return err
	}
	students := []ClassroomStudentResult{}
	for _, e := range roster {
		row := ClassroomStudentResult{UserID: e.UserID, Name: e.StudentName, StudentNumber: e.StudentNumber, Scores: best[e.UserID]}
		if row.Scores == nil {
			row.Scores = map[string]float64{}
		}
		if n := len(row.Scores); n > 0 {
			var sum float64
			for _, v := range row.Scores {
				sum += v
			}
			avg := math.Round(sum/float64(n)*10) / 10
			row.Average = &avg
		}
		students = append(students, row)
	}
	sort.Slice(students, func(i, j int) bool { return strings.ToLower(students[i].Name) < strings.ToLower(students[j].Name) })
	httpx.JSON(w, http.StatusOK, map[string]any{"quizzes": quizzes, "students": students})
	return nil
}
