package tutor

import (
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nadun96/quizplatform/tutoring/internal/authn"
	"github.com/nadun96/quizplatform/tutoring/internal/web"
)

// Handler is the whole service: /api, the sockets and /healthz.
func (s *Service) Handler(keys authn.Keys, platformOrigin string, pool *pgxpool.Pool) http.Handler {
	r := chi.NewRouter()
	r.Use(web.Recover, web.CORS(platformOrigin))
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			web.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db_unavailable"})
			return
		}
		web.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Route("/api", s.Routes(keys))
	// Sockets come from the platform's pages only.
	host := platformOrigin
	if u, err := url.Parse(platformOrigin); err == nil && u.Host != "" {
		host = u.Host
	}
	r.Get("/ws/sessions/{id}", s.ServeWS(keys, []string{host}))
	return r
}
