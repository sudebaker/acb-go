package api

import (
	"encoding/json"
	"net/http"

	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/models"
	"github.com/go-chi/chi/v5"
)

type AgentHandler struct {
	agentRepo *db.AgentRepo
	limiter   *RateLimiter
}

func (h *AgentHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
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

	if err := h.agentRepo.UpdateHeartbeat(input.Name); err != nil {
		WriteError(w, 404, "agent_not_found", err.Error())
		return
	}

	WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *AgentHandler) GetAgent(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	agent, err := h.agentRepo.GetByName(name)
	if err != nil {
		WriteError(w, 500, "get_failed", err.Error())
		return
	}
	if agent == nil {
		WriteError(w, 404, "not_found", "agent not found")
		return
	}

	agent.Token = ""
	WriteJSON(w, 200, agent)
}

// RegisterAgent creates or updates an agent registration.
// POST /agents  —  accepts webhook_url and webhook_secret
// SECURITY: Prevents token overwrite — agent can only register itself, not another.
func (h *AgentHandler) RegisterAgent(w http.ResponseWriter, r *http.Request) {
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
	agentNameFromAuth := r.Header.Get("X-Agent-Name")
	if agentNameFromAuth != "" && input.Name != agentNameFromAuth {
		WriteError(w, 403, "forbidden", "agent can only register itself")
		return
	}

	// If token is not provided, use the Bearer token from auth
	if input.Token == "" {
		input.Token = r.Header.Get("X-Auth-Token")
	}

	agent := &models.Agent{
		Name:          input.Name,
		Port:          input.Port,
		Token:         input.Token,
		Skills:        input.Skills,
		WebhookURL:    input.WebhookURL,
		WebhookSecret: input.WebhookSecret,
	}

	if err := h.agentRepo.UpsertAgent(agent); err != nil {
		WriteError(w, 500, "upsert_failed", err.Error())
		return
	}

	// Return the agent without the token
	agent.Token = ""
	WriteJSON(w, 200, agent)
}
