package repository

import (
	"context"
	"errors"
	"time"

	"queuectl/internal/domain"
)

// ErrConcurrencyConflict is returned when an optimistic locking check fails.
var ErrConcurrencyConflict = errors.New("concurrency conflict: record was updated by another process")

// JobRepository defines the storage interface for background jobs.
type JobRepository interface {
	// Insert persists a new job.
	Insert(ctx context.Context, job *domain.Job) error

	// GetByID retrieves a job by its ID.
	GetByID(ctx context.Context, id string) (*domain.Job, error)

	// Update updates job attributes. Must implement optimistic updates using
	// updated_at check to prevent race conditions.
	Update(ctx context.Context, job *domain.Job) error

	// AcquireNextPendingJob finds the next eligible job, marks it as running, and returns it.
	// Must be atomic to ensure concurrency safety.
	AcquireNextPendingJob(ctx context.Context, queue string) (*domain.Job, error)

	// ListJobs returns a filtered list of jobs, optionally searching payloads/errors.
	ListJobs(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error)

	// GetQueueStats aggregates job status counts grouped by queue.
	GetQueueStats(ctx context.Context) (map[string]map[domain.JobStatus]int, error)

	// Delete removes a job by its ID.
	Delete(ctx context.Context, id string) error

	// DeleteCompletedJobs deletes jobs that are completed and older than a specific duration.
	DeleteCompletedJobs(ctx context.Context, olderThan time.Duration) (int64, error)

	// WithTx runs a function within a transaction context.
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}
