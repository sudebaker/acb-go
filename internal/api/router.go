package api

import (
	"net/http"
	"time"

	"github.com/sudebaker/acb-go/internal/db"
	acbredis "github.com/sudebaker/acb-go/internal/redis"
	"github.com/sudebaker/acb-go/internal/rustfs"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"
)

func NewRouter(taskRepo *db.TaskRepo, gateRepo *db.GateRepo, agentRepo *db.AgentRepo, pub *acbredis.Publisher, rustfsClient *rustfs.Client) *chi.Mux {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(PanicRecovery)
	r.Use(RequestLogger)
	r.Use(JSONContentType)

	if agentRepo != nil {
		r.Use(AuthMiddleware(agentRepo))
	}

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, 200, map[string]string{"status": "ok"})
	})

	if taskRepo != nil && gateRepo != nil {
		h := &TaskHandler{taskRepo: taskRepo, gateRepo: gateRepo, agentRepo: agentRepo, pub: pub}
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

	if rustfsClient != nil && taskRepo != nil {
		ah := &ArtifactHandler{taskRepo: taskRepo, rustfs: rustfsClient}
		r.Post("/tasks/{id}/artifacts", ah.UploadArtifact)
		r.Get("/tasks/{id}/artifacts", ah.DispatchListOrDownload)
		r.Delete("/tasks/{id}/artifacts", ah.DeleteArtifact)
	}

	if agentRepo != nil {
		limiter := NewRateLimiter(rate.Every(6*time.Second), 1)
		ah := &AgentHandler{agentRepo: agentRepo, limiter: limiter}
		r.Post("/agents/heartbeat", ah.Heartbeat)
		r.Get("/agents/{name}", ah.GetAgent)
	}

	return r
}
