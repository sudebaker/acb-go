package db

import (
	"database/sql"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sudebaker/acb-go/internal/models"
)

var ErrAgentNotFound = errors.New("agent not found")

type AgentRepo struct {
	db *sql.DB
}

func NewAgentRepo(db *sql.DB) *AgentRepo {
	return &AgentRepo{db: db}
}

func (r *AgentRepo) UpsertAgent(ctx context.Context, agent *models.Agent) error {
	// Store token as Argon2id hash, never plaintext
	hash, prefix, err := hashToken(agent.Token)
	if err != nil {
		return fmt.Errorf("hash token: %w", err)
	}

	// Encrypt webhook_secret for at-rest protection
	var encryptedSecret string
	if agent.WebhookSecret != "" {
		encryptedSecret, err = EncryptWebhookSecret(agent.WebhookSecret)
		if err != nil {
			return fmt.Errorf("encrypt webhook secret: %w", err)
		}
	}

	skills, err := json.Marshal(agent.Skills)
	if err != nil {
		return fmt.Errorf("marshal skills: %w", err)
	}

	// Handle NULL last_heartbeat: if agent.LastHeartbeat is nil, insert NULL
	var heartbeat sql.NullTime
	if agent.LastHeartbeat != nil {
		heartbeat = sql.NullTime{Time: *agent.LastHeartbeat, Valid: true}
	}

	_, err = r.db.ExecContext(ctx, 
		`INSERT INTO agents (name, port, token, token_prefix, last_heartbeat, skills, webhook_url, webhook_secret)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT(name) DO UPDATE SET 
	port = EXCLUDED.port, 
	token = EXCLUDED.token,
	token_prefix = EXCLUDED.token_prefix,
	last_heartbeat = EXCLUDED.last_heartbeat,
	skills = EXCLUDED.skills,
	webhook_url = EXCLUDED.webhook_url,
	webhook_secret = EXCLUDED.webhook_secret`,
		agent.Name, agent.Port, hash, prefix, heartbeat, string(skills), agent.WebhookURL, encryptedSecret,
	)
	return err
}

func (r *AgentRepo) GetByName(ctx context.Context, name string) (*models.Agent, error) {
	row := r.db.QueryRowContext(ctx, 
		`SELECT name, port, token, last_heartbeat, skills, webhook_url, webhook_secret FROM agents WHERE name = $1`, name,
	)
	agent := &models.Agent{}
	var heartbeat sql.NullTime
	var skills, webhookSecret string
	if err := row.Scan(&agent.Name, &agent.Port, &agent.Token, &heartbeat, &skills, &agent.WebhookURL, &webhookSecret); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan agent: %w", err)
	}
	if heartbeat.Valid {
		agent.LastHeartbeat = &heartbeat.Time
	}
	// Always clear token in response
	agent.Token = ""
	agent.WebhookSecret = webhookSecret
	if err := json.Unmarshal([]byte(skills), &agent.Skills); err != nil {
		return nil, fmt.Errorf("unmarshal skills: %w", err)
	}
	return agent, nil
}

func (r *AgentRepo) UpdateHeartbeat(ctx context.Context, name string) error {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, 
		"UPDATE agents SET last_heartbeat = $1 WHERE name = $2", now, name,
	)
	if err != nil {
		return fmt.Errorf("update heartbeat: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrAgentNotFound, name)
	}
	return nil
}

func (r *AgentRepo) GetByToken(ctx context.Context, token string) (*models.Agent, error) {
	if len(token) < 8 {
		return nil, nil
	}

	// Strategy: try prefix lookup first (fast path).
	// We only try the hash-based prefix - no fallback to plaintext or full-scan
	// to prevent timing attacks that leak the number of agents.
	_, hashPrefix, err := hashToken(token)
	if err != nil {
		return nil, fmt.Errorf("hash token for lookup: %w", err)
	}

	// Try hash-based prefix lookup only
	agent, err := r.getByTokenPrefix(ctx, token, hashPrefix)
	if err != nil {
		return nil, err
	}
	if agent != nil {
		return agent, nil
	}

	// No fallback to full-scan - prevents timing attack that leaks agent count
	return nil, nil
}

func (r *AgentRepo) getByTokenPrefix(ctx context.Context, token, prefix string) (*models.Agent, error) {
	rows, err := r.db.QueryContext(ctx, 
		`SELECT name, port, token, last_heartbeat, skills, webhook_url, webhook_secret FROM agents WHERE token_prefix = $1`, prefix,
	)
	if err != nil {
		return nil, fmt.Errorf("query agents: %w", err)
	}
	defer rows.Close()
	return r.scanMatchingAgent(ctx, rows, token)
}

func (r *AgentRepo) scanMatchingAgent(ctx context.Context, rows *sql.Rows, token string) (*models.Agent, error) {
	for rows.Next() {
		var a models.Agent
		var heartbeat sql.NullTime
		var storedToken string
		var skills, webhookSecret string
		if err := rows.Scan(&a.Name, &a.Port, &storedToken, &heartbeat, &skills, &a.WebhookURL, &webhookSecret); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		if heartbeat.Valid {
			a.LastHeartbeat = &heartbeat.Time
		}

		// Only Argon2id verification - no plaintext fallback
		// Plaintext tokens should have been migrated before deploying this version
		match, err := verifyToken(token, storedToken)
		if err != nil {
			// Hash verification failed - skip this agent
			continue
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

func (r *AgentRepo) ListStale(ctx context.Context, dur time.Duration) ([]models.Agent, error) {
	cutoff := time.Now().UTC().Add(-dur)
	rows, err := r.db.QueryContext(ctx, 
		`SELECT name, port, last_heartbeat, skills, webhook_url FROM agents
WHERE last_heartbeat IS NULL OR last_heartbeat < $1`, cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("list stale agents: %w", err)
	}
	defer rows.Close()

	var agents []models.Agent
	for rows.Next() {
		var a models.Agent
		var heartbeat sql.NullTime
		var skills, webhookURL string
		if err := rows.Scan(&a.Name, &a.Port, &heartbeat, &skills, &webhookURL); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		if heartbeat.Valid {
			a.LastHeartbeat = &heartbeat.Time
		}
		a.WebhookURL = webhookURL
		if err := json.Unmarshal([]byte(skills), &a.Skills); err != nil {
			return nil, fmt.Errorf("unmarshal skills: %w", err)
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func (r *AgentRepo) GetSkills(ctx context.Context, name string) ([]string, error) {
	var skills string
	err := r.db.QueryRowContext(ctx, "SELECT skills FROM agents WHERE name = $1", name).Scan(&skills)
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

func (r *AgentRepo) HasRequiredSkills(ctx context.Context, agentName string, requiredSkills []string) (bool, error) {
	agentSkills, err := r.GetSkills(ctx, agentName)
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

// GetLastEventID returns the last_event_id for an agent (highest processed event ID).
// Returns nil if never set (first run).
func (r *AgentRepo) GetLastEventID(ctx context.Context, name string) (*int64, error) {
	var id sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		`SELECT last_event_id FROM agents WHERE name = $1`, name,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent %s not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("get last event id: %w", err)
	}
	if id.Valid {
		return &id.Int64, nil
	}
	return nil, nil
}

// UpdateLastEventID sets the last_event_id for an agent.
func (r *AgentRepo) UpdateLastEventID(ctx context.Context, name string, id int64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE agents SET last_event_id = $1 WHERE name = $2`,
		id, name,
	)
	if err != nil {
		return fmt.Errorf("update last event id: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrAgentNotFound, name)
	}
	return nil
}

// FindMatchingAgents returns all agents whose skills include ALL of the requiredSkills.
// If requiredSkills is empty, returns all agents.
func (r *AgentRepo) FindMatchingAgents(ctx context.Context, requiredSkills []string) ([]models.Agent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT name, port, token, last_heartbeat, skills, webhook_url, webhook_secret FROM agents`)
	if err != nil {
		return nil, fmt.Errorf("find matching agents: %w", err)
	}
	defer rows.Close()

	var result []models.Agent
	for rows.Next() {
		var a models.Agent
		var heartbeat sql.NullTime
		var skills string
		var webhookURL, webhookSecret sql.NullString
		if err := rows.Scan(&a.Name, &a.Port, &a.Token, &heartbeat, &skills, &webhookURL, &webhookSecret); err != nil {
			return nil, fmt.Errorf("scan agent row: %w", err)
		}
		if heartbeat.Valid {
			a.LastHeartbeat = &heartbeat.Time
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
