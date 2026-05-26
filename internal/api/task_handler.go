package api

import (
	"encoding/json"
	"errors"
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
	ctx := r.Context()
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
		MaxRetries          int      `json:"max_retries"`
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
			WriteError(w, 400, "invalid_required_skills", "one or more required skills are not in the allowed catalog")
			return
		}
	}

	// Validate skills against allowed list
	if len(input.Skills) > 0 {
		if invalid := h.cfg.ValidateSkills(input.Skills); len(invalid) > 0 {
			WriteError(w, 400, "invalid_skills", "one or more skills are not in the allowed catalog")
			return
		}
	}

	// Validate tags against allowed list
	if len(input.Tags) > 0 && h.cfg != nil {
		if invalid := h.cfg.ValidateTags(input.Tags); len(invalid) > 0 {
			WriteError(w, 400, "invalid_tags", "one or more tags are not in the allowed catalog")
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
		MaxRetries:          input.MaxRetries,
	}

	if err := h.taskRepo.Create(ctx, task); err != nil {
		WriteErrorSafe(w, 500, "create_failed", err)
		return
	}

	// Retrieve the task from DB to get server-generated timestamps (created_at, updated_at)
	createdTask, err := h.taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		WriteErrorSafe(w, 500, "get_task_failed", err)
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
	ctx := r.Context()
	status := r.URL.Query().Get("status")
	assignee := r.URL.Query().Get("assignee")
	requiredSkills := r.URL.Query()["required_skills"]

	tasks, err := h.taskRepo.List(ctx, status, assignee, requiredSkills...)
	if err != nil {
		WriteErrorSafe(w, 500, "list_failed", err)
		return
	}
	if tasks == nil {
		tasks = []models.Task{}
	}

	WriteJSON(w, 200, tasks)
}

// GetTask returns a single task by ID
func (h *TaskHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	task, err := h.taskRepo.GetByID(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "get_failed", err)
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}

	WriteJSON(w, 200, task)
}

func (h *TaskHandler) ClaimTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
	task, err := h.taskRepo.GetByID(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "get_failed", err)
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}
	oldStatus := task.Status

	// Validate parent tasks are completed
	if len(task.Parents) > 0 {
		parentsDone, err := h.taskRepo.CheckParentsCompleted(ctx, id)
		if err != nil {
			WriteErrorSafe(w, 500, "check_parents_failed", err)
			return
		}
		if !parentsDone {
			WriteError(w, 403, "parents_incomplete", "parent tasks must be completed before claiming")
			return
		}
	}

	// Validate agent has required skills
	if len(task.RequiredSkills) > 0 {
		agent, err := h.agentRepo.GetByName(ctx, input.Assignee)
		if err != nil {
			WriteErrorSafe(w, 500, "get_agent_failed", err)
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

	task, err = h.taskRepo.ClaimTask(ctx, id, input.Assignee)
	if err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "claim_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteErrorSafe(w, 409, "claim_failed", err)
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
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	// Get task before starting to capture old status
	task, err := h.taskRepo.GetByID(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "get_failed", err)
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}
	oldStatus := task.Status

	task, err = h.taskRepo.StartTask(ctx, id)
	if err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "start_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteErrorSafe(w, 409, "start_failed", err)
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
	ctx := r.Context()
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
	task, err := h.taskRepo.GetByID(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "get_task_failed", err)
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}
	oldStatus := task.Status

	if _, err := h.taskRepo.BlockTask(ctx, id); err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "block_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteErrorSafe(w, 409, "block_failed", err)
		}
		return
	}

	// Get updated task after blocking
	task, err = h.taskRepo.GetByID(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "get_task_failed", err)
		return
	}

	gate := &models.Gate{
		GateID:   input.GateID,
		TaskID:   id,
		Question: input.Question,
		Status:   "pending",
	}
	if err := h.gateRepo.CreateGate(ctx, gate); err != nil {
		WriteErrorSafe(w, 500, "gate_create_failed", err)
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
	ctx := r.Context()
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
	task, err := h.taskRepo.GetByID(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "get_task_failed", err)
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}
	oldStatus := task.Status

	if err := h.gateRepo.ResolveGate(ctx, input.GateID); err != nil {
		WriteErrorSafe(w, 409, "resolve_failed", err)
		return
	}

	if err := h.taskRepo.UpdateStatus(ctx, id, "in_progress"); err != nil {
		WriteErrorSafe(w, 500, "update_failed", err)
		return
	}

	// Get updated task after unblocking
	task, err = h.taskRepo.GetByID(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "get_task_failed", err)
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskUnblock, id, task.Assignee, input.GateID, "")

	// Notify agent of status change
	if h.dispatcher != nil {
		go h.dispatcher.NotifyStatusChange(task, oldStatus, "in_progress", "task_unblocked")
	}

	WriteJSON(w, 200, task)
}

// AnswerGate accepts an agent's answer to a gate and transitions it to "asked".
// The orchestrator then reviews the answer and decides whether to unblock.
// POST /tasks/{id}/gates/{gate_id}/answer
func (h *TaskHandler) AnswerGate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	gateID := chi.URLParam(r, "gate_id")

	var input struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, 400, "invalid_json", "invalid request body")
		return
	}
	if input.Answer == "" {
		WriteError(w, 400, "missing_answer", "answer is required")
		return
	}

	// Verify the gate exists and belongs to this task
	gate, err := h.gateRepo.GetGateByID(ctx, gateID)
	if err != nil {
		WriteErrorSafe(w, 500, "get_gate_failed", err)
		return
	}
	if gate == nil {
		WriteError(w, 404, "gate_not_found", "gate not found")
		return
	}
	if gate.TaskID != id {
		WriteError(w, 400, "gate_mismatch", "gate does not belong to this task")
		return
	}
	if gate.Status != "pending" {
		WriteError(w, 409, "invalid_gate_status", "gate is not in pending status")
		return
	}

	if err := h.gateRepo.AskGate(ctx, gateID, input.Answer); err != nil {
		WriteErrorSafe(w, 409, "ask_gate_failed", err)
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskGateAnswered, id, "", gateID, input.Answer)

	WriteJSON(w, 200, map[string]string{
		"gate_id": gateID,
		"status":  "asked",
	})
}

func (h *TaskHandler) CompleteTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var input struct {
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, 400, "invalid_json", "invalid request body")
		return
	}

	// Get task before completing to capture old status
	task, err := h.taskRepo.GetByID(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "get_task_failed", err)
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}
	oldStatus := task.Status

	task, err = h.taskRepo.CompleteTask(ctx, id, input.Summary)
	if err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "complete_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteErrorSafe(w, 409, "complete_failed", err)
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
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var input struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, 400, "invalid_json", "invalid request body")
		return
	}

	// Get task before failing to capture old status
	task, err := h.taskRepo.GetByID(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "get_task_failed", err)
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}
	oldStatus := task.Status

	result, err := h.taskRepo.FailTask(ctx, id, input.Reason)
	if err != nil {
		var ce *db.ConflictError
		if errors.As(err, &ce) {
			WriteConflict(w, "fail_failed", ce.Message, ce.CurrentStatus)
		} else {
			WriteErrorSafe(w, 409, "fail_failed", err)
		}
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskFailed, id, result.Task.Assignee, "", input.Reason, result.Task.RequiredSkills...)

	if result.DidRetry {
		// Auto-retry: notify as retry, then dispatch to matching agents
		if h.dispatcher != nil {
			go h.dispatcher.NotifyStatusChange(result.Task, oldStatus, "pending", "task_retried")
			// Dispatch to new matching agents since assignee was cleared
			go h.dispatcher.DispatchNewTask(result.Task)
		}
		go h.pub.PublishTaskEvent(acbredis.EventNewTask, id, "", "", "", result.Task.RequiredSkills...)
	} else {
		// Notify agent of status change
		if h.dispatcher != nil {
			go h.dispatcher.NotifyStatusChange(result.Task, oldStatus, "failed", "task_failed")
		}
	}

	WriteJSON(w, 200, result.Task)
}

// TaskHeartbeat updates the liveness timestamp for an in-progress task.
// POST /tasks/{id}/heartbeat
func (h *TaskHandler) TaskHeartbeat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	task, err := h.taskRepo.GetByID(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "get_failed", err)
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}
	if task.Status != "in_progress" && task.Status != "claimed" {
		WriteError(w, 409, "invalid_status", "heartbeat only allowed for claimed or in_progress tasks")
		return
	}

	if err := h.taskRepo.UpdateTaskHeartbeat(ctx, id); err != nil {
		WriteErrorSafe(w, 500, "heartbeat_failed", err)
		return
	}

	WriteJSON(w, 200, map[string]string{"status": "ok"})
}

// TaskGraph returns a task with its parent and child dependency tree.
// GET /tasks/{id}/graph
func (h *TaskHandler) TaskGraph(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	graph, err := h.taskRepo.GetDependencyGraph(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "graph_failed", err)
		return
	}
	if graph == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}

	WriteJSON(w, 200, graph)
}

// ListTaskEvents returns the event trail for a task.
// GET /tasks/{id}/events
func (h *TaskHandler) ListTaskEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	events, err := h.taskRepo.ListTaskEvents(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "list_events_failed", err)
		return
	}
	if events == nil {
		events = []models.TaskEvent{}
	}

	WriteJSON(w, 200, events)
}

// ListTaskGates returns all gates attached to a task.
// GET /tasks/{id}/gates
func (h *TaskHandler) ListTaskGates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	task, err := h.taskRepo.GetByID(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "get_task_failed", err)
		return
	}
	if task == nil {
		WriteError(w, 404, "not_found", "task not found")
		return
	}

	gates, err := h.gateRepo.GetByTaskID(ctx, id)
	if err != nil {
		WriteErrorSafe(w, 500, "list_gates_failed", err)
		return
	}
	if gates == nil {
		gates = []models.Gate{}
	}

	WriteJSON(w, 200, gates)
}

// DispatchNext returns the best-matching pending task for the requesting agent.
// GET /tasks/dispatch?agent=<name>
func (h *TaskHandler) DispatchNext(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentName := r.URL.Query().Get("agent")
	if agentName == "" {
		WriteError(w, 400, "missing_agent", "agent query parameter is required")
		return
	}

	task, err := dispatcher.FindNextForAgent(ctx, h.agentRepo, h.taskRepo, agentName)
	if err != nil {
		WriteErrorSafe(w, 500, "dispatch_failed", err)
		return
	}
	if task == nil {
		w.WriteHeader(204)
		return
	}

	WriteJSON(w, 200, task)
}

// ApproveGate transitions a gate from "asked" to "answered" (orchestrator approves the agent's answer).
// POST /tasks/{id}/gates/{gate_id}/approve
func (h *TaskHandler) ApproveGate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	gateID := chi.URLParam(r, "gate_id")

	var input struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, 400, "invalid_json", "invalid request body")
		return
	}
	if input.Answer == "" {
		WriteError(w, 400, "missing_answer", "answer is required")
		return
	}

	gate, err := h.gateRepo.GetGateByID(ctx, gateID)
	if err != nil {
		WriteErrorSafe(w, 500, "get_gate_failed", err)
		return
	}
	if gate == nil {
		WriteError(w, 404, "gate_not_found", "gate not found")
		return
	}
	if gate.TaskID != id {
		WriteError(w, 400, "gate_mismatch", "gate does not belong to this task")
		return
	}
	if gate.Status != "asked" {
		WriteError(w, 409, "invalid_gate_status", "gate is not in asked status, current: "+gate.Status)
		return
	}

	if err := h.gateRepo.AnswerGate(ctx, gateID, input.Answer); err != nil {
		WriteErrorSafe(w, 409, "approve_failed", err)
		return
	}

	go h.pub.PublishTaskEvent(acbredis.EventTaskGateApproved, id, "", gateID, input.Answer)

	WriteJSON(w, 200, map[string]string{
		"gate_id": gateID,
		"status":  "answered",
	})
}
