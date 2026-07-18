package tests

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
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

func TestConcurrency_DuplicatePreventionUnderStress(t *testing.T) {
	// 1. Setup in-memory SQLite DB
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

	// 2. Setup Config and logger
	cfg := &config.Config{}
	cfg.Worker.Concurrency = 5
	cfg.Worker.PollInterval = 10 * time.Millisecond
	cfg.Worker.MaxRetries = 3

	log := logger.NewNop()

	repo := sqliteRepo.NewSQLiteJobRepository(db)
	workerRepo := sqliteRepo.NewSQLiteWorkerRepository(db)
	execLogRepo := sqliteRepo.NewSQLiteExecutionLogRepository(db)

	sched := scheduler.NewScheduler(log)
	backoff := retry.NewExponentialBackoff(10*time.Millisecond, 100*time.Millisecond)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(repo)

	svc := service.NewJobService(repo, workerRepo, execLogRepo, cfg, log, retryEngine, dlqRouter, sched)

	// 3. Register job handler that increments atomic counter
	var runCounter int64
	var wg sync.WaitGroup
	totalJobs := 100
	wg.Add(totalJobs)

	err = svc.RegisterHandler("stress_task", func(ctx context.Context, payload string) error {
		atomic.AddInt64(&runCounter, 1)
		wg.Done()
		return nil
	})
	require.NoError(t, err)

	// 4. Enqueue 100 jobs (passing priorities dynamically to stress priority queues too!)
	for i := 0; i < totalJobs; i++ {
		priority := i % 5 // 0, 1, 2, 3, 4
		_, err := svc.Enqueue(ctx, "stress_task", `{}`, "stress_queue", priority, time.Time{}, 0)
		require.NoError(t, err)
	}

	// 5. Start 3 worker pools in parallel polling the same queue
	var pools []worker.WorkerPool
	numPools := 3
	for i := 0; i < numPools; i++ {
		pool := worker.NewWorkerPool(repo, svc, sched, cfg, log, "stress_queue")
		pools = append(pools, pool)
		go func(p worker.WorkerPool) {
			_ = p.Start(ctx)
		}(pool)
	}

	// Wake up workers
	sched.Notify()

	// Wait for executions to complete with a timeout
	doneChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatalf("Stress test timed out. Counter reached: %d/%d", atomic.LoadInt64(&runCounter), totalJobs)
	}

	// Stop all pools
	for _, pool := range pools {
		pool.Stop()
	}
	cancel()

	// 6. Verify assertions
	assert.Equal(t, int64(totalJobs), atomic.LoadInt64(&runCounter), "Each job must be executed exactly once")

	// Verify all jobs in DB are marked completed (no duplicate executions or locks remaining)
	jobs, err := repo.ListJobs(context.Background(), "stress_queue", domain.StatusCompleted, "", 200)
	require.NoError(t, err)
	assert.Len(t, jobs, totalJobs, "All 100 jobs must be completed in DB")
}
