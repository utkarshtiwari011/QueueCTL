package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"queuectl/internal/config"
	"queuectl/internal/dlq"
	"queuectl/internal/domain"
	"queuectl/internal/logger"
	"queuectl/internal/repository"
	"queuectl/internal/retry"
	"queuectl/internal/scheduler"
	"queuectl/internal/utils"

	"github.com/google/uuid"
)

var (
	ErrHandlerAlreadyRegistered = errors.New("handler already registered for this job type")
	ErrHandlerNotFound          = errors.New("no handler registered for this job type")
	ErrAlreadyReclaimed         = errors.New("job was already reclaimed or updated by another process")
)

// JobService defines the service interface for background jobs.
type JobService interface {
	// Enqueue creates and persists a new background job.
	Enqueue(ctx context.Context, jobType string, payload string, queue string, priority int, runAt time.Time, maxRetries int) (*domain.Job, error)

	// GetJob retrieves a job details by ID.
	GetJob(ctx context.Context, id string) (*domain.Job, error)

	// ListJobs lists jobs with filters.
	ListJobs(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error)

	// GetStats retrieves queue stats.
	GetStats(ctx context.Context) (map[string]map[domain.JobStatus]int, error)

	// PurgeCompleted deletes completed jobs.
	PurgeCompleted(ctx context.Context, olderThan time.Duration) (int64, error)

	// RegisterHandler registers a job handler function for a job type.
	RegisterHandler(jobType string, handler domain.JobHandler) error

	// ExecuteJob runs the handler associated with the job type.
	ExecuteJob(ctx context.Context, job *domain.Job) error

	// FetchNextJob acquires the next eligible job and starts an execution log entry.
	FetchNextJob(ctx context.Context, queue string, workerID string) (*domain.Job, error)

	// HandleJobSuccess marks the job as completed and updates execution log.
	HandleJobSuccess(ctx context.Context, job *domain.Job, workerID string) error

	// HandleJobFailure computes the retry backoff/DLQ and updates execution log.
	HandleJobFailure(ctx context.Context, job *domain.Job, workerID string, err error) error

	// RegisterWorker saves or updates worker heartbeats in the repository.
	RegisterWorker(ctx context.Context, w *domain.Worker) error

	// UnregisterWorker removes a worker from the active pool.
	UnregisterWorker(ctx context.Context, workerID string) error

	// ReclaimOrphanedJobs finds jobs running on stale/dead workers and resets/retries them.
	ReclaimOrphanedJobs(ctx context.Context) error
}

type jobService struct {
	repo        repository.JobRepository
	workerRepo  repository.WorkerRepository
	execLogRepo repository.ExecutionLogRepository
	cfg         *config.Config
	logger      logger.Logger
	handlersMu  sync.RWMutex
	handlers    map[string]domain.JobHandler
	retryEngine retry.RetryEngine
	dlqRouter   dlq.Router
	sched       scheduler.Scheduler
}

// NewJobService creates a new JobService.
func NewJobService(
	repo repository.JobRepository,
	workerRepo repository.WorkerRepository,
	execLogRepo repository.ExecutionLogRepository,
	cfg *config.Config,
	logger logger.Logger,
	retryEngine retry.RetryEngine,
	dlqRouter dlq.Router,
	sched scheduler.Scheduler,
) JobService {
	return &jobService{
		repo:        repo,
		workerRepo:  workerRepo,
		execLogRepo: execLogRepo,
		cfg:         cfg,
		logger:      logger,
		handlers:    make(map[string]domain.JobHandler),
		retryEngine: retryEngine,
		dlqRouter:   dlqRouter,
		sched:       sched,
	}
}

// Enqueue builds and stores a job in the queue database.
func (s *jobService) Enqueue(ctx context.Context, jobType string, payload string, queue string, priority int, runAt time.Time, maxRetries int) (*domain.Job, error) {
	// 1. Validation
	if jobType == "" {
		return nil, errors.New("job type cannot be empty")
	}
	if err := utils.ValidateJSON(payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}

	if queue == "" {
		queue = "default"
	}
	if runAt.IsZero() {
		runAt = time.Now()
	}
	if maxRetries <= 0 {
		maxRetries = s.cfg.Worker.MaxRetries
	}

	job, err := domain.NewJob(uuid.New().String(), jobType, payload, queue, priority, maxRetries, runAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create job entity: %w", err)
	}

	if err := s.repo.Insert(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to enqueue job: %w", err)
	}

	// Trigger scheduler notification to wake workers instantly
	if s.sched != nil {
		s.sched.Notify()
	}

	s.logger.Debug("job enqueued successfully",
		logger.String("job_id", job.ID),
		logger.String("type", job.Type),
		logger.String("queue", job.Queue),
		logger.Int("priority", job.Priority),
	)

	return job, nil
}

// GetJob retrieves the job details.
func (s *jobService) GetJob(ctx context.Context, id string) (*domain.Job, error) {
	if id == "" {
		return nil, errors.New("job ID cannot be empty")
	}
	job, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get job details: %w", err)
	}
	return job, nil
}

// ListJobs lists jobs based on filtering rules.
func (s *jobService) ListJobs(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error) {
	jobs, err := s.repo.ListJobs(ctx, queue, status, search, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}
	return jobs, nil
}

// GetStats returns counts by queue and status.
func (s *jobService) GetStats(ctx context.Context) (map[string]map[domain.JobStatus]int, error) {
	stats, err := s.repo.GetQueueStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get queue stats: %w", err)
	}
	return stats, nil
}

// PurgeCompleted deletes completed jobs older than duration.
func (s *jobService) PurgeCompleted(ctx context.Context, olderThan time.Duration) (int64, error) {
	count, err := s.repo.DeleteCompletedJobs(ctx, olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to purge completed jobs: %w", err)
	}
	return count, nil
}

// RegisterHandler registers a job handler function safely with concurrency locks.
func (s *jobService) RegisterHandler(jobType string, handler domain.JobHandler) error {
	if jobType == "" {
		return errors.New("handler job type cannot be empty")
	}
	if handler == nil {
		return errors.New("handler function cannot be nil")
	}

	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()

	if _, exists := s.handlers[jobType]; exists {
		return fmt.Errorf("%w: %s", ErrHandlerAlreadyRegistered, jobType)
	}

	s.handlers[jobType] = handler
	s.logger.Info("registered handler for job type", logger.String("job_type", jobType))
	return nil
}

// ExecuteJob retrieves and executes the registered handler for the job type.
func (s *jobService) ExecuteJob(ctx context.Context, job *domain.Job) error {
	s.handlersMu.RLock()
	handler, exists := s.handlers[job.Type]
	s.handlersMu.RUnlock()

	if !exists {
		return fmt.Errorf("%w: %s", ErrHandlerNotFound, job.Type)
	}

	return handler(ctx, job.Payload)
}

// FetchNextJob acquires a job and inserts an execution log in a single transaction.
func (s *jobService) FetchNextJob(ctx context.Context, queue string, workerID string) (*domain.Job, error) {
	if workerID == "" {
		return nil, errors.New("worker ID cannot be empty")
	}

	var acquiredJob *domain.Job
	err := s.repo.WithTx(ctx, func(txCtx context.Context) error {
		// 1. Acquire job
		job, err := s.repo.AcquireNextPendingJob(txCtx, queue)
		if err != nil {
			return err
		}
		if job == nil {
			return nil
		}

		// 2. Insert execution log
		execLog, err := domain.NewExecutionLog(
			uuid.New().String(),
			job.ID,
			workerID,
			job.RetriesCount+1,
			time.Now(),
		)
		if err != nil {
			return fmt.Errorf("failed to create execution log domain: %w", err)
		}

		if err := s.execLogRepo.Insert(txCtx, execLog); err != nil {
			return fmt.Errorf("failed to write execution log entry: %w", err)
		}

		acquiredJob = job
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to fetch next job: %w", err)
	}

	return acquiredJob, nil
}

// HandleJobSuccess updates job state and closes the execution log attempt.
func (s *jobService) HandleJobSuccess(ctx context.Context, job *domain.Job, workerID string) error {
	if job == nil {
		return errors.New("job cannot be nil")
	}

	if job.Status != domain.StatusRunning {
		return fmt.Errorf("invalid job state transition: cannot complete job in %s state", job.Status)
	}

	err := s.repo.WithTx(ctx, func(txCtx context.Context) error {
		// 1. Fetch fresh job status from DB inside the transaction
		latestJob, err := s.repo.GetByID(txCtx, job.ID)
		if err != nil {
			return err
		}

		if latestJob.Status != domain.StatusRunning {
			return ErrAlreadyReclaimed
		}

		// Update job status
		latestJob.Status = domain.StatusCompleted
		latestJob.ErrorMessage = ""

		if err := s.repo.Update(txCtx, latestJob); err != nil {
			return fmt.Errorf("failed to update job success status: %w", err)
		}

		// 2. Fetch and complete execution log
		logs, err := s.execLogRepo.GetByJobID(txCtx, latestJob.ID)
		if err != nil {
			return fmt.Errorf("failed to retrieve execution log for update: %w", err)
		}

		if len(logs) > 0 {
			latestLog := logs[len(logs)-1]
			if latestLog.WorkerID == workerID && latestLog.FinishedAt.IsZero() {
				err = latestLog.Complete(domain.ExecutionSuccess, time.Now(), "")
				if err != nil {
					return fmt.Errorf("failed to complete execution log structure: %w", err)
				}
				if err := s.execLogRepo.Update(txCtx, latestLog); err != nil {
					return fmt.Errorf("failed to write completed execution log: %w", err)
				}
			}
		}

		// Copy reloaded and updated state back to caller's object
		*job = *latestJob
		return nil
	})

	if err != nil {
		if errors.Is(err, ErrAlreadyReclaimed) {
			return ErrAlreadyReclaimed
		}
		return fmt.Errorf("failed to handle job success: %w", err)
	}

	s.logger.Info("job execution completed successfully", logger.String("job_id", job.ID))
	return nil
}

// HandleJobFailure calculates retry intervals or DLQ routing and logs outcomes.
func (s *jobService) HandleJobFailure(ctx context.Context, job *domain.Job, workerID string, failureErr error) error {
	if job == nil {
		return errors.New("job cannot be nil")
	}
	if failureErr == nil {
		return errors.New("error reason cannot be nil")
	}

	if job.Status != domain.StatusRunning {
		return fmt.Errorf("invalid job state transition: cannot fail job in %s state", job.Status)
	}

	txErr := s.repo.WithTx(ctx, func(txCtx context.Context) error {
		// 1. Fetch fresh job status from DB inside the transaction
		latestJob, err := s.repo.GetByID(txCtx, job.ID)
		if err != nil {
			return err
		}

		if latestJob.Status != domain.StatusRunning {
			return ErrAlreadyReclaimed
		}

		// Ensure the job is still owned by the expected worker (if workerID is not empty)
		if workerID != "" {
			logs, err := s.execLogRepo.GetByJobID(txCtx, latestJob.ID)
			if err != nil {
				return err
			}
			if len(logs) > 0 {
				latestLog := logs[len(logs)-1]
				if latestLog.WorkerID != workerID {
					return ErrAlreadyReclaimed
				}
				if !latestLog.FinishedAt.IsZero() {
					return ErrAlreadyReclaimed
				}
			}
		}

		// Delegate failure logic to RetryEngine
		_, shouldRetry, retryErr := s.retryEngine.HandleFailure(txCtx, latestJob, failureErr)
		if retryErr != nil {
			return fmt.Errorf("failed to evaluate retry parameters: %w", retryErr)
		}

		if !shouldRetry {
			if dlqErr := s.dlqRouter.Route(txCtx, latestJob, failureErr); dlqErr != nil {
				return fmt.Errorf("failed to route job to DLQ: %w", dlqErr)
			}
		} else {
			if updateErr := s.repo.Update(txCtx, latestJob); updateErr != nil {
				return fmt.Errorf("failed to update job status on failure: %w", updateErr)
			}
		}

		// Fetch and complete execution log
		logs, err := s.execLogRepo.GetByJobID(txCtx, latestJob.ID)
		if err != nil {
			return fmt.Errorf("failed to retrieve execution log for update: %w", err)
		}

		if len(logs) > 0 {
			latestLog := logs[len(logs)-1]
			if (workerID == "" || latestLog.WorkerID == workerID) && latestLog.FinishedAt.IsZero() {
				logErr := latestLog.Complete(domain.ExecutionFailed, time.Now(), latestJob.ErrorMessage)
				if logErr != nil {
					return fmt.Errorf("failed to complete execution log failure structure: %w", logErr)
				}
				if updateErr := s.execLogRepo.Update(txCtx, latestLog); updateErr != nil {
					return fmt.Errorf("failed to write failed execution log: %w", updateErr)
				}
			}
		}

		// Copy reloaded and updated state back to caller's object
		*job = *latestJob
		return nil
	})

	if txErr != nil {
		if errors.Is(txErr, ErrAlreadyReclaimed) {
			return ErrAlreadyReclaimed
		}
		return fmt.Errorf("failed to handle job failure: %w", txErr)
	}

	return nil
}

// RegisterWorker updates active status heartbeats of worker processes.
func (s *jobService) RegisterWorker(ctx context.Context, w *domain.Worker) error {
	if w == nil {
		return errors.New("worker cannot be nil")
	}
	if err := w.Validate(); err != nil {
		return fmt.Errorf("invalid worker attributes: %w", err)
	}

	if err := s.workerRepo.Upsert(ctx, w); err != nil {
		return fmt.Errorf("failed to register worker node: %w", err)
	}

	return nil
}

// UnregisterWorker removes worker process registry records.
func (s *jobService) UnregisterWorker(ctx context.Context, workerID string) error {
	if workerID == "" {
		return errors.New("worker ID cannot be empty")
	}

	if err := s.workerRepo.Delete(ctx, workerID); err != nil {
		return fmt.Errorf("failed to unregister worker node: %w", err)
	}

	return nil
}

// ReclaimOrphanedJobs prunes dead/inactive worker nodes and reclaims their executing jobs.
func (s *jobService) ReclaimOrphanedJobs(ctx context.Context) error {
	// 1. Prune stale workers (inactivity threshold of 30 seconds)
	staleCount, err := s.workerRepo.PruneStale(ctx, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to prune stale workers: %w", err)
	}
	if staleCount > 0 {
		s.logger.Info("pruned stale workers", logger.Int64("count", staleCount))
	}

	// 2. Fetch all running jobs
	runningJobs, err := s.repo.ListJobs(ctx, "", domain.StatusRunning, "", 1000)
	if err != nil {
		return fmt.Errorf("failed to list running jobs: %w", err)
	}

	for _, job := range runningJobs {
		// Fetch latest execution log to check worker status
		logs, err := s.execLogRepo.GetByJobID(ctx, job.ID)
		if err != nil {
			s.logger.Error("failed to get execution logs for running job", logger.String("job_id", job.ID), logger.Error(err))
			continue
		}

		var deadWorkerID string
		var isStale bool

		if len(logs) == 0 {
			// No execution logs! This is a stale running job from previous crashes/malfunctions.
			isStale = true
			deadWorkerID = ""
		} else {
			latestLog := logs[len(logs)-1]
			deadWorkerID = latestLog.WorkerID

			// Check if the worker that claimed the job is stopped or stale
			worker, err := s.workerRepo.GetByID(ctx, latestLog.WorkerID)
			if err != nil {
				// If the error indicates not found, then the worker is dead/pruned
				if strings.Contains(err.Error(), "worker not found") {
					isStale = true
				} else {
					s.logger.Error("failed to query worker status for running job", logger.String("job_id", job.ID), logger.String("worker_id", latestLog.WorkerID), logger.Error(err))
					continue
				}
			} else if worker.Status == domain.WorkerStatusStopped {
				isStale = true
			}
		}

		if isStale {
			s.logger.Warn("reclaiming orphaned running job",
				logger.String("job_id", job.ID),
				logger.String("worker_id", deadWorkerID),
			)
			reclaimErr := errors.New("worker process terminated unexpectedly")
			if err := s.HandleJobFailure(ctx, job, deadWorkerID, reclaimErr); err != nil {
				if errors.Is(err, ErrAlreadyReclaimed) {
					// 6. Ignore jobs already reclaimed by another worker instead of logging errors
					continue
				}
				s.logger.Error("failed to reclaim orphaned job", logger.String("job_id", job.ID), logger.Error(err))
			} else {
				// 10. Update metrics after successful reclaim (meaning scheduler notification for pending state is triggered)
				if job.Status == domain.StatusPending {
					s.sched.Notify()
				}
			}
		}
	}
	return nil
}
