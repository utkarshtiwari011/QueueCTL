package dlq

import (
	"context"
	"fmt"
	"time"

	"queuectl/internal/domain"
	"queuectl/internal/logger"
	"queuectl/internal/repository"
)

// Router handles transitioning exhausted jobs to the Dead Letter Queue (DLQ).
type Router interface {
	// Route moves a job to DLQ status and records the failure reason.
	Route(ctx context.Context, job *domain.Job, jobErr error) error
}

type sqliteDLQRouter struct {
	repo repository.JobRepository
}

// NewRouter instantiates a concrete Router using the JobRepository.
func NewRouter(repo repository.JobRepository) Router {
	return &sqliteDLQRouter{
		repo: repo,
	}
}

// Route sets the job status to StatusDeadLetter and stores the error message.
func (r *sqliteDLQRouter) Route(ctx context.Context, job *domain.Job, jobErr error) error {
	job.Status = domain.StatusDeadLetter
	if jobErr != nil {
		job.ErrorMessage = jobErr.Error()
	} else {
		job.ErrorMessage = "Unknown execution failure"
	}

	if err := r.repo.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to route job %s to DLQ: %w", job.ID, err)
	}

	return nil
}

// Service defines DLQ management commands (retry, delete, list, restore, stats).
type Service interface {
	List(ctx context.Context, queueFilter string, searchFilter string, limit int) ([]*domain.Job, error)
	Retry(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	Restore(ctx context.Context, id string, targetQueue string) error
	GetStats(ctx context.Context) (map[string]int, error)
}

type dlqService struct {
	repo   repository.JobRepository
	logger logger.Logger
}

// NewDLQService instantiates a new DLQService.
func NewDLQService(repo repository.JobRepository, log logger.Logger) Service {
	return &dlqService{
		repo:   repo,
		logger: log,
	}
}

// List queries Dead Letter Queue records matching optional filters and search strings.
func (s *dlqService) List(ctx context.Context, queueFilter string, searchFilter string, limit int) ([]*domain.Job, error) {
	jobs, err := s.repo.ListJobs(ctx, queueFilter, domain.StatusDeadLetter, searchFilter, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list DLQ records: %w", err)
	}
	return jobs, nil
}

// Retry schedules a dead job back to pending for immediate re-execution.
func (s *dlqService) Retry(ctx context.Context, id string) error {
	job, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to fetch DLQ job %s: %w", id, err)
	}

	if job.Status != domain.StatusDeadLetter {
		return fmt.Errorf("job %s is not in DLQ status (current: %s)", id, job.Status)
	}

	job.Status = domain.StatusPending
	job.RetriesCount = 0
	job.ErrorMessage = ""
	job.RunAt = time.Now().UTC()

	if err := s.repo.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to retry DLQ job %s: %w", id, err)
	}

	s.logger.Info("re-enqueued job from Dead Letter Queue", logger.String("job_id", id))
	return nil
}

// Delete removes a DLQ job from the database.
func (s *dlqService) Delete(ctx context.Context, id string) error {
	job, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to fetch DLQ job %s: %w", id, err)
	}

	if job.Status != domain.StatusDeadLetter {
		return fmt.Errorf("job %s is not in DLQ status (current: %s)", id, job.Status)
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete DLQ job %s: %w", id, err)
	}

	s.logger.Info("deleted job from Dead Letter Queue", logger.String("job_id", id))
	return nil
}

// Restore moves a DLQ job to a different queue and schedules it.
func (s *dlqService) Restore(ctx context.Context, id string, targetQueue string) error {
	if targetQueue == "" {
		return fmt.Errorf("target queue name cannot be empty")
	}

	job, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to fetch DLQ job %s: %w", id, err)
	}

	if job.Status != domain.StatusDeadLetter {
		return fmt.Errorf("job %s is not in DLQ status (current: %s)", id, job.Status)
	}

	job.Queue = targetQueue
	job.Status = domain.StatusPending
	job.RetriesCount = 0
	job.ErrorMessage = ""
	job.RunAt = time.Now().UTC()

	if err := s.repo.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to restore DLQ job %s: %w", id, err)
	}

	s.logger.Info("restored job from Dead Letter Queue to target queue",
		logger.String("job_id", id),
		logger.String("target_queue", targetQueue),
	)
	return nil
}

// GetStats returns aggregated statistics for jobs in DLQ grouped by queue name.
func (s *dlqService) GetStats(ctx context.Context) (map[string]int, error) {
	stats, err := s.repo.GetQueueStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get queue stats for DLQ: %w", err)
	}

	dlqStats := make(map[string]int)
	for queue, statusCounts := range stats {
		if count, ok := statusCounts[domain.StatusDeadLetter]; ok && count > 0 {
			dlqStats[queue] = count
		}
	}
	return dlqStats, nil
}
