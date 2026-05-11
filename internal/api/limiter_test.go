package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amphora/acb/internal/db"
	"github.com/amphora/acb/internal/models"
	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"
)

func TestRateLimiter_AllowsWithinLimit(t *testing.T) {
	rl := NewRateLimiter(rate.Every(10*time.Second), 1)
	if !rl.Allow("agent-a") {
		t.Error("expected first request to be allowed")
	}
}

func TestRateLimiter_BlocksExcess(t *testing.T) {
	rl := NewRateLimiter(rate.Every(10*time.Second), 1)
	rl.Allow("agent-a")
	if rl.Allow("agent-a") {
		t.Error("expected second request to be blocked")
	}
}

func TestRateLimiter_IndependentPerAgent(t *testing.T) {
	rl := NewRateLimiter(rate.Every(10*time.Second), 1)
	rl.Allow("agent-a")
	if !rl.Allow("agent-b") {
		t.Error("expected agent-b to be allowed independently")
	}
}

func TestHeartbeat_RateLimited(t *testing.T) {
	d := setupTestDB(t)
	agentRepo := db.NewAgentRepo(d)
	agentRepo.UpsertAgent(&models.Agent{Name: "worker-a", Token: "rt-token"})

	limiter := NewRateLimiter(rate.Every(1*time.Hour), 1)
	ah := &AgentHandler{agentRepo: agentRepo, limiter: limiter}

	r := chi.NewRouter()
	r.Use(JSONContentType)
	r.Use(AuthMiddleware(agentRepo))
	r.Post("/agents/heartbeat", ah.Heartbeat)

	req := func() *http.Request {
		req := httptest.NewRequest("POST", "/agents/heartbeat", strings.NewReader(`{"name":"worker-a"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer rt-token")
		return req
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req())
	if w.Code != 200 {
		t.Fatalf("first: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, req())
	if w.Code != 429 {
		t.Errorf("second: expected 429, got %d: %s", w.Code, w.Body.String())
	}
}
