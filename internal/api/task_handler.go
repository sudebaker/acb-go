package api

import (
	"encoding/json"
	"net/http"

	"github.com/amphora/acb/internal/db"
	"github.com/amphora/acb/internal/models"
	acbredis "github.com/amphora/acb/internal/redis"
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
		BodyGoal:            input.BodyGoal,
		BodyContext:         input.BodyContext,
		BodyDeliverableFmt:  input.BodyDeliverableFmt,
		BodyDeliverablePath: input.BodyDeliverablePath,
	}

	if err := h.taskRepo.Create(task); err != nil {
		WriteError(w, 500, "create_failed", err.Error())
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventNewTask, task.ID, task.Assignee)

	WriteJSON(w, 201, task)
}

func (h *TaskHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	assignee := r.URL.Query().Get("assignee")

	tasks, err := h.taskRepo.List(status, assignee)
	if err != nil {
		WriteError(w, 500, "list_failed", err.Error())
		return
	}
	if tasks == nil {
		tasks = []models.Task{}
	}

	WriteJSON(w, 200, tasks)
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

	if err := h.taskRepo.ClaimTask(id, input.Assignee); err != nil {
		WriteError(w, 409, "claim_failed", err.Error())
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskClaimed, id, input.Assignee)

	task, _ := h.taskRepo.GetByID(id)
	WriteJSON(w, 200, task)
}

func (h *TaskHandler) StartTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.taskRepo.StartTask(id); err != nil {
		WriteError(w, 409, "start_failed", err.Error())
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskStarted, id, "")

	task, _ := h.taskRepo.GetByID(id)
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

	if err := h.taskRepo.BlockTask(id); err != nil {
		WriteError(w, 409, "block_failed", err.Error())
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

	go h.pub.PublishTaskEvent(acbredis.EventTaskBlocked, id, "")

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
	go h.pub.PublishTaskEvent(acbredis.EventTaskUnblock, id, "")

	task, _ := h.taskRepo.GetByID(id)
	WriteJSON(w, 200, task)
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

	if err := h.taskRepo.CompleteTask(id, input.Summary); err != nil {
		WriteError(w, 409, "complete_failed", err.Error())
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskDone, id, "")

	task, _ := h.taskRepo.GetByID(id)
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

	if err := h.taskRepo.FailTask(id, input.Reason); err != nil {
		WriteError(w, 409, "fail_failed", err.Error())
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskFailed, id, "")

	task, _ := h.taskRepo.GetByID(id)
	WriteJSON(w, 200, task)
}
