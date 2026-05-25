package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/sudebaker/acb-go/internal/models"
)

// PostgreSQL timestamp formats (used in parseTime helpers)
const (
	pgTimestampMicro = "2006-01-02 15:04:05.999999+00"
	pgTimestampSec   = "2006-01-02 15:04:05+00"
	pgTimestampNoTZ  = "2006-01-02 15:04:05"
)

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
			summary, artifacts_json)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		task.ID, task.Title, task.Assignee, task.Status, task.Priority,
		string(parents), string(skills), string(requiredSkills), string(tags),
		task.BodyGoal, task.BodyContext,
		task.BodyDeliverableFmt, task.BodyDeliverablePath,
		task.Summary, string(artifacts),
	)
	return err
}

func (r *TaskRepo) GetByID(id string) (*models.Task, error) {
	row := r.db.QueryRow(
		`SELECT id, title, assignee, status, priority, parents, skills, required_skills, tags,
			body_goal, body_context, body_deliverable_format, body_deliverable_path,
			created_at, updated_at, summary, artifacts_json
		FROM tasks WHERE id = $1`, id,
	)

	task := &models.Task{}
	var parents, skills, requiredSkills, tags, artifacts string
	var createdAt, updatedAt string
	err := row.Scan(
		&task.ID, &task.Title, &task.Assignee, &task.Status, &task.Priority,
		&parents, &skills, &requiredSkills, &tags,
		&task.BodyGoal, &task.BodyContext,
		&task.BodyDeliverableFmt, &task.BodyDeliverablePath,
		&createdAt, &updatedAt, &task.Summary, &artifacts,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}

	task.CreatedAt = parseTime(createdAt)
	task.UpdatedAt = parseTime(updatedAt)
	if err := json.Unmarshal([]byte(parents), &task.Parents); err != nil {
		log.Printf("[WARN] GetByID: failed to unmarshal parents: %v", err)
	}
	if err := json.Unmarshal([]byte(skills), &task.Skills); err != nil {
		log.Printf("[WARN] GetByID: failed to unmarshal skills: %v", err)
	}
	if err := json.Unmarshal([]byte(requiredSkills), &task.RequiredSkills); err != nil {
		log.Printf("[WARN] GetByID: failed to unmarshal required_skills: %v", err)
	}
	if err := json.Unmarshal([]byte(tags), &task.Tags); err != nil {
		log.Printf("[WARN] GetByID: failed to unmarshal tags: %v", err)
	}
	if err := json.Unmarshal([]byte(artifacts), &task.Artifacts); err != nil {
		log.Printf("[WARN] GetByID: failed to unmarshal artifacts: %v", err)
	}

	if task.Status == "" {
		task.Status = "pending"
	}
	return task, nil
}

func (r *TaskRepo) List(status, assignee string, requiredSkills ...string) ([]models.Task, error) {
	query := "SELECT id, title, assignee, status, priority, parents, skills, required_skills, tags, body_goal, body_context, body_deliverable_format, body_deliverable_path, created_at, updated_at, summary, artifacts_json FROM tasks WHERE 1=1"
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
		args = append(args, fmt.Sprintf("%%%s%%", skill))
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
		if err := rows.Scan(&t.ID, &t.Title, &t.Assignee, &t.Status, &t.Priority, &parents, &skills, &reqSkills, &tags, &t.BodyGoal, &t.BodyContext, &t.BodyDeliverableFmt, &t.BodyDeliverablePath, &createdAt, &updatedAt, &t.Summary, &artifacts); err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		t.CreatedAt = parseTime(createdAt.String)
		t.UpdatedAt = parseTime(updatedAt.String)
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

	return r.GetByID(id)
}

func (r *TaskRepo) FailTask(id, reason string) (*models.Task, error) {
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

	return r.GetByID(id)
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

// logTaskEvent registers an event for a task transition
func (r *TaskRepo) logTaskEvent(taskID, event, agent, detail string) {
	if r.eventRepo != nil {
		_ = r.eventRepo.InsertEvent(taskID, event, agent, detail)
	}
}