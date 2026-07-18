package metrics_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"queuectl/internal/database"
	"queuectl/internal/domain"
	"queuectl/internal/metrics"
	sqliteRepo "queuectl/internal/repository/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	err = database.RunMigrations(context.Background(), db)
	require.NoError(t, err)

	return db
}

func TestMetricsService_GetMetrics(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	repo := sqliteRepo.NewSQLiteJobRepository(db)
	workerRepo := sqliteRepo.NewSQLiteWorkerRepository(db)
	execLogRepo := sqliteRepo.NewSQLiteExecutionLogRepository(db)

	metricsSvc := metrics.NewMetricsService(repo, workerRepo, execLogRepo)

	// Seed workers
	w1, _ := domain.NewWorker("worker-1", "host-1", "default", 2)
	w1.SetStatus(domain.WorkerStatusActive)
	require.NoError(t, workerRepo.Upsert(ctx, w1))

	w2, _ := domain.NewWorker("worker-2", "host-2", "default", 2)
	w2.SetStatus(domain.WorkerStatusIdle)
	w2.LastHeartbeat = time.Now().UTC().Add(-60 * time.Second) // Stale/Stopped
	require.NoError(t, workerRepo.Upsert(ctx, w2))

	// Seed jobs
	job1 := &domain.Job{ID: "job-1", Type: "task", Queue: "default", Status: domain.StatusCompleted, RunAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	job2 := &domain.Job{ID: "job-2", Type: "task", Queue: "default", Status: domain.StatusPending, RunAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	require.NoError(t, repo.Insert(ctx, job1))
	require.NoError(t, repo.Insert(ctx, job2))

	// Seed execution logs
	startTime := time.Now().UTC().Truncate(time.Second)
	endTime := startTime.Add(2 * time.Second)
	log, _ := domain.NewExecutionLog("log-1", "job-1", "worker-1", 1, startTime)
	_ = log.Complete(domain.ExecutionSuccess, endTime, "")
	require.NoError(t, execLogRepo.Insert(ctx, log))
	require.NoError(t, execLogRepo.Update(ctx, log))

	// Fetch metrics
	stats, err := metricsSvc.GetMetrics(ctx)
	require.NoError(t, err)

	// Active worker-1 remains, stale worker-2 gets pruned during GetMetrics
	assert.Equal(t, 1, stats.TotalWorkersCount)
	assert.Equal(t, 1, stats.ActiveWorkersCount)
	assert.Equal(t, float64(100), stats.WorkerUtilization)
	assert.InDelta(t, 2.0, stats.AverageRuntime.Seconds(), 0.01)
}

func TestMetricsService_GetMetrics_ContextCanceled(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repo := sqliteRepo.NewSQLiteJobRepository(db)
	workerRepo := sqliteRepo.NewSQLiteWorkerRepository(db)
	execLogRepo := sqliteRepo.NewSQLiteExecutionLogRepository(db)

	metricsSvc := metrics.NewMetricsService(repo, workerRepo, execLogRepo)

	_, err := metricsSvc.GetMetrics(ctx)
	assert.Error(t, err)
}
