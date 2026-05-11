package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

func enableWAL(db *sql.DB) error {
	_, err := db.Exec("PRAGMA journal_mode=WAL")
	if err != nil {
		return fmt.Errorf("enable WAL: %w", err)
	}
	_, err = db.Exec("PRAGMA busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("set busy_timeout: %w", err)
	}
	return nil
}

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := enableWAL(db); err != nil {
		return nil, err
	}
	return db, nil
}
