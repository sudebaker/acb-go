package db

import (
	"net/http"
	"strings"
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

	repo.UpsertAgent(&models.Agent{Name: "agent-alpha", Port: 8081, Token: "tok_123"})

	got, err := repo.GetByToken("tok_123")
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
	if got.LastHeartbeat == "" {
		t.Error("expected heartbeat to be set")
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

	repo.UpsertAgent(&models.Agent{Name: "agent-js", Port: 8081, Token: "tok_js", Skills: []string{"javascript", "node"}})

	got, err := repo.GetByToken("tok_js")
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

func TestClaimTask_SkillsValidation(t *testing.T) {
	db := setupTestDB(t)
	taskRepo := NewTaskRepo(db)
	agentRepo := NewAgentRepo(db)
	repo := NewGateRepo(db)

	h := &TaskHandler{taskRepo: taskRepo, gateRepo: repo, agentRepo: agentRepo}

	// Create agent with skills
	agentRepo.UpsertAgent(&models.Agent{Name: "agent-dev", Port: 8081, Skills: []string{"python"}})

	// Create task requiring skills
	task := &models.Task{ID: "task-skill-test", Title: "Test", RequiredSkills: []string{"python"}}
	taskRepo.Create(task)

	// Agent can claim task - should succeed
	req, _ := http.NewRequest("POST", "/tasks/task-skill-test/claim", strings.NewReader(`{"assignee":"agent-dev"}`))
	w := httptest.NewRecorder()
	h.ClaimTask(w, req)
	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestClaimTask_MissingSkills(t *testing.T) {
	db := setupTestDB(t)
	taskRepo := NewTaskRepo(db)
	agentRepo := NewAgentRepo(db)
	repo := NewGateRepo(db)

	h := &TaskHandler{taskRepo: taskRepo, gateRepo: repo, agentRepo: agentRepo}

	// Create agent without required skills
	agentRepo.UpsertAgent(&models.Agent{Name: "agent-js", Port: 8081, Skills: []string{"javascript"}})

	// Create task requiring skills
	task := &models.Task{ID: "task-python", Title: "Test", RequiredSkills: []string{"python"}}
	taskRepo.Create(task)

	// Agent tries to claim task - should fail with 403
	req, _ := http.NewRequest("POST", "/tasks/task-python/claim", strings.NewReader(`{"assignee":"agent-js"}`))
	w := httptest.NewRecorder()
	h.ClaimTask(w, req)
	if w.Code != 403 {
		t.Errorf("expected 403 for missing skills, got %d: %s", w.Code, w.Body.String())
	}
}
