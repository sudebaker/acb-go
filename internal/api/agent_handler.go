package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sudebaker/acb-go/internal/config"
	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/dispatcher"
	"github.com/sudebaker/acb-go/internal/models"
)

type AgentHandler struct {
	agentRepo *db.AgentRepo
	limiter   *RateLimiter
	cfg       *config.Config
}

// GetAgentCursor returns the last_event_id for the authenticated agent (from X-Agent-Name).
func (h *AgentHandler) GetAgentCursor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentName := r.Header.Get("X-Agent-Name")
	if agentName == "" {
		WriteError(w, 401, "unauthorized", "X-Agent-Name header is missing")
		return
	}

	lastID, err := h.agentRepo.GetLastEventID(ctx, agentName)
	if err != nil {
		WriteError(w, 404, "agent_not_found", "agent not found")
		return
	}

	resp := map[string]interface{}{"cursor": lastID}
	WriteJSON(w, 200, resp)
}

// UpdateAgentCursor sets the last_event_id for the authenticated agent (from X-Agent-Name).
func (h *AgentHandler) UpdateAgentCursor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentName := r.Header.Get("X-Agent-Name")
	if agentName == "" {
		WriteError(w, 401, "unauthorized", "X-Agent-Name header is missing")
		return
	}

	var input struct {
		Cursor int64 `json:"cursor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, 400, "invalid_json", "invalid request body")
		return
	}
	if input.Cursor <= 0 {
		WriteError(w, 400, "missing_cursor", "cursor must be a positive integer")
		return
	}

	if err := h.agentRepo.UpdateLastEventID(ctx, agentName, input.Cursor); err != nil {
		if errors.Is(err, db.ErrAgentNotFound) {
			WriteError(w, 404, "agent_not_found", "agent not found")
		} else {
			WriteErrorSafe(w, 500, "update_cursor_failed", err)
		}
		return
	}

	WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *AgentHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, 400, "invalid_json", "invalid request body")
		return
	}
	if input.Name == "" {
		input.Name = r.Header.Get("X-Agent-Name")
	}
	if input.Name == "" {
		WriteError(w, 400, "missing_name", "agent name is required")
		return
	}

	if h.limiter != nil && !h.limiter.Allow(input.Name) {
		WriteError(w, 429, "rate_limited", "too many heartbeats")
		return
	}

	if err := h.agentRepo.UpdateHeartbeat(ctx, input.Name); err != nil {
		WriteErrorSafe(w, 404, "agent_not_found", err)
		return
	}

	WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *AgentHandler) GetAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := chi.URLParam(r, "name")

	agent, err := h.agentRepo.GetByName(ctx, name)
	if err != nil {
		WriteErrorSafe(w, 500, "get_failed", err)
		return
	}
	if agent == nil {
		WriteError(w, 404, "not_found", "agent not found")
		return
	}

	agent.Token = ""
	agent.WebhookSecret = ""
	WriteJSON(w, 200, agent)
}

// RegisterAgent creates or updates an agent registration.
// POST /agents  —  accepts webhook_url and webhook_secret
// SECURITY: Prevents token overwrite — agent can only register itself, not another.
func (h *AgentHandler) RegisterAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Rate limit agent registration to prevent abuse
	if h.limiter != nil && !h.limiter.Allow("register") {
		WriteError(w, 429, "rate_limited", "too many registration requests")
		return
	}

	var input struct {
		Name          string   `json:"name"`
		Port          int      `json:"port"`
		Token         string   `json:"token"`
		Skills        []string `json:"skills"`
		WebhookURL    string   `json:"webhook_url"`
		WebhookSecret string   `json:"webhook_secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, 400, "invalid_json", "invalid request body")
		return
	}
	if input.Name == "" {
		input.Name = r.Header.Get("X-Agent-Name")
	}
	if input.Name == "" {
		WriteError(w, 400, "missing_name", "agent name is required")
		return
	}

	// SECURITY: Validate that registering agent is the same as X-Agent-Name (from auth middleware)
	// Exception: admin token (X-Agent-Name=admin) can register any agent (bootstrap)
	agentNameFromAuth := r.Header.Get("X-Agent-Name")
	if agentNameFromAuth == "" {
		WriteError(w, 401, "unauthorized", "X-Agent-Name header is missing")
		return
	}
	if agentNameFromAuth != "admin" && agentNameFromAuth != input.Name {
		WriteError(w, 403, "forbidden", "agent can only register itself")
		return
	}

	// Validate skills against allowed list
	if len(input.Skills) > 0 {
		if invalid := h.cfg.ValidateSkills(input.Skills); len(invalid) > 0 {
			WriteError(w, 400, "invalid_skills", "one or more skills are not in the allowed catalog")
			return
		}
	}

	// If token is not provided, use the Bearer token from auth
	if input.Token == "" {
		input.Token = r.Header.Get("X-Auth-Token")
	}

	if input.WebhookURL != "" {
		if err := dispatcher.ValidateWebhookURL(input.WebhookURL); err != nil {
			WriteError(w, 400, "invalid_webhook_url", err.Error())
			return
		}
	}

	agent := &models.Agent{Name: input.Name, Port: input.Port, Token: input.Token, Skills: input.Skills, WebhookURL: input.WebhookURL, WebhookSecret: input.WebhookSecret}

	if err := h.agentRepo.UpsertAgent(ctx, agent); err != nil {
		WriteErrorSafe(w, 500, "upsert_failed", err)
		return
	}

	// Return the agent without the token
	agent.Token = ""
	agent.WebhookSecret = ""
	WriteJSON(w, 200, agent)
}
