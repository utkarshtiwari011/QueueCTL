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
	"queuectl/internal/metrics"
	sqliteRepo "queuectl/internal/repository/sqlite"
	"queuectl/internal/retry"
	"queuectl/internal/scheduler"
	"queuectl/internal/service"

	_ "modernc.org/sqlite"
)

func BenchmarkScheduler_Notify(b *testing.B) {
	log := logger.NewNop()
	sched := scheduler.NewScheduler(log)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sched.Notify()
	}
}

func BenchmarkSQLiteJobRepository_Insert(b *testing.B) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	_ = database.RunMigrations(context.Background(), db)
	repo := sqliteRepo.NewSQLiteJobRepository(db)
	ctx := context.Background()

	job, _ := domain.NewJob("bench-job", "task", "{}", "default", 0, 3, time.Now())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Use a distinct ID for each insert to avoid unique constraint violations
		job.ID = string(rune(i))
		_ = repo.Insert(ctx, job)
	}
}

func BenchmarkSQLiteJobRepository_Update(b *testing.B) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	_ = database.RunMigrations(context.Background(), db)
	repo := sqliteRepo.NewSQLiteJobRepository(db)
	ctx := context.Background()

	job, _ := domain.NewJob("bench-job", "task", "{}", "default", 0, 3, time.Now().UTC())
	_ = repo.Insert(ctx, job)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		job.Status = domain.StatusRunning
		_ = repo.Update(ctx, job)
	}
}

func BenchmarkRetryEngine_HandleFailure(b *testing.B) {
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(10*time.Millisecond, 100*time.Millisecond)
	engine := retry.NewRetryEngine(backoff, log)
	job, _ := domain.NewJob("bench-job", "task", "{}", "default", 0, 3, time.Now())
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = engine.HandleFailure(ctx, job, assertAnError)
	}
}

var assertAnError = &mockError{}

type mockError struct{}

func (e *mockError) Error() string { return "bench error" }

func BenchmarkJobService_Enqueue(b *testing.B) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	_ = database.RunMigrations(context.Background(), db)

	repo := sqliteRepo.NewSQLiteJobRepository(db)
	workerRepo := sqliteRepo.NewSQLiteWorkerRepository(db)
	execLogRepo := sqliteRepo.NewSQLiteExecutionLogRepository(db)
	log := logger.NewNop()
	sched := scheduler.NewScheduler(log)
	backoff := retry.NewExponentialBackoff(10*time.Millisecond, 100*time.Millisecond)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(repo)
	cfg := &config.Config{}

	svc := service.NewJobService(repo, workerRepo, execLogRepo, cfg, log, retryEngine, dlqRouter, sched)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.Enqueue(ctx, "task", "{}", "default", 0, time.Time{}, 3)
	}
}

func BenchmarkWorker_Poll(b *testing.B) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	_ = database.RunMigrations(context.Background(), db)

	repo := sqliteRepo.NewSQLiteJobRepository(db)
	workerRepo := sqliteRepo.NewSQLiteWorkerRepository(db)
	execLogRepo := sqliteRepo.NewSQLiteExecutionLogRepository(db)
	log := logger.NewNop()
	sched := scheduler.NewScheduler(log)
	backoff := retry.NewExponentialBackoff(10*time.Millisecond, 100*time.Millisecond)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(repo)
	cfg := &config.Config{}
	cfg.Worker.Concurrency = 100

	svc := service.NewJobService(repo, workerRepo, execLogRepo, cfg, log, retryEngine, dlqRouter, sched)
	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		_, _ = svc.Enqueue(ctx, "task", "{}", "default", 0, time.Time{}, 3)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.FetchNextJob(ctx, "default", "bench-worker")
	}
}

func BenchmarkDLQService_Operations(b *testing.B) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	_ = database.RunMigrations(context.Background(), db)

	repo := sqliteRepo.NewSQLiteJobRepository(db)
	log := logger.NewNop()
	dlqSvc := dlq.NewDLQService(repo, log)
	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		job, _ := domain.NewJob(string(rune(i)), "task", "{}", "default", 0, 3, time.Now().UTC())
		job.Status = domain.StatusDeadLetter
		_ = repo.Insert(ctx, job)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = dlqSvc.List(ctx, "default", "", 10)
	}
}

func BenchmarkMetrics_GetMetrics(b *testing.B) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	_ = database.RunMigrations(context.Background(), db)

	repo := sqliteRepo.NewSQLiteJobRepository(db)
	workerRepo := sqliteRepo.NewSQLiteWorkerRepository(db)
	execLogRepo := sqliteRepo.NewSQLiteExecutionLogRepository(db)
	metricsSvc := metrics.NewMetricsService(repo, workerRepo, execLogRepo)
	ctx := context.Background()

	for i := 0; i < 100; i++ {
		job, _ := domain.NewJob(string(rune(i)), "task", "{}", "default", 0, 3, time.Now().UTC())
		_ = repo.Insert(ctx, job)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = metricsSvc.GetMetrics(ctx)
	}
}
