package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"queuectl/internal/config"
	"queuectl/internal/dlq"
	"queuectl/internal/domain"
	"queuectl/internal/logger"
	"queuectl/internal/retry"
	"queuectl/internal/scheduler"
	"queuectl/internal/service"

	"github.com/stretchr/testify/assert"
)

type mockJobRepository struct {
	insertFunc                func(ctx context.Context, job *domain.Job) error
	getByIDFunc               func(ctx context.Context, id string) (*domain.Job, error)
	updateFunc                func(ctx context.Context, job *domain.Job) error
	acquireNextPendingJobFunc func(ctx context.Context, queue string) (*domain.Job, error)
	listJobsFunc              func(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error)
	getQueueStatsFunc         func(ctx context.Context) (map[string]map[domain.JobStatus]int, error)
	deleteFunc                func(ctx context.Context, id string) error
	deleteCompletedJobsFunc   func(ctx context.Context, olderThan time.Duration) (int64, error)
	withTxFunc                func(ctx context.Context, fn func(ctx context.Context) error) error
}

func (m *mockJobRepository) Insert(ctx context.Context, job *domain.Job) error {
	if m.insertFunc != nil {
		return m.insertFunc(ctx, job)
	}
	return nil
}

func (m *mockJobRepository) GetByID(ctx context.Context, id string) (*domain.Job, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return &domain.Job{ID: id, Status: domain.StatusRunning, MaxRetries: 3}, nil
}

func (m *mockJobRepository) Update(ctx context.Context, job *domain.Job) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, job)
	}
	return nil
}

func (m *mockJobRepository) AcquireNextPendingJob(ctx context.Context, queue string) (*domain.Job, error) {
	if m.acquireNextPendingJobFunc != nil {
		return m.acquireNextPendingJobFunc(ctx, queue)
	}
	return nil, nil
}

func (m *mockJobRepository) ListJobs(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error) {
	if m.listJobsFunc != nil {
		return m.listJobsFunc(ctx, queue, status, search, limit)
	}
	return nil, nil
}

func (m *mockJobRepository) GetQueueStats(ctx context.Context) (map[string]map[domain.JobStatus]int, error) {
	if m.getQueueStatsFunc != nil {
		return m.getQueueStatsFunc(ctx)
	}
	return nil, nil
}

func (m *mockJobRepository) DeleteCompletedJobs(ctx context.Context, olderThan time.Duration) (int64, error) {
	if m.deleteCompletedJobsFunc != nil {
		return m.deleteCompletedJobsFunc(ctx, olderThan)
	}
	return 0, nil
}

func (m *mockJobRepository) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockJobRepository) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if m.withTxFunc != nil {
		return m.withTxFunc(ctx, fn)
	}
	return fn(ctx)
}

type mockWorkerRepository struct {
	upsertFunc     func(ctx context.Context, w *domain.Worker) error
	getByIDFunc    func(ctx context.Context, id string) (*domain.Worker, error)
	deleteFunc     func(ctx context.Context, id string) error
	listActiveFunc func(ctx context.Context) ([]*domain.Worker, error)
	pruneStaleFunc func(ctx context.Context, maxInactivity time.Duration) (int64, error)
}

func (m *mockWorkerRepository) Upsert(ctx context.Context, w *domain.Worker) error {
	if m.upsertFunc != nil {
		return m.upsertFunc(ctx, w)
	}
	return nil
}

func (m *mockWorkerRepository) GetByID(ctx context.Context, id string) (*domain.Worker, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockWorkerRepository) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockWorkerRepository) ListActive(ctx context.Context) ([]*domain.Worker, error) {
	if m.listActiveFunc != nil {
		return m.listActiveFunc(ctx)
	}
	return nil, nil
}

func (m *mockWorkerRepository) PruneStale(ctx context.Context, maxInactivity time.Duration) (int64, error) {
	if m.pruneStaleFunc != nil {
		return m.pruneStaleFunc(ctx, maxInactivity)
	}
	return 0, nil
}

type mockExecutionLogRepository struct {
	insertFunc            func(ctx context.Context, l *domain.ExecutionLog) error
	updateFunc            func(ctx context.Context, l *domain.ExecutionLog) error
	getByJobIDFunc        func(ctx context.Context, jobID string) ([]*domain.ExecutionLog, error)
	getAverageRuntimeFunc func(ctx context.Context) (time.Duration, error)
	getStatsFunc          func(ctx context.Context) (int, int, error)
}

func (m *mockExecutionLogRepository) Insert(ctx context.Context, l *domain.ExecutionLog) error {
	if m.insertFunc != nil {
		return m.insertFunc(ctx, l)
	}
	return nil
}

func (m *mockExecutionLogRepository) Update(ctx context.Context, l *domain.ExecutionLog) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, l)
	}
	return nil
}

func (m *mockExecutionLogRepository) GetByJobID(ctx context.Context, jobID string) ([]*domain.ExecutionLog, error) {
	if m.getByJobIDFunc != nil {
		return m.getByJobIDFunc(ctx, jobID)
	}
	return nil, nil
}

func (m *mockExecutionLogRepository) GetAverageRuntime(ctx context.Context) (time.Duration, error) {
	if m.getAverageRuntimeFunc != nil {
		return m.getAverageRuntimeFunc(ctx)
	}
	return 0, nil
}

func (m *mockExecutionLogRepository) GetStats(ctx context.Context) (int, int, error) {
	if m.getStatsFunc != nil {
		return m.getStatsFunc(ctx)
	}
	return 0, 0, nil
}

func TestEnqueue(t *testing.T) {
	mockRepo := &mockJobRepository{}
	mockWrk := &mockWorkerRepository{}
	mockExec := &mockExecutionLogRepository{}
	cfg := &config.Config{}
	cfg.Worker.MaxRetries = 5
	log := logger.NewNop()

	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(mockRepo)
	sched := scheduler.NewScheduler(log)
	svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retryEngine, dlqRouter, sched)

	var insertedJob *domain.Job
	mockRepo.insertFunc = func(ctx context.Context, job *domain.Job) error {
		insertedJob = job
		return nil
	}

	ctx := context.Background()
	job, err := svc.Enqueue(ctx, "send_email", `{"email":"test@example.com"}`, "high-priority", 0, time.Time{}, 0)

	require := assert.New(t)
	require.NoError(err)
	require.NotNil(job)
	require.Equal("send_email", job.Type)
	require.Equal(`{"email":"test@example.com"}`, job.Payload)
	require.Equal("high-priority", job.Queue)
	require.Equal(domain.StatusPending, job.Status)
	require.Equal(5, job.MaxRetries) // defaults to cfg
	require.Equal(insertedJob, job)
}

func TestHandleJobSuccess(t *testing.T) {
	mockRepo := &mockJobRepository{}
	mockWrk := &mockWorkerRepository{}
	mockExec := &mockExecutionLogRepository{}
	cfg := &config.Config{}
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(mockRepo)
	sched := scheduler.NewScheduler(log)
	svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retryEngine, dlqRouter, sched)

	var updatedJob *domain.Job
	mockRepo.updateFunc = func(ctx context.Context, job *domain.Job) error {
		updatedJob = job
		return nil
	}

	job := &domain.Job{
		ID:           "test-job",
		Status:       domain.StatusRunning,
		ErrorMessage: "previous error",
	}

	err := svc.HandleJobSuccess(context.Background(), job, "test-worker")
	assert.NoError(t, err)
	assert.Equal(t, domain.StatusCompleted, updatedJob.Status)
	assert.Equal(t, "", updatedJob.ErrorMessage)
}

func TestHandleJobFailure_RetryWithBackoff(t *testing.T) {
	mockRepo := &mockJobRepository{}
	mockWrk := &mockWorkerRepository{}
	mockExec := &mockExecutionLogRepository{}
	cfg := &config.Config{}
	cfg.Worker.BackoffBaseDelay = 2 * time.Second
	cfg.Worker.BackoffMaxDelay = 30 * time.Second
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(mockRepo)
	sched := scheduler.NewScheduler(log)
	svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retryEngine, dlqRouter, sched)

	var updatedJob *domain.Job
	mockRepo.updateFunc = func(ctx context.Context, job *domain.Job) error {
		updatedJob = job
		return nil
	}

	job := &domain.Job{
		ID:           "test-job",
		Status:       domain.StatusRunning,
		MaxRetries:   3,
		RetriesCount: 0,
	}

	mockRepo.getByIDFunc = func(ctx context.Context, id string) (*domain.Job, error) {
		return job, nil
	}

	// 1st failure: retries count becomes 1, backoff is 2s * 2^0 = 2s
	now := time.Now()
	err := svc.HandleJobFailure(context.Background(), job, "test-worker", errors.New("something went wrong"))
	assert.NoError(t, err)
	assert.Equal(t, domain.StatusPending, updatedJob.Status)
	assert.Equal(t, 1, updatedJob.RetriesCount)
	assert.Equal(t, "something went wrong", updatedJob.ErrorMessage)
	assert.WithinDuration(t, now.Add(2*time.Second), updatedJob.RunAt, 100*time.Millisecond)

	// 2nd failure: retries count becomes 2, backoff is 2s * 2^1 = 4s
	job.Status = domain.StatusRunning
	err = svc.HandleJobFailure(context.Background(), job, "test-worker", errors.New("failed again"))
	assert.NoError(t, err)
	assert.Equal(t, domain.StatusPending, updatedJob.Status)
	assert.Equal(t, 2, updatedJob.RetriesCount)
	assert.WithinDuration(t, time.Now().Add(4*time.Second), updatedJob.RunAt, 100*time.Millisecond)
}

func TestHandleJobFailure_ToDLQ(t *testing.T) {
	mockRepo := &mockJobRepository{}
	mockWrk := &mockWorkerRepository{}
	mockExec := &mockExecutionLogRepository{}
	cfg := &config.Config{}
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(mockRepo)
	sched := scheduler.NewScheduler(log)
	svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retryEngine, dlqRouter, sched)

	var updatedJob *domain.Job
	mockRepo.updateFunc = func(ctx context.Context, job *domain.Job) error {
		updatedJob = job
		return nil
	}

	job := &domain.Job{
		ID:           "test-job",
		Status:       domain.StatusRunning,
		MaxRetries:   2,
		RetriesCount: 2, // Already retried twice
	}

	mockRepo.getByIDFunc = func(ctx context.Context, id string) (*domain.Job, error) {
		return job, nil
	}

	// 3rd failure: retries count becomes 3 > max retries (2). Sent to DLQ.
	err := svc.HandleJobFailure(context.Background(), job, "test-worker", errors.New("fatal exception"))
	assert.NoError(t, err)
	assert.Equal(t, domain.StatusDeadLetter, updatedJob.Status)
	assert.Equal(t, 3, updatedJob.RetriesCount)
	assert.Equal(t, "fatal exception", updatedJob.ErrorMessage)
}

func TestGetJob(t *testing.T) {
	mockRepo := &mockJobRepository{}
	mockWrk := &mockWorkerRepository{}
	mockExec := &mockExecutionLogRepository{}
	cfg := &config.Config{}
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(mockRepo)
	sched := scheduler.NewScheduler(log)
	svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retryEngine, dlqRouter, sched)

	mockRepo.getByIDFunc = func(ctx context.Context, id string) (*domain.Job, error) {
		return &domain.Job{ID: id, Type: "email"}, nil
	}

	job, err := svc.GetJob(context.Background(), "test-job-id")
	assert.NoError(t, err)
	assert.Equal(t, "test-job-id", job.ID)
}

func TestListJobs(t *testing.T) {
	mockRepo := &mockJobRepository{}
	mockWrk := &mockWorkerRepository{}
	mockExec := &mockExecutionLogRepository{}
	cfg := &config.Config{}
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(mockRepo)
	sched := scheduler.NewScheduler(log)
	svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retryEngine, dlqRouter, sched)

	mockRepo.listJobsFunc = func(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error) {
		return []*domain.Job{{ID: "job-1", Queue: queue}}, nil
	}

	jobs, err := svc.ListJobs(context.Background(), "default", domain.StatusPending, "", 10)
	assert.NoError(t, err)
	assert.Len(t, jobs, 1)
	assert.Equal(t, "default", jobs[0].Queue)
}

func TestGetStats(t *testing.T) {
	mockRepo := &mockJobRepository{}
	mockWrk := &mockWorkerRepository{}
	mockExec := &mockExecutionLogRepository{}
	cfg := &config.Config{}
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(mockRepo)
	sched := scheduler.NewScheduler(log)
	svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retryEngine, dlqRouter, sched)

	mockRepo.getQueueStatsFunc = func(ctx context.Context) (map[string]map[domain.JobStatus]int, error) {
		return map[string]map[domain.JobStatus]int{"default": {domain.StatusPending: 5}}, nil
	}

	stats, err := svc.GetStats(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 5, stats["default"][domain.StatusPending])
}

func TestPurgeCompleted(t *testing.T) {
	mockRepo := &mockJobRepository{}
	mockWrk := &mockWorkerRepository{}
	mockExec := &mockExecutionLogRepository{}
	cfg := &config.Config{}
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(mockRepo)
	sched := scheduler.NewScheduler(log)
	svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retryEngine, dlqRouter, sched)

	mockRepo.deleteCompletedJobsFunc = func(ctx context.Context, olderThan time.Duration) (int64, error) {
		return 3, nil
	}

	count, err := svc.PurgeCompleted(context.Background(), 24*time.Hour)
	assert.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestRegisterHandlerAndExecuteJob(t *testing.T) {
	mockRepo := &mockJobRepository{}
	mockWrk := &mockWorkerRepository{}
	mockExec := &mockExecutionLogRepository{}
	cfg := &config.Config{}
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(mockRepo)
	sched := scheduler.NewScheduler(log)
	svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retryEngine, dlqRouter, sched)

	// Handler registration
	err := svc.RegisterHandler("task", func(ctx context.Context, payload string) error {
		if payload == "fail" {
			return errors.New("handler error")
		}
		return nil
	})
	assert.NoError(t, err)

	// Execute successfully
	err = svc.ExecuteJob(context.Background(), &domain.Job{Type: "task", Payload: "success"})
	assert.NoError(t, err)

	// Execute failure
	err = svc.ExecuteJob(context.Background(), &domain.Job{Type: "task", Payload: "fail"})
	assert.Error(t, err)

	// Unregistered handler execution
	err = svc.ExecuteJob(context.Background(), &domain.Job{Type: "unregistered", Payload: ""})
	assert.Error(t, err)
}

func TestFetchNextJob(t *testing.T) {
	mockRepo := &mockJobRepository{}
	mockWrk := &mockWorkerRepository{}
	mockExec := &mockExecutionLogRepository{}
	cfg := &config.Config{}
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(mockRepo)
	sched := scheduler.NewScheduler(log)
	svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retryEngine, dlqRouter, sched)

	mockRepo.acquireNextPendingJobFunc = func(ctx context.Context, queue string) (*domain.Job, error) {
		return &domain.Job{ID: "job-1", Queue: queue}, nil
	}

	mockWrk.getByIDFunc = func(ctx context.Context, id string) (*domain.Worker, error) {
		w, _ := domain.NewWorker(id, "host", "default", 1)
		return w, nil
	}

	job, err := svc.FetchNextJob(context.Background(), "default", "worker-1")
	assert.NoError(t, err)
	assert.NotNil(t, job)
	assert.Equal(t, "job-1", job.ID)
}

func TestWorkerRegistration(t *testing.T) {
	mockRepo := &mockJobRepository{}
	mockWrk := &mockWorkerRepository{}
	mockExec := &mockExecutionLogRepository{}
	cfg := &config.Config{}
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(mockRepo)
	sched := scheduler.NewScheduler(log)
	svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retryEngine, dlqRouter, sched)

	w, _ := domain.NewWorker("worker-1", "host", "default", 1)

	// Test Register
	err := svc.RegisterWorker(context.Background(), w)
	assert.NoError(t, err)

	// Test Unregister
	err = svc.UnregisterWorker(context.Background(), "worker-1")
	assert.NoError(t, err)
}

func TestReclaimOrphanedJobs(t *testing.T) {
	mockRepo := &mockJobRepository{}
	mockWrk := &mockWorkerRepository{}
	mockExec := &mockExecutionLogRepository{}
	cfg := &config.Config{}
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(mockRepo)
	sched := scheduler.NewScheduler(log)
	svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retryEngine, dlqRouter, sched)

	mockWrk.pruneStaleFunc = func(ctx context.Context, maxInactivity time.Duration) (int64, error) {
		return 1, nil
	}

	mockWrk.getByIDFunc = func(ctx context.Context, id string) (*domain.Worker, error) {
		w, _ := domain.NewWorker(id, "host", "default", 1)
		w.SetStatus(domain.WorkerStatusStopped)
		return w, nil
	}

	mockWrk.listActiveFunc = func(ctx context.Context) ([]*domain.Worker, error) {
		// return worker marked stopped
		w, _ := domain.NewWorker("stale-worker", "host", "default", 1)
		w.SetStatus(domain.WorkerStatusStopped)
		return []*domain.Worker{w}, nil
	}

	mockRepo.listJobsFunc = func(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error) {
		return []*domain.Job{{ID: "job-stale", Status: domain.StatusRunning, MaxRetries: 3, RetriesCount: 0}}, nil
	}

	mockExec.getByJobIDFunc = func(ctx context.Context, jobID string) ([]*domain.ExecutionLog, error) {
		log, _ := domain.NewExecutionLog("log-stale", jobID, "stale-worker", 1, time.Now().UTC())
		return []*domain.ExecutionLog{log}, nil
	}

	var updatedJob *domain.Job
	mockRepo.updateFunc = func(ctx context.Context, job *domain.Job) error {
		updatedJob = job
		return nil
	}

	err := svc.ReclaimOrphanedJobs(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, updatedJob)
	assert.Equal(t, domain.StatusPending, updatedJob.Status)
	assert.Equal(t, 1, updatedJob.RetriesCount)
}

func TestJobService_ErrorsAndRollbacks(t *testing.T) {
	ctx := context.Background()
	log := logger.NewNop()
	cfg := &config.Config{}

	t.Run("Enqueue_InsertError", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		svc := service.NewJobService(mockRepo, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))
		mockRepo.insertFunc = func(ctx context.Context, job *domain.Job) error {
			return errors.New("database disk full")
		}
		_, err := svc.Enqueue(ctx, "task", "{}", "default", 0, time.Time{}, 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database disk full")
	})

	t.Run("GetJob_Error", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		svc := service.NewJobService(mockRepo, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))
		mockRepo.getByIDFunc = func(ctx context.Context, id string) (*domain.Job, error) {
			return nil, errors.New("connection reset")
		}
		_, err := svc.GetJob(ctx, "job-id")
		assert.Error(t, err)
	})

	t.Run("ListJobs_Error", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		svc := service.NewJobService(mockRepo, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))
		mockRepo.listJobsFunc = func(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error) {
			return nil, errors.New("read timeout")
		}
		_, err := svc.ListJobs(ctx, "default", domain.StatusPending, "", 10)
		assert.Error(t, err)
	})

	t.Run("GetStats_Error", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		svc := service.NewJobService(mockRepo, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))
		mockRepo.getQueueStatsFunc = func(ctx context.Context) (map[string]map[domain.JobStatus]int, error) {
			return nil, errors.New("query failed")
		}
		_, err := svc.GetStats(ctx)
		assert.Error(t, err)
	})

	t.Run("PurgeCompleted_Error", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		svc := service.NewJobService(mockRepo, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))
		mockRepo.deleteCompletedJobsFunc = func(ctx context.Context, olderThan time.Duration) (int64, error) {
			return 0, errors.New("delete failed")
		}
		_, err := svc.PurgeCompleted(ctx, 24*time.Hour)
		assert.Error(t, err)
	})

	t.Run("RegisterWorker_Error", func(t *testing.T) {
		mockWrk := &mockWorkerRepository{}
		svc := service.NewJobService(&mockJobRepository{}, mockWrk, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(&mockJobRepository{}), scheduler.NewScheduler(log))
		mockWrk.upsertFunc = func(ctx context.Context, w *domain.Worker) error {
			return errors.New("upsert failed")
		}
		w, _ := domain.NewWorker("worker-1", "host", "default", 1)
		err := svc.RegisterWorker(ctx, w)
		assert.Error(t, err)
	})

	t.Run("UnregisterWorker_Error", func(t *testing.T) {
		mockWrk := &mockWorkerRepository{}
		svc := service.NewJobService(&mockJobRepository{}, mockWrk, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(&mockJobRepository{}), scheduler.NewScheduler(log))
		mockWrk.deleteFunc = func(ctx context.Context, id string) error {
			return errors.New("delete failed")
		}
		err := svc.UnregisterWorker(ctx, "worker-1")
		assert.Error(t, err)
	})

	t.Run("FetchNextJob_AcquireError", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		svc := service.NewJobService(mockRepo, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))
		mockRepo.acquireNextPendingJobFunc = func(ctx context.Context, queue string) (*domain.Job, error) {
			return nil, errors.New("acquire failed")
		}
		_, err := svc.FetchNextJob(ctx, "default", "worker-1")
		assert.Error(t, err)
	})

	t.Run("FetchNextJob_LogInsertError", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		mockExec := &mockExecutionLogRepository{}
		svc := service.NewJobService(mockRepo, &mockWorkerRepository{}, mockExec, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))
		mockRepo.acquireNextPendingJobFunc = func(ctx context.Context, queue string) (*domain.Job, error) {
			return &domain.Job{ID: "job-1", Status: domain.StatusPending}, nil
		}
		mockExec.insertFunc = func(ctx context.Context, l *domain.ExecutionLog) error {
			return errors.New("log insert failed")
		}
		_, err := svc.FetchNextJob(ctx, "default", "worker-1")
		assert.Error(t, err)
	})

	t.Run("HandleJobSuccess_UpdateError", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		svc := service.NewJobService(mockRepo, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))
		mockRepo.updateFunc = func(ctx context.Context, job *domain.Job) error {
			return errors.New("update lock conflict")
		}
		job := &domain.Job{ID: "job-1", Status: domain.StatusRunning}
		err := svc.HandleJobSuccess(ctx, job, "worker-1")
		assert.Error(t, err)
	})

	t.Run("HandleJobFailure_UpdateError", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		svc := service.NewJobService(mockRepo, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))
		mockRepo.updateFunc = func(ctx context.Context, job *domain.Job) error {
			return errors.New("update failed")
		}
		job := &domain.Job{ID: "job-1", Status: domain.StatusRunning, MaxRetries: 3, RetriesCount: 0}
		err := svc.HandleJobFailure(ctx, job, "worker-1", errors.New("handler error"))
		assert.Error(t, err)
	})
}

func TestJobService_EdgeCases(t *testing.T) {
	ctx := context.Background()
	log := logger.NewNop()
	cfg := &config.Config{}

	t.Run("RegisterHandler_EdgeCases", func(t *testing.T) {
		svc := service.NewJobService(&mockJobRepository{}, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(&mockJobRepository{}), scheduler.NewScheduler(log))

		// 1. empty jobType
		err := svc.RegisterHandler("", func(ctx context.Context, payload string) error { return nil })
		assert.Error(t, err)

		// 2. nil handler
		err = svc.RegisterHandler("task", nil)
		assert.Error(t, err)

		// 3. duplicate registration
		err = svc.RegisterHandler("task", func(ctx context.Context, payload string) error { return nil })
		assert.NoError(t, err)
		err = svc.RegisterHandler("task", func(ctx context.Context, payload string) error { return nil })
		assert.Error(t, err)
	})

	t.Run("ExecuteJob_NotFound", func(t *testing.T) {
		svc := service.NewJobService(&mockJobRepository{}, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(&mockJobRepository{}), scheduler.NewScheduler(log))
		job := &domain.Job{Type: "non-existent"}
		err := svc.ExecuteJob(ctx, job)
		assert.Error(t, err)
	})

	t.Run("RegisterWorker_EdgeCases", func(t *testing.T) {
		svc := service.NewJobService(&mockJobRepository{}, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(&mockJobRepository{}), scheduler.NewScheduler(log))

		// 1. nil worker
		err := svc.RegisterWorker(ctx, nil)
		assert.Error(t, err)

		// 2. invalid worker (empty ID)
		w, _ := domain.NewWorker("", "host", "default", 1)
		err = svc.RegisterWorker(ctx, w)
		assert.Error(t, err)
	})

	t.Run("UnregisterWorker_EmptyID", func(t *testing.T) {
		svc := service.NewJobService(&mockJobRepository{}, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(&mockJobRepository{}), scheduler.NewScheduler(log))
		err := svc.UnregisterWorker(ctx, "")
		assert.Error(t, err)
	})

	t.Run("HandleJobSuccess_EdgeCases", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		mockExec := &mockExecutionLogRepository{}
		svc := service.NewJobService(mockRepo, &mockWorkerRepository{}, mockExec, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))

		// 1. nil job
		err := svc.HandleJobSuccess(ctx, nil, "worker-1")
		assert.Error(t, err)

		// 2. invalid status
		job := &domain.Job{ID: "job-1", Status: domain.StatusPending}
		err = svc.HandleJobSuccess(ctx, job, "worker-1")
		assert.Error(t, err)

		// 3. no execution logs found (does not fail transaction, but skips update)
		job2 := &domain.Job{ID: "job-2", Status: domain.StatusRunning}
		mockExec.getByJobIDFunc = func(ctx context.Context, jobID string) ([]*domain.ExecutionLog, error) {
			return nil, nil
		}
		err = svc.HandleJobSuccess(ctx, job2, "worker-1")
		assert.NoError(t, err)
	})

	t.Run("HandleJobFailure_EdgeCases", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		svc := service.NewJobService(mockRepo, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))

		// 1. nil job
		err := svc.HandleJobFailure(ctx, nil, "worker-1", errors.New("err"))
		assert.Error(t, err)

		// 2. nil err
		job := &domain.Job{ID: "job-1", Status: domain.StatusRunning}
		err = svc.HandleJobFailure(ctx, job, "worker-1", nil)
		assert.Error(t, err)

		// 3. invalid status
		job2 := &domain.Job{ID: "job-2", Status: domain.StatusPending}
		err = svc.HandleJobFailure(ctx, job2, "worker-1", errors.New("err"))
		assert.Error(t, err)
	})

	t.Run("ReclaimOrphanedJobs_PruneStaleError", func(t *testing.T) {
		mockWrk := &mockWorkerRepository{}
		svc := service.NewJobService(&mockJobRepository{}, mockWrk, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(&mockJobRepository{}), scheduler.NewScheduler(log))
		mockWrk.pruneStaleFunc = func(ctx context.Context, maxInactivity time.Duration) (int64, error) {
			return 0, errors.New("prune failed")
		}
		err := svc.ReclaimOrphanedJobs(ctx)
		assert.Error(t, err)
	})

	t.Run("ReclaimOrphanedJobs_ListJobsError", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		svc := service.NewJobService(mockRepo, &mockWorkerRepository{}, &mockExecutionLogRepository{}, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))
		mockRepo.listJobsFunc = func(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error) {
			return nil, errors.New("list failed")
		}
		err := svc.ReclaimOrphanedJobs(ctx)
		assert.Error(t, err)
	})

	t.Run("ReclaimOrphanedJobs_ExecutionLogGetError", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		mockWrk := &mockWorkerRepository{}
		mockExec := &mockExecutionLogRepository{}
		svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))

		mockRepo.listJobsFunc = func(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error) {
			return []*domain.Job{{ID: "job-1", Status: domain.StatusRunning}}, nil
		}
		mockExec.getByJobIDFunc = func(ctx context.Context, jobID string) ([]*domain.ExecutionLog, error) {
			return nil, errors.New("get logs failed")
		}
		err := svc.ReclaimOrphanedJobs(ctx)
		assert.NoError(t, err) // ReclaimOrphanedJobs logs the error and continues
	})

	t.Run("ReclaimOrphanedJobs_NoExecutionLogs", func(t *testing.T) {
		mockRepo := &mockJobRepository{}
		mockWrk := &mockWorkerRepository{}
		mockExec := &mockExecutionLogRepository{}
		svc := service.NewJobService(mockRepo, mockWrk, mockExec, cfg, log, retry.NewRetryEngine(retry.NewExponentialBackoff(1*time.Second, 2*time.Second), log), dlq.NewRouter(mockRepo), scheduler.NewScheduler(log))

		mockRepo.listJobsFunc = func(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error) {
			return []*domain.Job{{ID: "job-1", Status: domain.StatusRunning}}, nil
		}
		mockExec.getByJobIDFunc = func(ctx context.Context, jobID string) ([]*domain.ExecutionLog, error) {
			return nil, nil
		}
		err := svc.ReclaimOrphanedJobs(ctx)
		assert.NoError(t, err)
	})
}
