package timeout

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/models"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func getTestDSN() string {
	host := os.Getenv("ACB_PG_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ACB_PG_PORT")
	if port == "" {
		port = "5433"
	}
	user := os.Getenv("ACB_PG_USER")
	if user == "" {
		user = "acb"
	}
	password := os.Getenv("ACB_PG_PASSWORD")
	if password == "" {
		password = "acb-secure-pg-pass-2026"
	}
	database := os.Getenv("ACB_PG_DATABASE")
	if database == "" {
		database = "acb"
	}
	return "host=" + host + " port=" + port + " user=" + user + " password=" + password + " dbname=" + database + " sslmode=disable"
}

func setupTimeoutTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := getTestDSN()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Ping(); err != nil {
		t.Fatal(err)
	}
	// Clean tables gracefully (may not exist yet)
	tables := []string{"task_events", "gates", "agents", "tasks", "schema_version"}
	for _, table := range tables {
		database.Exec("DELETE FROM " + table)
	}
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		tables := []string{"task_events", "gates", "agents", "tasks"}
		for _, table := range tables {
			database.Exec("DELETE FROM " + table)
		}
		database.Close()
	})
	return database
}

func TestTimeoutService_Disabled(t *testing.T) {
	database := setupTimeoutTestDB(t)
	repo := db.NewTaskRepo(database)

	// timeout=0 means disabled — should not start goroutine
	svc := NewTimeoutService(repo, nil, 0, 0, 0, 1*time.Second)
	svc.Start()
	svc.Stop()
	// If we get here without panic, disabled mode works
}

func TestStop_WaitsForGoroutine(t *testing.T) {
	database := setupTimeoutTestDB(t)
	repo := db.NewTaskRepo(database)

	svc := NewTimeoutService(repo, nil, 15, 0, 0, 500*time.Millisecond)
	svc.Start()
	svc.Stop()
	// If we get here without panic, Stop() waited for goroutine
}

func TestPendingTimeoutService_RunsCheck(t *testing.T) {
	database := setupTimeoutTestDB(t)
	repo := db.NewTaskRepo(database)

	task := &models.Task{
		ID:       "timeout-test-1",
		Title:    "Test timeout",
		Status:   "pending",
		Priority: 3,
	}
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	// Backdate created_at (PostgreSQL syntax)
	database.Exec("UPDATE tasks SET created_at = NOW() - interval '20 minutes' WHERE id = $1", "timeout-test-1")

	// Start service with 15 min timeout, 500ms check
	svc := NewTimeoutService(repo, nil, 15, 0, 0, 500*time.Millisecond)
	svc.Start()
	defer svc.Stop()

	// Poll for the task to be expired — check every 50ms for up to 3 seconds
	var got *models.Task
	for i := 0; i < 60; i++ {
		time.Sleep(50 * time.Millisecond)
		got, _ = repo.GetByID(context.Background(), "timeout-test-1")
		if got != nil && got.Status == "failed" {
			break
		}
	}
	if got == nil {
		t.Fatal("task not found")
	}
	if got.Status != "failed" {
		t.Errorf("expected task to be expired (failed), got status=%s", got.Status)
	}
}