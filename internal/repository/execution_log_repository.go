package repository

import (
	"context"
	"time"

	"queuectl/internal/domain"
)

// ExecutionLogRepository defines persistence interfaces for historical execution metrics.
type ExecutionLogRepository interface {
	// Insert saves a new execution attempt log.
	Insert(ctx context.Context, log *domain.ExecutionLog) error

	// Update records execution completion state.
	Update(ctx context.Context, log *domain.ExecutionLog) error

	// GetByJobID returns all execution logs recorded for a specific job.
	GetByJobID(ctx context.Context, jobID string) ([]*domain.ExecutionLog, error)

	// GetAverageRuntime calculates the average duration of successful job executions.
	GetAverageRuntime(ctx context.Context) (time.Duration, error)

	// GetStats aggregates the count of successful and failed execution attempts.
	GetStats(ctx context.Context) (successCount int, failedCount int, err error)
}
