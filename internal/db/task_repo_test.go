package db

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/sudebaker/acb-go/internal/models"

	_ "github.com/mattn/go-sqlite3"
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
