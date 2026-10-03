package quiz

import (
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nadun96/quizplatform/internal/auth"
	"github.com/nadun96/quizplatform/internal/platform/httpx"
)

// TeacherRoutes mounts quiz authoring under /api/teacher (role enforced by caller).
func (s *Service) TeacherRoutes(r chi.Router) {
	r.Method("GET", "/topics/{id}/quizzes", httpx.Handler(s.hListQuizzes))
	r.Method("POST", "/topics/{id}/quizzes", httpx.Handler(s.hCreateQuiz))
	r.Method("GET", "/quizzes/{id}", httpx.Handler(s.hGetQuiz))
	r.Method("PATCH", "/quizzes/{id}", httpx.Handler(s.hUpdateQuiz))
	r.Method("DELETE", "/quizzes/{id}", httpx.Handler(s.hDeleteQuiz))
	r.Method("POST", "/quizzes/{id}/status", httpx.Handler(s.hSetStatus))
	r.Method("GET", "/quizzes/{id}/readiness", httpx.Handler(s.hReadiness))
	r.Method("GET", "/quizzes/{id}/preview", httpx.Handler(s.hPreview))

	r.Method("GET", "/quizzes/{id}/questions", httpx.Handler(s.hListQuestions))
	r.Method("POST", "/quizzes/{id}/questions", httpx.Handler(s.hCreateQuestion))
	r.Method("PUT", "/quizzes/{id}/questions/order", httpx.Handler(s.hReorder))
	r.Method("POST", "/quizzes/{id}/questions/import", httpx.Handler(s.hImport))
	r.Method("POST", "/quizzes/{id}/resources/import", httpx.Handler(s.hImportResources))
	r.Method("POST", "/quizzes/{id}/resources/check", httpx.Handler(s.hCheck))
	r.Method("GET", "/questions/{id}", httpx.Handler(s.hGetQuestion))
	r.Method("PUT", "/questions/{id}", httpx.Handler(s.hUpdateQuestion))
	r.Method("DELETE", "/questions/{id}", httpx.Handler(s.hDeleteQuestion))
	r.Method("POST", "/questions/{id}/duplicate", httpx.Handler(s.hDuplicate))
	r.Method("PUT", "/questions/{id}/resources", httpx.Handler(s.hSetResource))
	r.Method("DELETE", "/resources/{id}", httpx.Handler(s.hDeleteResource))

	r.Get("/quiz-template.csv", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="quiz-template.csv"`)
		_, _ = io.WriteString(w, Template)
	})
}

func uid(r *http.Request) string { return auth.MustUser(r.Context()).ID }
func id(r *http.Request) string  { return chi.URLParam(r, "id") }

func (s *Service) hListQuizzes(w http.ResponseWriter, r *http.Request) error {
	list, err := s.ListQuizzes(r.Context(), uid(r), id(r))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, map[string]any{"quizzes": list})
	return nil
}

func (s *Service) hCreateQuiz(w http.ResponseWriter, r *http.Request) error {
	var in QuizInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	q, err := s.CreateQuiz(r.Context(), uid(r), id(r), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 201, q)
	return nil
}

func (s *Service) hGetQuiz(w http.ResponseWriter, r *http.Request) error {
	q, err := s.GetQuiz(r.Context(), uid(r), id(r))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, q)
	return nil
}

func (s *Service) hUpdateQuiz(w http.ResponseWriter, r *http.Request) error {
	var in QuizInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	q, err := s.UpdateQuiz(r.Context(), uid(r), id(r), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, q)
	return nil
}

func (s *Service) hDeleteQuiz(w http.ResponseWriter, r *http.Request) error {
	archived, err := s.DeleteQuiz(r.Context(), uid(r), id(r))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, map[string]bool{"archived": archived, "deleted": !archived})
	return nil
}

func (s *Service) hSetStatus(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Status         string `json:"status"`
		AcceptWarnings bool   `json:"accept_warnings"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	q, err := s.SetStatus(r.Context(), uid(r), id(r), in.Status, in.AcceptWarnings)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, q)
	return nil
}

func (s *Service) hReadiness(w http.ResponseWriter, r *http.Request) error {
	blocking, warnings, err := s.Readiness(r.Context(), uid(r), id(r))
	if err != nil {
		return err
	}
	if blocking == nil {
		blocking = []string{}
	}
	if warnings == nil {
		warnings = []string{}
	}
	httpx.JSON(w, 200, map[string]any{"blocking": blocking, "warnings": warnings})
	return nil
}

func (s *Service) hPreview(w http.ResponseWriter, r *http.Request) error {
	qs, err := s.Preview(r.Context(), uid(r), id(r))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, map[string]any{"questions": qs})
	return nil
}

func (s *Service) hListQuestions(w http.ResponseWriter, r *http.Request) error {
	qs, err := s.ListQuestions(r.Context(), uid(r), id(r))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, map[string]any{"questions": qs})
	return nil
}

// questionInput is the writable part of a question.
type questionInput struct {
	Code          string   `json:"code"`
	Type          Type     `json:"type"`
	Text          string   `json:"text"`
	Body          Body     `json:"body"`
	Key           Key      `json:"key"`
	Feedback      Feedback `json:"feedback"`
	Marks         float64  `json:"marks"`
	NegativeMarks float64  `json:"negative_marks"`
	PartialCredit *bool    `json:"partial_credit"`
	Settings      struct {
		QuestionTimeLimitSec *int    `json:"question_time_limit_sec"`
		EvaluationMethod     *string `json:"evaluation_method"`
		LLMKeyID             *string `json:"llm_key_id"`
		LLMModel             *string `json:"llm_model"`
		FeedbackMode         *string `json:"feedback_mode"`
		OptionOrder          *string `json:"option_order"`
	} `json:"settings"`
}

func (in questionInput) toQuestion() Question {
	q := Question{Code: in.Code, Type: in.Type, Text: in.Text, Body: in.Body, Key: in.Key, Feedback: in.Feedback,
		Marks: in.Marks, NegativeMarks: in.NegativeMarks}
	q.PartialCredit = DefaultPartialCredit(Type(strings.ToUpper(string(in.Type))))
	if in.PartialCredit != nil {
		q.PartialCredit = *in.PartialCredit
	}
	st := in.Settings
	q.Settings.QuestionTimeLimitSec, q.Settings.EvaluationMethod = st.QuestionTimeLimitSec, st.EvaluationMethod
	q.Settings.LLMKeyID, q.Settings.LLMModel = st.LLMKeyID, st.LLMModel
	q.Settings.FeedbackMode, q.Settings.OptionOrder = st.FeedbackMode, st.OptionOrder
	return q
}

func (s *Service) hCreateQuestion(w http.ResponseWriter, r *http.Request) error {
	var in questionInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	q, err := s.CreateQuestion(r.Context(), uid(r), id(r), in.toQuestion())
	if err != nil {
		return err
	}
	httpx.JSON(w, 201, q)
	return nil
}

func (s *Service) hGetQuestion(w http.ResponseWriter, r *http.Request) error {
	q, err := s.GetQuestion(r.Context(), uid(r), id(r))
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, q)
	return nil
}

func (s *Service) hUpdateQuestion(w http.ResponseWriter, r *http.Request) error {
	var in questionInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	q, err := s.UpdateQuestion(r.Context(), uid(r), id(r), in.toQuestion())
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, q)
	return nil
}

func (s *Service) hDeleteQuestion(w http.ResponseWriter, r *http.Request) error {
	if err := s.DeleteQuestion(r.Context(), uid(r), id(r)); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

func (s *Service) hDuplicate(w http.ResponseWriter, r *http.Request) error {
	q, err := s.DuplicateQuestion(r.Context(), uid(r), id(r))
	if err != nil {
		return err
	}
	httpx.JSON(w, 201, q)
	return nil
}

func (s *Service) hReorder(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := s.Reorder(r.Context(), uid(r), id(r), in.IDs); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

// readCSVBody accepts a raw text/csv body or a multipart "file" field.
func readCSVBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxCSVBytes+64<<10)
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	var src io.Reader = r.Body
	if mt == "multipart/form-data" {
		f, _, err := r.FormFile("file")
		if err != nil {
			return nil, httpx.BadRequest("upload the CSV in a form field named 'file'")
		}
		defer f.Close()
		src = f
	}
	data, err := io.ReadAll(io.LimitReader(src, MaxCSVBytes+1))
	if err != nil {
		return nil, httpx.BadRequest("could not read the upload (files are limited to 2 MB)")
	}
	return data, nil
}

func (s *Service) hImport(w http.ResponseWriter, r *http.Request) error {
	data, err := readCSVBody(w, r)
	if err != nil {
		return err
	}
	rep, err := s.ImportQuestions(r.Context(), uid(r), id(r), data)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, rep)
	return nil
}

func (s *Service) hImportResources(w http.ResponseWriter, r *http.Request) error {
	data, err := readCSVBody(w, r)
	if err != nil {
		return err
	}
	rep, err := s.ImportResources(r.Context(), uid(r), id(r), data)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, rep)
	return nil
}

func (s *Service) hCheck(w http.ResponseWriter, r *http.Request) error {
	if err := s.RequestCheck(r.Context(), uid(r), id(r)); err != nil {
		return err
	}
	w.WriteHeader(202)
	return nil
}

func (s *Service) hSetResource(w http.ResponseWriter, r *http.Request) error {
	var in ResourceInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	in.Role = Role(strings.ToUpper(string(in.Role)))
	q, err := s.SetResource(r.Context(), uid(r), id(r), in)
	if err != nil {
		return err
	}
	httpx.JSON(w, 200, q)
	return nil
}

func (s *Service) hDeleteResource(w http.ResponseWriter, r *http.Request) error {
	if err := s.DeleteResource(r.Context(), uid(r), id(r)); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}
