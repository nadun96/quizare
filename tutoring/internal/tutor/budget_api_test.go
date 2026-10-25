package tutor_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nadun96/quizplatform/tutoring/internal/media"
	"github.com/nadun96/quizplatform/tutoring/internal/testenv"
	"github.com/nadun96/quizplatform/tutoring/internal/tutor"
)

// Phase 4 (D-81): the bandwidth budget, session plans, admission control,
// and the monitor that switches things off when tutoring is over budget.

type apiError struct {
	Code    string
	Message string
	Fields  map[string]string
}

func TestPlansAndPresets(t *testing.T) {
	f := setup(t, "auto")
	if f.sess.Plan != tutor.DefaultPlan || len(f.sess.AutoOff) != 0 {
		t.Fatalf("a new session's plan = %+v, auto_off %v", f.sess.Plan, f.sess.AutoOff)
	}
	// The lecture default: 30 students, a screen and a camera, two students sharing.
	var pv tutor.PlanView
	f.teacher.Call("GET", f.path("/plan"), nil, 200, &pv)
	if pv.Forecast.Mbps != 55.76 || !pv.Fits || pv.Limit != 240 || pv.HardFree != 270 || len(pv.Forecast.Items) != 5 {
		t.Fatalf("default plan = %+v", pv)
	}
	ann := f.student("Ann")
	f.join(ann, 200)
	ann.Call("GET", f.path("/plan"), nil, 404, nil)
	ann.Call("POST", "/api/plans/estimate", tutor.DefaultPlan, 403, nil)
	co := f.e.NewPerson("teacher", "Mr Silva")
	f.teacher.Call("POST", f.path("/coteachers"), map[string]string{"email": co.Email}, 201, nil)
	f.join(co, 200)
	co.Call("GET", f.path("/plan"), nil, 200, nil)
	co.Call("PUT", f.path("/plan"), tutor.DefaultPlan, 403, nil) // the lead's

	// Limits: the broadcaster's tracks, and students sharing within the admin's caps.
	var bad apiError
	f.teacher.Call("PUT", f.path("/plan"), tutor.Plan{Students: 10, Screens: 3, Cameras: 2, StudentCameras: 3, Quality: "4k"}, 422, &bad)
	if len(bad.Fields) != 3 || bad.Fields["screens"] == "" || bad.Fields["student_cameras"] == "" || bad.Fields["quality"] == "" {
		t.Fatalf("bad plan = %+v", bad)
	}

	// Too big for the server at all: advice, and no starting even when insisting.
	big := tutor.Plan{Students: 200, Screens: 1, Cameras: 1, Quality: "smooth", StudentMics: 2, StudentCameras: 2, StudentScreens: 2}
	f.teacher.Call("PUT", f.path("/plan"), big, 200, &pv)
	if pv.Fits || pv.Forecast.Mbps != 594.56 || !slices.Equal(pv.Advice, []string{"plan for at most 188 students"}) {
		t.Fatalf("200 students, smooth = %+v", pv)
	}
	f.teacher.Call("POST", f.path("/start"), map[string]bool{"force": true}, 409, &bad)
	if bad.Code != "over_budget" || !strings.Contains(bad.Message, "at most 270") {
		t.Fatalf("starting over the hard limit = %+v", bad)
	}
	// Over the budget but under the hard limit: the teacher may insist.
	mid := tutor.Plan{Students: 200, Screens: 1, Quality: "readable", StudentMics: 2, StudentCameras: 2, StudentScreens: 2}
	f.teacher.Call("PUT", f.path("/plan"), mid, 200, &pv)
	if pv.Fits || pv.Forecast.Mbps != 254.56 {
		t.Fatalf("200 students, one screen = %+v", pv)
	}
	f.teacher.Call("POST", f.path("/start"), nil, 409, &bad)
	if !strings.HasSuffix(bad.Message, "or start anyway") {
		t.Fatalf("starting over the budget = %+v", bad)
	}
	f.teacher.Call("POST", f.path("/start"), map[string]bool{"force": true}, 204, nil)
	var logged int
	if err := f.e.Pool.QueryRow(t.Context(), `SELECT count(*) FROM tutoring.log WHERE action='budget_overridden'`).Scan(&logged); err != nil || logged != 1 {
		t.Fatalf("overrides logged = %d, %v", logged, err)
	}
	if v := f.view(ann); v.Session.Plan != mid {
		t.Fatalf("the session's plan = %+v", v.Session.Plan)
	}

	// An estimate before there is a session; a session made with a plan.
	f.teacher.Call("POST", "/api/plans/estimate", tutor.Plan{Students: 5, Screens: 1}, 200, &pv)
	if pv.Forecast.Mbps != 6.2 || !pv.Fits || pv.Plan.Quality != "readable" {
		t.Fatalf("estimate = %+v", pv)
	}
	var made tutor.Session
	f.teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": f.classroom, "title": "Small", "plan": tutor.Plan{Students: 5, Screens: 1}}, 201, &made)
	if made.Plan.Students != 5 || made.Plan.Cameras != 0 {
		t.Fatalf("made with a plan = %+v", made.Plan)
	}
	f.teacher.Call("POST", "/api/sessions", map[string]any{"classroom_id": f.classroom, "title": "Bad", "plan": tutor.Plan{Students: 0}}, 422, nil)

	// Presets: saved by name, replaced by the same name, each teacher's own.
	var pr tutor.Preset
	f.teacher.Call("POST", "/api/plan-presets", map[string]any{"name": " Lecture ", "plan": tutor.DefaultPlan}, 200, &pr)
	f.teacher.Call("POST", "/api/plan-presets", map[string]any{"name": "Lecture", "plan": mid}, 200, nil)
	f.teacher.Call("POST", "/api/plan-presets", map[string]any{"name": "", "plan": mid}, 422, nil)
	f.teacher.Call("POST", "/api/plan-presets", map[string]any{"name": "Huge", "plan": tutor.Plan{Students: 501}}, 422, nil)
	ann.Call("POST", "/api/plan-presets", map[string]any{"name": "Mine", "plan": mid}, 403, nil)
	var list struct{ Presets []tutor.Preset }
	f.teacher.Call("GET", "/api/plan-presets", nil, 200, &list)
	if len(list.Presets) != 1 || list.Presets[0].Name != "Lecture" || list.Presets[0].Plan != mid || list.Presets[0].ID != pr.ID {
		t.Fatalf("presets = %+v", list.Presets)
	}
	co.Call("GET", "/api/plan-presets", nil, 200, &list)
	if len(list.Presets) != 0 {
		t.Fatalf("another teacher's presets = %+v", list.Presets)
	}
	co.Call("DELETE", "/api/plan-presets/"+pr.ID, nil, 404, nil)
	f.teacher.Call("DELETE", "/api/plan-presets/"+pr.ID, nil, 204, nil)
	f.teacher.Call("DELETE", "/api/plan-presets/"+pr.ID, nil, 404, nil)
}

func TestBudgetAndMonitor(t *testing.T) {
	f := setup(t, "auto")
	ctx := context.Background()
	admin := f.e.NewPerson("admin", "Admin")

	// The admin's budget (TS-FR-31): defaults, checked, changed at once.
	var ov tutor.BudgetOverview
	f.teacher.Call("GET", "/api/admin/budget", nil, 403, nil)
	admin.Call("GET", "/api/admin/budget", nil, 200, &ov)
	if ov.Budget != tutor.DefaultBudget || len(ov.Sessions) != 0 {
		t.Fatalf("overview = %+v", ov)
	}
	setBudget := func(total, hard float64) {
		t.Helper()
		b := tutor.DefaultBudget
		b.TotalMbps, b.HardCapMbps = total, hard
		admin.Call("PUT", "/api/admin/budget", b, 200, nil)
	}
	var bad apiError
	admin.Call("PUT", "/api/admin/budget", tutor.Budget{TotalMbps: 50, HardCapMbps: 40, MaxTeacherTracks: 4, MaxStudentCams: 1, MaxStudentScreen: 2}, 422, &bad)
	if len(bad.Fields) != 2 || bad.Fields["hard_cap_mbps"] == "" || bad.Fields["max_student_cameras"] == "" {
		t.Fatalf("bad budget = %+v", bad)
	}
	f.teacher.Call("PUT", "/api/admin/budget", tutor.DefaultBudget, 403, nil)
	setBudget(40, 50)

	small := tutor.Plan{Students: 10, Screens: 1, Cameras: 1, Quality: "readable", StudentMics: 2, StudentCameras: 2, StudentScreens: 2}
	f.teacher.Call("PUT", f.path("/plan"), small, 200, nil)
	ann, bob, cy := f.student("Ann"), f.student("Bob"), f.student("Cy")
	for _, p := range []*testenv.Person{f.teacher, ann, bob, cy} {
		f.join(p, 200)
	}
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	ts, as := f.teacher.Dial(f.sess.ID), ann.Dial(f.sess.ID)
	bob.Dial(f.sess.ID).Must("state")
	cy.Dial(f.sess.ID).Must("state")
	ts.Must("state")
	as.Must("state")

	// Students sharing: two at once, the plan's and the admin's cap (TS-FR-25, TS-FR-32).
	yes := true
	f.teacher.Call("PUT", f.path("/permissions"), tutor.PermissionsInput{UserIDs: []string{ann.ID, bob.ID}, Camera: &yes}, 200, nil)
	f.teacher.Call("PUT", f.path("/permissions"), tutor.PermissionsInput{UserIDs: []string{cy.ID}, Camera: &yes}, 409, &bad)
	if bad.Code != "over_budget" || !strings.Contains(bad.Message, "2 students share their cameras") {
		t.Fatalf("a third camera = %+v", bad)
	}
	f.teacher.Call("PUT", f.path("/permissions"), tutor.PermissionsInput{All: true, Camera: &yes}, 409, nil)
	f.teacher.Call("PUT", f.path("/permissions"), tutor.PermissionsInput{UserIDs: []string{cy.ID}, Mic: &yes}, 200, nil) // voices aren't capped

	// Breakout groups with cameras, within the budget.
	var formed struct{ Groups []tutor.Group }
	f.teacher.Call("PUT", f.path("/groups"), map[string]any{"mode": "manual", "groups": []map[string]any{{"name": "A", "user_ids": []string{ann.ID, bob.ID}}}}, 200, &formed)
	f.teacher.Call("POST", f.path("/groups/open"), nil, 204, nil)
	f.teacher.Call("PUT", f.path("/groups/settings"), map[string]any{"group_camera": true}, 204, nil)
	group := fmt.Sprintf("%s-g%d", f.sess.ID, formed.Groups[0].ID)

	// What LiveKit says is sent: the teacher's two screens, a camera and a voice;
	// Ann's camera backstage; Ann and Bob's cameras in their group.
	lk := f.e.LiveKit
	lk.Publish(f.sess.ID, f.teacher.ID, media.Track{Sid: "S1", Source: "SCREEN_SHARE"}, media.Track{Sid: "S2", Source: "SCREEN_SHARE"},
		media.Track{Sid: "C1", Source: "CAMERA"}, media.Track{Sid: "M1", Source: "MICROPHONE"})
	lk.Publish(f.sess.ID+"-stage", ann.ID, media.Track{Sid: "A1", Source: "CAMERA"})
	lk.Publish(group, ann.ID, media.Track{Sid: "G1", Source: "CAMERA"})
	lk.Publish(group, bob.ID, media.Track{Sid: "G2", Source: "CAMERA"})

	// The panel (TS-FR-33): 3 watching; screen 3.6 + 0.6, camera 1.2, voice 0.12,
	// backstage 0.4, groups 0.8.
	check := func() {
		t.Helper()
		if err := f.e.Service.CheckLoad(ctx); err != nil {
			t.Fatal(err)
		}
	}
	check()
	res := ts.Must("resources")["data"].(map[string]any)
	load := res["load"].(map[string]any)
	if load["mbps"] != 6.72 || load["viewers"] != 3.0 || load["screens"] != 2.0 || load["students_sharing"] != 1.0 || res["total_mbps"] != 6.72 || res["limit_mbps"] != 40.0 {
		t.Fatalf("resources = %v", res)
	}
	as.Quiet("resources", 200*time.Millisecond)
	var r tutor.Resources
	f.teacher.Call("GET", f.path("/resources"), nil, 200, &r)
	if r.Load.Mbps != 6.72 || len(r.Load.Items) != 6 || r.Plan.Mbps != 22.96 {
		t.Fatalf("GET resources = %+v", r)
	}
	ann.Call("GET", f.path("/resources"), nil, 404, nil)
	admin.Call("GET", "/api/admin/budget", nil, 200, &ov)
	if ov.Total != 6.72 || len(ov.Sessions) != 1 || ov.Sessions[0].Viewers != 3 {
		t.Fatalf("overview = %+v", ov)
	}

	// Asking before adding a track (TS-FR-32): only the broadcaster.
	var est tutor.Estimate
	f.teacher.Call("POST", f.path("/budget/track"), tutor.TrackCheck{Kind: "camera"}, 200, &est)
	if est.Mbps != 0.6 {
		t.Fatalf("another camera = %+v", est)
	}
	ann.Call("POST", f.path("/budget/track"), tutor.TrackCheck{Kind: "camera"}, 403, nil)
	f.teacher.Call("POST", f.path("/budget/track"), tutor.TrackCheck{Kind: "slides"}, 422, nil)

	// Over the budget for two checks in a row: video in groups goes first (TS-FR-38).
	setBudget(5, 6)
	f.teacher.Call("POST", f.path("/budget/track"), tutor.TrackCheck{Kind: "camera"}, 409, &bad)
	if !strings.Contains(bad.Message, "hard limit") {
		t.Fatalf("over the hard limit = %+v", bad)
	}
	lk.Calls("UpdateParticipant")
	check()
	ts.Quiet("auto_disabled", 200*time.Millisecond)
	check()
	off := ts.Must("auto_disabled")["data"].(map[string]any)
	if off["step"] != "group_camera" || !strings.Contains(off["message"].(string), "Cameras in groups") {
		t.Fatalf("first step = %v", off)
	}
	if v := f.view(f.teacher); v.Session.GroupCamera || !slices.Equal(v.Session.AutoOff, []string{"group_camera"}) {
		t.Fatalf("after step 1: camera %v, auto_off %v", v.Session.GroupCamera, v.Session.AutoOff)
	}
	// Re-applied to both members (each keeps a camera the teacher allowed them personally, TS-FR-53).
	applied := 0
	for _, c := range lk.Calls("UpdateParticipant") {
		if c.Body["room"] == group {
			applied++
		}
	}
	if applied != 2 {
		t.Fatalf("group permissions re-applied %d times", applied)
	}
	lk.Publish(group, ann.ID)
	lk.Publish(group, bob.ID)

	// Still over: two students share, so nobody loses that; the extra screen stops.
	check()
	check()
	if off = ts.Must("auto_disabled")["data"].(map[string]any); off["step"] != "extra_tracks" {
		t.Fatalf("second step = %v", off)
	}
	if got := lk.Published(f.sess.ID, f.teacher.ID); !got[1].Muted || got[0].Muted {
		t.Fatalf("after step 3: %+v", got)
	}
	f.teacher.Call("POST", f.path("/budget/track"), tutor.TrackCheck{Kind: "screen"}, 409, &bad)
	if !strings.Contains(bad.Message, "extra screens and cameras were stopped") {
		t.Fatalf("another screen after step 3 = %+v", bad)
	}
	// Then the camera: 5.32 is still over 5.
	check()
	check()
	if off = ts.Must("auto_disabled")["data"].(map[string]any); off["step"] != "camera" {
		t.Fatalf("third step = %v", off)
	}
	if got := lk.Published(f.sess.ID, f.teacher.ID); !got[2].Muted || got[3].Muted {
		t.Fatalf("after step 4: %+v", got)
	}
	// 4.12 is under the budget: nothing more goes, but it isn't room enough (80 %) to offer.
	check()
	check()
	ts.Quiet("auto_disabled", 200*time.Millisecond)
	ts.Quiet("resources_available", 100*time.Millisecond)

	// Room again: the teachers are told once, and choose (TS-FR-39).
	setBudget(10, 12)
	check()
	avail := ts.Must("resources_available")["data"].(map[string]any)
	if fmt.Sprint(avail["auto_off"]) != "[camera extra_tracks group_camera]" {
		t.Fatalf("resources available = %v", avail)
	}
	check()
	ts.Quiet("resources_available", 200*time.Millisecond)
	f.teacher.Call("POST", f.path("/budget/track"), tutor.TrackCheck{Kind: "camera"}, 409, &bad)
	if !strings.Contains(bad.Message, "camera was turned off") {
		t.Fatalf("the camera while off = %+v", bad)
	}
	ann.Call("POST", f.path("/budget/restore"), tutor.RestoreInput{Step: "camera"}, 404, nil)
	f.teacher.Call("POST", f.path("/budget/restore"), tutor.RestoreInput{Step: "camera"}, 204, nil)
	if m := ts.Must("restored")["data"].(map[string]any); m["step"] != "camera" {
		t.Fatalf("restored = %v", m)
	}
	f.teacher.Call("POST", f.path("/budget/restore"), tutor.RestoreInput{Step: "camera"}, 422, nil)
	f.teacher.Call("POST", f.path("/budget/track"), tutor.TrackCheck{Kind: "camera"}, 200, nil)
	// Video in groups adds 3.2: over 10 only with force; then it's on again.
	setBudget(6, 12)
	f.teacher.Call("POST", f.path("/budget/restore"), tutor.RestoreInput{Step: "group_camera"}, 409, &bad)
	if !strings.HasSuffix(bad.Message, "or turn it on anyway") {
		t.Fatalf("restoring over the budget = %+v", bad)
	}
	f.teacher.Call("POST", f.path("/budget/restore"), tutor.RestoreInput{Step: "group_camera", Force: true}, 204, nil)
	if v := f.view(f.teacher); !v.Session.GroupCamera || !slices.Equal(v.Session.AutoOff, []string{"extra_tracks"}) {
		t.Fatalf("after restoring: camera %v, auto_off %v", v.Session.GroupCamera, v.Session.AutoOff)
	}
	// Turning group video on by hand is checked too.
	f.teacher.Call("PUT", f.path("/groups/settings"), map[string]any{"group_screen": true}, 409, nil)
	f.teacher.Call("PUT", f.path("/groups/settings"), map[string]any{"group_screen": true, "force": true}, 204, nil) // 4.8 more: under the hard limit
	var logged int
	if err := f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM tutoring.log WHERE action='auto_disabled' AND actor_id IS NULL`).Scan(&logged); err != nil || logged != 3 {
		t.Fatalf("logged = %d, %v", logged, err)
	}
}

// When students beyond two share (the plan allows more), they lose it first after group video.
func TestMonitorCutsStudentShares(t *testing.T) {
	f := setup(t, "auto")
	admin := f.e.NewPerson("admin", "Admin")
	b := tutor.DefaultBudget
	b.MaxStudentCams = 4
	admin.Call("PUT", "/api/admin/budget", b, 200, nil)
	f.teacher.Call("PUT", f.path("/plan"), tutor.Plan{Students: 10, Screens: 1, StudentCameras: 4}, 200, nil)
	var ss []*testenv.Person
	for _, n := range []string{"Ann", "Bob", "Cy", "Dee"} {
		s := f.student(n)
		f.join(s, 200)
		ss = append(ss, s)
	}
	f.join(f.teacher, 200)
	f.teacher.Call("POST", f.path("/start"), nil, 204, nil)
	ts := f.teacher.Dial(f.sess.ID)
	ts.Must("state")
	ds := ss[3].Dial(f.sess.ID)
	ds.Must("state")
	yes := true
	ids := []string{ss[0].ID, ss[1].ID, ss[2].ID, ss[3].ID}
	f.teacher.Call("PUT", f.path("/permissions"), tutor.PermissionsInput{UserIDs: ids, Camera: &yes}, 200, nil)
	b.TotalMbps, b.HardCapMbps = 1, 2
	admin.Call("PUT", "/api/admin/budget", b, 200, nil)
	f.e.LiveKit.Publish(f.sess.ID+"-stage", ss[3].ID, media.Track{Sid: "D1", Source: "CAMERA"})
	f.e.LiveKit.Publish(f.sess.ID+"-stage", ss[2].ID, media.Track{Sid: "C1", Source: "CAMERA"})
	f.e.LiveKit.Publish(f.sess.ID+"-stage", ss[1].ID, media.Track{Sid: "B1", Source: "CAMERA"})
	for range 2 {
		if err := f.e.Service.CheckLoad(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if off := ts.Must("auto_disabled")["data"].(map[string]any); off["step"] != "student_shares" {
		t.Fatalf("step = %v", off)
	}
	ds.Must("auto_disabled") // Dee joined last: she is told
	v := f.view(f.teacher)
	var allowed []string
	for _, p := range v.Participants {
		if p.AllowCamera {
			allowed = append(allowed, p.Name)
		}
	}
	if !slices.Equal(allowed, []string{"Ann", "Bob"}) || !slices.Equal(v.Session.AutoOff, []string{"student_shares"}) {
		t.Fatalf("allowed after = %v, auto_off %v", allowed, v.Session.AutoOff)
	}
	// While it's off, only two may share, whatever the plan says.
	f.teacher.Call("PUT", f.path("/permissions"), tutor.PermissionsInput{UserIDs: []string{ss[2].ID}, Camera: &yes}, 409, nil)
}
