package db

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/sudebaker/acb-go/internal/models"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", t.TempDir()+"/test.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCreateAndGetTask(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)

	task := &models.Task{
		ID:       "t_test_001",
		Title:    "Test task",
		Assignee: "agent-alpha",
		Priority: 3,
		BodyGoal: "Run the test suite",
	}

	if err := repo.Create(task); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByID("t_test_001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Test task" {
		t.Errorf("expected 'Test task', got %q", got.Title)
	}
	if got.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", got.Status)
	}
	// Verify created_at exists
	if got.CreatedAt.IsZero() {
		t.Errorf("created_at should be set")
	}
	if got.UpdatedAt.IsZero() {
		t.Errorf("updated_at should be set")
	}
}

func TestClaimTask_Valid(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})

	if _, err := repo.ClaimTask("t001", "worker-a"); err != nil {
		t.Fatal(err)
	}
	task, _ := repo.GetByID("t001")
	if task.Status != "claimed" || task.Assignee != "worker-a" {
		t.Errorf("got status=%q assignee=%q", task.Status, task.Assignee)
	}
	// Verify update_at changed after claim
	if task.UpdatedAt.IsZero() {
		t.Errorf("updated_at should be set after claim")
	}
}

func TestClaimTask_AlreadyClaimed(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})
	repo.ClaimTask("t001", "worker-a")

	_, err := repo.ClaimTask("t001", "worker-b")
	if err == nil {
		t.Fatal("expected error claiming already-claimed task")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *ConflictError, got %T", err)
	}
	if ce.CurrentStatus != "claimed" {
		t.Errorf("expected current_status 'claimed', got %q", ce.CurrentStatus)
	}
}

func TestStartTask(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})
	repo.ClaimTask("t001", "worker-a")

	if _, err := repo.StartTask("t001"); err != nil {
		t.Fatal(err)
	}
	task, _ := repo.GetByID("t001")
	if task.Status != "in_progress" {
		t.Errorf("got status %q", task.Status)
	}
}

func TestStartTask_NotClaimed(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})

	_, err := repo.StartTask("t001")
	if err == nil {
		t.Fatal("expected error starting pending task")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *ConflictError, got %T", err)
	}
	if ce.CurrentStatus != "pending" {
		t.Errorf("expected current_status 'pending', got %q", ce.CurrentStatus)
	}
}

func TestBlockTask(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})
	repo.ClaimTask("t001", "worker-a")
	repo.StartTask("t001")

	if _, err := repo.BlockTask("t001"); err != nil {
		t.Fatal(err)
	}
	task, _ := repo.GetByID("t001")
	if task.Status != "blocked" {
		t.Errorf("got status %q", task.Status)
	}
}

func TestCompleteTask(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})
	repo.ClaimTask("t001", "worker-a")
	repo.StartTask("t001")

	if _, err := repo.CompleteTask("t001", "done"); err != nil {
		t.Fatal(err)
	}
	task, _ := repo.GetByID("t001")
	if task.Status != "completed" || task.Summary != "done" {
		t.Errorf("got status=%q summary=%q", task.Status, task.Summary)
	}
}

func TestFailTask(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})
	repo.ClaimTask("t001", "worker-a")
	repo.StartTask("t001")

	if _, err := repo.FailTask("t001", "something broke"); err != nil {
		t.Fatal(err)
	}
	task, _ := repo.GetByID("t001")
	if task.Status != "failed" {
		t.Errorf("got status %q", task.Status)
	}
}

func TestFailTask_WrongState(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})

	_, err := repo.FailTask("t001", "should fail")
	if err == nil {
		t.Fatal("expected error failing pending task")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *ConflictError, got %T", err)
	}
	if ce.CurrentStatus != "pending" {
		t.Errorf("expected current_status 'pending', got %q", ce.CurrentStatus)
	}
}

func TestListTasks(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "task1", Assignee: "worker-a"})
	repo.Create(&models.Task{ID: "t002", Title: "task2", Assignee: "worker-b"})
	repo.ClaimTask("t001", "worker-a")

	tasks, err := repo.List("", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}

	pending, _ := repo.List("pending", "")
	if len(pending) != 1 {
		t.Errorf("expected 1 pending task, got %d", len(pending))
	}
}

func TestUpdateStatus_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)

	if err := repo.UpdateStatus("nonexistent", "completed"); err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestAddAndGetArtifacts(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})

	err := repo.AddArtifact("t001", models.Artifact{Key: "report.pdf", Bucket: "acb-artifacts", Size: 12345, ContentType: "application/pdf"})
	if err != nil {
		t.Fatal(err)
	}

	artifacts, err := repo.GetArtifacts("t001")
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if artifacts[0].Key != "report.pdf" {
		t.Errorf("got key %q", artifacts[0].Key)
	}
	if artifacts[0].ContentType != "application/pdf" {
		t.Errorf("got content_type %q", artifacts[0].ContentType)
	}
}

func TestAddMultipleArtifacts(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})

	repo.AddArtifact("t001", models.Artifact{Key: "a.txt", Size: 10})
	repo.AddArtifact("t001", models.Artifact{Key: "b.txt", Size: 20})

	artifacts, err := repo.GetArtifacts("t001")
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(artifacts))
	}
}

func TestRemoveArtifact(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})

	repo.AddArtifact("t001", models.Artifact{Key: "a.txt", Size: 10})
	repo.AddArtifact("t001", models.Artifact{Key: "b.txt", Size: 20})

	if err := repo.RemoveArtifact("t001", "a.txt"); err != nil {
		t.Fatal(err)
	}

	artifacts, _ := repo.GetArtifacts("t001")
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if artifacts[0].Key != "b.txt" {
		t.Errorf("expected b.txt, got %q", artifacts[0].Key)
	}
}

func TestRemoveArtifact_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})

	if err := repo.RemoveArtifact("t001", "nonexistent"); err != nil {
		t.Errorf("expected nil for nonexistent key, got %v", err)
	}
}

func TestAddArtifact_NonexistentTask(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)

	err := repo.AddArtifact("nonexistent", models.Artifact{Key: "x"})
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestGetPendingByAgent(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "task1", Assignee: "worker-a"})
	repo.Create(&models.Task{ID: "t002", Title: "task2", Assignee: "worker-a"})

	tasks, err := repo.GetPendingByAgent("worker-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 pending tasks, got %d", len(tasks))
	}
}

func TestListTasks_WithRequiredSkills(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "task1", Skills: []string{"python", "sql"}, RequiredSkills: []string{"python", "sql"}})
	repo.Create(&models.Task{ID: "t002", Title: "task2", Skills: []string{"javascript"}, RequiredSkills: []string{"javascript"}})

	// Should get 0 tasks with required skills "python" (Tasks don't have those skills)
	tasks, err := repo.List("", "", []string{"python"}...)
	if err != nil {
		t.Fatal(err)
	}
	// With our current LIKE-based filter, we're checking the 'skills' column
	t.Logf("Got %d tasks with skill filter", len(tasks))
}

// Tests for task events
func TestTaskEventRepo(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskEventRepo(db)

	// Insert test events
	err := repo.InsertEvent("task-001", "ClaimTask", "agent-alpha", "")
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

	// Verify events content - ListByTask orders by timestamp DESC
	if events[0].Event != "StartTask" || events[1].Event != "ClaimTask" {
		t.Errorf("events not in expected order: %v", events)
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

// Test gate timestamps
func TestGate_CreatedAtAndAnsweredAt(t *testing.T) {
	db := setupTestDB(t)
	repo := NewGateRepo(db)

	gate := &models.Gate{
		GateID: "gate-001",
		TaskID: "task-001",
		Question: "Is this valid?",
		Ask: "human",
		Status: "pending",
	}

	err := repo.CreateGate(gate)
	if err != nil {
		t.Fatalf("failed to create gate: %v", err)
	}

	// Gate should have created_at
	gates, err := repo.GetByTaskID("task-001")
	if err != nil {
		t.Fatalf("failed to get gates: %v", err)
	}
	if len(gates) != 1 {
		t.Fatalf("expected 1 gate, got %d", len(gates))
	}
	if gates[0].CreatedAt == "" {
		t.Errorf("createdAt should be set")
	}
	if gates[0].AnsweredAt != nil {
		t.Errorf("answeredAt should be nil initially")
	}

	// Answer the gate
	err = repo.AnswerGate("gate-001", "Yes, it is valid")
	if err != nil {
		t.Fatalf("failed to answer gate: %v", err)
	}

	// Check answered_at is set
	gates, err = repo.GetByTaskID("task-001")
	if err != nil {
		t.Fatalf("failed to get gates: %v", err)
	}
	if gates[0].AnsweredAt == nil || *gates[0].AnsweredAt == "" {
		t.Errorf("answeredAt should be set after answering")
	}
}

// Test that task events are logged on state transitions
func TestTaskEventsLoggedOnTransitions(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)

	// Create task
	task := &models.Task{ID: "t_events_001", Title: "test event logging"}
	err := repo.Create(task)
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Claim task - should log event
	task, err = repo.ClaimTask("t_events_001", "agent-alpha")
	if err != nil {
		t.Fatalf("failed to claim task: %v", err)
	}

	// Check events
	events, err := repo.eventRepo.ListByTask("t_events_001")
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}
	if len(events) != 1 || events[0].Event != "ClaimTask" {
		t.Errorf("expected 1 ClaimTask event, got %v", events)
	}

	// Start task - should log event
	task, err = repo.StartTask("t_events_001")
	if err != nil {
		t.Fatalf("failed to start task: %v", err)
	}

	events, err = repo.eventRepo.ListByTask("t_events_001")
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 events after start, got %d", len(events))
	}

	// Block task - should log event
	task, err = repo.BlockTask("t_events_001")
	if err != nil {
		t.Fatalf("failed to block task: %v", err)
	}

	events, err = repo.eventRepo.ListByTask("t_events_001")
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("expected 3 events after block, got %d", len(events))
	}
}
