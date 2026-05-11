package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/amphora/acb/internal/models"
)

type AgentRepo struct {
	db *sql.DB
}

func NewAgentRepo(db *sql.DB) *AgentRepo {
	return &AgentRepo{db: db}
}

func (r *AgentRepo) UpsertAgent(agent *models.Agent) error {
	_, err := r.db.Exec(
		`INSERT INTO agents (name, port, token)
		VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET port = excluded.port, token = excluded.token`,
		agent.Name, agent.Port, agent.Token,
	)
	return err
}

func (r *AgentRepo) GetByName(name string) (*models.Agent, error) {
	row := r.db.QueryRow(
		`SELECT name, port, token, last_heartbeat FROM agents WHERE name = ?`, name,
	)
	agent := &models.Agent{}
	var heartbeat sql.NullString
	if err := row.Scan(&agent.Name, &agent.Port, &agent.Token, &heartbeat); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan agent: %w", err)
	}
	if heartbeat.Valid {
		agent.LastHeartbeat = heartbeat.String
	}
	return agent, nil
}

func (r *AgentRepo) UpdateHeartbeat(name string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := r.db.Exec(
		"UPDATE agents SET last_heartbeat = ? WHERE name = ?", now, name,
	)
	if err != nil {
		return fmt.Errorf("update heartbeat: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent %s not found", name)
	}
	return nil
}

func (r *AgentRepo) ListStale(dur time.Duration) ([]models.Agent, error) {
	cutoff := time.Now().UTC().Add(-dur).Format(time.RFC3339)
	rows, err := r.db.Query(
		`SELECT name, port, last_heartbeat FROM agents
		WHERE last_heartbeat IS NULL OR last_heartbeat < ?`, cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("list stale agents: %w", err)
	}
	defer rows.Close()

	var agents []models.Agent
	for rows.Next() {
		var a models.Agent
		var heartbeat sql.NullString
		if err := rows.Scan(&a.Name, &a.Port, &heartbeat); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		if heartbeat.Valid {
			a.LastHeartbeat = heartbeat.String
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}
