package db

import (
	"testing"
	"time"

	"github.com/sudebaker/acb-go/internal/models"
)

func TestUpsertAndGetAgent(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)

	agent := &models.Agent{Name: "agent-alpha", Port: 8081, Token: "tok_123"}
	if err := repo.UpsertAgent(agent); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByName("agent-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "agent-alpha" || got.Port != 8081 {
		t.Errorf("got %+v", got)
	}
	if got.Token != "" {
		t.Errorf("expected token to be cleared by GetByName, got %q", got.Token)
	}
}

func TestUpsertAgent_UpdateExisting(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)

	repo.UpsertAgent(&models.Agent{Name: "agent-alpha", Port: 8081, Token: "tok_123"})
	repo.UpsertAgent(&models.Agent{Name: "agent-alpha", Port: 8082, Token: "tok_456"})

	got, _ := repo.GetByName("agent-alpha")
	if got.Port != 8082 {
		t.Errorf("got %+v", got)
	}
}

func TestGetByToken(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)

	repo.UpsertAgent(&models.Agent{Name: "agent-alpha", Port: 8081, Token: "tok_12345"})

	got, err := repo.GetByToken("tok_12345")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "agent-alpha" {
		t.Errorf("expected name agent-alpha, got %q", got.Name)
	}
	if got.Token != "" {
		t.Errorf("expected token to be cleared by GetByToken, got %q", got.Token)
	}
}

func TestGetByToken_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)

	got, err := repo.GetByToken("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent token")
	}
}

func TestGetByName_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)

	got, err := repo.GetByName("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent agent")
	}
}

func TestUpdateHeartbeat(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)
	repo.UpsertAgent(&models.Agent{Name: "agent-alpha", Port: 8081})

	if err := repo.UpdateHeartbeat("agent-alpha"); err != nil {
		t.Fatal(err)
	}

	got, _ := repo.GetByName("agent-alpha")
	if got.LastHeartbeat == nil {
		t.Error("expected heartbeat to be set")
	}
	if got.LastHeartbeat != nil && time.Since(*got.LastHeartbeat) > 5*time.Second {
		t.Errorf("heartbeat time too far in the past: %v", got.LastHeartbeat)
	}
}

func TestUpdateHeartbeat_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)

	if err := repo.UpdateHeartbeat("nonexistent"); err == nil {
		t.Error("expected error for nonexistent agent")
	}
}

func TestListStale(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)
	repo.UpsertAgent(&models.Agent{Name: "agent-alpha", Port: 8081})
	repo.UpsertAgent(&models.Agent{Name: "agent-beta", Port: 8082})
	repo.UpdateHeartbeat("agent-beta")

	stale, err := repo.ListStale(1 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, a := range stale {
		if a.Name == "agent-alpha" {
			found = true
		}
	}
	if !found {
		t.Error("expected agent-alpha (no heartbeat) to be stale")
	}
}

func TestUpsertAgentWithSkills(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)

	agent := &models.Agent{Name: "agent-python", Port: 8081, Skills: []string{"python", "sql", "api"}}
	if err := repo.UpsertAgent(agent); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByName("agent-python")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 3 {
		t.Errorf("expected 3 skills, got %d: %+v", len(got.Skills), got.Skills)
	}
	for _, s := range []string{"python", "sql", "api"} {
		found := false
		for _, gs := range got.Skills {
			if gs == s {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected skill %s not found in %+v", s, got.Skills)
		}
	}
}

func TestGetByTokenWithSkills(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)

	repo.UpsertAgent(&models.Agent{Name: "agent-js", Port: 8081, Token: "tok_js_abc", Skills: []string{"javascript", "node"}})

	got, err := repo.GetByToken("tok_js_abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d: %+v", len(got.Skills), got.Skills)
	}
}

func TestGetSkills(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)

	repo.UpsertAgent(&models.Agent{Name: "agent-rust", Port: 8081, Skills: []string{"rust", "tokio"}})

	skills, err := repo.GetSkills("agent-rust")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Errorf("expected 2 skills, got %d: %+v", len(skills), skills)
	}
}

func TestHasRequiredSkills(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)

	repo.UpsertAgent(&models.Agent{Name: "agent-fullstack", Port: 8081, Skills: []string{"python", "javascript", "sql"}})

	task := &models.Task{RequiredSkills: []string{"python", "sql"}}
	has, err := repo.HasRequiredSkills("agent-fullstack", task.RequiredSkills)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("expected agent to have all required skills")
	}

	task.RequiredSkills = []string{"python", "rust"}
	has, _ = repo.HasRequiredSkills("agent-fullstack", task.RequiredSkills)
	if has {
		t.Error("expected agent NOT to have rust skill")
	}
}

func TestUpsertAgentWithWebhook(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)

	agent := &models.Agent{
		Name:          "webhook-agent",
		Port:          8081,
		Token:         "tok-wh",
		Skills:        []string{"go"},
		WebhookURL:    "http://localhost:8645/webhooks/amanda",
		WebhookSecret: "secret-123",
	}
	if err := repo.UpsertAgent(agent); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByName("webhook-agent")
	if err != nil {
		t.Fatal(err)
	}
	if got.WebhookURL != "http://localhost:8645/webhooks/amanda" {
		t.Errorf("expected webhook_url, got %q", got.WebhookURL)
	}
	// Webhook secret should be encrypted at rest
	if got.WebhookSecret == "" {
		t.Fatal("expected webhook_secret to be stored (encrypted)")
	}
	if got.WebhookSecret == "secret-123" {
		t.Error("webhook_secret should be encrypted, not plaintext")
	}
	// Verify it can be decrypted
	decrypted, err := DecryptWebhookSecret(got.WebhookSecret)
	if err != nil {
		t.Errorf("failed to decrypt webhook_secret: %v", err)
	}
	if decrypted != "secret-123" {
		t.Errorf("expected decrypted secret to be %q, got %q", "secret-123", decrypted)
	}
	if got.Token != "" {
		t.Errorf("expected token to be cleared, got %q", got.Token)
	}
}

func TestFindMatchingAgents(t *testing.T) {
	db := setupTestDB(t)
	repo := NewAgentRepo(db)

	repo.UpsertAgent(&models.Agent{Name: "go-agent", Port: 8081, Skills: []string{"go", "testing"}})
	repo.UpsertAgent(&models.Agent{Name: "python-agent", Port: 8082, Skills: []string{"python", "sql"}})
	repo.UpsertAgent(&models.Agent{Name: "fullstack", Port: 8083, Skills: []string{"go", "python", "sql"}})

	// Find agents with "go" skill
	agents, err := repo.FindMatchingAgents([]string{"go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 {
		t.Errorf("expected 2 agents with 'go' skill, got %d", len(agents))
	}

	// Find agents with all skills
	agents, err = repo.FindMatchingAgents([]string{"go", "python"})
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Errorf("expected 1 agent with both 'go' and 'python' skills, got %d", len(agents))
	}

	// Find agents with empty required skills (all agents)
	agents, err = repo.FindMatchingAgents(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 3 {
		t.Errorf("expected 3 agents with no skill filter, got %d", len(agents))
	}

	// Find agents with non-existent skill
	agents, err = repo.FindMatchingAgents([]string{"rust"})
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents with 'rust' skill, got %d", len(agents))
	}
}