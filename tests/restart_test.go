package tests

import (
	"context"
	"database/sql"
	"path/filepath"
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

func TestRestart_Recovery(t *testing.T) {
	// 1. Setup file-based DB in unique test directory to survive connection restarts
	dbPath := filepath.Join(t.TempDir(), "test_restart.db")

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

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

	cfg := &config.Config{}
	cfg.Worker.Concurrency = 2
	cfg.Worker.PollInterval = 10 * time.Millisecond
	cfg.Worker.MaxRetries = 1
	cfg.Worker.BackoffBaseDelay = 10 * time.Millisecond
	cfg.Worker.BackoffMaxDelay = 50 * time.Millisecond

	log := logger.NewNop()
	repo := sqliteRepo.NewSQLiteJobRepository(db)
	workerRepo := sqliteRepo.NewSQLiteWorkerRepository(db)
	execLogRepo := sqliteRepo.NewSQLiteExecutionLogRepository(db)
	sched := scheduler.NewScheduler(log)
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(repo)
	svc := service.NewJobService(repo, workerRepo, execLogRepo, cfg, log, retryEngine, dlqRouter, sched)

	var runCounter int64
	var wg sync.WaitGroup
	wg.Add(2)

	err = svc.RegisterHandler("recoverable_task", func(ctx context.Context, payload string) error {
		atomic.AddInt64(&runCounter, 1)
		wg.Done()
		return nil
	})
	require.NoError(t, err)

	// Enqueue 2 jobs
	job1, err := svc.Enqueue(ctx, "recoverable_task", `{}`, "default", 0, time.Time{}, 1)
	require.NoError(t, err)
	job2, err := svc.Enqueue(ctx, "recoverable_task", `{}`, "default", 0, time.Time{}, 1)
	require.NoError(t, err)

	// 2. Setup dead worker and fetch jobs to running state
	deadWorker, err := domain.NewWorker("pool1-worker", "pool1-host", "default", 2)
	require.NoError(t, err)
	deadWorker.SetStatus(domain.WorkerStatusActive)
	err = svc.RegisterWorker(ctx, deadWorker)
	require.NoError(t, err)

	// Fetch jobs to running state under the dead worker ID
	fetchedJob1, err := svc.FetchNextJob(ctx, "default", "pool1-worker")
	require.NoError(t, err)
	require.NotNil(t, fetchedJob1)
	fetchedJob2, err := svc.FetchNextJob(ctx, "default", "pool1-worker")
	require.NoError(t, err)
	require.NotNil(t, fetchedJob2)

	// pool1-worker is marked active, jobs are running
	// Verify jobs are running in DB
	dbJob1, err := repo.GetByID(ctx, job1.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusRunning, dbJob1.Status)

	// Simulate crash/kill by directly backdating worker heartbeat in DB

	// Manually backdate the worker's heartbeat in DB to simulate stale worker (>30s)
	_, err = db.Exec("UPDATE workers SET last_heartbeat = ?", time.Now().UTC().Add(-60*time.Second))
	require.NoError(t, err)

	// Close database connection to simulate full reboot
	err = db.Close()
	require.NoError(t, err)

	// Sleep briefly to let the OS release any asynchronous file handles on Windows
	time.Sleep(200 * time.Millisecond)

	// 3. RESTART: Re-open DB and start Pool 2
	db2, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db2.Close()

	db2.SetMaxOpenConns(1)
	db2.SetMaxIdleConns(1)
	db2.SetConnMaxLifetime(0)

	repo2 := sqliteRepo.NewSQLiteJobRepository(db2)
	workerRepo2 := sqliteRepo.NewSQLiteWorkerRepository(db2)
	execLogRepo2 := sqliteRepo.NewSQLiteExecutionLogRepository(db2)
	dlqRouter2 := dlq.NewRouter(repo2)
	svc2 := service.NewJobService(repo2, workerRepo2, execLogRepo2, cfg, log, retryEngine, dlqRouter2, sched)

	// Register handler in restarted service
	err = svc2.RegisterHandler("recoverable_task", func(ctx context.Context, payload string) error {
		atomic.AddInt64(&runCounter, 1)
		wg.Done()
		return nil
	})
	require.NoError(t, err)

	pool2Ctx, pool2Cancel := context.WithCancel(context.Background())
	defer pool2Cancel()

	pool2 := worker.NewWorkerPool(repo2, svc2, sched, cfg, log, "default")

	// Start pool2. It should run ReclaimOrphanedJobs immediately on startup
	go func() {
		_ = pool2.Start(pool2Ctx)
	}()

	sched.Notify()

	// Wait for Pool 2 to reclaim the jobs and execute them
	doneChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		// Success!
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for jobs to be recovered and completed")
	}

	pool2.Stop()

	// Verify both jobs are marked completed in DB
	finalJob1, err := repo2.GetByID(context.Background(), job1.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCompleted, finalJob1.Status)

	finalJob2, err := repo2.GetByID(context.Background(), job2.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCompleted, finalJob2.Status)
}
