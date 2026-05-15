package db

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestTaskEventRepo(t *testing.T) {
	db, err := sql.Open("sqlite3", t.TempDir()+"/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}

	repo := NewTaskEventRepo(db)

	// Insert test events
	err = repo.InsertEvent("task-001", "ClaimTask", "agent-alpha", "")
	if err != nil {
		t.Fatalf("failed to insert event: %v", err)
	}

	err = repo.InsertEvent("task-001", "StartTask", "", "started processing")
	if err != nil {
		t.Fatalf("failed to insert second event: %v", err)
	}

	// List events
	events, err := repo.ListByTask("task-001")
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	// Verify events content - order may vary if timestamps are identical (same second)
	eventNames := make(map[string]bool)
	for _, e := range events {
		eventNames[e.Event] = true
	}
	if !eventNames["StartTask"] || !eventNames["ClaimTask"] {
		t.Errorf("expected both StartTask and ClaimTask events, got %v", events)
	}

	// Test non-existent task
	events, err = repo.ListByTask("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error for nonexistent task: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events for nonexistent task, got %d", len(events))
	}
}
