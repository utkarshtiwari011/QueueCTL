package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// Schema holds the DDL statements for creating the jobs table.
const Schema = `
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    payload TEXT NOT NULL,
    queue TEXT NOT NULL,
    status TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL,
    retries_count INTEGER NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    run_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_jobs_status_priority_run_at ON jobs (status, priority DESC, run_at);
CREATE INDEX IF NOT EXISTS idx_jobs_queue_status_priority_run_at ON jobs (queue, status, priority DESC, run_at);

CREATE TABLE IF NOT EXISTS workers (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    queue TEXT NOT NULL,
    concurrency INTEGER NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    last_heartbeat DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS execution_logs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    worker_id TEXT NOT NULL,
    attempt INTEGER NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    finished_at DATETIME NOT NULL,
    error_message TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_execution_logs_job_id ON execution_logs (job_id);
`

// Connect opens and configures a SQLite connection with production pragmas.
func Connect(dbPath string) (*sql.DB, error) {
	if dbPath != ":memory:" && !strings.HasPrefix(dbPath, "file::memory:") {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory for %s: %w", dbPath, err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database at %s: %w", dbPath, err)
	}

	// Enable Write-Ahead Logging (WAL) for high concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	// Prevent "database is locked" errors under concurrent writes
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Set synchronous to NORMAL for speed and durability under WAL
	if _, err := db.Exec("PRAGMA synchronous=NORMAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to set synchronous NORMAL: %w", err)
	}

	// Enable foreign key constraint enforcement
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to set foreign keys ON: %w", err)
	}

	// Optimize connection pool limits for SQLite single-writer model
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	return db, nil
}

// RunMigrations executes the initial schema migrations.
func RunMigrations(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, Schema)
	if err != nil {
		return fmt.Errorf("failed to run database migrations: %w", err)
	}
	return nil
}
