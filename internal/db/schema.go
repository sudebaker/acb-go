package db

import "database/sql"

func RunMigrations(db *sql.DB) error {
	schema := `
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
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at TEXT NOT NULL DEFAULT (datetime('now')),
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
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	answered_at TEXT DEFAULT NULL
);

CREATE TABLE IF NOT EXISTS agents (
	name TEXT PRIMARY KEY,
	port INTEGER NOT NULL DEFAULT 0,
	token TEXT NOT NULL DEFAULT '',
	last_heartbeat TEXT,
	skills TEXT NOT NULL DEFAULT '[]',
	webhook_url TEXT NOT NULL DEFAULT '',
	webhook_secret TEXT NOT NULL DEFAULT ''
	token_prefix TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS task_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id TEXT NOT NULL REFERENCES tasks(id),
	event TEXT NOT NULL,
	agent TEXT NOT NULL,
	timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
	detail TEXT
);

CREATE INDEX IF NOT EXISTS idx_task_events_task ON task_events(task_id);
	CREATE INDEX IF NOT EXISTS idx_agents_last_heartbeat ON agents(last_heartbeat);
	`
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_agents_token_prefix ON agents(token_prefix)`)

	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Migrations for existing databases that lack newer columns
	migrations := []string{
		`ALTER TABLE agents ADD COLUMN webhook_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE agents ADD COLUMN webhook_secret TEXT NOT NULL DEFAULT ''`,
	}
	for _, m := range migrations {
		// Ignore errors — column may already exist
		db.Exec(m)
	}
	return nil
}
