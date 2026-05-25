package db

import (
	"database/sql"
	"log"
)

func RunMigrations(db *sql.DB) error {
	// Create schema_version table for proper migration tracking
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS schema_version (
	version INTEGER PRIMARY KEY,
	applied_at TIMESTAMP NOT NULL DEFAULT NOW()
);
	`); err != nil {
		return err
	}

	// Check current version
	var currentVersion int
	row := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&currentVersion); err != nil {
		return err
	}

	type migration struct {
		version int
		sql     string
	}

	migrations := []migration{
		{
			version: 1,
			sql: `
CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	assignee TEXT DEFAULT NULL,
	status TEXT NOT NULL DEFAULT 'pending'
		CHECK(status IN ('pending','claimed','in_progress','blocked','completed','failed')),
	priority INTEGER NOT NULL DEFAULT 3,
	parents TEXT NOT NULL DEFAULT '[]',
	skills TEXT NOT NULL DEFAULT '[]',
	required_skills TEXT NOT NULL DEFAULT '[]',
	tags TEXT NOT NULL DEFAULT '[]',
	body_goal TEXT NOT NULL DEFAULT '',
	body_context TEXT NOT NULL DEFAULT '',
	body_deliverable_format TEXT NOT NULL DEFAULT 'markdown',
	body_deliverable_path TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
	summary TEXT NOT NULL DEFAULT '',
	artifacts_json TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS gates (
	gate_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	question TEXT NOT NULL,
	ask TEXT NOT NULL DEFAULT 'human',
	status TEXT NOT NULL DEFAULT 'pending'
		CHECK(status IN ('pending','asked','answered','resolved')),
	answer TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL DEFAULT NOW(),
	answered_at TIMESTAMP DEFAULT NULL
);

CREATE TABLE IF NOT EXISTS agents (
	name TEXT PRIMARY KEY,
	port INTEGER NOT NULL DEFAULT 0,
	token TEXT NOT NULL DEFAULT '',
	last_heartbeat TIMESTAMP,
	skills TEXT NOT NULL DEFAULT '[]',
	webhook_url TEXT NOT NULL DEFAULT '',
	webhook_secret TEXT NOT NULL DEFAULT '',
	token_prefix TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS task_events (
	id SERIAL PRIMARY KEY,
	task_id TEXT NOT NULL REFERENCES tasks(id),
	event TEXT NOT NULL,
	agent TEXT NOT NULL,
	timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	detail TEXT
);

CREATE INDEX IF NOT EXISTS idx_task_events_task ON task_events(task_id);
CREATE INDEX IF NOT EXISTS idx_agents_last_heartbeat ON agents(last_heartbeat);
CREATE INDEX IF NOT EXISTS idx_agents_token_prefix ON agents(token_prefix);
`,
		},
		{
			version: 2,
			sql: `
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS last_heartbeat TIMESTAMP DEFAULT NULL;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS max_retries INT NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS retry_count INT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_tasks_last_heartbeat ON tasks(last_heartbeat);
`,
		},
	}

	for _, m := range migrations {
		if m.version <= currentVersion {
			continue
		}
		log.Printf("[DB] Applying migration version %d", m.version)
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.sql); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES ($1) ON CONFLICT DO NOTHING`, m.version); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		log.Printf("[DB] Migration version %d applied", m.version)
	}

	return nil
}