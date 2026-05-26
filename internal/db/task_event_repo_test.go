package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/sudebaker/acb-go/internal/models"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestTaskEventRepo(t *testing.T) {
	dsn := getTestDSN()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}

	// Clean and run migrations
	cleanTables(t, db)
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}

	// Create a parent task first to satisfy FK constraint
	taskRepo := NewTaskRepo(db)
	if err := taskRepo.Create(context.Background(), &models.Task{ID: "task-001", Title: "test task for events"}); err != nil {
		t.Fatalf("failed to create parent task: %v", err)
	}

	repo := NewTaskEventRepo(db)

	// Insert test events
	err = repo.InsertEvent(context.Background(), "task-001", "ClaimTask", "agent-alpha", "")
	if err != nil {
		t.Fatalf("failed to insert event: %v", err)
	}

	err = repo.InsertEvent(context.Background(), "task-001", "StartTask", "", "started processing")
	if err != nil {
		t.Fatalf("failed to insert second event: %v", err)
	}

	// List events
	events, err := repo.ListByTask(context.Background(), "task-001")
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events (CreateTask + ClaimTask + StartTask), got %d", len(events))
	}

	// Verify events content - order may vary if timestamps are identical (same second)
	eventNames := make(map[string]bool)
	for _, e := range events {
		eventNames[e.Event] = true
	}
	if !eventNames["CreateTask"] || !eventNames["StartTask"] || !eventNames["ClaimTask"] {
		t.Errorf("expected CreateTask, StartTask and ClaimTask events, got %v", events)
	}

	// Test non-existent task
	events, err = repo.ListByTask(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error for nonexistent task: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events for nonexistent task, got %d", len(events))
	}
}