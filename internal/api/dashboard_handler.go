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
	counts, err := h.taskRepo.GetTaskCounts()
	if err != nil {
		WriteError(w, 500, "counts_failed", err.Error())
		return
	}

	tasks, err := h.taskRepo.List("", "")
	if err != nil {
		WriteError(w, 500, "list_failed", err.Error())
		return
	}
	if tasks == nil {
		tasks = []models.Task{}
	}

	staleCount := 0
	if h.staleAgentMin > 0 && h.agentRepo != nil {
		staleDur := time.Duration(h.staleAgentMin) * time.Minute
		staleAgents, err := h.agentRepo.ListStale(staleDur)
		if err == nil {
			staleCount = len(staleAgents)
		}
	}

	WriteJSON(w, 200, map[string]interface{}{
		"counts":          counts,
		"tasks_by_status": groupTasksByStatus(tasks),
		"stale_agents":    staleCount,
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
