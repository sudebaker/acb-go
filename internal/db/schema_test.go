package db

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestRunMigrations_CreatesTables(t *testing.T) {
	db, err := sql.Open("sqlite3", t.TempDir()+"/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}

	tables := []string{"tasks", "gates", "agents", "task_events"}
	for _, name := range tables {
		var count int
		row := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name)
		if err := row.Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %s not found after migration", name)
		}
	}

	// Test task_events index exists
	var idxCount int
	row := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_task_events_task'")
	if err := row.Scan(&idxCount); err != nil {
		t.Fatal(err)
	}
	if idxCount != 1 {
		t.Errorf("index idx_task_events_task not found")
	}
}
