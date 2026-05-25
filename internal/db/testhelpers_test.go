package db

import (
	"database/sql"
	"os"
	"testing"

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

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := getTestDSN()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	// Clean all tables before each test for isolation
	cleanTables(t, db)
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanTables(t, db)
		db.Close()
	})
	return db
}

func cleanTables(t *testing.T, db *sql.DB) {
	t.Helper()
	// Delete all data but keep the schema; use DO block to handle missing tables gracefully
	_, err := db.Exec(`
		DO $$
		BEGIN
			DELETE FROM task_events;
		EXCEPTION WHEN undefined_table THEN NULL;
		END $$;
	`)
	if err != nil {
		t.Logf("cleanTables: task_events: %v", err)
	}
	_, err = db.Exec(`
		DO $$
		BEGIN
			DELETE FROM gates;
		EXCEPTION WHEN undefined_table THEN NULL;
		END $$;
	`)
	if err != nil {
		t.Logf("cleanTables: gates: %v", err)
	}
	_, err = db.Exec(`
		DO $$
		BEGIN
			DELETE FROM agents;
		EXCEPTION WHEN undefined_table THEN NULL;
		END $$;
	`)
	if err != nil {
		t.Logf("cleanTables: agents: %v", err)
	}
	_, err = db.Exec(`
		DO $$
		BEGIN
			DELETE FROM tasks;
		EXCEPTION WHEN undefined_table THEN NULL;
		END $$;
	`)
	if err != nil {
		t.Logf("cleanTables: tasks: %v", err)
	}
	// Reset schema_version so migrations run fresh
	_, _ = db.Exec(`
		DO $$
		BEGIN
			DELETE FROM schema_version;
		EXCEPTION WHEN undefined_table THEN NULL;
		END $$;
	`)
}