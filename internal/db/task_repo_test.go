package db

import (
	"database/sql"
	"testing"

	"github.com/amphora/acb/internal/models"

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

	if err := repo.ClaimTask("t001", "worker-a"); err != nil {
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

	if err := repo.ClaimTask("t001", "worker-b"); err == nil {
		t.Error("expected error claiming already-claimed task")
	}
}

func TestStartTask(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})
	repo.ClaimTask("t001", "worker-a")

	if err := repo.StartTask("t001"); err != nil {
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

	if err := repo.StartTask("t001"); err == nil {
		t.Error("expected error starting pending task")
	}
}

func TestBlockTask(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTaskRepo(db)
	repo.Create(&models.Task{ID: "t001", Title: "test"})
	repo.ClaimTask("t001", "worker-a")
	repo.StartTask("t001")

	if err := repo.BlockTask("t001"); err != nil {
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

	if err := repo.CompleteTask("t001", "done"); err != nil {
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

	if err := repo.FailTask("t001", "something broke"); err != nil {
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

	if err := repo.FailTask("t001", "should fail"); err == nil {
		t.Error("expected error failing pending task")
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
