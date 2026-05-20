package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/sudebaker/acb-go/internal/config"
	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/dispatcher"
	"github.com/sudebaker/acb-go/internal/models"
	acbredis "github.com/sudebaker/acb-go/internal/redis"
	"github.com/go-chi/chi/v5"
)

type TaskHandler struct {
	taskRepo   *db.TaskRepo
	gateRepo   *db.GateRepo
	agentRepo  *db.AgentRepo
	pub        *acbredis.Publisher
	dispatcher *dispatcher.Dispatcher
	cfg        *config.Config
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
	if input.ID == "" {
		input.ID = uuid.New().String()
	}

	// Validate required_skills against allowed list
	if len(input.RequiredSkills) > 0 {
		if invalid := h.cfg.ValidateSkills(input.RequiredSkills); len(invalid) > 0 {
			WriteError(w, 400, "invalid_required_skills", fmt.Sprintf("these skills are not allowed: %v", invalid))
			return
		}
	}

	// Validate skills against allowed list
	if len(input.Skills) > 0 {
		if invalid := h.cfg.ValidateSkills(input.Skills); len(invalid) > 0 {
			WriteError(w, 400, "invalid_skills", fmt.Sprintf("these skills are not allowed: %v", invalid))
			return
		}
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

	// Retrieve the task from DB to get server-generated timestamps (created_at, updated_at)
	createdTask, err := h.taskRepo.GetByID(task.ID)
	if err != nil {
		WriteError(w, 500, "get_task_failed", err.Error())
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventNewTask, task.ID, task.Assignee, "", "", task.RequiredSkills...)

	// Dispatch webhook to matching agents
	if h.dispatcher != nil {
		go h.dispatcher.DispatchNewTask(createdTask)
	}

	WriteJSON(w, 201, createdTask)
}

func (h *TaskHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	assignee := r.URL.Query().Get("assignee")
	requiredSkills := r.URL.Query()["required_skills"]

	tasks, err := h.taskRepo.List(status, assignee, requiredSkills...)
	if err != nil {
		WriteError(w, 500, "list_failed", err.Error())
		return
	}
	if tasks == nil {
		tasks = []models.Task{}
	}

	WriteJSON(w, 200, tasks)
}

// GetTask returns a single task by ID
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

	// Get task first to check required skills and old status
	task, err := h.taskRepo.GetByID(id)
	if err != nil {
		WriteError(w, 500, "get_failed", err.Error())
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}
	oldStatus := task.Status

	// Validate agent has required skills
	if len(task.RequiredSkills) > 0 {
		agent, err := h.agentRepo.GetByName(input.Assignee)
		if err != nil {
			WriteError(w, 500, "get_agent_failed", err.Error())
			return
		}
		if agent == nil {
			WriteError(w, 404, "agent_not_found", "agent not found")
			return
		}

		// Check if agent has all required skills
		hasSkills := true
		for _, req := range task.RequiredSkills {
			found := false
			for _, skill := range agent.Skills {
				if skill == req {
					found = true
					break
				}
			}
			if !found {
				hasSkills = false
				break
			}
		}
		if !hasSkills {
			WriteError(w, 403, "missing_skills", "agent lacks required skills")
			return
		}
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

	go h.pub.PublishTaskEvent(acbredis.EventTaskClaimed, id, input.Assignee, "", "", task.RequiredSkills...)

	// Notify agent of status change
	if h.dispatcher != nil {
		go h.dispatcher.NotifyStatusChange(task, oldStatus, "claimed", "task_claimed")
	}

	WriteJSON(w, 200, task)
}

func (h *TaskHandler) StartTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Get task before starting to capture old status
	task, err := h.taskRepo.GetByID(id)
	if err != nil {
		WriteError(w, 500, "get_failed", err.Error())
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}
	oldStatus := task.Status

	task, err = h.taskRepo.StartTask(id)
	if err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "start_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteError(w, 409, "start_failed", err.Error())
		}
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskStarted, id, task.Assignee, "", "", task.RequiredSkills...)

	// Notify agent of status change
	if h.dispatcher != nil {
		go h.dispatcher.NotifyStatusChange(task, oldStatus, "in_progress", "task_started")
	}

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

	// Get task before blocking to include in webhook and capture old status
	task, err := h.taskRepo.GetByID(id)
	if err != nil {
		WriteError(w, 500, "get_task_failed", err.Error())
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}
	oldStatus := task.Status

	if _, err := h.taskRepo.BlockTask(id); err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "block_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteError(w, 409, "block_failed", err.Error())
		}
		return
	}

	// Get updated task after blocking
	task, err = h.taskRepo.GetByID(id)
	if err != nil {
		WriteError(w, 500, "get_task_failed", err.Error())
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

	go h.pub.PublishTaskEvent(acbredis.EventTaskBlocked, id, task.Assignee, input.GateID, "", task.RequiredSkills...)

	// Notify agent of status change
	if h.dispatcher != nil {
		go h.dispatcher.NotifyStatusChange(task, oldStatus, "blocked", "task_blocked")
	}

	WriteJSON(w, 200, task)
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

	// Get task before unblocking to capture old status
	task, err := h.taskRepo.GetByID(id)
	if err != nil {
		WriteError(w, 500, "get_task_failed", err.Error())
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}
	oldStatus := task.Status

	if err := h.gateRepo.ResolveGate(input.GateID); err != nil {
		WriteError(w, 409, "resolve_failed", err.Error())
		return
	}

	if err := h.taskRepo.UpdateStatus(id, "in_progress"); err != nil {
		WriteError(w, 500, "update_failed", err.Error())
		return
	}

	// Get updated task after unblocking
	task, err = h.taskRepo.GetByID(id)
	if err != nil {
		WriteError(w, 500, "get_task_failed", err.Error())
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskUnblock, id, task.Assignee, input.GateID, "")

	// Notify agent of status change
	if h.dispatcher != nil {
		go h.dispatcher.NotifyStatusChange(task, oldStatus, "in_progress", "task_unblocked")
	}

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

	// Get task before completing to capture old status
	task, err := h.taskRepo.GetByID(id)
	if err != nil {
		WriteError(w, 500, "get_task_failed", err.Error())
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}
	oldStatus := task.Status

	task, err = h.taskRepo.CompleteTask(id, input.Summary)
	if err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "complete_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteError(w, 409, "complete_failed", err.Error())
		}
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskDone, id, task.Assignee, "", input.Summary, task.RequiredSkills...)

	// Notify agent of status change
	if h.dispatcher != nil {
		go h.dispatcher.NotifyStatusChange(task, oldStatus, "completed", "task_completed")
	}

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

	// Get task before failing to capture old status
	task, err := h.taskRepo.GetByID(id)
	if err != nil {
		WriteError(w, 500, "get_task_failed", err.Error())
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}
	oldStatus := task.Status

	task, err = h.taskRepo.FailTask(id, input.Reason)
	if err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "fail_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteError(w, 409, "fail_failed", err.Error())
		}
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskFailed, id, task.Assignee, "", input.Reason, task.RequiredSkills...)

	// Notify agent of status change
	if h.dispatcher != nil {
		go h.dispatcher.NotifyStatusChange(task, oldStatus, "failed", "task_failed")
	}

	WriteJSON(w, 200, task)
}

// DispatchNext returns the best-matching pending task for the requesting agent.
// GET /tasks/dispatch?agent=<name>
func (h *TaskHandler) DispatchNext(w http.ResponseWriter, r *http.Request) {
	agentName := r.URL.Query().Get("agent")
	if agentName == "" {
		WriteError(w, 400, "missing_agent", "agent query parameter is required")
		return
	}

	task, err := dispatcher.FindNextForAgent(h.agentRepo, h.taskRepo, agentName)
	if err != nil {
		WriteError(w, 500, "dispatch_failed", err.Error())
		return
	}
	if task == nil {
		w.WriteHeader(204)
		return
	}

	WriteJSON(w, 200, task)
}
