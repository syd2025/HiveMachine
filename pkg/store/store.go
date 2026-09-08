// Package store provides SQLite-backed persistent storage for HiveMachine.
package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("record not found")

// ErrInsufficientBalance is returned when an account has insufficient funds.
var ErrInsufficientBalance = errors.New("insufficient balance")

// DB wraps a sqlite database connection.
type DB struct {
	*sql.DB
}

// Open opens (or creates) a SQLite database at the given path.
// Creates the directory if it doesn't exist.
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("store path cannot be empty")
	}
	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path+"?_journal=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	return &DB{db}, nil
}

// OpenDefault opens the default store at ~/.hivemachine/store.db.
func OpenDefault() (*DB, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return Open(filepath.Join(home, ".hivemachine", "store.db"))
}

func migrate(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS balances (
			api_key     TEXT PRIMARY KEY,
			balance     INTEGER NOT NULL DEFAULT 0,
			last_update INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS quotas (
			api_key      TEXT PRIMARY KEY,
			limit_tokens INTEGER NOT NULL DEFAULT 0,
			remaining    INTEGER NOT NULL DEFAULT 0,
			period_end   INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS receipts (
			id                TEXT PRIMARY KEY,
			api_key           TEXT NOT NULL,
			model             TEXT NOT NULL,
			provider_id       TEXT NOT NULL,
			prompt_tokens     INTEGER NOT NULL,
			completion_tokens INTEGER NOT NULL,
			total_tokens      INTEGER NOT NULL,
			cost_cents        INTEGER NOT NULL,
			created_at        INTEGER NOT NULL,
			signature         BLOB
		)`,
		`CREATE INDEX IF NOT EXISTS idx_receipts_api_key ON receipts(api_key)`,
		`CREATE INDEX IF NOT EXISTS idx_receipts_created_at ON receipts(created_at)`,
	}
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return err
		}
	}
	return nil
}
