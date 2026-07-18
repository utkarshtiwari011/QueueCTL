package dlq_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"queuectl/internal/dlq"
	"queuectl/internal/domain"
	"queuectl/internal/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockJobRepository struct {
	getByIDFunc       func(ctx context.Context, id string) (*domain.Job, error)
	updateFunc        func(ctx context.Context, job *domain.Job) error
	listJobsFunc      func(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error)
	deleteFunc        func(ctx context.Context, id string) error
	getQueueStatsFunc func(ctx context.Context) (map[string]map[domain.JobStatus]int, error)
}

func (m *mockJobRepository) Insert(ctx context.Context, job *domain.Job) error { return nil }
func (m *mockJobRepository) GetByID(ctx context.Context, id string) (*domain.Job, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, nil
}
func (m *mockJobRepository) Update(ctx context.Context, job *domain.Job) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, job)
	}
	return nil
}
func (m *mockJobRepository) AcquireNextPendingJob(ctx context.Context, queue string) (*domain.Job, error) {
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
func (m *mockJobRepository) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}
func (m *mockJobRepository) DeleteCompletedJobs(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}
func (m *mockJobRepository) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

func TestDLQService_Route(t *testing.T) {
	mockRepo := &mockJobRepository{}
	router := dlq.NewRouter(mockRepo)

	job := &domain.Job{
		ID:           "test-job",
		Status:       domain.StatusRunning,
		RetriesCount: 3,
	}

	var updatedJob *domain.Job
	mockRepo.updateFunc = func(ctx context.Context, j *domain.Job) error {
		updatedJob = j
		return nil
	}

	err := router.Route(context.Background(), job, errors.New("persistent handler crash"))
	require.NoError(t, err)
	assert.Equal(t, domain.StatusDeadLetter, updatedJob.Status)
	assert.Equal(t, "persistent handler crash", updatedJob.ErrorMessage)
}

func TestDLQService_Retry(t *testing.T) {
	mockRepo := &mockJobRepository{}
	log := logger.NewNop()
	svc := dlq.NewDLQService(mockRepo, log)

	job := &domain.Job{
		ID:           "dead-job",
		Status:       domain.StatusDeadLetter,
		ErrorMessage: "previous failure reason",
		RetriesCount: 3,
	}

	mockRepo.getByIDFunc = func(ctx context.Context, id string) (*domain.Job, error) {
		assert.Equal(t, "dead-job", id)
		return job, nil
	}

	var updatedJob *domain.Job
	mockRepo.updateFunc = func(ctx context.Context, j *domain.Job) error {
		updatedJob = j
		return nil
	}

	err := svc.Retry(context.Background(), "dead-job")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusPending, updatedJob.Status)
	assert.Equal(t, 0, updatedJob.RetriesCount)
	assert.Equal(t, "", updatedJob.ErrorMessage)
}

func TestDLQService_Delete(t *testing.T) {
	mockRepo := &mockJobRepository{}
	log := logger.NewNop()
	svc := dlq.NewDLQService(mockRepo, log)

	job := &domain.Job{
		ID:     "delete-job",
		Status: domain.StatusDeadLetter,
	}

	mockRepo.getByIDFunc = func(ctx context.Context, id string) (*domain.Job, error) {
		return job, nil
	}

	var deletedID string
	mockRepo.deleteFunc = func(ctx context.Context, id string) error {
		deletedID = id
		return nil
	}

	err := svc.Delete(context.Background(), "delete-job")
	require.NoError(t, err)
	assert.Equal(t, "delete-job", deletedID)
}

func TestDLQService_Restore(t *testing.T) {
	mockRepo := &mockJobRepository{}
	log := logger.NewNop()
	svc := dlq.NewDLQService(mockRepo, log)

	job := &domain.Job{
		ID:     "restore-job",
		Queue:  "default",
		Status: domain.StatusDeadLetter,
	}

	mockRepo.getByIDFunc = func(ctx context.Context, id string) (*domain.Job, error) {
		return job, nil
	}

	var updatedJob *domain.Job
	mockRepo.updateFunc = func(ctx context.Context, j *domain.Job) error {
		updatedJob = j
		return nil
	}

	err := svc.Restore(context.Background(), "restore-job", "high-priority")
	require.NoError(t, err)
	assert.Equal(t, "high-priority", updatedJob.Queue)
	assert.Equal(t, domain.StatusPending, updatedJob.Status)
	assert.Equal(t, 0, updatedJob.RetriesCount)
}

func TestDLQService_GetStats(t *testing.T) {
	mockRepo := &mockJobRepository{}
	log := logger.NewNop()
	svc := dlq.NewDLQService(mockRepo, log)

	mockRepo.getQueueStatsFunc = func(ctx context.Context) (map[string]map[domain.JobStatus]int, error) {
		return map[string]map[domain.JobStatus]int{
			"default": {
				domain.StatusPending:    10,
				domain.StatusDeadLetter: 5,
			},
			"processing": {
				domain.StatusRunning:    2,
				domain.StatusDeadLetter: 12,
			},
		}, nil
	}

	stats, err := svc.GetStats(context.Background())
	require.NoError(t, err)
	assert.Len(t, stats, 2)
	assert.Equal(t, 5, stats["default"])
	assert.Equal(t, 12, stats["processing"])
}

func TestDLQRouter_RouteNilError(t *testing.T) {
	mockRepo := &mockJobRepository{}
	router := dlq.NewRouter(mockRepo)

	job := &domain.Job{
		ID:     "test-job",
		Status: domain.StatusRunning,
	}

	var updatedJob *domain.Job
	mockRepo.updateFunc = func(ctx context.Context, j *domain.Job) error {
		updatedJob = j
		return nil
	}

	err := router.Route(context.Background(), job, nil)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusDeadLetter, updatedJob.Status)
	assert.Equal(t, "Unknown execution failure", updatedJob.ErrorMessage)
}

func TestDLQRouter_RouteUpdateError(t *testing.T) {
	mockRepo := &mockJobRepository{}
	router := dlq.NewRouter(mockRepo)

	job := &domain.Job{ID: "test-job"}
	mockRepo.updateFunc = func(ctx context.Context, j *domain.Job) error {
		return errors.New("write failed")
	}

	err := router.Route(context.Background(), job, nil)
	assert.Error(t, err)
}

func TestDLQService_ListError(t *testing.T) {
	mockRepo := &mockJobRepository{}
	svc := dlq.NewDLQService(mockRepo, logger.NewNop())

	mockRepo.listJobsFunc = func(ctx context.Context, q string, s domain.JobStatus, search string, limit int) ([]*domain.Job, error) {
		return nil, errors.New("list failed")
	}

	_, err := svc.List(context.Background(), "", "", 10)
	assert.Error(t, err)
}

func TestDLQService_RetryErrors(t *testing.T) {
	mockRepo := &mockJobRepository{}
	svc := dlq.NewDLQService(mockRepo, logger.NewNop())

	// 1. Fetch error
	mockRepo.getByIDFunc = func(ctx context.Context, id string) (*domain.Job, error) {
		return nil, errors.New("not found")
	}
	err := svc.Retry(context.Background(), "job-1")
	assert.Error(t, err)

	// 2. Status check error
	job := &domain.Job{ID: "job-1", Status: domain.StatusPending}
	mockRepo.getByIDFunc = func(ctx context.Context, id string) (*domain.Job, error) {
		return job, nil
	}
	err = svc.Retry(context.Background(), "job-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is not in DLQ status")

	// 3. Update error
	job.Status = domain.StatusDeadLetter
	mockRepo.updateFunc = func(ctx context.Context, j *domain.Job) error {
		return errors.New("update failed")
	}
	err = svc.Retry(context.Background(), "job-1")
	assert.Error(t, err)
}

func TestDLQService_DeleteErrors(t *testing.T) {
	mockRepo := &mockJobRepository{}
	svc := dlq.NewDLQService(mockRepo, logger.NewNop())

	// 1. Fetch error
	mockRepo.getByIDFunc = func(ctx context.Context, id string) (*domain.Job, error) {
		return nil, errors.New("not found")
	}
	err := svc.Delete(context.Background(), "job-1")
	assert.Error(t, err)

	// 2. Status check error
	job := &domain.Job{ID: "job-1", Status: domain.StatusPending}
	mockRepo.getByIDFunc = func(ctx context.Context, id string) (*domain.Job, error) {
		return job, nil
	}
	err = svc.Delete(context.Background(), "job-1")
	assert.Error(t, err)

	// 3. Delete error
	job.Status = domain.StatusDeadLetter
	mockRepo.deleteFunc = func(ctx context.Context, id string) error {
		return errors.New("delete failed")
	}
	err = svc.Delete(context.Background(), "job-1")
	assert.Error(t, err)
}

func TestDLQService_RestoreErrors(t *testing.T) {
	mockRepo := &mockJobRepository{}
	svc := dlq.NewDLQService(mockRepo, logger.NewNop())

	// 1. Target empty
	err := svc.Restore(context.Background(), "job-1", "")
	assert.Error(t, err)

	// 2. Fetch error
	mockRepo.getByIDFunc = func(ctx context.Context, id string) (*domain.Job, error) {
		return nil, errors.New("not found")
	}
	err = svc.Restore(context.Background(), "job-1", "queue")
	assert.Error(t, err)

	// 3. Status check error
	job := &domain.Job{ID: "job-1", Status: domain.StatusPending}
	mockRepo.getByIDFunc = func(ctx context.Context, id string) (*domain.Job, error) {
		return job, nil
	}
	err = svc.Restore(context.Background(), "job-1", "queue")
	assert.Error(t, err)

	// 4. Update error
	job.Status = domain.StatusDeadLetter
	mockRepo.updateFunc = func(ctx context.Context, j *domain.Job) error {
		return errors.New("update failed")
	}
	err = svc.Restore(context.Background(), "job-1", "queue")
	assert.Error(t, err)
}

func TestDLQService_GetStatsError(t *testing.T) {
	mockRepo := &mockJobRepository{}
	svc := dlq.NewDLQService(mockRepo, logger.NewNop())

	mockRepo.getQueueStatsFunc = func(ctx context.Context) (map[string]map[domain.JobStatus]int, error) {
		return nil, errors.New("stats query failed")
	}

	_, err := svc.GetStats(context.Background())
	assert.Error(t, err)
}
