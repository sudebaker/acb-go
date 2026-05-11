package db

import (
	"testing"

	"github.com/amphora/acb/internal/models"
)

func TestCreateGate(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGateRepo(db)

	gate := &models.Gate{
		GateID:   "g001",
		TaskID:   "t001",
		Question: "Should we proceed?",
		Ask:      "human",
	}
	if err := repo.CreateGate(gate); err != nil {
		t.Fatal(err)
	}
}

func TestGetGatesByTaskID(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGateRepo(db)

	repo.CreateGate(&models.Gate{GateID: "g001", TaskID: "t001", Question: "Q1"})
	repo.CreateGate(&models.Gate{GateID: "g002", TaskID: "t001", Question: "Q2"})

	gates, err := repo.GetByTaskID("t001")
	if err != nil {
		t.Fatal(err)
	}
	if len(gates) != 2 {
		t.Errorf("expected 2 gates, got %d", len(gates))
	}
}

func TestAnswerGate(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGateRepo(db)
	repo.CreateGate(&models.Gate{GateID: "g001", TaskID: "t001", Question: "Q1"})

	// Directly set status to asked for testing
	db.Exec("UPDATE gates SET status = 'asked' WHERE gate_id = 'g001'")

	if err := repo.AnswerGate("g001", "Yes"); err != nil {
		t.Fatal(err)
	}

	gates, _ := repo.GetByTaskID("t001")
	if len(gates) != 1 || gates[0].Status != "answered" || gates[0].Answer != "Yes" {
		t.Errorf("got status=%q answer=%q", gates[0].Status, gates[0].Answer)
	}
}

func TestAnswerGate_NotAsked(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGateRepo(db)
	repo.CreateGate(&models.Gate{GateID: "g001", TaskID: "t001", Question: "Q1"})

	if err := repo.AnswerGate("g001", "Yes"); err == nil {
		t.Error("expected error answering gate not in 'asked' status")
	}
}

func TestResolveGate(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGateRepo(db)
	repo.CreateGate(&models.Gate{GateID: "g001", TaskID: "t001", Question: "Q1"})
	db.Exec("UPDATE gates SET status = 'asked' WHERE gate_id = 'g001'")
	repo.AnswerGate("g001", "Yes")

	if err := repo.ResolveGate("g001"); err != nil {
		t.Fatal(err)
	}

	gates, _ := repo.GetByTaskID("t001")
	if len(gates) != 1 || gates[0].Status != "resolved" {
		t.Errorf("got status %q", gates[0].Status)
	}
}

func TestResolveGate_NotAnswered(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGateRepo(db)
	repo.CreateGate(&models.Gate{GateID: "g001", TaskID: "t001", Question: "Q1"})

	if err := repo.ResolveGate("g001"); err == nil {
		t.Error("expected error resolving gate not in 'answered' status")
	}
}
