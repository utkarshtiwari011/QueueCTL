package worker_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"queuectl/internal/config"
	"queuectl/internal/domain"
	"queuectl/internal/logger"
	"queuectl/internal/scheduler"
	"queuectl/internal/worker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockJobRepository struct {
	acquireNextPendingJobFunc func(ctx context.Context, queue string) (*domain.Job, error)
	updateFunc                func(ctx context.Context, job *domain.Job) error
}

func (m *mockJobRepository) Insert(ctx context.Context, job *domain.Job) error { return nil }
func (m *mockJobRepository) GetByID(ctx context.Context, id string) (*domain.Job, error) {
	return nil, nil
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
	return nil, nil
}
func (m *mockJobRepository) GetQueueStats(ctx context.Context) (map[string]map[domain.JobStatus]int, error) {
	return nil, nil
}
func (m *mockJobRepository) Delete(ctx context.Context, id string) error { return nil }
func (m *mockJobRepository) DeleteCompletedJobs(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}
func (m *mockJobRepository) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type mockJobService struct {
	registerWorkerFunc      func(ctx context.Context, w *domain.Worker) error
	unregisterWorkerFunc    func(ctx context.Context, workerID string) error
	fetchNextJobFunc        func(ctx context.Context, queue string, workerID string) (*domain.Job, error)
	executeJobFunc          func(ctx context.Context, job *domain.Job) error
	handleJobSuccessFunc    func(ctx context.Context, job *domain.Job, workerID string) error
	handleJobFailureFunc    func(ctx context.Context, job *domain.Job, workerID string, err error) error
	reclaimOrphanedJobsFunc func(ctx context.Context) error
}

func (m *mockJobService) Enqueue(ctx context.Context, jobType string, payload string, queue string, priority int, runAt time.Time, maxRetries int) (*domain.Job, error) {
	return nil, nil
}
func (m *mockJobService) GetJob(ctx context.Context, id string) (*domain.Job, error) {
	return nil, nil
}
func (m *mockJobService) ListJobs(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error) {
	return nil, nil
}
func (m *mockJobService) GetStats(ctx context.Context) (map[string]map[domain.JobStatus]int, error) {
	return nil, nil
}
func (m *mockJobService) PurgeCompleted(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}
func (m *mockJobService) RegisterHandler(jobType string, handler domain.JobHandler) error { return nil }
func (m *mockJobService) ExecuteJob(ctx context.Context, job *domain.Job) error {
	if m.executeJobFunc != nil {
		return m.executeJobFunc(ctx, job)
	}
	return nil
}
func (m *mockJobService) FetchNextJob(ctx context.Context, queue string, workerID string) (*domain.Job, error) {
	if m.fetchNextJobFunc != nil {
		return m.fetchNextJobFunc(ctx, queue, workerID)
	}
	return nil, nil
}
func (m *mockJobService) HandleJobSuccess(ctx context.Context, job *domain.Job, workerID string) error {
	if m.handleJobSuccessFunc != nil {
		return m.handleJobSuccessFunc(ctx, job, workerID)
	}
	return nil
}
func (m *mockJobService) HandleJobFailure(ctx context.Context, job *domain.Job, workerID string, err error) error {
	if m.handleJobFailureFunc != nil {
		return m.handleJobFailureFunc(ctx, job, workerID, err)
	}
	return nil
}
func (m *mockJobService) RegisterWorker(ctx context.Context, w *domain.Worker) error {
	if m.registerWorkerFunc != nil {
		return m.registerWorkerFunc(ctx, w)
	}
	return nil
}
func (m *mockJobService) UnregisterWorker(ctx context.Context, workerID string) error {
	if m.unregisterWorkerFunc != nil {
		return m.unregisterWorkerFunc(ctx, workerID)
	}
	return nil
}
func (m *mockJobService) ReclaimOrphanedJobs(ctx context.Context) error {
	if m.reclaimOrphanedJobsFunc != nil {
		return m.reclaimOrphanedJobsFunc(ctx)
	}
	return nil
}

func TestWorkerPool_PollingAndExecution(t *testing.T) {
	log := logger.NewNop()
	mockRepo := &mockJobRepository{}
	mockSvc := &mockJobService{}
	sched := scheduler.NewScheduler(log)

	cfg := &config.Config{}
	cfg.Worker.Concurrency = 2
	cfg.Worker.PollInterval = 50 * time.Millisecond

	// Setup job mock
	job := &domain.Job{
		ID:     "exec-job",
		Type:   "test",
		Status: domain.StatusPending,
	}

	mockSvc.fetchNextJobFunc = func(ctx context.Context, queue string, workerID string) (*domain.Job, error) {
		return job, nil
	}

	var executeWG sync.WaitGroup
	executeWG.Add(1)

	var successCalled bool
	mockSvc.executeJobFunc = func(ctx context.Context, j *domain.Job) error {
		assert.Equal(t, "exec-job", j.ID)
		return nil
	}

	mockSvc.handleJobSuccessFunc = func(ctx context.Context, j *domain.Job, workerID string) error {
		successCalled = true
		executeWG.Done()
		return nil
	}

	pool := worker.NewWorkerPool(mockRepo, mockSvc, sched, cfg, log, "default")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = pool.Start(ctx)
	}()

	// Wake poller immediately
	sched.Notify()

	// Wait for handler execution
	doneChan := make(chan struct{})
	go func() {
		executeWG.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for job execution")
	}

	pool.Stop()
	assert.True(t, successCalled)
}

func TestWorkerPool_PanicRecovery(t *testing.T) {
	log := logger.NewNop()
	mockRepo := &mockJobRepository{}
	mockSvc := &mockJobService{}
	sched := scheduler.NewScheduler(log)

	cfg := &config.Config{}
	cfg.Worker.Concurrency = 1
	cfg.Worker.PollInterval = 50 * time.Millisecond

	job := &domain.Job{
		ID:     "panic-job",
		Type:   "panic_demo",
		Status: domain.StatusPending,
	}

	mockSvc.fetchNextJobFunc = func(ctx context.Context, queue string, workerID string) (*domain.Job, error) {
		return job, nil
	}

	var failureWG sync.WaitGroup
	failureWG.Add(1)

	mockSvc.executeJobFunc = func(ctx context.Context, j *domain.Job) error {
		panic("boom") // Simulate handler crash
	}

	var failureError error
	mockSvc.handleJobFailureFunc = func(ctx context.Context, j *domain.Job, workerID string, err error) error {
		failureError = err
		failureWG.Done()
		return nil
	}

	pool := worker.NewWorkerPool(mockRepo, mockSvc, sched, cfg, log, "default")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = pool.Start(ctx)
	}()

	sched.Notify()

	doneChan := make(chan struct{})
	go func() {
		failureWG.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for panic recovery failure handler")
	}

	pool.Stop()
	require.NotNil(t, failureError)
	assert.Contains(t, failureError.Error(), "handler panic: boom")
}

func TestWorkerPool_Start_RegisterWorkerError(t *testing.T) {
	log := logger.NewNop()
	mockRepo := &mockJobRepository{}
	mockSvc := &mockJobService{}
	sched := scheduler.NewScheduler(log)

	cfg := &config.Config{}
	cfg.Worker.Concurrency = 1
	cfg.Worker.PollInterval = 50 * time.Millisecond

	mockSvc.registerWorkerFunc = func(ctx context.Context, w *domain.Worker) error {
		return errors.New("registration failed")
	}

	pool := worker.NewWorkerPool(mockRepo, mockSvc, sched, cfg, log, "default")
	err := pool.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to register worker node at startup")
}

func TestWorkerPool_pollAndDispatch_FetchNextJobError(t *testing.T) {
	log := logger.NewNop()
	mockRepo := &mockJobRepository{}
	mockSvc := &mockJobService{}
	sched := scheduler.NewScheduler(log)

	cfg := &config.Config{}
	cfg.Worker.Concurrency = 1
	cfg.Worker.PollInterval = 50 * time.Millisecond

	mockSvc.fetchNextJobFunc = func(ctx context.Context, q string, w string) (*domain.Job, error) {
		return nil, errors.New("db query failed")
	}

	pool := worker.NewWorkerPool(mockRepo, mockSvc, sched, cfg, log, "default")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = pool.Start(ctx)
	}()

	sched.Notify()
	time.Sleep(100 * time.Millisecond)
	pool.Stop()
}

func TestWorkerPool_executeWorker_SuccessError(t *testing.T) {
	log := logger.NewNop()
	mockRepo := &mockJobRepository{}
	mockSvc := &mockJobService{}
	sched := scheduler.NewScheduler(log)

	cfg := &config.Config{}
	cfg.Worker.Concurrency = 1
	cfg.Worker.PollInterval = 50 * time.Millisecond

	job := &domain.Job{ID: "job-1", Type: "test"}
	mockSvc.fetchNextJobFunc = func(ctx context.Context, q string, w string) (*domain.Job, error) {
		return job, nil
	}
	mockSvc.executeJobFunc = func(ctx context.Context, j *domain.Job) error {
		return nil
	}
	mockSvc.handleJobSuccessFunc = func(ctx context.Context, j *domain.Job, w string) error {
		return errors.New("success write failed")
	}

	pool := worker.NewWorkerPool(mockRepo, mockSvc, sched, cfg, log, "default")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = pool.Start(ctx)
	}()

	sched.Notify()
	time.Sleep(100 * time.Millisecond)
	pool.Stop()
}

func TestWorkerPool_executeWorker_FailureError(t *testing.T) {
	log := logger.NewNop()
	mockRepo := &mockJobRepository{}
	mockSvc := &mockJobService{}
	sched := scheduler.NewScheduler(log)

	cfg := &config.Config{}
	cfg.Worker.Concurrency = 1
	cfg.Worker.PollInterval = 50 * time.Millisecond

	job := &domain.Job{ID: "job-1", Type: "test"}
	mockSvc.fetchNextJobFunc = func(ctx context.Context, q string, w string) (*domain.Job, error) {
		return job, nil
	}
	mockSvc.executeJobFunc = func(ctx context.Context, j *domain.Job) error {
		return errors.New("run error")
	}
	mockSvc.handleJobFailureFunc = func(ctx context.Context, j *domain.Job, w string, e error) error {
		return errors.New("failure write failed")
	}

	pool := worker.NewWorkerPool(mockRepo, mockSvc, sched, cfg, log, "default")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = pool.Start(ctx)
	}()

	sched.Notify()
	time.Sleep(100 * time.Millisecond)
	pool.Stop()
}
