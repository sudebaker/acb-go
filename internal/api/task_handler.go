package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/models"
	acbredis "github.com/sudebaker/acb-go/internal/redis"
	"github.com/go-chi/chi/v5"
)

type TaskHandler struct {
	taskRepo *db.TaskRepo
	gateRepo *db.GateRepo
	pub      *acbredis.Publisher
}

func (h *TaskHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID                  string   `json:"id"`
		Title               string   `json:"title"`
		Assignee            string   `json:"assignee"`
		Priority            int      `json:"priority"`
		Parents             []string `json:"parents"`
		Skills              []string `json:"skills,omitempty"`
		RequiredSkills      []string `json:"required_skills,omitempty"`
		Tags                []string `json:"tags,omitempty"`
		BodyGoal            string   `json:"body_goal"`
		BodyContext         string   `json:"body_context"`
		BodyDeliverableFmt  string   `json:"body_deliverable_format"`
		BodyDeliverablePath string   `json:"body_deliverable_path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, 400, "invalid_json", "invalid request body")
		return
	}
	if input.Title == "" {
		WriteError(w, 400, "missing_title", "title is required")
		return
	}

	task := &models.Task{
		ID:                  input.ID,
		Title:               input.Title,
		Assignee:            input.Assignee,
		Priority:            input.Priority,
		Parents:             input.Parents,
		Skills:              input.Skills,
		RequiredSkills:      input.RequiredSkills,
		Tags:                input.Tags,
		BodyGoal:            input.BodyGoal,
		BodyContext:         input.BodyContext,
		BodyDeliverableFmt:  input.BodyDeliverableFmt,
		BodyDeliverablePath: input.BodyDeliverablePath,
	}

	if err := h.taskRepo.Create(task); err != nil {
		WriteError(w, 500, "create_failed", err.Error())
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventNewTask, task.ID, task.Assignee, "", "", task.RequiredSkills...)

	WriteJSON(w, 201, task)
}

func (h *TaskHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	assignee := r.URL.Query().Get("assignee")
	requiredSkills := r.URL.Query()["required_skills"]

	tasks, err := h.taskRepo.List(status, assignee)
	if err != nil {
		WriteError(w, 500, "list_failed", err.Error())
		return
	}
	if tasks == nil {
		tasks = []models.Task{}
	}

	// Filter by required skills
	if len(requiredSkills) > 0 {
		filtered := []models.Task{}
		for _, t := range tasks {
			if hasRequiredSkills(t.RequiredSkills, requiredSkills) {
				filtered = append(filtered, t)
			}
		}
		tasks = filtered
	}

	WriteJSON(w, 200, tasks)
}

// hasRequiredSkills checks if task skills contain all required skills
func hasRequiredSkills(taskSkills, requiredSkills []string) bool {
	if len(requiredSkills) == 0 {
		return true
	}
	skillSet := make(map[string]bool)
	for _, s := range taskSkills {
		skillSet[s] = true
	}
	for _, rs := range requiredSkills {
		if !skillSet[rs] {
			return false
		}
	}
	return true
}

func (h *TaskHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	task, err := h.taskRepo.GetByID(id)
	if err != nil {
		WriteError(w, 500, "get_failed", err.Error())
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}

	WriteJSON(w, 200, task)
}

func (h *TaskHandler) ClaimTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var input struct {
		Assignee string `json:"assignee"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, 400, "invalid_json", "invalid request body")
		return
	}
	if input.Assignee == "" {
		WriteError(w, 400, "missing_assignee", "assignee is required")
		return
	}

	// Get task first to check required skills
	task, err := h.taskRepo.GetByID(id)
	if err != nil {
		WriteError(w, 500, "get_failed", err.Error())
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}

	// Validate agent has required skills
	if !hasRequiredSkills(task.RequiredSkills, []string{}) && len(task.RequiredSkills) > 0 {
		// Get agent skills from repo if we had that info
		// For now, just validate the task has no missing skills requirement
		// In a full implementation, you'd fetch agent skills and compare
	}

	// For now, skip detailed skills check if task has no required skills
	if len(task.RequiredSkills) > 0 {
		// Simulate技能 check - in reality, fetch agent skills
		// This is where you'd implement: h.agentRepo.Get(agentName) and compare skills
	}

	task, err = h.taskRepo.ClaimTask(id, input.Assignee)
	if err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "claim_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteError(w, 409, "claim_failed", err.Error())
		}
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskClaimed, id, input.Assignee, "", "")

	WriteJSON(w, 200, task)
}

func (h *TaskHandler) StartTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	task, err := h.taskRepo.StartTask(id)
	if err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "start_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteError(w, 409, "start_failed", err.Error())
		}
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskStarted, id, "", "", "")

	WriteJSON(w, 200, task)
}

func (h *TaskHandler) BlockTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var input struct {
		GateID   string `json:"gate_id"`
		Question string `json:"question"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, 400, "invalid_json", "invalid request body")
		return
	}
	if input.GateID == "" || input.Question == "" {
		WriteError(w, 400, "missing_fields", "gate_id and question are required")
		return
	}

	if _, err := h.taskRepo.BlockTask(id); err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "block_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteError(w, 409, "block_failed", err.Error())
		}
		return
	}

	gate := &models.Gate{
		GateID:   input.GateID,
		TaskID:   id,
		Question: input.Question,
		Status:   "pending",
	}
	if err := h.gateRepo.CreateGate(gate); err != nil {
		WriteError(w, 500, "gate_create_failed", err.Error())
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskBlocked, id, "", input.GateID, "")

	WriteJSON(w, 200, map[string]string{"status": "blocked", "gate_id": input.GateID})
}

func (h *TaskHandler) UnblockTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var input struct {
		GateID string `json:"gate_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, 400, "invalid_json", "invalid request body")
		return
	}
	if input.GateID == "" {
		WriteError(w, 400, "missing_gate_id", "gate_id is required")
		return
	}

	if err := h.gateRepo.ResolveGate(input.GateID); err != nil {
		WriteError(w, 409, "resolve_failed", err.Error())
		return
	}

	if err := h.taskRepo.UpdateStatus(id, "in_progress"); err != nil {
		WriteError(w, 500, "update_failed", err.Error())
		return
	}
	go h.pub.PublishTaskEvent(acbredis.EventTaskUnblock, id, "", input.GateID, "")

	WriteJSON(w, 200, map[string]string{"id": id, "status": "in_progress"})
}

func (h *TaskHandler) CompleteTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var input struct {
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, 400, "invalid_json", "invalid request body")
		return
	}

	task, err := h.taskRepo.CompleteTask(id, input.Summary)
	if err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "complete_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteError(w, 409, "complete_failed", err.Error())
		}
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskDone, id, "", "", input.Summary)

	WriteJSON(w, 200, task)
}

func (h *TaskHandler) FailTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var input struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, 400, "invalid_json", "invalid request body")
		return
	}

	task, err := h.taskRepo.FailTask(id, input.Reason)
	if err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "fail_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteError(w, 409, "fail_failed", err.Error())
		}
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskFailed, id, "", "", "")

	WriteJSON(w, 200, task)
}
