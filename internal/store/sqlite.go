package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schemaVersion = 1

// Store owns the SQLite connection pool and schema lifecycle.
type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path must not be empty")
	}
	if path != ":memory:" && path != "file::memory:?cache=shared" {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	dsn := path
	if path == ":memory:" {
		dsn = "file:controller-memory?mode=memory&cache=shared"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	s := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping SQLite database: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable SQLite foreign keys: %w", err)
	}
	if path != ":memory:" && path != "file::memory:?cache=shared" {
		if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("enable SQLite WAL: %w", err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

// Migrate applies each schema version in a transaction and records its version.
func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema migration table: %w", err)
	}
	var current int
	err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current > schemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", current, schemaVersion)
	}
	if current < 1 {
		statements := []string{
			`CREATE TABLE workers (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				endpoint TEXT NOT NULL,
				state TEXT NOT NULL,
				last_heartbeat TEXT,
				available_memory_bytes INTEGER NOT NULL DEFAULT 0 CHECK (available_memory_bytes >= 0),
				memory_limit_bytes INTEGER NOT NULL CHECK (memory_limit_bytes > 0),
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE TABLE vms (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				worker_id TEXT REFERENCES workers(id),
				vcpus INTEGER NOT NULL CHECK (vcpus > 0),
				memory_bytes INTEGER NOT NULL CHECK (memory_bytes > 0),
				image_id TEXT NOT NULL,
				desired_state TEXT NOT NULL,
				observed_state TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE TABLE operations (
				id TEXT PRIMARY KEY,
				vm_id TEXT NOT NULL REFERENCES vms(id),
				type TEXT NOT NULL,
				idempotency_key TEXT UNIQUE,
				state TEXT NOT NULL,
				attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
				error_code TEXT,
				error_message TEXT,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE TABLE reservations (
				vm_id TEXT PRIMARY KEY REFERENCES vms(id),
				worker_id TEXT NOT NULL REFERENCES workers(id),
				memory_bytes INTEGER NOT NULL CHECK (memory_bytes > 0),
				state TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE INDEX operations_vm_id_idx ON operations(vm_id)`,
			`CREATE INDEX operations_state_idx ON operations(state)`,
			`CREATE INDEX reservations_worker_state_idx ON reservations(worker_id, state)`,
			`INSERT INTO schema_migrations(version, applied_at) VALUES (1, '` + time.Now().UTC().Format(time.RFC3339Nano) + `')`,
		}
		for _, statement := range statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply schema version 1: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration: %w", err)
	}
	return nil
}

// WithTx executes fn in a serializable transaction. Any returned error rolls back.
func (s *Store) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	if fn == nil {
		return errors.New("transaction callback must not be nil")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
