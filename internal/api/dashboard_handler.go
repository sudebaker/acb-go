package api

import (
	"net/http"
	"time"

	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/models"
)

type DashboardHandler struct {
	taskRepo      *db.TaskRepo
	agentRepo     *db.AgentRepo
	staleAgentMin int
}

func NewDashboardHandler(taskRepo *db.TaskRepo, agentRepo *db.AgentRepo, staleAgentMin int) *DashboardHandler {
	return &DashboardHandler{taskRepo: taskRepo, agentRepo: agentRepo, staleAgentMin: staleAgentMin}
}

// Dashboard returns task counts grouped by status.
// GET /dashboard
func (h *DashboardHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts, err := h.taskRepo.GetTaskCounts(ctx)
	if err != nil {
		WriteErrorSafe(w, 500, "counts_failed", err)
		return
	}

	tasks, err := h.taskRepo.List(ctx, "", "")
	if err != nil {
		WriteErrorSafe(w, 500, "list_failed", err)
		return
	}
	if tasks == nil {
		tasks = []models.Task{}
	}

	staleCount := 0
	if h.staleAgentMin > 0 && h.agentRepo != nil {
		staleDur := time.Duration(h.staleAgentMin) * time.Minute
		staleAgents, err := h.agentRepo.ListStale(ctx, staleDur)
		if err == nil {
			staleCount = len(staleAgents)
		}
	}

	WriteJSON(w, 200, map[string]interface{}{
		"counts":          counts,
		"tasks_by_status": groupTasksByStatus(tasks),
		"stale_agents":    staleCount,
		"total":           counts.Total,
	})
}

func groupTasksByStatus(tasks []models.Task) map[string][]models.Task {
	grouped := map[string][]models.Task{
		"pending":     {},
		"claimed":     {},
		"in_progress": {},
		"blocked":     {},
		"completed":   {},
		"failed":      {},
	}
	for _, t := range tasks {
		s := t.Status
		if s == "" {
			s = "pending"
		}
		grouped[s] = append(grouped[s], t)
	}
	return grouped
}
