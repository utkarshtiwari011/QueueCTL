package tests

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"queuectl/internal/config"
	"queuectl/internal/database"
	"queuectl/internal/dlq"
	"queuectl/internal/domain"
	"queuectl/internal/logger"
	sqliteRepo "queuectl/internal/repository/sqlite"
	"queuectl/internal/retry"
	"queuectl/internal/scheduler"
	"queuectl/internal/service"
	"queuectl/internal/worker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestFailure_PanicRecovery verifies that handler panics do not crash the worker pool
// and that the job transitions to pending (retry) or dead_letter.
func TestFailure_PanicRecovery(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = database.RunMigrations(ctx, db)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.Worker.Concurrency = 1
	cfg.Worker.PollInterval = 10 * time.Millisecond
	cfg.Worker.MaxRetries = 1
	cfg.Worker.BackoffBaseDelay = 5 * time.Second
	cfg.Worker.BackoffMaxDelay = 10 * time.Second

	log := logger.NewNop()
	repo := sqliteRepo.NewSQLiteJobRepository(db)
	workerRepo := sqliteRepo.NewSQLiteWorkerRepository(db)
	execLogRepo := sqliteRepo.NewSQLiteExecutionLogRepository(db)
	sched := scheduler.NewScheduler(log)
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(repo)
	svc := service.NewJobService(repo, workerRepo, execLogRepo, cfg, log, retryEngine, dlqRouter, sched)

	// Register a handler that panics
	err = svc.RegisterHandler("panic_job", func(ctx context.Context, payload string) error {
		panic("something went critically wrong inside handler")
	})
	require.NoError(t, err)

	// Enqueue job with max_retries = 1
	job, err := svc.Enqueue(ctx, "panic_job", `{}`, "default", 0, time.Time{}, 1)
	require.NoError(t, err)

	pool := worker.NewWorkerPool(repo, svc, sched, cfg, log, "default")
	go func() {
		_ = pool.Start(ctx)
	}()

	sched.Notify()

	// Wait for worker execution and state persistence
	time.Sleep(150 * time.Millisecond)
	pool.Stop()

	// Verify job is rescheduled back to pending (since max_retries is 1 and it failed once)
	updatedJob, err := repo.GetByID(context.Background(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusPending, updatedJob.Status)
	assert.Equal(t, 1, updatedJob.RetriesCount)
	assert.Contains(t, updatedJob.ErrorMessage, "handler panic")
}

// TestFailure_ContextTimeout verifies that job context cancellation is handled properly.
func TestFailure_ContextTimeout(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = database.RunMigrations(ctx, db)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.Worker.Concurrency = 1
	cfg.Worker.PollInterval = 10 * time.Millisecond
	cfg.Worker.MaxRetries = 0

	log := logger.NewNop()
	repo := sqliteRepo.NewSQLiteJobRepository(db)
	workerRepo := sqliteRepo.NewSQLiteWorkerRepository(db)
	execLogRepo := sqliteRepo.NewSQLiteExecutionLogRepository(db)
	sched := scheduler.NewScheduler(log)
	backoff := retry.NewExponentialBackoff(10*time.Millisecond, 20*time.Millisecond)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(repo)
	svc := service.NewJobService(repo, workerRepo, execLogRepo, cfg, log, retryEngine, dlqRouter, sched)

	// Handler respects context timeout/cancellation
	err = svc.RegisterHandler("timeout_job", func(ctx context.Context, payload string) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			return nil
		}
	})
	require.NoError(t, err)

	job, err := svc.Enqueue(ctx, "timeout_job", `{}`, "default", 0, time.Time{}, 0)
	require.NoError(t, err)

	pool := worker.NewWorkerPool(repo, svc, sched, cfg, log, "default")
	go func() {
		_ = pool.Start(ctx)
	}()

	sched.Notify()

	// Wait briefly for job start, then cancel pool context
	time.Sleep(50 * time.Millisecond)
	cancel()
	pool.Stop()

	// Since context was cancelled, job should have failed and been sent to DLQ (retries=0)
	updatedJob, err := repo.GetByID(context.Background(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusDeadLetter, updatedJob.Status)
	assert.Contains(t, updatedJob.ErrorMessage, "context canceled")
}

// TestFailure_StaleWorkerReclamation verifies that orphaned running jobs from dead workers are successfully reclaimed.
func TestFailure_StaleWorkerReclamation(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = database.RunMigrations(ctx, db)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.Worker.Concurrency = 1
	cfg.Worker.PollInterval = 10 * time.Millisecond
	cfg.Worker.MaxRetries = 1

	log := logger.NewNop()
	repo := sqliteRepo.NewSQLiteJobRepository(db)
	workerRepo := sqliteRepo.NewSQLiteWorkerRepository(db)
	execLogRepo := sqliteRepo.NewSQLiteExecutionLogRepository(db)
	sched := scheduler.NewScheduler(log)
	backoff := retry.NewExponentialBackoff(10*time.Millisecond, 20*time.Millisecond)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(repo)
	svc := service.NewJobService(repo, workerRepo, execLogRepo, cfg, log, retryEngine, dlqRouter, sched)

	// Register worker node manually
	w, err := domain.NewWorker("dead-worker", "dead-host", "default", 1)
	require.NoError(t, err)
	w.SetStatus(domain.WorkerStatusActive)
	// Artificially backdate its last heartbeat to make it stale
	w.LastHeartbeat = time.Now().UTC().Add(-60 * time.Second)
	err = svc.RegisterWorker(ctx, w)
	require.NoError(t, err)

	// Create and insert a job marked as running under the dead worker's latest execution log
	job := &domain.Job{
		ID:           "orphaned-job",
		Type:         "task",
		Payload:      "{}",
		Queue:        "default",
		Status:       domain.StatusRunning,
		MaxRetries:   2,
		RetriesCount: 0,
		RunAt:        time.Now().UTC(),
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	err = repo.Insert(ctx, job)
	require.NoError(t, err)

	// Create execution log for this job tied to the dead worker
	execLog, err := domain.NewExecutionLog("log-id", job.ID, w.ID, 1, time.Now().UTC())
	require.NoError(t, err)
	err = execLogRepo.Insert(ctx, execLog)
	require.NoError(t, err)

	// Execute reclamation manually
	err = svc.ReclaimOrphanedJobs(ctx)
	require.NoError(t, err)

	// The job should now be reset to pending (retried) because the worker was stale
	reclaimedJob, err := repo.GetByID(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusPending, reclaimedJob.Status)
	assert.Equal(t, 1, reclaimedJob.RetriesCount)
	assert.Contains(t, reclaimedJob.ErrorMessage, "worker process terminated unexpectedly")
}
