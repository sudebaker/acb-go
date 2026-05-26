package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/models"
)

// RegisterAgent requires auth (Bearer token of an existing agent, or admin token for bootstrap).
// Tests that register a *new* agent use the admin token (bypasses the self-only check).
// Tests that register the *same* agent use its own testToken.

func TestRegisterAgent_200(t *testing.T) {
	d := setupTestDB(t)
	agentRepo := db.NewAgentRepo(d)
	taskRepo := db.NewTaskRepo(d)
	gateRepo := db.NewGateRepo(d)

	// Preregister an agent so we have a valid token for auth
	agentRepo.UpsertAgent(context.Background(), &models.Agent{Name: "test-agent", Token: testToken})

	r := NewRouter(taskRepo, gateRepo, agentRepo, nil, nil, nil, nil, nil, nil)

	body := `{
		"name": "test-reg-agent",
		"port": 8090,
		"token": "reg-token-1",
		"skills": ["go", "testing"]
	}`
	req := httptest.NewRequest("POST", "/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Admin token can register any agent (bootstrap flow)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["name"] != "test-reg-agent" {
		t.Errorf("expected name 'test-reg-agent', got %v", resp["name"])
	}
	// Token must not be returned (omitempty with empty string)
	if resp["token"] != nil {
		t.Errorf("expected token to be omitted in response, got %v", resp["token"])
	}
}

func TestRegisterAgent_NoWebhook(t *testing.T) {
	d := setupTestDB(t)
	agentRepo := db.NewAgentRepo(d)
	taskRepo := db.NewTaskRepo(d)
	gateRepo := db.NewGateRepo(d)

	agentRepo.UpsertAgent(context.Background(), &models.Agent{Name: "test-agent", Token: testToken})

	r := NewRouter(taskRepo, gateRepo, agentRepo, nil, nil, nil, nil, nil, nil)

	body := `{
		"name": "simple-agent",
		"port": 8091,
		"token": "reg-token-2",
		"skills": ["python"]
	}`
	req := httptest.NewRequest("POST", "/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the agent was stored and can be retrieved via repo
	agent, err := agentRepo.GetByName(context.Background(), "simple-agent")
	if err != nil {
		t.Fatal(err)
	}
	if agent.WebhookURL != "" {
		t.Errorf("expected empty webhook_url, got %q", agent.WebhookURL)
	}
}

func TestRegisterAgent_MissingName(t *testing.T) {
	d := setupTestDB(t)
	agentRepo := db.NewAgentRepo(d)

	agentRepo.UpsertAgent(context.Background(), &models.Agent{Name: "test-agent", Token: testToken})

	r := NewRouter(db.NewTaskRepo(d), db.NewGateRepo(d), agentRepo, nil, nil, nil, nil, nil, nil)

	body := `{"port": 8091}`
	req := httptest.NewRequest("POST", "/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// With admin token, X-Agent-Name is set to "admin", so name="admin" is used
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Admin token registers itself as "admin" when no name provided
	if w.Code != 200 {
		t.Errorf("expected 200 (admin token registers itself), got %d: %s", w.Code, w.Body.String())
	}

	// Verify "admin" agent was created
	agent, err := agentRepo.GetByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if agent == nil {
		t.Fatal("expected admin agent to be created")
	}
}

func TestRegisterAgent_Upsert(t *testing.T) {
	d := setupTestDB(t)
	agentRepo := db.NewAgentRepo(d)
	taskRepo := db.NewTaskRepo(d)
	gateRepo := db.NewGateRepo(d)

	agentRepo.UpsertAgent(context.Background(), &models.Agent{Name: "test-agent", Token: testToken})

	r := NewRouter(taskRepo, gateRepo, agentRepo, nil, nil, nil, nil, nil, nil)

	// First registration — use admin token (bootstrap flow, creating a new agent)
	body1 := `{
		"name": "upsert-agent",
		"port": 8092,
		"token": "tok-upsert",
		"skills": ["go"]
	}`
	req1 := httptest.NewRequest("POST", "/agents", strings.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer "+adminToken)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	if w1.Code != 200 {
		t.Fatalf("first registration failed: %d: %s", w1.Code, w1.Body.String())
	}

	// Second registration (upsert)
	body2 := `{
		"name": "upsert-agent",
		"port": 8093,
		"token": "tok-upsert-v2",
		"skills": ["go", "webhook"]
	}`
	req2 := httptest.NewRequest("POST", "/agents", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	// Admin token can also upsert any agent
	req2.Header.Set("Authorization", "Bearer "+adminToken)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("upsert registration failed: %d: %s", w2.Code, w2.Body.String())
	}

	// Verify updated values
	agent, _ := agentRepo.GetByName(context.Background(), "upsert-agent")
	if agent.Port != 8093 {
		t.Errorf("expected updated port 8093, got %d", agent.Port)
	}
}

func TestGetAgent_200(t *testing.T) {
	d := setupTestDB(t)
	agentRepo := db.NewAgentRepo(d)
	taskRepo := db.NewTaskRepo(d)
	gateRepo := db.NewGateRepo(d)

	agentRepo.UpsertAgent(context.Background(), &models.Agent{
		Name:          "fetch-agent",
		Port:          8094,
		Token:         "tok-fetch",
		Skills:        []string{"go"},
		WebhookURL:    "http://localhost:8094/hook",
		WebhookSecret: "secret-fetch",
	})
	// Need a valid auth token — use the registered agent
	agentRepo.UpsertAgent(context.Background(), &models.Agent{Name: "test-agent", Token: testToken})

	r := NewRouter(taskRepo, gateRepo, agentRepo, nil, nil, nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/agents/fetch-agent", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["webhook_url"] != "http://localhost:8094/hook" {
		t.Errorf("expected webhook_url in response, got %v", resp["webhook_url"])
	}
	if resp["token"] != nil {
		t.Errorf("expected token to be omitted in response, got %v", resp["token"])
	}
}