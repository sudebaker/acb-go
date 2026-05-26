package api

import (
	"database/sql"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/sudebaker/acb-go/internal/config"
	"github.com/sudebaker/acb-go/internal/db"
	acbredis "github.com/sudebaker/acb-go/internal/redis"
	"github.com/sudebaker/acb-go/internal/dispatcher"
	"github.com/sudebaker/acb-go/internal/rustfs"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"
)

func NewRouter(taskRepo *db.TaskRepo, gateRepo *db.GateRepo, agentRepo *db.AgentRepo, pub *acbredis.Publisher, rustfsClient *rustfs.Client, disp *dispatcher.Dispatcher, cfg *config.Config, database *sql.DB, rdb *goredis.Client) *chi.Mux {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(PanicRecovery)
	r.Use(RequestLogger)
	r.Use(JSONContentType)

	if agentRepo != nil {
		r.Use(AuthMiddleware(agentRepo))
	}

	hh := NewHealthHandler(database, rdb, rustfsClient)
	r.Get("/health", hh.Check)

	if taskRepo != nil && gateRepo != nil {
		h := &TaskHandler{taskRepo: taskRepo, gateRepo: gateRepo, agentRepo: agentRepo, pub: pub, dispatcher: disp, cfg: cfg}
		r.Post("/tasks", h.CreateTask)
		r.Get("/tasks", h.ListTasks)
		r.Get("/tasks/dispatch", h.DispatchNext)
		r.Get("/tasks/{id}", h.GetTask)
		r.Post("/tasks/{id}/claim", h.ClaimTask)
		r.Post("/tasks/{id}/start", h.StartTask)
		r.Post("/tasks/{id}/block", h.BlockTask)
		r.Post("/tasks/{id}/unblock", h.UnblockTask)
		r.Post("/tasks/{id}/complete", h.CompleteTask)
		r.Post("/tasks/{id}/fail", h.FailTask)
		r.Post("/tasks/{id}/heartbeat", h.TaskHeartbeat)
		r.Get("/tasks/{id}/events", h.ListTaskEvents)
		r.Get("/tasks/{id}/graph", h.TaskGraph)
		r.Post("/tasks/{id}/gates/{gate_id}/answer", h.AnswerGate)
	}

	if taskRepo != nil && agentRepo != nil {
		staleMin := 10 // default
		if cfg != nil {
			staleMin = cfg.AgentStaleMin
		}
		dh := NewDashboardHandler(taskRepo, agentRepo, staleMin)
		r.Get("/dashboard", dh.Dashboard)
	}

	if rustfsClient != nil && taskRepo != nil {
		ah := &ArtifactHandler{taskRepo: taskRepo, rustfs: rustfsClient, cfg: cfg}
		r.Post("/tasks/{id}/artifacts", ah.UploadArtifact)
		r.Get("/tasks/{id}/artifacts", ah.DispatchListOrDownload)
		r.Delete("/tasks/{id}/artifacts", ah.DeleteArtifact)
		r.Post("/tasks/{id}/artifacts/cleanup", ah.CleanupArtifacts)
	}

	if agentRepo != nil {
		limiter := NewRateLimiter(rate.Every(6*time.Second), 1)
		ah := &AgentHandler{agentRepo: agentRepo, limiter: limiter, cfg: cfg}
		r.Post("/agents", ah.RegisterAgent)
		r.Post("/agents/heartbeat", ah.Heartbeat)
		r.Get("/agents/{name}", ah.GetAgent)
	}

	return r
}