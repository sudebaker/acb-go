package db

import (
	"testing"
	"time"

	"github.com/amphora/acb/internal/models"
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
	if got.Name != "agent-alpha" || got.Token != "tok_123" {
		t.Errorf("got %+v", got)
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
