package db

import (
	"database/sql"
	"context"
	"fmt"

	"github.com/sudebaker/acb-go/internal/models"
)

type TaskEventRepo struct {
	db *sql.DB
}

func NewTaskEventRepo(db *sql.DB) *TaskEventRepo {
	return &TaskEventRepo{db: db}
}

func (r *TaskEventRepo) InsertEvent(ctx context.Context, taskID, event, agent, detail string) error {
	_, err := r.db.ExecContext(ctx, 
		`INSERT INTO task_events (task_id, event, agent, detail) VALUES ($1, $2, $3, $4)`,
		taskID, event, agent, detail,
	)
	if err != nil {
		return fmt.Errorf("insert task event: %w", err)
	}
	return nil
}

// ListSince returns task events, optionally filtered by agent.
// When useID is true, uses ID-based cursor (WHERE e.id > afterID, with 0 = no filter).
// When useID is false, uses timestamp-based cursor (WHERE e.timestamp > since).
// Results are ordered by id DESC (newest first).
func (r *TaskEventRepo) ListSince(ctx context.Context, since, agent string, limit int, afterID int64, useID bool) ([]models.TaskEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var rows *sql.Rows
	var err error

	if useID {
		// ID-based cursor (most reliable, no timestamp edge cases)
		if agent != "" {
			rows, err = r.db.QueryContext(ctx,
				`SELECT e.id, e.task_id, e.event, e.agent, e.timestamp, e.detail, COALESCE(t.title, e.task_id)
				 FROM task_events e
				 LEFT JOIN tasks t ON t.id = e.task_id
				 WHERE e.id > $1
				 AND (e.agent = $2 OR e.agent = '')
				 ORDER BY e.id DESC
				 LIMIT $3`,
				afterID, agent, limit,
			)
		} else {
			rows, err = r.db.QueryContext(ctx,
				`SELECT e.id, e.task_id, e.event, e.agent, e.timestamp, e.detail, COALESCE(t.title, e.task_id)
				 FROM task_events e
				 LEFT JOIN tasks t ON t.id = e.task_id
				 WHERE e.id > $1
				 ORDER BY e.id DESC
				 LIMIT $2`,
				afterID, limit,
			)
		}
	} else {
		// Timestamp-based cursor (fallback for manual queries)
		if agent != "" {
			rows, err = r.db.QueryContext(ctx,
				`SELECT e.id, e.task_id, e.event, e.agent, e.timestamp, e.detail, COALESCE(t.title, e.task_id)
				 FROM task_events e
				 LEFT JOIN tasks t ON t.id = e.task_id
				 WHERE e.timestamp > $1::timestamptz
				 AND (e.agent = $2 OR e.agent = '')
				 ORDER BY e.id DESC
				 LIMIT $3`,
				since, agent, limit,
			)
		} else {
			rows, err = r.db.QueryContext(ctx,
				`SELECT e.id, e.task_id, e.event, e.agent, e.timestamp, e.detail, COALESCE(t.title, e.task_id)
				 FROM task_events e
				 LEFT JOIN tasks t ON t.id = e.task_id
				 WHERE e.timestamp > $1::timestamptz
				 ORDER BY e.id DESC
				 LIMIT $2`,
				since, limit,
			)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("list events since: %w", err)
	}
	defer rows.Close()

	var events []models.TaskEvent
	for rows.Next() {
		var e models.TaskEvent
		var timestamp string
		if err := rows.Scan(&e.ID, &e.TaskID, &e.Event, &e.Agent, &timestamp, &e.Detail, &e.Title); err != nil {
			return nil, fmt.Errorf("scan task event: %w", err)
		}
		e.Timestamp = timestamp
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return events, nil
}

func (r *TaskEventRepo) ListByTask(ctx context.Context, taskID string) ([]models.TaskEvent, error) {
	rows, err := r.db.QueryContext(ctx, 
		`SELECT task_id, event, agent, timestamp, detail
		 FROM task_events
		 WHERE task_id = $1
		 ORDER BY timestamp DESC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("list task events: %w", err)
	}
	defer rows.Close()

	var events []models.TaskEvent
	for rows.Next() {
		var e models.TaskEvent
		var timestamp string
		if err := rows.Scan(&e.TaskID, &e.Event, &e.Agent, &timestamp, &e.Detail); err != nil {
			return nil, fmt.Errorf("scan task event: %w", err)
		}
		e.Timestamp = timestamp
		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return events, nil
}
