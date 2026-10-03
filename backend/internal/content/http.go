package content

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// TeacherRoutes mounts teacher endpoints (teacher role enforced by caller).
func (s *Service) TeacherRoutes(r chi.Router) {
	r.Method("GET", "/classrooms", httpx.Handler(s.hListClassrooms))
	r.Method("POST", "/classrooms", httpx.Handler(s.hCreateClassroom))
	r.Method("GET", "/classrooms/{id}", httpx.Handler(s.hGetClassroom))
	r.Method("PATCH", "/classrooms/{id}", httpx.Handler(s.hUpdateClassroom))
	r.Method("DELETE", "/classrooms/{id}", httpx.Handler(s.hDeleteClassroom))
	r.Method("POST", "/classrooms/{id}/archive", httpx.Handler(s.hArchive(true)))
	r.Method("POST", "/classrooms/{id}/unarchive", httpx.Handler(s.hArchive(false)))
	r.Method("POST", "/classrooms/{id}/join-code", httpx.Handler(s.hRegenerateCode))
	r.Method("GET", "/classrooms/{id}/enrolments", httpx.Handler(s.hListEnrolments))
	r.Method("PATCH", "/enrolments/{id}", httpx.Handler(s.hUpdateEnrolment))

	r.Method("GET", "/classrooms/{id}/modules", httpx.Handler(s.hListModules))
	r.Method("POST", "/classrooms/{id}/modules", httpx.Handler(s.hCreateModule))
	r.Method("PATCH", "/modules/{id}", httpx.Handler(s.hUpdateModule))
	r.Method("DELETE", "/modules/{id}", httpx.Handler(s.hDeleteModule))
	r.Method("GET", "/modules/{id}/topics", httpx.Handler(s.hListTopics))
	r.Method("POST", "/modules/{id}/topics", httpx.Handler(s.hCreateTopic))
	r.Method("PATCH", "/topics/{id}", httpx.Handler(s.hUpdateTopic))
	r.Method("DELETE", "/topics/{id}", httpx.Handler(s.hDeleteTopic))
}

// StudentRoutes mounts endpoints for any logged-in user (enrolment is by code).
func (s *Service) StudentRoutes(r chi.Router) {
	r.Method("GET", "/join/classrooms/{code}", httpx.Handler(s.hPreview))
	r.Method("POST", "/enrolments", httpx.Handler(s.hEnrol))
	r.Method("GET", "/my/classrooms", httpx.Handler(s.hMyClassrooms))
}

func uid(r *http.Request) string { return auth.MustUser(r.Context()).ID }

func (s *Service) hListClassrooms(w http.ResponseWriter, r *http.Request) error {
	list, err := s.ListClassrooms(r.Context(), uid(r), r.URL.Query().Get("archived") == "1")
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, map[string]any{"classrooms": list})
	return nil
}

func (s *Service) hCreateClassroom(w http.ResponseWriter, r *http.Request) error {
	var in ClassroomInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	c, err := s.CreateClassroom(r.Context(), uid(r), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 201, c)
	return nil
}

func (s *Service) hGetClassroom(w http.ResponseWriter, r *http.Request) error {
	c, err := s.GetClassroom(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, c)
	return nil
}

func (s *Service) hUpdateClassroom(w http.ResponseWriter, r *http.Request) error {
	var in ClassroomInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	c, err := s.UpdateClassroom(r.Context(), uid(r), chi.URLParam(r, "id"), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, c)
	return nil
}

func (s *Service) hDeleteClassroom(w http.ResponseWriter, r *http.Request) error {
	if err := s.DeleteClassroom(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

func (s *Service) hArchive(archived bool) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		if err := s.SetArchived(r.Context(), uid(r), chi.URLParam(r, "id"), archived); err != nil {
			return err
		}
		w.WriteHeader(204)
		return nil
	}
}

func (s *Service) hRegenerateCode(w http.ResponseWriter, r *http.Request) error {
	code, err := s.RegenerateJoinCode(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, map[string]string{"join_code": code})
	return nil
}

func (s *Service) hListEnrolments(w http.ResponseWriter, r *http.Request) error {
	list, err := s.ListEnrolments(r.Context(), uid(r), chi.URLParam(r, "id"), r.URL.Query().Get("status"))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, map[string]any{"enrolments": list})
	return nil
}

func (s *Service) hUpdateEnrolment(w http.ResponseWriter, r *http.Request) error {
	var in EnrolmentUpdate
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	e, err := s.UpdateEnrolment(r.Context(), uid(r), chi.URLParam(r, "id"), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, e)
	return nil
}

func (s *Service) hListModules(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	if _, err := s.GetClassroom(r.Context(), uid(r), id); err != nil {
		return err
	}
	list, err := s.ListModules(r.Context(), uid(r), id)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, map[string]any{"modules": list})
	return nil
}

func (s *Service) hCreateModule(w http.ResponseWriter, r *http.Request) error {
	var in NodeInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	m, err := s.CreateModule(r.Context(), uid(r), chi.URLParam(r, "id"), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 201, m)
	return nil
}

func (s *Service) hUpdateModule(w http.ResponseWriter, r *http.Request) error {
	var in NodeInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.UpdateModule(r.Context(), uid(r), chi.URLParam(r, "id"), in); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

func (s *Service) hDeleteModule(w http.ResponseWriter, r *http.Request) error {
	if err := s.DeleteModule(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

func (s *Service) hListTopics(w http.ResponseWriter, r *http.Request) error {
	list, err := s.ListTopics(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, map[string]any{"topics": list})
	return nil
}

func (s *Service) hCreateTopic(w http.ResponseWriter, r *http.Request) error {
	var in NodeInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	t, err := s.CreateTopic(r.Context(), uid(r), chi.URLParam(r, "id"), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 201, t)
	return nil
}

func (s *Service) hUpdateTopic(w http.ResponseWriter, r *http.Request) error {
	var in NodeInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.UpdateTopic(r.Context(), uid(r), chi.URLParam(r, "id"), in); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

func (s *Service) hDeleteTopic(w http.ResponseWriter, r *http.Request) error {
	if err := s.DeleteTopic(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

func (s *Service) hPreview(w http.ResponseWriter, r *http.Request) error {
	p, err := s.Preview(r.Context(), uid(r), chi.URLParam(r, "code"))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, p)
	return nil
}

func (s *Service) hEnrol(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		JoinCode      string `json:"join_code"`
		StudentNumber string `json:"student_number"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	e, err := s.EnrolByCode(r.Context(), uid(r), in.JoinCode, in.StudentNumber)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, e)
	return nil
}

func (s *Service) hMyClassrooms(w http.ResponseWriter, r *http.Request) error {
	list, err := s.MyClassrooms(r.Context(), uid(r))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, map[string]any{"classrooms": list})
	return nil
}
