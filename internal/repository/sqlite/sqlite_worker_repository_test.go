package sqlite_test

import (
	"context"
	"testing"
	"time"

	"queuectl/internal/domain"
	"queuectl/internal/repository/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteWorkerRepository_UpsertAndGet(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteWorkerRepository(db)
	ctx := context.Background()

	w, err := domain.NewWorker("worker-1", "host-1", "default", 5)
	require.NoError(t, err)
	w.SetStatus(domain.WorkerStatusActive)

	// Test Insert via Upsert
	err = repo.Upsert(ctx, w)
	require.NoError(t, err)

	// Test GetByID
	fetched, err := repo.GetByID(ctx, "worker-1")
	require.NoError(t, err)
	assert.Equal(t, "worker-1", fetched.ID)
	assert.Equal(t, "host-1", fetched.Hostname)
	assert.Equal(t, "default", fetched.Queue)
	assert.Equal(t, domain.WorkerStatusActive, fetched.GetStatus())

	// Test Update via Upsert
	w.SetStatus(domain.WorkerStatusIdle)
	err = repo.Upsert(ctx, w)
	require.NoError(t, err)

	fetched2, err := repo.GetByID(ctx, "worker-1")
	require.NoError(t, err)
	assert.Equal(t, domain.WorkerStatusIdle, fetched2.GetStatus())
}

func TestSQLiteWorkerRepository_DeleteAndList(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteWorkerRepository(db)
	ctx := context.Background()

	w1, _ := domain.NewWorker("worker-1", "host-1", "default", 5)
	w1.SetStatus(domain.WorkerStatusActive)
	w2, _ := domain.NewWorker("worker-2", "host-2", "default", 2)
	w2.SetStatus(domain.WorkerStatusActive)

	_ = repo.Upsert(ctx, w1)
	_ = repo.Upsert(ctx, w2)

	// List active workers
	active, err := repo.ListActive(ctx)
	require.NoError(t, err)
	assert.Len(t, active, 2)

	// Delete worker-1
	err = repo.Delete(ctx, "worker-1")
	require.NoError(t, err)

	// Verify delete
	activeAfter, err := repo.ListActive(ctx)
	require.NoError(t, err)
	assert.Len(t, activeAfter, 1)
	assert.Equal(t, "worker-2", activeAfter[0].ID)

	_, err = repo.GetByID(ctx, "worker-1")
	assert.Error(t, err)
}

func TestSQLiteWorkerRepository_PruneStale(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteWorkerRepository(db)
	ctx := context.Background()

	w, _ := domain.NewWorker("worker-stale", "host-1", "default", 5)
	w.SetStatus(domain.WorkerStatusActive)
	w.LastHeartbeat = time.Now().UTC().Add(-60 * time.Second)

	err := repo.Upsert(ctx, w)
	require.NoError(t, err)

	// Prune workers stale for more than 30 seconds
	prunedCount, err := repo.PruneStale(ctx, 30*time.Second)
	require.NoError(t, err)
	assert.Equal(t, int64(1), prunedCount)

	// Check status updated to stopped
	fetched, err := repo.GetByID(ctx, "worker-stale")
	require.NoError(t, err)
	assert.Equal(t, domain.WorkerStatusStopped, fetched.GetStatus())
}

func TestSQLiteWorkerRepository_ContextCancellation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteWorkerRepository(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel context

	w, _ := domain.NewWorker("w-cancel", "host", "default", 1)

	err := repo.Upsert(ctx, w)
	assert.Error(t, err)

	_, err = repo.GetByID(ctx, "w-cancel")
	assert.Error(t, err)
}
