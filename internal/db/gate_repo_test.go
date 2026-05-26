package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/sudebaker/acb-go/internal/models"
)

// createParentTask inserts a task row so gates can reference it via FK.
func createParentTask(t *testing.T, db *sql.DB, taskID string) {
	t.Helper()
	taskRepo := NewTaskRepo(db)
	if err := taskRepo.Create(context.Background(), &models.Task{ID: taskID, Title: "parent task for gate"}); err != nil {
		t.Fatal(err)
	}
}

func TestCreateGate(t *testing.T) {
	db := setupTestDB(t)
	createParentTask(t, db, "t001")

	repo := NewGateRepo(db)
	gate := &models.Gate{
		GateID:   "g001",
		TaskID:   "t001",
		Question: "Should we proceed?",
		Ask:      "human",
	}
	if err := repo.CreateGate(context.Background(), gate); err != nil {
		t.Fatal(err)
	}
}

func TestGetGatesByTaskID(t *testing.T) {
	db := setupTestDB(t)
	createParentTask(t, db, "t001")

	repo := NewGateRepo(db)
	repo.CreateGate(context.Background(), &models.Gate{GateID: "g001", TaskID: "t001", Question: "Q1"})
	repo.CreateGate(context.Background(), &models.Gate{GateID: "g002", TaskID: "t001", Question: "Q2"})

	gates, err := repo.GetByTaskID(context.Background(), "t001")
	if err != nil {
		t.Fatal(err)
	}
	if len(gates) != 2 {
		t.Errorf("expected 2 gates, got %d", len(gates))
	}
}

func TestAnswerGate(t *testing.T) {
	db := setupTestDB(t)
	createParentTask(t, db, "t001")

	repo := NewGateRepo(db)
	repo.CreateGate(context.Background(), &models.Gate{GateID: "g001", TaskID: "t001", Question: "Q1"})

	// Transition gate to asked for testing
	repo.AskGate(context.Background(), "g001")

	if err := repo.AnswerGate(context.Background(), "g001", "Yes"); err != nil {
		t.Fatal(err)
	}

	gates, _ := repo.GetByTaskID(context.Background(), "t001")
	if len(gates) != 1 || gates[0].Status != "answered" || gates[0].Answer != "Yes" {
		t.Errorf("got status=%q answer=%q", gates[0].Status, gates[0].Answer)
	}
}

func TestAnswerGate_NotAsked(t *testing.T) {
	db := setupTestDB(t)
	createParentTask(t, db, "t001")

	repo := NewGateRepo(db)
	repo.CreateGate(context.Background(), &models.Gate{GateID: "g001", TaskID: "t001", Question: "Q1"})

	if err := repo.AnswerGate(context.Background(), "g001", "Yes"); err == nil {
		t.Error("expected error answering gate not in 'asked' status")
	}
}

func TestResolveGate(t *testing.T) {
	db := setupTestDB(t)
	createParentTask(t, db, "t001")

	repo := NewGateRepo(db)
	repo.CreateGate(context.Background(), &models.Gate{GateID: "g001", TaskID: "t001", Question: "Q1"})
	repo.AskGate(context.Background(), "g001")
	repo.AnswerGate(context.Background(), "g001", "Yes")

	if err := repo.ResolveGate(context.Background(), "g001"); err != nil {
		t.Fatal(err)
	}

	gates, _ := repo.GetByTaskID(context.Background(), "t001")
	if len(gates) != 1 || gates[0].Status != "resolved" {
		t.Errorf("got status %q", gates[0].Status)
	}
}

func TestResolveGate_NotAnswered(t *testing.T) {
	db := setupTestDB(t)
	createParentTask(t, db, "t001")

	repo := NewGateRepo(db)
	repo.CreateGate(context.Background(), &models.Gate{GateID: "g001", TaskID: "t001", Question: "Q1"})

	if err := repo.ResolveGate(context.Background(), "g001"); err == nil {
		t.Error("expected error resolving gate not in 'answered' status")
	}
}