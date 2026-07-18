package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"queuectl/internal/domain"
	"queuectl/internal/repository"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// txKeyType is a unique private type for context keys to prevent collisions.
type txKeyType struct{}

var txKey = txKeyType{}

// sqliteJobRepository implements repository.JobRepository.
type sqliteJobRepository struct {
	db *sql.DB
}

// NewSQLiteJobRepository creates and configures a new SQLite job repository.
func NewSQLiteJobRepository(db *sql.DB) repository.JobRepository {
	return &sqliteJobRepository{
		db: db,
	}
}

// getExecutor returns either the active transaction from context or the default DB connection.
func (r *sqliteJobRepository) getExecutor(ctx context.Context) executor {
	if tx, ok := ctx.Value(txKey).(*sql.Tx); ok {
		return tx
	}
	return r.db
}

// executor defines common database operations implemented by both *sql.DB and *sql.Tx.
type executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// WithTx starts a transaction and runs the provided function.
func (r *sqliteJobRepository) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	// If there's already an active transaction, reuse it (nested transaction support is ignored/flattend)
	if _, ok := ctx.Value(txKey).(*sql.Tx); ok {
		return fn(ctx)
	}

	// SQLite benefits from BEGIN IMMEDIATE to prevent deadlock on concurrent write transactions
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p) // re-throw panic after rollback
		}
	}()

	txCtx := context.WithValue(ctx, txKey, tx)

	// Force SQLite to acquire a write lock immediately to prevent deadlocks during lock upgrades
	if _, err := tx.ExecContext(txCtx, "UPDATE jobs SET id = id WHERE 1=0"); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed to upgrade SQLite transaction lock: %w", err)
	}

	if err := fn(txCtx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("transaction failed: %v, rollback failed: %w", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Insert persists a new job to SQLite.
func (r *sqliteJobRepository) Insert(ctx context.Context, job *domain.Job) error {
	const query = `
		INSERT INTO jobs (id, type, payload, queue, status, priority, max_retries, retries_count, error_message, run_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	exec := r.getExecutor(ctx)
	_, err := exec.ExecContext(ctx, query,
		job.ID,
		job.Type,
		job.Payload,
		job.Queue,
		string(job.Status),
		job.Priority,
		job.MaxRetries,
		job.RetriesCount,
		job.ErrorMessage,
		job.RunAt,
		job.CreatedAt,
		job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert job %s: %w", job.ID, err)
	}
	return nil
}

// GetByID retrieves a job by ID.
func (r *sqliteJobRepository) GetByID(ctx context.Context, id string) (*domain.Job, error) {
	const query = `
		SELECT id, type, payload, queue, status, priority, max_retries, retries_count, error_message, run_at, created_at, updated_at
		FROM jobs
		WHERE id = ?
	`
	exec := r.getExecutor(ctx)
	row := exec.QueryRowContext(ctx, query, id)

	var job domain.Job
	var statusStr string
	err := row.Scan(
		&job.ID,
		&job.Type,
		&job.Payload,
		&job.Queue,
		&statusStr,
		&job.Priority,
		&job.MaxRetries,
		&job.RetriesCount,
		&job.ErrorMessage,
		&job.RunAt,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("job not found: %s", id)
		}
		return nil, fmt.Errorf("failed to scan job %s: %w", id, err)
	}
	job.Status = domain.JobStatus(statusStr)
	return &job, nil
}

// Update updates job attributes in SQLite using Optimistic Concurrency Control.
func (r *sqliteJobRepository) Update(ctx context.Context, job *domain.Job) error {
	const query = `
		UPDATE jobs
		SET status = ?, retries_count = ?, error_message = ?, run_at = ?, updated_at = ?
		WHERE id = ? AND updated_at = ?
	`
	exec := r.getExecutor(ctx)
	oldUpdatedAt := job.UpdatedAt.UTC()
	newUpdatedAt := time.Now().UTC()

	res, err := exec.ExecContext(ctx, query,
		string(job.Status),
		job.RetriesCount,
		job.ErrorMessage,
		job.RunAt,
		newUpdatedAt,
		job.ID,
		oldUpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update job %s: %w", job.ID, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected for update job %s: %w", job.ID, err)
	}
	if rows == 0 {
		// Double check if the row exists at all to differentiate between Not Found and Concurrency Conflict
		var exists int
		checkErr := exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs WHERE id = ?", job.ID).Scan(&exists)
		if checkErr == nil && exists > 0 {
			return repository.ErrConcurrencyConflict
		}
		return fmt.Errorf("job to update not found: %s", job.ID)
	}

	// Update domain model timestamp on success
	job.UpdatedAt = newUpdatedAt
	return nil
}

// AcquireNextPendingJob finds the next eligible job, marks it as running, and returns it.
// This is completed atomically inside a SQLite immediate transaction.
func (r *sqliteJobRepository) AcquireNextPendingJob(ctx context.Context, queue string) (*domain.Job, error) {
	var acquiredJob *domain.Job

	err := r.WithTx(ctx, func(txCtx context.Context) error {
		const selectQuery = `
			SELECT id, type, payload, queue, status, priority, max_retries, retries_count, error_message, run_at, created_at, updated_at
			FROM jobs
			WHERE queue = ? AND status = ? AND run_at <= ?
			ORDER BY priority DESC, run_at ASC, created_at ASC
			LIMIT 1
		`
		now := time.Now().UTC()
		exec := r.getExecutor(txCtx)

		row := exec.QueryRowContext(txCtx, selectQuery, queue, string(domain.StatusPending), now)

		var job domain.Job
		var statusStr string
		err := row.Scan(
			&job.ID,
			&job.Type,
			&job.Payload,
			&job.Queue,
			&statusStr,
			&job.Priority,
			&job.MaxRetries,
			&job.RetriesCount,
			&job.ErrorMessage,
			&job.RunAt,
			&job.CreatedAt,
			&job.UpdatedAt,
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil // No pending jobs to acquire
			}
			return fmt.Errorf("failed to fetch pending job: %w", err)
		}

		job.Status = domain.JobStatus(statusStr)

		// Atomically mark the job as running
		const updateQuery = `
			UPDATE jobs
			SET status = ?, updated_at = ?
			WHERE id = ?
		`
		_, err = exec.ExecContext(txCtx, updateQuery, string(domain.StatusRunning), now, job.ID)
		if err != nil {
			return fmt.Errorf("failed to mark job as running: %w", err)
		}

		job.Status = domain.StatusRunning
		job.UpdatedAt = now
		acquiredJob = &job
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to acquire next pending job: %w", err)
	}

	return acquiredJob, nil
}

// ListJobs returns list of jobs filtering by queue, status, and search query.
func (r *sqliteJobRepository) ListJobs(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error) {
	var query = `
		SELECT id, type, payload, queue, status, priority, max_retries, retries_count, error_message, run_at, created_at, updated_at
		FROM jobs
		WHERE 1=1
	`
	var args []any

	if queue != "" {
		query += " AND queue = ?"
		args = append(args, queue)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, string(status))
	}
	if search != "" {
		query += " AND (payload LIKE ? OR error_message LIKE ? OR type LIKE ?)"
		pattern := "%" + search + "%"
		args = append(args, pattern, pattern, pattern)
	}

	query += " ORDER BY priority DESC, created_at DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	exec := r.getExecutor(ctx)
	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*domain.Job
	for rows.Next() {
		var job domain.Job
		var statusStr string
		err := rows.Scan(
			&job.ID,
			&job.Type,
			&job.Payload,
			&job.Queue,
			&statusStr,
			&job.Priority,
			&job.MaxRetries,
			&job.RetriesCount,
			&job.ErrorMessage,
			&job.RunAt,
			&job.CreatedAt,
			&job.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan query job row: %w", err)
		}
		job.Status = domain.JobStatus(statusStr)
		jobs = append(jobs, &job)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading query job rows: %w", err)
	}

	return jobs, nil
}

// GetQueueStats aggregates job status counts grouped by queue.
func (r *sqliteJobRepository) GetQueueStats(ctx context.Context) (map[string]map[domain.JobStatus]int, error) {
	const query = `
		SELECT queue, status, COUNT(*) as count
		FROM jobs
		GROUP BY queue, status
	`
	exec := r.getExecutor(ctx)
	rows, err := exec.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query queue stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]map[domain.JobStatus]int)
	for rows.Next() {
		var queue string
		var statusStr string
		var count int
		if err := rows.Scan(&queue, &statusStr, &count); err != nil {
			return nil, fmt.Errorf("failed to scan queue stats row: %w", err)
		}

		status := domain.JobStatus(statusStr)
		if _, ok := stats[queue]; !ok {
			stats[queue] = make(map[domain.JobStatus]int)
		}
		stats[queue][status] = count
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading queue stats rows: %w", err)
	}

	return stats, nil
}

// DeleteCompletedJobs deletes jobs that are completed and older than a specific duration.
func (r *sqliteJobRepository) DeleteCompletedJobs(ctx context.Context, olderThan time.Duration) (int64, error) {
	const query = `
		DELETE FROM jobs
		WHERE status = ? AND updated_at < ?
	`
	cutoffTime := time.Now().Add(-olderThan)
	exec := r.getExecutor(ctx)
	res, err := exec.ExecContext(ctx, query, string(domain.StatusCompleted), cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to delete completed jobs: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to check rows affected for completed jobs deletion: %w", err)
	}

	return rows, err
}

// Delete removes a job by its ID.
func (r *sqliteJobRepository) Delete(ctx context.Context, id string) error {
	const query = `DELETE FROM jobs WHERE id = ?`
	exec := r.getExecutor(ctx)
	res, err := exec.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete job %s: %w", id, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected for delete job %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("job to delete not found: %s", id)
	}
	return nil
}
