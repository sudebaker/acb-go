package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sudebaker/acb-go/internal/models"
)

type AgentRepo struct {
	db *sql.DB
}

func NewAgentRepo(db *sql.DB) *AgentRepo {
	return &AgentRepo{db: db}
}

func (r *AgentRepo) UpsertAgent(agent *models.Agent) error {
	skills, err := json.Marshal(agent.Skills)
	if err != nil {
		return fmt.Errorf("marshal skills: %w", err)
	}
	_, err = r.db.Exec(
		`INSERT INTO agents (name, port, token, last_heartbeat, skills)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(name) DO UPDATE SET 
		port = excluded.port, 
		token = excluded.token,
		last_heartbeat = excluded.last_heartbeat,
		skills = excluded.skills`,
		agent.Name, agent.Port, agent.Token, agent.LastHeartbeat, string(skills),
	)
	return err
}

func (r *AgentRepo) GetByName(name string) (*models.Agent, error) {
	row := r.db.QueryRow(
		`SELECT name, port, token, last_heartbeat, skills FROM agents WHERE name = ?`, name,
	)
	agent := &models.Agent{}
	var heartbeat sql.NullString
	var skills string
	if err := row.Scan(&agent.Name, &agent.Port, &agent.Token, &heartbeat, &skills); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan agent: %w", err)
	}
	if heartbeat.Valid {
		agent.LastHeartbeat = heartbeat.String
	}
	agent.Token = ""
	if err := json.Unmarshal([]byte(skills), &agent.Skills); err != nil {
		return nil, fmt.Errorf("unmarshal skills: %w", err)
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

func (r *AgentRepo) GetByToken(token string) (*models.Agent, error) {
	row := r.db.QueryRow(
		`SELECT name, port, token, last_heartbeat, skills FROM agents WHERE token = ?`, token,
	)
	agent := &models.Agent{}
	var heartbeat sql.NullString
	var skills string
	if err := row.Scan(&agent.Name, &agent.Port, &agent.Token, &heartbeat, &skills); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan agent by token: %w", err)
	}
	if heartbeat.Valid {
		agent.LastHeartbeat = heartbeat.String
	}
	agent.Token = ""
	if err := json.Unmarshal([]byte(skills), &agent.Skills); err != nil {
		return nil, fmt.Errorf("unmarshal skills: %w", err)
	}
	return agent, nil
}

func (r *AgentRepo) ListStale(dur time.Duration) ([]models.Agent, error) {
	cutoff := time.Now().UTC().Add(-dur).Format(time.RFC3339)
	rows, err := r.db.Query(
		`SELECT name, port, last_heartbeat, skills FROM agents
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
		var skills string
		if err := rows.Scan(&a.Name, &a.Port, &heartbeat, &skills); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		if heartbeat.Valid {
			a.LastHeartbeat = heartbeat.String
		}
		if err := json.Unmarshal([]byte(skills), &a.Skills); err != nil {
			return nil, fmt.Errorf("unmarshal skills: %w", err)
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// GetSkills returns the skills of an agent by name
func (r *AgentRepo) GetSkills(name string) ([]string, error) {
	var skills string
	err := r.db.QueryRow("SELECT skills FROM agents WHERE name = ?", name).Scan(&skills)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent %s not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("get skills: %w", err)
	}
	var result []string
	if err := json.Unmarshal([]byte(skills), &result); err != nil {
		return nil, fmt.Errorf("unmarshal skills: %w", err)
	}
	return result, nil
}

// HasRequiredSkills checks if an agent has all required skills
func (r *AgentRepo) HasRequiredSkills(agentName string, requiredSkills []string) (bool, error) {
	agentSkills, err := r.GetSkills(agentName)
	if err != nil {
		return false, err
	}
	
	// Check if agent has all required skills
	for _, req := range requiredSkills {
		found := false
		for _, skill := range agentSkills {
			if skill == req {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}
