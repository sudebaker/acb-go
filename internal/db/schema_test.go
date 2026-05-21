package db

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestRunMigrations_CreatesTables(t *testing.T) {
	dsn := getTestDSN()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}

	// Clean schema_version to force migration
	db.Exec(`
		DO $$
		BEGIN
			DELETE FROM schema_version;
		EXCEPTION WHEN undefined_table THEN NULL;
		END $$;
	`)

	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}

	tables := []string{"tasks", "gates", "agents", "task_events", "schema_version"}
	for _, name := range tables {
		var count int
		row := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1", name)
		if err := row.Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %s not found after migration", name)
		}
	}

	// Test task_events index exists
	var idxCount int
	row := db.QueryRow("SELECT COUNT(*) FROM pg_indexes WHERE indexname = 'idx_task_events_task'")
	if err := row.Scan(&idxCount); err != nil {
		t.Fatal(err)
	}
	if idxCount != 1 {
		t.Errorf("index idx_task_events_task not found")
	}
}