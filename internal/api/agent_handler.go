package api

import (
	"encoding/json"
	"net/http"

	"github.com/sudebaker/acb-go/internal/db"
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
