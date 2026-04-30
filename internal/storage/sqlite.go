package storage

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

// ErrNotFound is returned when a review record does not exist.
var ErrNotFound = errors.New("review not found")

// defaultDatabasePath returns the default location for the SQLite database file.
// It checks NANO_DATA_DIR env var; if set, returns <NANO_DATA_DIR>/reviews.db.
func defaultDatabasePath() string {
	if dir := os.Getenv("NANO_DATA_DIR"); dir != "" {
		return filepath.Join(dir, "reviews.db")
	}
	return "/app/data/reviews.db"
}

type sqliteStore struct {
	db *sql.DB
}

// Open creates (or opens) the SQLite database at the given path,
// runs schema migrations, and returns a *sqliteStore.
// If dbPath is empty, defaultDatabasePath() is used.
func Open(dbPath string) (*sqliteStore, error) {
	if dbPath == "" {
		dbPath = defaultDatabasePath()
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create database directory %s: %w", filepath.Dir(dbPath), err)
	}

	dsn := fmt.Sprintf("file:%s?mode=rwc&_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on", dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", dbPath, err)
	}

	// SQLite: one writer at a time. WAL mode allows concurrent reads.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) CleanupStaleReviews(ctx context.Context) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx, cleanupStaleSQL, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}
