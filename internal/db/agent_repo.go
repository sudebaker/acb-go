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
	// Store token as Argon2id hash, never plaintext
	hash, err := hashToken(agent.Token)
	if err != nil {
		return fmt.Errorf("hash token: %w", err)
	}

	skills, err := json.Marshal(agent.Skills)
	if err != nil {
		return fmt.Errorf("marshal skills: %w", err)
	}
	_, err = r.db.Exec(
		`INSERT INTO agents (name, port, token, last_heartbeat, skills, webhook_url, webhook_secret)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET 
	port = excluded.port, 
	token = excluded.token,
	last_heartbeat = excluded.last_heartbeat,
	skills = excluded.skills,
	webhook_url = excluded.webhook_url,
	webhook_secret = excluded.webhook_secret`,
		agent.Name, agent.Port, hash, agent.LastHeartbeat, string(skills), agent.WebhookURL, agent.WebhookSecret,
	)
	return err
}

func (r *AgentRepo) GetByName(name string) (*models.Agent, error) {
	row := r.db.QueryRow(
		`SELECT name, port, token, last_heartbeat, skills, webhook_url, webhook_secret FROM agents WHERE name = ?`, name,
	)
	agent := &models.Agent{}
	var heartbeat sql.NullString
	var skills, webhookSecret string
	if err := row.Scan(&agent.Name, &agent.Port, &agent.Token, &heartbeat, &skills, &agent.WebhookURL, &webhookSecret); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan agent: %w", err)
	}
	if heartbeat.Valid {
		agent.LastHeartbeat = heartbeat.String
	}
	// Always clear token in response
	agent.Token = ""
	agent.WebhookSecret = webhookSecret
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
	// Get all agents and check token
	// Supports both Argon2id hashes and plaintext tokens for migration
	rows, err := r.db.Query(
		`SELECT name, port, token, last_heartbeat, skills, webhook_url, webhook_secret FROM agents`,
	)
	if err != nil {
		return nil, fmt.Errorf("query agents: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var a models.Agent
		var heartbeat sql.NullString
		var storedToken string
		var skills, webhookSecret string
		if err := rows.Scan(&a.Name, &a.Port, &storedToken, &heartbeat, &skills, &a.WebhookURL, &webhookSecret); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		if heartbeat.Valid {
			a.LastHeartbeat = heartbeat.String
		}

		var match bool
		// Try Argon2id verification first
		hashMatch, hashErr := verifyToken(token, storedToken)
		if hashMatch && hashErr == nil {
			match = true
		} else {
			// Fallback: plaintext comparison (for migration from pre-hash tokens)
			match = (token == storedToken)
		}

		if match {
			a.Token = ""
			a.WebhookSecret = webhookSecret
			if err := json.Unmarshal([]byte(skills), &a.Skills); err != nil {
				return nil, fmt.Errorf("unmarshal skills: %w", err)
			}
			return &a, nil
		}
	}
	return nil, nil
}

func (r *AgentRepo) ListStale(dur time.Duration) ([]models.Agent, error) {
	cutoff := time.Now().UTC().Add(-dur).Format(time.RFC3339)
	rows, err := r.db.Query(
		`SELECT name, port, last_heartbeat, skills, webhook_url FROM agents
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
		var skills, webhookURL string
		if err := rows.Scan(&a.Name, &a.Port, &heartbeat, &skills, &webhookURL); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		if heartbeat.Valid {
			a.LastHeartbeat = heartbeat.String
		}
		a.WebhookURL = webhookURL
		if err := json.Unmarshal([]byte(skills), &a.Skills); err != nil {
			return nil, fmt.Errorf("unmarshal skills: %w", err)
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

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

func (r *AgentRepo) HasRequiredSkills(agentName string, requiredSkills []string) (bool, error) {
	agentSkills, err := r.GetSkills(agentName)
	if err != nil {
		return false, err
	}

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

func (r *AgentRepo) GetByWebhookSecret(secret string) (*models.Agent, error) {
	row := r.db.QueryRow(
		`SELECT name, port, token, last_heartbeat, skills, webhook_url, webhook_secret FROM agents WHERE webhook_secret = ?`, secret,
	)
	agent := &models.Agent{}
	var heartbeat sql.NullString
	var skills, webhookSecret string
	if err := row.Scan(&agent.Name, &agent.Port, &agent.Token, &heartbeat, &skills, &agent.WebhookURL, &webhookSecret); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan agent by webhook_secret: %w", err)
	}
	if heartbeat.Valid {
		agent.LastHeartbeat = heartbeat.String
	}
	agent.Token = ""
	agent.WebhookSecret = webhookSecret
	if err := json.Unmarshal([]byte(skills), &agent.Skills); err != nil {
		return nil, fmt.Errorf("unmarshal skills: %w", err)
	}
	return agent, nil
}

// FindMatchingAgents returns all agents whose skills include ALL of the requiredSkills.
// If requiredSkills is empty, returns all agents.
func (r *AgentRepo) FindMatchingAgents(requiredSkills []string) ([]models.Agent, error) {
	rows, err := r.db.Query(`SELECT name, port, token, last_heartbeat, skills, webhook_url, webhook_secret FROM agents`)
	if err != nil {
		return nil, fmt.Errorf("find matching agents: %w", err)
	}
	defer rows.Close()

	var result []models.Agent
	for rows.Next() {
		var a models.Agent
		var heartbeat sql.NullString
		var skills string
		var webhookURL, webhookSecret sql.NullString
		if err := rows.Scan(&a.Name, &a.Port, &a.Token, &heartbeat, &skills, &webhookURL, &webhookSecret); err != nil {
			return nil, fmt.Errorf("scan agent row: %w", err)
		}
		if heartbeat.Valid {
			a.LastHeartbeat = heartbeat.String
		}
		if webhookURL.Valid {
			a.WebhookURL = webhookURL.String
		}
		if webhookSecret.Valid {
			a.WebhookSecret = webhookSecret.String
		}
		if err := json.Unmarshal([]byte(skills), &a.Skills); err != nil {
			return nil, fmt.Errorf("unmarshal skills: %w", err)
		}

		// Filter by required skills
		if len(requiredSkills) > 0 {
			match := true
			for _, req := range requiredSkills {
				found := false
				for _, s := range a.Skills {
					if s == req {
						found = true
						break
					}
				}
				if !found {
					match = false
					break
				}
			}
			if !match {
				continue
			}
		}
		result = append(result, a)
	}
	return result, rows.Err()
}