package repository

import (
	"context"
	"time"

	"queuectl/internal/domain"
)

// WorkerRepository defines the storage interface for active worker processes.
type WorkerRepository interface {
	// Upsert inserts or updates worker registration and heartbeat timestamps.
	Upsert(ctx context.Context, worker *domain.Worker) error

	// GetByID retrieves worker parameters.
	GetByID(ctx context.Context, id string) (*domain.Worker, error)

	// Delete unregisters a worker node.
	Delete(ctx context.Context, id string) error

	// ListActive returns all workers with status active or idle.
	ListActive(ctx context.Context) ([]*domain.Worker, error)

	// PruneStale transitions workers that haven't sent heartbeats since threshold duration to stopped.
	PruneStale(ctx context.Context, maxInactivity time.Duration) (int64, error)
}
