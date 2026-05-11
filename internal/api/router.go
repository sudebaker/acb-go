package api

import (
	"net/http"

	"github.com/amphora/acb/internal/db"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func NewRouter(taskRepo *db.TaskRepo, gateRepo *db.GateRepo, agentRepo *db.AgentRepo, redisPub interface{}) *chi.Mux {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(PanicRecovery)
	r.Use(RequestLogger)
	r.Use(JSONContentType)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, 200, map[string]string{"status": "ok"})
	})

	if taskRepo != nil && gateRepo != nil {
		h := &TaskHandler{taskRepo: taskRepo, gateRepo: gateRepo}
		r.Post("/tasks", h.CreateTask)
		r.Get("/tasks", h.ListTasks)
		r.Get("/tasks/{id}", h.GetTask)
		r.Post("/tasks/{id}/claim", h.ClaimTask)
		r.Post("/tasks/{id}/start", h.StartTask)
		r.Post("/tasks/{id}/block", h.BlockTask)
		r.Post("/tasks/{id}/unblock", h.UnblockTask)
		r.Post("/tasks/{id}/complete", h.CompleteTask)
		r.Post("/tasks/{id}/fail", h.FailTask)
	}

	return r
}
