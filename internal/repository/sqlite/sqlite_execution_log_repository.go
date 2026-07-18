package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"queuectl/internal/domain"
	"queuectl/internal/repository"
)

type sqliteExecutionLogRepository struct {
	db *sql.DB
}

// NewSQLiteExecutionLogRepository instantiates a SQLite execution log repository.
func NewSQLiteExecutionLogRepository(db *sql.DB) repository.ExecutionLogRepository {
	return &sqliteExecutionLogRepository{
		db: db,
	}
}

func (r *sqliteExecutionLogRepository) getExecutor(ctx context.Context) executor {
	if tx, ok := ctx.Value(txKey).(*sql.Tx); ok {
		return tx
	}
	return r.db
}

// Insert persists a new job attempt execution log.
func (r *sqliteExecutionLogRepository) Insert(ctx context.Context, l *domain.ExecutionLog) error {
	const query = `
		INSERT INTO execution_logs (id, job_id, worker_id, attempt, status, started_at, finished_at, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	exec := r.getExecutor(ctx)
	_, err := exec.ExecContext(ctx, query,
		l.ID,
		l.JobID,
		l.WorkerID,
		l.Attempt,
		string(l.Status),
		l.StartedAt,
		l.FinishedAt,
		l.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("failed to insert execution log %s: %w", l.ID, err)
	}
	return nil
}

// Update records the finished state and execution outcome of an attempt.
func (r *sqliteExecutionLogRepository) Update(ctx context.Context, l *domain.ExecutionLog) error {
	const query = `
		UPDATE execution_logs
		SET status = ?, finished_at = ?, error_message = ?
		WHERE id = ?
	`
	exec := r.getExecutor(ctx)
	res, err := exec.ExecContext(ctx, query,
		string(l.Status),
		l.FinishedAt,
		l.ErrorMessage,
		l.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update execution log %s: %w", l.ID, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check updated rows count for execution log %s: %w", l.ID, err)
	}
	if rows == 0 {
		return fmt.Errorf("execution log to update not found: %s", l.ID)
	}

	return nil
}

// GetByJobID returns execution history logs for a specific job.
func (r *sqliteExecutionLogRepository) GetByJobID(ctx context.Context, jobID string) ([]*domain.ExecutionLog, error) {
	const query = `
		SELECT id, job_id, worker_id, attempt, status, started_at, finished_at, error_message
		FROM execution_logs
		WHERE job_id = ?
		ORDER BY attempt ASC
	`
	exec := r.getExecutor(ctx)
	rows, err := exec.QueryContext(ctx, query, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to query execution logs by job %s: %w", jobID, err)
	}
	defer rows.Close()

	var logs []*domain.ExecutionLog
	for rows.Next() {
		var l domain.ExecutionLog
		var statusStr string
		err := rows.Scan(
			&l.ID,
			&l.JobID,
			&l.WorkerID,
			&l.Attempt,
			&statusStr,
			&l.StartedAt,
			&l.FinishedAt,
			&l.ErrorMessage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan execution log row: %w", err)
		}
		l.Status = domain.ExecutionStatus(statusStr)
		logs = append(logs, &l)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading execution log rows: %w", err)
	}

	return logs, nil
}

// GetAverageRuntime calculates the average duration of successful job executions in SQLite.
func (r *sqliteExecutionLogRepository) GetAverageRuntime(ctx context.Context) (time.Duration, error) {
	// julianday computes date difference in days. Multiply by 86400 to get seconds.
	const query = `
		SELECT COALESCE(AVG((julianday(substr(finished_at, 1, 19)) - julianday(substr(started_at, 1, 19))) * 86400), 0)
		FROM execution_logs
		WHERE status = ?
	`
	exec := r.getExecutor(ctx)
	var avgSeconds float64
	err := exec.QueryRowContext(ctx, query, string(domain.ExecutionSuccess)).Scan(&avgSeconds)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate average runtime: %w", err)
	}
	return time.Duration(avgSeconds * float64(time.Second)), nil
}

// GetStats aggregates the count of successful and failed execution attempts in SQLite.
func (r *sqliteExecutionLogRepository) GetStats(ctx context.Context) (successCount int, failedCount int, err error) {
	const query = `
		SELECT status, COUNT(*)
		FROM execution_logs
		GROUP BY status
	`
	exec := r.getExecutor(ctx)
	rows, err := exec.QueryContext(ctx, query)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query execution log stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var statusStr string
		var count int
		if err := rows.Scan(&statusStr, &count); err != nil {
			return 0, 0, fmt.Errorf("failed to scan execution log stats row: %w", err)
		}

		switch domain.ExecutionStatus(statusStr) {
		case domain.ExecutionSuccess:
			successCount = count
		case domain.ExecutionFailed:
			failedCount = count
		}
	}

	if err = rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("error reading execution log stats rows: %w", err)
	}

	return successCount, failedCount, nil
}
