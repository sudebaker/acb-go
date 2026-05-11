package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func NewRouter(taskRepo interface{}, gateRepo interface{}, agentRepo interface{}, redisPub interface{}) *chi.Mux {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(PanicRecovery)
	r.Use(RequestLogger)
	r.Use(JSONContentType)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, 200, map[string]string{"status": "ok"})
	})

	return r
}
