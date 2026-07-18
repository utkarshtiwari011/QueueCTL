package tests

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"queuectl/internal/config"
	"queuectl/internal/database"
	"queuectl/internal/dlq"
	"queuectl/internal/domain"
	"queuectl/internal/logger"
	"queuectl/internal/repository/sqlite"
	"queuectl/internal/retry"
	"queuectl/internal/scheduler"
	"queuectl/internal/service"
	"queuectl/internal/worker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestQueueCTLEndToEnd(t *testing.T) {
	// 1. Setup shared in-memory DB
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	_, _ = db.Exec("PRAGMA journal_mode=WAL;")
	_, _ = db.Exec("PRAGMA busy_timeout=5000;")
	_, _ = db.Exec("PRAGMA synchronous=NORMAL;")
	_, _ = db.Exec("PRAGMA foreign_keys=ON;")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = database.RunMigrations(ctx, db)
	require.NoError(t, err)

	// 2. Setup Config and Services
	cfg := &config.Config{}
	cfg.Worker.Concurrency = 2
	cfg.Worker.PollInterval = 100 * time.Millisecond
	cfg.Worker.MaxRetries = 2
	cfg.Worker.BackoffBaseDelay = 50 * time.Millisecond
	cfg.Worker.BackoffMaxDelay = 500 * time.Millisecond

	log := logger.NewNop()

	repo := sqlite.NewSQLiteJobRepository(db)
	workerRepo := sqlite.NewSQLiteWorkerRepository(db)
	execLogRepo := sqlite.NewSQLiteExecutionLogRepository(db)

	sched := scheduler.NewScheduler(log)
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(repo)
	svc := service.NewJobService(repo, workerRepo, execLogRepo, cfg, log, retryEngine, dlqRouter, sched)

	// Register a job handler with a waitgroup to track completion
	var wg sync.WaitGroup
	wg.Add(1)

	var receivedPayload string
	err = svc.RegisterHandler("test_job", func(ctx context.Context, payload string) error {
		receivedPayload = payload
		wg.Done()
		return nil
	})
	require.NoError(t, err)

	// 3. Enqueue job (passing priority = 0)
	job, err := svc.Enqueue(ctx, "test_job", `{"key":"value"}`, "default", 0, time.Time{}, 0)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusPending, job.Status)

	// 4. Start Worker Pool in background
	pool := worker.NewWorkerPool(repo, svc, sched, cfg, log, "default")
	go func() {
		_ = pool.Start(ctx)
	}()

	// Wait for handler execution
	doneChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		// Handler executed successfully
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for job execution")
	}

	// Stop worker pool
	pool.Stop()
	cancel()

	// 5. Verify database job state is completed
	updatedJob, err := repo.GetByID(context.Background(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCompleted, updatedJob.Status)
	assert.Equal(t, `{"key":"value"}`, receivedPayload)
}
