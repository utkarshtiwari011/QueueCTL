package retry

import (
	"context"
	"time"

	"queuectl/internal/domain"
	"queuectl/internal/logger"
)

// RetryEngine defines the contract for handling job failures and retry scheduling.
type RetryEngine interface {
	// HandleFailure evaluates a job failure, increments its attempt count, and determines
	// if the job should be retried (returning the delay duration and true) or routed to DLQ (returning false).
	HandleFailure(ctx context.Context, job *domain.Job, jobErr error) (time.Duration, bool, error)
}

type retryEngine struct {
	backoff Backoff
	logger  logger.Logger
}

// NewRetryEngine instantiates a concrete RetryEngine.
func NewRetryEngine(backoff Backoff, logger logger.Logger) RetryEngine {
	return &retryEngine{
		backoff: backoff,
		logger:  logger,
	}
}

// HandleFailure calculates the retry parameters and mutates the job state.
func (e *retryEngine) HandleFailure(ctx context.Context, job *domain.Job, jobErr error) (time.Duration, bool, error) {
	if job == nil {
		return 0, false, nil
	}

	job.RetriesCount++
	job.ErrorMessage = jobErr.Error()

	// Check if retry threshold is breached
	if job.RetriesCount > job.MaxRetries {
		e.logger.Warn("job failed and exceeded max retries limit",
			logger.String("job_id", job.ID),
			logger.Int("retries_count", job.RetriesCount),
			logger.Int("max_retries", job.MaxRetries),
			logger.Error(jobErr),
		)
		return 0, false, nil
	}

	// Calculate exponential backoff duration
	backoffDuration := e.backoff.Calculate(job.RetriesCount)
	job.Status = domain.StatusPending
	job.RunAt = time.Now().Add(backoffDuration).UTC()

	e.logger.Info("job execution failed; scheduled for retry",
		logger.String("job_id", job.ID),
		logger.Int("retry_attempt", job.RetriesCount),
		logger.Duration("backoff_delay", backoffDuration),
		logger.Error(jobErr),
	)

	return backoffDuration, true, nil
}
