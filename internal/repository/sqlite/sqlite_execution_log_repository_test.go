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

func TestSQLiteExecutionLogRepository_InsertAndUpdate(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteExecutionLogRepository(db)
	ctx := context.Background()

	log, err := domain.NewExecutionLog("log-1", "job-1", "worker-1", 1, time.Now().UTC())
	require.NoError(t, err)

	// Test Insert
	err = repo.Insert(ctx, log)
	require.NoError(t, err)

	// Test GetByJobID
	fetched, err := repo.GetByJobID(ctx, "job-1")
	require.NoError(t, err)
	assert.Len(t, fetched, 1)
	assert.Equal(t, "log-1", fetched[0].ID)
	assert.Equal(t, "job-1", fetched[0].JobID)
	assert.Equal(t, "worker-1", fetched[0].WorkerID)

	// Test Update
	_ = log.Complete(domain.ExecutionSuccess, time.Now().UTC(), "")
	err = repo.Update(ctx, log)
	require.NoError(t, err)

	fetched2, err := repo.GetByJobID(ctx, "job-1")
	require.NoError(t, err)
	assert.Equal(t, domain.ExecutionSuccess, fetched2[0].Status)
}

func TestSQLiteExecutionLogRepository_StatsAndAverage(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteExecutionLogRepository(db)
	ctx := context.Background()

	startTime := time.Now().UTC().Truncate(time.Second)
	endTime := startTime.Add(2 * time.Second)

	log1, _ := domain.NewExecutionLog("log-1", "job-1", "worker-1", 1, startTime)
	_ = log1.Complete(domain.ExecutionSuccess, endTime, "")
	err := repo.Insert(ctx, log1)
	require.NoError(t, err)
	err = repo.Update(ctx, log1)
	require.NoError(t, err)

	log2, _ := domain.NewExecutionLog("log-2", "job-2", "worker-1", 1, startTime)
	_ = log2.Complete(domain.ExecutionSuccess, endTime.Add(2*time.Second), "")
	err = repo.Insert(ctx, log2)
	require.NoError(t, err)
	err = repo.Update(ctx, log2)
	require.NoError(t, err)

	// Get average runtime
	avg, err := repo.GetAverageRuntime(ctx)
	require.NoError(t, err)
	// log1 is 2s, log2 is 4s, average should be 3s
	assert.InDelta(t, 3.0, avg.Seconds(), 0.01)

	// Get execution statistics
	successCount, failedCount, err := repo.GetStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, successCount)
	assert.Equal(t, 0, failedCount)
}

func TestSQLiteExecutionLogRepository_ContextCancellation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := sqlite.NewSQLiteExecutionLogRepository(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel context

	log, _ := domain.NewExecutionLog("log-cancel", "job-1", "worker-1", 1, time.Now().UTC())

	err := repo.Insert(ctx, log)
	assert.Error(t, err)
}
