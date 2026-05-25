package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/sudebaker/acb-go/internal/models"
)

// PostgreSQL timestamp formats (used in parseTime helpers)
const (
	pgTimestampMicro = "2006-01-02 15:04:05.999999+00"
	pgTimestampSec   = "2006-01-02 15:04:05+00"
	pgTimestampNoTZ  = "2006-01-02 15:04:05"
)

// taskColumns is the column list used in task queries.
const taskColumns = `id, title, assignee, status, priority, parents, skills, required_skills, tags,
	body_goal, body_context, body_deliverable_format, body_deliverable_path,
	created_at, updated_at, summary, artifacts_json,
	last_heartbeat, max_retries, retry_count`

// scanTaskRow scans a row into a Task and unmarshals JSON fields.
func scanTaskRow(scanner interface {
	Scan(dest ...interface{}) error
}) (*models.Task, error) {
	task := &models.Task{}
	var parents, skills, requiredSkills, tags, artifacts string
	var createdAt, updatedAt string
	var lastHeartbeat sql.NullTime
	err := scanner.Scan(
		&task.ID, &task.Title, &task.Assignee, &task.Status, &task.Priority,
		&parents, &skills, &requiredSkills, &tags,
		&task.BodyGoal, &task.BodyContext,
		&task.BodyDeliverableFmt, &task.BodyDeliverablePath,
		&createdAt, &updatedAt, &task.Summary, &artifacts,
		&lastHeartbeat, &task.MaxRetries, &task.RetryCount,
	)
	if err != nil {
		return nil, err
	}

	task.CreatedAt = parseTime(createdAt)
	task.UpdatedAt = parseTime(updatedAt)
	if err := json.Unmarshal([]byte(parents), &task.Parents); err != nil {
		log.Printf("[WARN] scanTaskRow: failed to unmarshal parents: %v", err)
	}
	if err := json.Unmarshal([]byte(skills), &task.Skills); err != nil {
		log.Printf("[WARN] scanTaskRow: failed to unmarshal skills: %v", err)
	}
	if err := json.Unmarshal([]byte(requiredSkills), &task.RequiredSkills); err != nil {
		log.Printf("[WARN] scanTaskRow: failed to unmarshal required_skills: %v", err)
	}
	if err := json.Unmarshal([]byte(tags), &task.Tags); err != nil {
		log.Printf("[WARN] scanTaskRow: failed to unmarshal tags: %v", err)
	}
	if err := json.Unmarshal([]byte(artifacts), &task.Artifacts); err != nil {
		log.Printf("[WARN] scanTaskRow: failed to unmarshal artifacts: %v", err)
	}
	if lastHeartbeat.Valid {
		task.LastHeartbeat = &lastHeartbeat.Time
	}
	if task.Status == "" {
		task.Status = "pending"
	}
	return task, nil
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, fmt := range []string{pgTimestampMicro, pgTimestampSec, pgTimestampNoTZ, time.RFC3339} {
		if parsed, err := time.Parse(fmt, s); err == nil {
			return parsed
		}
	}
	log.Printf("[WARN] parseTime: failed to parse %q", s)
	return time.Time{}
}

type ConflictError struct {
	CurrentStatus string
	Message       string
}

func (e *ConflictError) Error() string {
	return e.Message
}

type TaskRepo struct {
	db        *sql.DB
	eventRepo *TaskEventRepo
}

func NewTaskRepo(db *sql.DB) *TaskRepo {
	eventRepo := NewTaskEventRepo(db)
	return &TaskRepo{db: db, eventRepo: eventRepo}
}

func (r *TaskRepo) WithEventRepo(eventRepo *TaskEventRepo) {
	r.eventRepo = eventRepo
}

func (r *TaskRepo) Create(task *models.Task) error {
	if task.Status == "" {
		task.Status = "pending"
	}
	parents, err := json.Marshal(task.Parents)
	if err != nil {
		return fmt.Errorf("marshal parents: %w", err)
	}
	skills, err := json.Marshal(task.Skills)
	if err != nil {
		return fmt.Errorf("marshal skills: %w", err)
	}
	requiredSkills, err := json.Marshal(task.RequiredSkills)
	if err != nil {
		return fmt.Errorf("marshal required_skills: %w", err)
	}
	tags, err := json.Marshal(task.Tags)
	if err != nil {
		return fmt.Errorf("marshal tags: %w", err)
	}
	artifacts, err := json.Marshal(task.Artifacts)
	if err != nil {
		return fmt.Errorf("marshal artifacts: %w", err)
	}

	_, err = r.db.Exec(
		`INSERT INTO tasks (id, title, assignee, status, priority, parents, skills, required_skills, tags,
			body_goal, body_context, body_deliverable_format, body_deliverable_path,
			summary, artifacts_json, max_retries, retry_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		task.ID, task.Title, task.Assignee, task.Status, task.Priority,
		string(parents), string(skills), string(requiredSkills), string(tags),
		task.BodyGoal, task.BodyContext,
		task.BodyDeliverableFmt, task.BodyDeliverablePath,
		task.Summary, string(artifacts),
		task.MaxRetries, task.RetryCount,
	)
	if err != nil {
		return err
	}

	r.logTaskEvent(task.ID, "CreateTask", task.Assignee, "")
	return nil
}

func (r *TaskRepo) GetByID(id string) (*models.Task, error) {
	row := r.db.QueryRow(
		`SELECT `+taskColumns+` FROM tasks WHERE id = $1`, id,
	)

	task, err := scanTaskRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	return task, nil
}

// getTasksByIDs returns all tasks matching the given IDs in a single batch query.
func (r *TaskRepo) getTasksByIDs(ids []string) ([]*models.Task, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	query := fmt.Sprintf("SELECT "+taskColumns+" FROM tasks WHERE id IN (%s)", strings.Join(placeholders, ","))
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("batch get tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*models.Task
	for rows.Next() {
		task, err := scanTaskRow(rows)
		if err != nil {
			return nil, fmt.Errorf("batch scan task: %w", err)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// AreParentsCompleted returns true if all parent tasks with the given IDs have status 'completed'.
func (r *TaskRepo) AreParentsCompleted(parentIDs []string) (bool, error) {
	if len(parentIDs) == 0 {
		return true, nil
	}
	placeholders := make([]string, len(parentIDs))
	args := make([]interface{}, len(parentIDs))
	for i, pid := range parentIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = pid
	}
	query := fmt.Sprintf("SELECT COUNT(*) FROM tasks WHERE id IN (%s) AND status = 'completed'", strings.Join(placeholders, ","))
	var count int
	if err := r.db.QueryRow(query, args...).Scan(&count); err != nil {
		return false, fmt.Errorf("check parents: %w", err)
	}
	return count == len(parentIDs), nil
}

func (r *TaskRepo) List(status, assignee string, requiredSkills ...string) ([]models.Task, error) {
	query := "SELECT id, title, assignee, status, priority, parents, skills, required_skills, tags, body_goal, body_context, body_deliverable_format, body_deliverable_path, created_at, updated_at, summary, artifacts_json, last_heartbeat, max_retries, retry_count FROM tasks WHERE 1=1"
	var args []interface{}
	paramIdx := 1
	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", paramIdx)
		args = append(args, status)
		paramIdx++
	}
	if assignee != "" {
		query += fmt.Sprintf(" AND assignee = $%d", paramIdx)
		args = append(args, assignee)
		paramIdx++
	}

	// Filter by required_skills - task must have ALL required skills
	for _, skill := range requiredSkills {
		query += fmt.Sprintf(" AND required_skills LIKE $%d", paramIdx)
		escaped := strings.NewReplacer(`%`, `\%`, `_`, `\_`).Replace(skill)
		args = append(args, fmt.Sprintf("%%%s%%", escaped))
		paramIdx++
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []models.Task
	for rows.Next() {
		var t models.Task
		var parents, skills, reqSkills, tags, artifacts string
		var createdAt, updatedAt sql.NullString
		var lastHeartbeat sql.NullTime
		if err := rows.Scan(&t.ID, &t.Title, &t.Assignee, &t.Status, &t.Priority, &parents, &skills, &reqSkills, &tags, &t.BodyGoal, &t.BodyContext, &t.BodyDeliverableFmt, &t.BodyDeliverablePath, &createdAt, &updatedAt, &t.Summary, &artifacts, &lastHeartbeat, &t.MaxRetries, &t.RetryCount); err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		t.CreatedAt = parseTime(createdAt.String)
		t.UpdatedAt = parseTime(updatedAt.String)
		if lastHeartbeat.Valid {
			t.LastHeartbeat = &lastHeartbeat.Time
		}
		if err := json.Unmarshal([]byte(parents), &t.Parents); err != nil {
			log.Printf("[WARN] List: failed to unmarshal parents: %v", err)
		}
		if err := json.Unmarshal([]byte(skills), &t.Skills); err != nil {
			log.Printf("[WARN] List: failed to unmarshal skills: %v", err)
		}
		if err := json.Unmarshal([]byte(reqSkills), &t.RequiredSkills); err != nil {
			log.Printf("[WARN] List: failed to unmarshal required_skills: %v", err)
		}
		if err := json.Unmarshal([]byte(tags), &t.Tags); err != nil {
			log.Printf("[WARN] List: failed to unmarshal tags: %v", err)
		}
		if err := json.Unmarshal([]byte(artifacts), &t.Artifacts); err != nil {
			log.Printf("[WARN] List: failed to unmarshal artifacts: %v", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (r *TaskRepo) UpdateStatus(id, status string) error {
	res, err := r.db.Exec(
		`UPDATE tasks SET status = $1, updated_at = NOW() WHERE id = $2`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	// Log a generic event for status updates not covered by specific methods
	r.logTaskEvent(id, "UpdateStatus", "", "")
	return nil
}

func (r *TaskRepo) ClaimTask(id, assignee string) (*models.Task, error) {
	res, err := r.db.Exec(
		"UPDATE tasks SET status = 'claimed', assignee = $1, updated_at = NOW() WHERE id = $2 AND status = 'pending'",
		assignee, id,
	)
	if err != nil {
		return nil, fmt.Errorf("claim task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var current string
		r.db.QueryRow("SELECT status FROM tasks WHERE id = $1", id).Scan(&current)
		if current == "" {
			return nil, fmt.Errorf("task %s not found", id)
		}
		return nil, &ConflictError{CurrentStatus: current, Message: "task is not in pending status"}
	}

	r.logTaskEvent(id, "ClaimTask", assignee, "")

	return r.GetByID(id)
}

func (r *TaskRepo) StartTask(id string) (*models.Task, error) {
	res, err := r.db.Exec(
		"UPDATE tasks SET status = 'in_progress', updated_at = NOW() WHERE id = $1 AND status = 'claimed'",
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("start task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var current string
		r.db.QueryRow("SELECT status FROM tasks WHERE id = $1", id).Scan(&current)
		if current == "" {
			return nil, fmt.Errorf("task %s not found", id)
		}
		return nil, &ConflictError{CurrentStatus: current, Message: "task is not in claimed status"}
	}

	r.logTaskEvent(id, "StartTask", "", "")

	return r.GetByID(id)
}

func (r *TaskRepo) BlockTask(id string) (*models.Task, error) {
	res, err := r.db.Exec("UPDATE tasks SET status = 'blocked', updated_at = NOW() WHERE id = $1 AND status IN ('in_progress', 'claimed')", id)
	if err != nil {
		return nil, fmt.Errorf("block task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var current string
		r.db.QueryRow("SELECT status FROM tasks WHERE id = $1", id).Scan(&current)
		return nil, &ConflictError{CurrentStatus: current, Message: "task is not in in_progress or claimed status"}
	}

	r.logTaskEvent(id, "BlockTask", "", "")

	return r.GetByID(id)
}

func (r *TaskRepo) CompleteTask(id, summary string) (*models.Task, error) {
	res, err := r.db.Exec("UPDATE tasks SET status = 'completed', summary = $1, updated_at = NOW() WHERE id = $2 AND status = 'in_progress'", summary, id)
	if err != nil {
		return nil, fmt.Errorf("complete task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var current string
		r.db.QueryRow("SELECT status FROM tasks WHERE id = $1", id).Scan(&current)
		if current == "" {
			return nil, fmt.Errorf("task %s not found", id)
		}
		return nil, &ConflictError{CurrentStatus: current, Message: "task is not in in_progress status"}
	}

	r.logTaskEvent(id, "CompleteTask", "", summary)

	completedTask, err := r.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get task after complete: %w", err)
	}

	// Promote children whose parents are now all completed
	if err := r.PromoteChildren(id); err != nil {
		log.Printf("[WARN] CompleteTask: promote children for %s: %v", id, err)
	}

	return completedTask, nil
}

// FailTaskResult contains the result of a fail operation.
type FailTaskResult struct {
	Task     *models.Task
	DidRetry bool // true if the task was auto-requeued (retry_count < max_retries)
	Attempt  int  // current retry attempt (0 if no retry)
	MaxRetry int  // configured max retries
}

func (r *TaskRepo) FailTask(id, reason string) (*FailTaskResult, error) {
	res, err := r.db.Exec("UPDATE tasks SET status = 'failed', summary = $1, updated_at = NOW() WHERE id = $2 AND status = 'in_progress'", reason, id)
	if err != nil {
		return nil, fmt.Errorf("fail task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var current string
		r.db.QueryRow("SELECT status FROM tasks WHERE id = $1", id).Scan(&current)
		if current == "" {
			return nil, fmt.Errorf("task %s not found", id)
		}
		return nil, &ConflictError{CurrentStatus: current, Message: "task is not in in_progress status"}
	}

	r.logTaskEvent(id, "FailTask", "", reason)

	// Check for auto-retry
	task, err := r.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get task after fail: %w", err)
	}
	result := &FailTaskResult{Task: task, MaxRetry: task.MaxRetries}

	if task.MaxRetries > 0 && task.RetryCount < task.MaxRetries {
		_, err := r.db.Exec(
			"UPDATE tasks SET status = 'pending', retry_count = retry_count + 1, assignee = '', updated_at = NOW() WHERE id = $1",
			id,
		)
		if err != nil {
			return nil, fmt.Errorf("fail task retry: %w", err)
		}
		r.logTaskEvent(id, "TaskRetry", "", fmt.Sprintf("retry %d/%d: %s", task.RetryCount+1, task.MaxRetries, reason))

		task, err = r.GetByID(id)
		if err != nil {
			return nil, fmt.Errorf("get task after retry: %w", err)
		}
		result.Task = task
		result.DidRetry = true
		result.Attempt = task.RetryCount
	}

	return result, nil
}

// ExpirePendingTasks cancels tasks that have been in 'pending' status for longer
// than timeoutMinutes and returns the IDs of expired tasks for logging.
func (r *TaskRepo) ExpirePendingTasks(timeoutMinutes int) ([]string, error) {
	if timeoutMinutes <= 0 {
		return nil, fmt.Errorf("invalid timeout: %d must be > 0", timeoutMinutes)
	}
	intervalStr := fmt.Sprintf("%d minutes", timeoutMinutes)
	rows, err := r.db.Query(
		`SELECT id, title FROM tasks
		 WHERE status = 'pending'
		 AND created_at < NOW() - $1::interval`,
		intervalStr,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending expired tasks: %w", err)
	}
	defer rows.Close()

	var expiredIDs []string
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			log.Printf("[WARN] ExpirePendingTasks: scan error: %v", err)
			continue
		}
		expiredIDs = append(expiredIDs, id)
	}

	if len(expiredIDs) == 0 {
		return nil, nil
	}

	for _, id := range expiredIDs {
		_, err := r.db.Exec(
			`UPDATE tasks SET status = 'failed', summary = 'Task expired: no agent claimed within timeout period', updated_at = NOW() WHERE id = $1 AND status = 'pending'`,
			id,
		)
		if err != nil {
			log.Printf("[ERROR] ExpirePendingTasks: failed to expire task %s: %v", id, err)
			continue
		}
		r.logTaskEvent(id, "PendingTimeout", "", fmt.Sprintf("Task expired after %d minutes in pending status", timeoutMinutes))
	}

	return expiredIDs, nil
}

// ReleaseAgentTasks resets all claimed and in-progress tasks for a given agent
// back to pending. Used when an agent is detected as stale (no heartbeat).
func (r *TaskRepo) ReleaseAgentTasks(agentName string) ([]string, error) {
	rows, err := r.db.Query(
		`SELECT id FROM tasks WHERE assignee = $1 AND status IN ('claimed', 'in_progress')`,
		agentName,
	)
	if err != nil {
		return nil, fmt.Errorf("query agent tasks: %w", err)
	}
	defer rows.Close()

	var released []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			log.Printf("[WARN] ReleaseAgentTasks: scan error: %v", err)
			continue
		}
		released = append(released, id)
	}

	for _, id := range released {
		_, err := r.db.Exec(
			`UPDATE tasks SET status = 'pending', assignee = '', updated_at = NOW() WHERE id = $1`,
			id,
		)
		if err != nil {
			log.Printf("[ERROR] ReleaseAgentTasks: failed to release task %s: %v", id, err)
			continue
		}
		r.logTaskEvent(id, "StaleAgentRelease", "", fmt.Sprintf("released from stale agent %s", agentName))
	}

	return released, nil
}

// UpdateTaskHeartbeat updates the last_heartbeat timestamp for a task.
func (r *TaskRepo) UpdateTaskHeartbeat(taskID string) error {
	res, err := r.db.Exec(
		`UPDATE tasks SET last_heartbeat = NOW(), updated_at = NOW() WHERE id = $1`,
		taskID,
	)
	if err != nil {
		return fmt.Errorf("update task heartbeat: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}
	return nil
}

// ExpireStaleInProgressTasks cancels tasks stuck in 'in_progress' whose
// last_heartbeat is older than timeoutMinutes, or that have no heartbeat
// set since created_at.
func (r *TaskRepo) ExpireStaleInProgressTasks(timeoutMinutes int) ([]string, error) {
	if timeoutMinutes <= 0 {
		return nil, nil
	}
	intervalStr := fmt.Sprintf("%d minutes", timeoutMinutes)
	rows, err := r.db.Query(
		`SELECT id, max_retries, retry_count FROM tasks
		 WHERE status = 'in_progress'
		 AND (
		   last_heartbeat IS NOT NULL AND last_heartbeat < NOW() - $1::interval
		   OR
		   last_heartbeat IS NULL AND created_at < NOW() - $1::interval
		 )`,
		intervalStr,
	)
	if err != nil {
		return nil, fmt.Errorf("query stale in-progress tasks: %w", err)
	}
	defer rows.Close()

	var staleIDs []string
	for rows.Next() {
		var id string
		var maxRetries, retryCount int
		if err := rows.Scan(&id, &maxRetries, &retryCount); err != nil {
			log.Printf("[WARN] ExpireStaleInProgressTasks: scan error: %v", err)
			continue
		}
		staleIDs = append(staleIDs, id)
	}

	if len(staleIDs) == 0 {
		return nil, nil
	}

	for _, id := range staleIDs {
		_, err := r.db.Exec(
			`UPDATE tasks SET status = 'failed', summary = $1, updated_at = NOW() WHERE id = $2 AND status = 'in_progress'`,
			fmt.Sprintf("Task timed out after %d minutes without heartbeat", timeoutMinutes), id,
		)
		if err != nil {
			log.Printf("[ERROR] ExpireStaleInProgressTasks: failed to expire task %s: %v", id, err)
			continue
		}
		r.logTaskEvent(id, "TaskHeartbeatTimeout", "", fmt.Sprintf("Task expired after %d minutes without heartbeat", timeoutMinutes))
	}

	return staleIDs, nil
}

func (r *TaskRepo) getArtifactsJSON(id string) (string, error) {
	var raw string
	err := r.db.QueryRow("SELECT artifacts_json FROM tasks WHERE id = $1", id).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("task %s not found", id)
	}
	if err != nil {
		return "", fmt.Errorf("get artifacts json: %w", err)
	}
	return raw, nil
}

func (r *TaskRepo) SetArtifactsJSON(id, raw string) error {
	_, err := r.db.Exec("UPDATE tasks SET artifacts_json = $1 WHERE id = $2", raw, id)
	return err
}

func (r *TaskRepo) AddArtifact(taskID string, artifact models.Artifact) error {
	raw, err := r.getArtifactsJSON(taskID)
	if err != nil {
		return err
	}

	var list []models.Artifact
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			return fmt.Errorf("unmarshal artifacts: %w", err)
		}
	}

	list = append(list, artifact)
	data, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("marshal artifacts: %w", err)
	}

	return r.SetArtifactsJSON(taskID, string(data))
}

func (r *TaskRepo) RemoveArtifact(taskID string, key string) error {
	raw, err := r.getArtifactsJSON(taskID)
	if err != nil {
		return err
	}

	var list []models.Artifact
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			return fmt.Errorf("unmarshal artifacts: %w", err)
		}
	}

	var filtered []models.Artifact
	for _, a := range list {
		if a.Key != key {
			filtered = append(filtered, a)
		}
	}

	data, err := json.Marshal(filtered)
	if err != nil {
		return fmt.Errorf("marshal artifacts: %w", err)
	}

	return r.SetArtifactsJSON(taskID, string(data))
}

func (r *TaskRepo) GetArtifacts(taskID string) ([]models.Artifact, error) {
	raw, err := r.getArtifactsJSON(taskID)
	if err != nil {
		return nil, err
	}

	if raw == "" || raw == "[]" {
		return []models.Artifact{}, nil
	}

	var list []models.Artifact
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("unmarshal artifacts: %w", err)
	}
	return list, nil
}

func (r *TaskRepo) GetPendingByAgent(agent string) ([]models.Task, error) {
	return r.List("pending", "")
}

// GetTaskCounts returns the count of tasks per status.
type TaskCounts struct {
	Pending     int `json:"pending"`
	Claimed     int `json:"claimed"`
	InProgress  int `json:"in_progress"`
	Blocked     int `json:"blocked"`
	Completed   int `json:"completed"`
	Failed      int `json:"failed"`
	Total       int `json:"total"`
}

func (r *TaskRepo) GetTaskCounts() (*TaskCounts, error) {
	counts := &TaskCounts{}
	rows, err := r.db.Query(
		`SELECT status, COUNT(*) as cnt FROM tasks GROUP BY status`,
	)
	if err != nil {
		return nil, fmt.Errorf("get task counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var cnt int
		if err := rows.Scan(&status, &cnt); err != nil {
			return nil, fmt.Errorf("scan count: %w", err)
		}
		switch status {
		case "pending":
			counts.Pending = cnt
		case "claimed":
			counts.Claimed = cnt
		case "in_progress":
			counts.InProgress = cnt
		case "blocked":
			counts.Blocked = cnt
		case "completed":
			counts.Completed = cnt
		case "failed":
			counts.Failed = cnt
		}
	}
	counts.Total = counts.Pending + counts.Claimed + counts.InProgress + counts.Blocked + counts.Completed + counts.Failed
	return counts, rows.Err()
}

// GetDependencyGraph returns a task with its parent and child tasks.
type TaskGraph struct {
	Task     *models.Task   `json:"task"`
	Parents  []*models.Task `json:"parents"`
	Children []*models.Task `json:"children"`
}

func (r *TaskRepo) GetDependencyGraph(taskID string) (*TaskGraph, error) {
	task, err := r.GetByID(taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, nil
	}

	graph := &TaskGraph{Task: task, Parents: []*models.Task{}, Children: []*models.Task{}}

	// Batch load parents
	if len(task.Parents) > 0 {
		parents, err := r.getTasksByIDs(task.Parents)
		if err != nil {
			log.Printf("[WARN] GetDependencyGraph: batch load parents: %v", err)
		} else {
			graph.Parents = parents
		}
	}

	// Find children (tasks that list this task as a parent)
	rows, err := r.db.Query(
		`SELECT id FROM tasks WHERE parents LIKE $1`,
		fmt.Sprintf("%%%s%%", taskID),
	)
	if err != nil {
		return nil, fmt.Errorf("query children: %w", err)
	}
	defer rows.Close()

	var childIDs []string
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			log.Printf("[WARN] GetDependencyGraph: scan child: %v", err)
			continue
		}
		childIDs = append(childIDs, childID)
	}

	// Batch load children
	if len(childIDs) > 0 {
		children, err := r.getTasksByIDs(childIDs)
		if err != nil {
			log.Printf("[WARN] GetDependencyGraph: batch load children: %v", err)
		} else {
			graph.Children = children
		}
	}

	return graph, nil
}

// CheckParentsCompleted returns true if all parent tasks of the given task
// have status 'completed'. Tasks with no parents return true.
func (r *TaskRepo) CheckParentsCompleted(taskID string) (bool, error) {
	task, err := r.GetByID(taskID)
	if err != nil {
		return false, fmt.Errorf("get task for parent check: %w", err)
	}
	if task == nil {
		return false, fmt.Errorf("task %s not found", taskID)
	}
	return r.AreParentsCompleted(task.Parents)
}

// PromoteChildren evaluates all tasks that list the given task as a parent.
// Children whose all parents are now completed remain in pending (already claimable).
// This is called after a task is completed.
func (r *TaskRepo) PromoteChildren(taskID string) error {
	rows, err := r.db.Query(
		`SELECT id FROM tasks WHERE parents LIKE $1 AND status = 'pending'`,
		fmt.Sprintf("%%%s%%", taskID),
	)
	if err != nil {
		return fmt.Errorf("query children for promotion: %w", err)
	}
	defer rows.Close()

	var childIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			log.Printf("[WARN] PromoteChildren: scan error: %v", err)
			continue
		}
		childIDs = append(childIDs, id)
	}

	for _, childID := range childIDs {
		// Verify all parents are completed before promoting
		allDone, err := r.CheckParentsCompleted(childID)
		if err != nil {
			log.Printf("[WARN] PromoteChildren: check parents for %s: %v", childID, err)
			continue
		}
		if allDone {
			r.logTaskEvent(childID, "ParentsCompleted", "", fmt.Sprintf("parent %s completed", taskID))
		}
	}

	return nil
}

// ListTaskEvents returns the event trail for a task.
func (r *TaskRepo) ListTaskEvents(taskID string) ([]models.TaskEvent, error) {
	if r.eventRepo == nil {
		return nil, fmt.Errorf("event repo not initialized")
	}
	return r.eventRepo.ListByTask(taskID)
}

// logTaskEvent registers an event for a task transition
func (r *TaskRepo) logTaskEvent(taskID, event, agent, detail string) {
	if r.eventRepo != nil {
		_ = r.eventRepo.InsertEvent(taskID, event, agent, detail)
	}
}