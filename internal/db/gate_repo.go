package db

import (
	"database/sql"
	"fmt"

	"github.com/sudebaker/acb-go/internal/models"
)

type GateRepo struct {
	db *sql.DB
}

func NewGateRepo(db *sql.DB) *GateRepo {
	return &GateRepo{db: db}
}

func (r *GateRepo) CreateGate(gate *models.Gate) error {
	if gate.Status == "" {
		gate.Status = "pending"
	}
	_, err := r.db.Exec(
		`INSERT INTO gates (gate_id, task_id, question, ask, status, answer)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		gate.GateID, gate.TaskID, gate.Question, gate.Ask, gate.Status, gate.Answer,
	)
	return err
}

func (r *GateRepo) GetByTaskID(taskID string) ([]models.Gate, error) {
	rows, err := r.db.Query(
		`SELECT gate_id, task_id, question, ask, status, answer, created_at, answered_at
		 FROM gates WHERE task_id = $1`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("get gates by task: %w", err)
	}
	defer rows.Close()

	var gates []models.Gate
	for rows.Next() {
		var g models.Gate
		var answeredAt sql.NullTime

		if err := rows.Scan(&g.GateID, &g.TaskID, &g.Question, &g.Ask, &g.Status, &g.Answer, &g.CreatedAt, &answeredAt); err != nil {
			return nil, fmt.Errorf("scan gate: %w", err)
		}
		if answeredAt.Valid {
			g.AnsweredAt = &answeredAt.Time
		}
		gates = append(gates, g)
	}
	return gates, rows.Err()
}

func (r *GateRepo) AnswerGate(gateID, answer string) error {
	res, err := r.db.Exec(
		`UPDATE gates SET status = 'answered', answer = $1, answered_at = NOW() WHERE gate_id = $2 AND status = 'asked'`,
		answer, gateID,
	)
	if err != nil {
		return fmt.Errorf("answer gate: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("gate %s not found or not in 'asked' status", gateID)
	}
	return nil
}

func (r *GateRepo) ResolveGate(gateID string) error {
	res, err := r.db.Exec(
		`UPDATE gates SET status = 'resolved' WHERE gate_id = $1 AND status = 'answered'`,
		gateID,
	)
	if err != nil {
		return fmt.Errorf("resolve gate: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("gate %s not found or not in 'answered' status", gateID)
	}
	return nil
}