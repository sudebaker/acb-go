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
