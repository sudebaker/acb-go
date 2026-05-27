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

func TestTaskEventRepo_ListSince(t *testing.T) {
	dsn := getTestDSN()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}

	cleanTables(t, db)
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}

	taskRepo := NewTaskRepo(db)
	taskRepo.Create(context.Background(), &models.Task{ID: "task-002", Title: "test task for events"})
	taskRepo.ClaimTask(context.Background(), "task-002", "agent-alpha")
	taskRepo.StartTask(context.Background(), "task-002")
	taskRepo.CompleteTask(context.Background(), "task-002", "done")

	repo := NewTaskEventRepo(db)

	// ListSince with a past timestamp should find all events
	events, err := repo.ListSince(context.Background(), "2020-01-01T00:00:00Z", "", 100, 0, false)
	if err != nil {
		t.Fatalf("ListSince failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got 0")
	}

	// Filter by agent should only return matching events + global events
	events, err = repo.ListSince(context.Background(), "2020-01-01T00:00:00Z", "agent-alpha", 100, 0, false)
	if err != nil {
		t.Fatalf("ListSince with agent failed: %v", err)
	}
	if len(events) < 1 {
		t.Fatal("expected at least 1 event for agent-alpha")
	}
	for _, e := range events {
		if e.Agent != "agent-alpha" && e.Agent != "" {
			t.Errorf("expected agent-alpha or empty agent, got %q for event %s", e.Agent, e.Event)
		}
	}

	// ListSince with future timestamp should return empty
	events, err = repo.ListSince(context.Background(), "2099-01-01T00:00:00Z", "", 100, 0, false)
	if err != nil {
		t.Fatalf("ListSince failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events for future timestamp, got %d", len(events))
	}

	// ListSince with limit
	events, err = repo.ListSince(context.Background(), "2020-01-01T00:00:00Z", "", 2, 0, false)
	if err != nil {
		t.Fatalf("ListSince with limit failed: %v", err)
	}
	if len(events) > 2 {
		t.Errorf("expected at most 2 events with limit=2, got %d", len(events))
	}

	// ListSince with afterID=0 should return all events
	events, err = repo.ListSince(context.Background(), "", "", 100, 0, true)
	if err != nil {
		t.Fatalf("ListSince with afterID=0 failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events with afterID=0, got 0")
	}
	var maxID int64
	for _, e := range events {
		if e.ID > maxID {
			maxID = e.ID
		}
	}

	// ListSince with afterID = maxID should return empty
	events, err = repo.ListSince(context.Background(), "", "", 100, maxID, true)
	if err != nil {
		t.Fatalf("ListSince with afterID=%d failed: %v", maxID, err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events after maxID, got %d", len(events))
	}

	// ListSince with afterID + agent filter
	events, err = repo.ListSince(context.Background(), "", "agent-alpha", 100, 0, true)
	if err != nil {
		t.Fatalf("ListSince with afterID + agent failed: %v", err)
	}
	for _, e := range events {
		if e.Agent != "agent-alpha" && e.Agent != "" {
			t.Errorf("expected agent-alpha or empty agent, got %q for event %s", e.Agent, e.Event)
		}
	}

	// ListSince with afterID + limit
	events, err = repo.ListSince(context.Background(), "", "", 2, 0, true)
	if err != nil {
		t.Fatalf("ListSince with afterID + limit failed: %v", err)
	}
	if len(events) > 2 {
		t.Errorf("expected at most 2 events with afterID + limit=2, got %d", len(events))
	}
}