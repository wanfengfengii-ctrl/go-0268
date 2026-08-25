// Package store provides the transactional SQLite persistence layer used by
// every business package. It owns the schema migrations, connection lifetime,
// and the low-level read/write primitives that the domain services build on.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // register the pure-Go SQLite driver
)

// Store wraps the SQL database and exposes the persistence primitives shared
// by the catalog, task, ledger, withering, and arbiter services.
type Store struct {
	DB *sql.DB
}

// Open opens (or creates) the SQLite database at path, enables WAL and
// foreign keys, and applies all pending migrations. A ":memory:" path is
// supported for tests. If path has a parent directory it is created first.
func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if dir := filepath.Dir(path); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create data dir: %w", err)
			}
		}
	}

	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if path == ":memory:" {
		if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("enable foreign keys: %w", err)
		}
	}
	// Pin the pool to a single connection so per-connection pragmas (foreign
	// keys, busy timeout) and WAL visibility are consistent across all
	// transactions. This also makes the bare ":memory:" database shareable.
	db.SetMaxOpenConns(1)

	s := &Store{DB: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error { return s.DB.Close() }

// migrate applies the versioned migration list inside transactions, guarding
// against concurrent or repeated application via the schema version table.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for i := current; i < schemaVersion && i < len(migrations); i++ {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration: %w", err)
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version(version) VALUES (?)`, i+1); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", i+1, err)
		}
	}
	return nil
}

// withTx runs fn inside a transaction, rolling back on any error and
// returning the commit result. It is the single transaction boundary used by
// all atomic business operations.
func (s *Store) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
