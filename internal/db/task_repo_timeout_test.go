package db

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sudebaker/acb-go/internal/models"
)

func TestExpirePendingTasks_NoExpired(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)

	// Create a task that was just created (not expired)
	task := &models.Task{
		ID:     "task-fresh",
		Title:  "Fresh task",
		Status: "pending",
	}
	if err := repo.Create(task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	ids, err := repo.ExpirePendingTasks(15)
	if err != nil {
		t.Fatalf("ExpirePendingTasks: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 expired, got %d: %v", len(ids), ids)
	}

	// Task should still be pending
	got, _ := repo.GetByID("task-fresh")
	if got.Status != "pending" {
		t.Errorf("expected status=pending, got %s", got.Status)
	}
}

func TestExpirePendingTasks_ExpiredTask(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)

	// Create a task and backdate its created_at
	task := &models.Task{
		ID:     "task-old",
		Title:  "Old task",
		Status: "pending",
	}
	if err := repo.Create(task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Backdate created_at to 20 minutes ago
	_, err := db.Exec("UPDATE tasks SET created_at = datetime('now', '-20 minutes') WHERE id = ?", "task-old")
	if err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// Expire with 15 minute timeout
	ids, err := repo.ExpirePendingTasks(15)
	if err != nil {
		t.Fatalf("ExpirePendingTasks: %v", err)
	}
	if len(ids) != 1 || ids[0] != "task-old" {
		t.Errorf("expected [task-old], got %v", ids)
	}

	// Task should now be failed
	got, _ := repo.GetByID("task-old")
	if got.Status != "failed" {
		t.Errorf("expected status=failed, got %s", got.Status)
	}
	if got.Summary == "" {
		t.Error("expected non-empty summary on expired task")
	}
}

func TestExpirePendingTasks_ClaimedNotExpired(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)

	// Create a task, then claim it
	task := &models.Task{
		ID:     "task-claimed-old",
		Title:  "Old claimed task",
		Status: "pending",
	}
	if err := repo.Create(task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.ClaimTask("task-claimed-old", "test-agent"); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	// Backdate
	_, _ = db.Exec("UPDATE tasks SET created_at = datetime('now', '-30 minutes') WHERE id = ?", "task-claimed-old")

	ids, err := repo.ExpirePendingTasks(15)
	if err != nil {
		t.Fatalf("ExpirePendingTasks: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 expired (claimed tasks should not be expired), got %d", len(ids))
	}

	got, _ := repo.GetByID("task-claimed-old")
	if got.Status != "claimed" {
		t.Errorf("expected status=claimed, got %s", got.Status)
	}
}

func TestExpirePendingTasks_MultipleExpired(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)

	for i := 0; i < 5; i++ {
		task := &models.Task{
			ID:     fmt.Sprintf("task-%d", i),
			Title:  fmt.Sprintf("Task %d", i),
			Status: "pending",
		}
		if err := repo.Create(task); err != nil {
			t.Fatalf("Create task-%d: %v", i, err)
		}
	}

	// Backdate first 3 tasks
	for i := 0; i < 3; i++ {
		_, _ = db.Exec("UPDATE tasks SET created_at = datetime('now', '-20 minutes') WHERE id = ?", fmt.Sprintf("task-%d", i))
	}

	ids, err := repo.ExpirePendingTasks(15)
	if err != nil {
		t.Fatalf("ExpirePendingTasks: %v", err)
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 expired, got %d: %v", len(ids), ids)
	}

	// Remaining 2 should still be pending
	for i := 3; i < 5; i++ {
		got, _ := repo.GetByID(fmt.Sprintf("task-%d", i))
		if got.Status != "pending" {
			t.Errorf("task-%d: expected pending, got %s", i, got.Status)
		}
	}
}

func TestExpirePendingTasks_ZeroTimeout(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)

	_, err := repo.ExpirePendingTasks(0)
	if err == nil {
		t.Fatal("expected error for zero timeout, got nil")
	}
	if !strings.Contains(err.Error(), "invalid timeout") {
		t.Errorf("expected error containing 'invalid timeout', got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "0") {
		t.Errorf("expected error to reference value 0, got %q", err.Error())
	}
}

func TestExpirePendingTasks_NegativeTimeout(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)

	_, err := repo.ExpirePendingTasks(-5)
	if err == nil {
		t.Fatal("expected error for negative timeout, got nil")
	}
	if !strings.Contains(err.Error(), "invalid timeout") {
		t.Errorf("expected error containing 'invalid timeout', got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "-5") {
		t.Errorf("expected error to reference value -5, got %q", err.Error())
	}
}