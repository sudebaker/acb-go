package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/amphora/acb/internal/models"
)

type TaskRepo struct {
	db *sql.DB
}

func NewTaskRepo(db *sql.DB) *TaskRepo {
	return &TaskRepo{db: db}
}

func (r *TaskRepo) Create(task *models.Task) error {
	if task.Status == "" {
		task.Status = "pending"
	}
	parents, err := json.Marshal(task.Parents)
	if err != nil {
		return fmt.Errorf("marshal parents: %w", err)
	}
	artifacts, err := json.Marshal(task.Artifacts)
	if err != nil {
		return fmt.Errorf("marshal artifacts: %w", err)
	}

	_, err = r.db.Exec(
		`INSERT INTO tasks (id, title, assignee, status, priority, parents,
			body_goal, body_context, body_deliverable_format, body_deliverable_path,
			summary, artifacts_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.Title, task.Assignee, task.Status, task.Priority,
		string(parents), task.BodyGoal, task.BodyContext,
		task.BodyDeliverableFmt, task.BodyDeliverablePath,
		task.Summary, string(artifacts),
	)
	return err
}

func (r *TaskRepo) GetByID(id string) (*models.Task, error) {
	row := r.db.QueryRow(
		`SELECT id, title, assignee, status, priority, parents,
			body_goal, body_context, body_deliverable_format, body_deliverable_path,
			created_at, summary, artifacts_json
		FROM tasks WHERE id = ?`, id,
	)

	task := &models.Task{}
	var parents, artifacts string
	var createdAt string
	err := row.Scan(
		&task.ID, &task.Title, &task.Assignee, &task.Status, &task.Priority,
		&parents, &task.BodyGoal, &task.BodyContext,
		&task.BodyDeliverableFmt, &task.BodyDeliverablePath,
		&createdAt, &task.Summary, &artifacts,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}

	task.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	json.Unmarshal([]byte(parents), &task.Parents)
	json.Unmarshal([]byte(artifacts), &task.Artifacts)

	if task.Status == "" {
		task.Status = "pending"
	}
	return task, nil
}

func (r *TaskRepo) List(status, assignee string) ([]models.Task, error) {
	query := "SELECT id, title, assignee, status FROM tasks WHERE 1=1"
	var args []interface{}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	if assignee != "" {
		query += " AND assignee = ?"
		args = append(args, assignee)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []models.Task
	for rows.Next() {
		var t models.Task
		if err := rows.Scan(&t.ID, &t.Title, &t.Assignee, &t.Status); err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (r *TaskRepo) UpdateStatus(id, status string) error {
	res, err := r.db.Exec("UPDATE tasks SET status = ? WHERE id = ?", status, id)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

func (r *TaskRepo) ClaimTask(id, assignee string) error {
	var currentStatus string
	err := r.db.QueryRow("SELECT status FROM tasks WHERE id = ?", id).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		return fmt.Errorf("task %s not found", id)
	}
	if err != nil {
		return fmt.Errorf("get task status: %w", err)
	}
	if currentStatus != "pending" {
		return fmt.Errorf("task %s is %s, expected pending", id, currentStatus)
	}

	_, err = r.db.Exec("UPDATE tasks SET status = 'claimed', assignee = ? WHERE id = ?", assignee, id)
	return err
}

func (r *TaskRepo) StartTask(id string) error {
	var currentStatus string
	err := r.db.QueryRow("SELECT status FROM tasks WHERE id = ?", id).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		return fmt.Errorf("task %s not found", id)
	}
	if err != nil {
		return fmt.Errorf("get task status: %w", err)
	}
	if currentStatus != "claimed" {
		return fmt.Errorf("task %s is %s, expected claimed", id, currentStatus)
	}

	_, err = r.db.Exec("UPDATE tasks SET status = 'in_progress' WHERE id = ?", id)
	return err
}

func (r *TaskRepo) BlockTask(id string) error {
	res, err := r.db.Exec("UPDATE tasks SET status = 'blocked' WHERE id = ? AND status IN ('in_progress', 'claimed')", id)
	if err != nil {
		return fmt.Errorf("block task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var current string
		r.db.QueryRow("SELECT status FROM tasks WHERE id = ?", id).Scan(&current)
		return fmt.Errorf("task %s is %s, expected in_progress or claimed", id, current)
	}
	return nil
}

func (r *TaskRepo) CompleteTask(id, summary string) error {
	res, err := r.db.Exec("UPDATE tasks SET status = 'completed', summary = ? WHERE id = ? AND status = 'in_progress'", summary, id)
	if err != nil {
		return fmt.Errorf("complete task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task %s not found or not in_progress", id)
	}
	return nil
}

func (r *TaskRepo) FailTask(id, reason string) error {
	res, err := r.db.Exec("UPDATE tasks SET status = 'failed', summary = ? WHERE id = ? AND status = 'in_progress'", reason, id)
	if err != nil {
		return fmt.Errorf("fail task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task %s not found or not in_progress", id)
	}
	return nil
}

func (r *TaskRepo) GetPendingByAgent(agent string) ([]models.Task, error) {
	return r.List("pending", "")
}
