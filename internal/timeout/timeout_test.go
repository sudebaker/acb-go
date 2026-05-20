package timeout

import (
	"testing"
	"time"

	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/models"
)

func TestPendingTimeoutService_Disabled(t *testing.T) {
	// Create in-memory DB
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	db.RunMigrations(database)
	repo := db.NewTaskRepo(database)

	// timeout=0 means disabled — should not start goroutine
	svc := NewPendingTimeoutService(repo, 0, 1*time.Second)
	svc.Start()
	svc.Stop()
	// If we get here without panic, disabled mode works
}

func TestStop_WaitsForGoroutine(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	db.RunMigrations(database)
	repo := db.NewTaskRepo(database)

	svc := NewPendingTimeoutService(repo, 15, 1*time.Second)
	svc.Start()

	// Use a channel to detect when Stop() returns.
	goroutineDone := make(chan struct{})

	go func() {
		svc.Stop()
		close(goroutineDone)
	}()

	select {
	case <-goroutineDone:
		// Stop() returned — wg.Wait() completed, goroutine exited
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return within timeout — wg.Wait() may be broken")
	}
}

func TestPendingTimeoutService_RunsCheck(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	db.RunMigrations(database)
	repo := db.NewTaskRepo(database)

	// Create a task and backdate it
	task := &models.Task{
		ID:     "timeout-test-1",
		Title:  "Should expire",
		Status: "pending",
	}
	if err := repo.Create(task); err != nil {
		t.Fatal(err)
	}
	// Backdate created_at
	database.Exec("UPDATE tasks SET created_at = datetime('now', '-20 minutes') WHERE id = ?", "timeout-test-1")

	// Start service with 15 min timeout, 500ms check
	svc := NewPendingTimeoutService(repo, 15, 500*time.Millisecond)
	svc.Start()
	defer svc.Stop()

	// Wait for a check cycle
	time.Sleep(1 * time.Second)

	got, _ := repo.GetByID("timeout-test-1")
	if got.Status != "failed" {
		t.Errorf("expected task to be expired (failed), got status=%s", got.Status)
	}
}