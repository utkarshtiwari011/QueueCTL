package tests

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"queuectl/internal/config"
	"queuectl/internal/database"
	"queuectl/internal/dlq"
	"queuectl/internal/logger"
	sqliteRepo "queuectl/internal/repository/sqlite"
	"queuectl/internal/retry"
	"queuectl/internal/scheduler"
	"queuectl/internal/service"
	"queuectl/internal/worker"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestStress_HighVolume(t *testing.T) {
	volumes := []int{100, 1000, 5000}

	for _, volume := range volumes {
		t.Run(fmt.Sprintf("Volume_%d", volume), func(t *testing.T) {
			// 1. Setup DB
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

			// 2. Setup services
			cfg := &config.Config{}
			cfg.Worker.Concurrency = 10
			cfg.Worker.PollInterval = 5 * time.Millisecond
			cfg.Worker.MaxRetries = 1

			log := logger.NewNop()
			repo := sqliteRepo.NewSQLiteJobRepository(db)
			workerRepo := sqliteRepo.NewSQLiteWorkerRepository(db)
			execLogRepo := sqliteRepo.NewSQLiteExecutionLogRepository(db)
			sched := scheduler.NewScheduler(log)
			backoff := retry.NewExponentialBackoff(5*time.Millisecond, 20*time.Millisecond)
			retryEngine := retry.NewRetryEngine(backoff, log)
			dlqRouter := dlq.NewRouter(repo)
			svc := service.NewJobService(repo, workerRepo, execLogRepo, cfg, log, retryEngine, dlqRouter, sched)

			var runCounter int64
			var wg sync.WaitGroup
			wg.Add(volume)

			err = svc.RegisterHandler("stress_task", func(ctx context.Context, payload string) error {
				atomic.AddInt64(&runCounter, 1)
				wg.Done()
				return nil
			})
			require.NoError(t, err)

			// Enqueue jobs
			for i := 0; i < volume; i++ {
				_, err := svc.Enqueue(ctx, "stress_task", `{}`, "stress_queue", 0, time.Time{}, 0)
				require.NoError(t, err)
			}

			// Capture baseline metrics
			var memStatsStart runtime.MemStats
			runtime.ReadMemStats(&memStatsStart)
			goroutinesStart := runtime.NumGoroutine()
			startTime := time.Now()

			// Start Worker Pool
			pool := worker.NewWorkerPool(repo, svc, sched, cfg, log, "stress_queue")
			go func() {
				_ = pool.Start(ctx)
			}()

			sched.Notify()

			// Wait for execution completion
			doneChan := make(chan struct{})
			go func() {
				wg.Wait()
				close(doneChan)
			}()

			timeout := 20 * time.Second
			if volume > 1000 {
				timeout = 90 * time.Second
			}

			select {
			case <-doneChan:
				// Completed successfully
			case <-time.After(timeout):
				t.Fatalf("Stress test timed out for volume %d. Counter: %d/%d", volume, atomic.LoadInt64(&runCounter), volume)
			}

			duration := time.Since(startTime)
			pool.Stop()
			cancel()

			// Capture ending metrics
			var memStatsEnd runtime.MemStats
			runtime.ReadMemStats(&memStatsEnd)
			goroutinesEnd := runtime.NumGoroutine()

			throughput := float64(volume) / duration.Seconds()
			avgLatency := duration.Seconds() * 1000 / float64(volume)
			memUsed := float64(memStatsEnd.TotalAlloc-memStatsStart.TotalAlloc) / 1024 / 1024

			t.Logf("=== Metrics for Volume %d ===", volume)
			t.Logf("Duration:       %v", duration)
			t.Logf("Throughput:     %.2f jobs/sec", throughput)
			t.Logf("Avg Latency:    %.2f ms/job", avgLatency)
			t.Logf("Memory Alloc:   %.2f MB", memUsed)
			t.Logf("Goroutine Delta: %d -> %d", goroutinesStart, goroutinesEnd)
		})
	}
}
