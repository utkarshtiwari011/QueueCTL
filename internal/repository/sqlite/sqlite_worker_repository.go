package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"queuectl/internal/domain"
	"queuectl/internal/repository"
)

type sqliteWorkerRepository struct {
	db *sql.DB
}

// NewSQLiteWorkerRepository instantiates a SQLite worker repository.
func NewSQLiteWorkerRepository(db *sql.DB) repository.WorkerRepository {
	return &sqliteWorkerRepository{
		db: db,
	}
}

func (r *sqliteWorkerRepository) getExecutor(ctx context.Context) executor {
	if tx, ok := ctx.Value(txKey).(*sql.Tx); ok {
		return tx
	}
	return r.db
}

// Upsert performs insert or atomic updates on worker registries.
func (r *sqliteWorkerRepository) Upsert(ctx context.Context, w *domain.Worker) error {
	const query = `
		INSERT INTO workers (id, hostname, queue, concurrency, status, started_at, last_heartbeat)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			last_heartbeat = excluded.last_heartbeat
	`
	exec := r.getExecutor(ctx)
	_, err := exec.ExecContext(ctx, query,
		w.ID,
		w.Hostname,
		w.Queue,
		w.Concurrency,
		string(w.GetStatus()),
		w.StartedAt,
		w.GetLastHeartbeat(),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert worker node %s: %w", w.ID, err)
	}
	return nil
}

// GetByID returns the registered worker parameters.
func (r *sqliteWorkerRepository) GetByID(ctx context.Context, id string) (*domain.Worker, error) {
	const query = `
		SELECT id, hostname, queue, concurrency, status, started_at, last_heartbeat
		FROM workers
		WHERE id = ?
	`
	exec := r.getExecutor(ctx)
	row := exec.QueryRowContext(ctx, query, id)

	var w domain.Worker
	var statusStr string
	err := row.Scan(
		&w.ID,
		&w.Hostname,
		&w.Queue,
		&w.Concurrency,
		&statusStr,
		&w.StartedAt,
		&w.LastHeartbeat,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("worker not found: %s", id)
		}
		return nil, fmt.Errorf("failed to fetch worker row %s: %w", id, err)
	}
	w.Status = domain.WorkerStatus(statusStr)
	return &w, nil
}

// Delete removes a worker node.
func (r *sqliteWorkerRepository) Delete(ctx context.Context, id string) error {
	const query = `DELETE FROM workers WHERE id = ?`
	exec := r.getExecutor(ctx)
	res, err := exec.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete worker row %s: %w", id, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check deleted rows count for worker %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("worker to delete not found: %s", id)
	}
	return nil
}

// ListActive lists workers running or ready to poll.
func (r *sqliteWorkerRepository) ListActive(ctx context.Context) ([]*domain.Worker, error) {
	const query = `
		SELECT id, hostname, queue, concurrency, status, started_at, last_heartbeat
		FROM workers
		WHERE status != ?
		ORDER BY started_at ASC
	`
	exec := r.getExecutor(ctx)
	rows, err := exec.QueryContext(ctx, query, string(domain.WorkerStatusStopped))
	if err != nil {
		return nil, fmt.Errorf("failed to query active workers list: %w", err)
	}
	defer rows.Close()

	var workers []*domain.Worker
	for rows.Next() {
		var w domain.Worker
		var statusStr string
		err := rows.Scan(
			&w.ID,
			&w.Hostname,
			&w.Queue,
			&w.Concurrency,
			&statusStr,
			&w.StartedAt,
			&w.LastHeartbeat,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan worker row: %w", err)
		}
		w.Status = domain.WorkerStatus(statusStr)
		workers = append(workers, &w)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading active workers rows: %w", err)
	}

	return workers, nil
}

// PruneStale marks unresponsive workers as stopped.
func (r *sqliteWorkerRepository) PruneStale(ctx context.Context, maxInactivity time.Duration) (int64, error) {
	const query = `
		UPDATE workers
		SET status = ?, last_heartbeat = ?
		WHERE status != ? AND last_heartbeat < ?
	`
	now := time.Now().UTC()
	cutoff := now.Add(-maxInactivity)
	exec := r.getExecutor(ctx)

	res, err := exec.ExecContext(ctx, query,
		string(domain.WorkerStatusStopped),
		now,
		string(domain.WorkerStatusStopped),
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to prune stale workers: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to check pruned stale workers count: %w", err)
	}

	return rows, nil
}
