package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"queuectl/internal/domain"
	"queuectl/internal/logger"
	"queuectl/internal/retry"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryEngine_HandleFailure_ScheduleRetry(t *testing.T) {
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(1*time.Second, 10*time.Second)
	engine := retry.NewRetryEngine(backoff, log)

	job := &domain.Job{
		ID:           "test-job",
		Status:       domain.StatusRunning,
		MaxRetries:   3,
		RetriesCount: 0,
	}

	ctx := context.Background()
	errReason := errors.New("temporary network timeout")

	// 1. First failure
	now := time.Now()
	delay, shouldRetry, err := engine.HandleFailure(ctx, job, errReason)
	require.NoError(t, err)
	assert.True(t, shouldRetry)
	assert.Equal(t, 1*time.Second, delay)
	assert.Equal(t, domain.StatusPending, job.Status)
	assert.Equal(t, 1, job.RetriesCount)
	assert.Equal(t, "temporary network timeout", job.ErrorMessage)
	assert.WithinDuration(t, now.Add(1*time.Second), job.RunAt, 100*time.Millisecond)

	// 2. Second failure (should calculate base * 2^1 = 2s)
	delay, shouldRetry, err = engine.HandleFailure(ctx, job, errReason)
	require.NoError(t, err)
	assert.True(t, shouldRetry)
	assert.Equal(t, 2*time.Second, delay)
	assert.Equal(t, 2, job.RetriesCount)
	assert.WithinDuration(t, time.Now().Add(2*time.Second), job.RunAt, 100*time.Millisecond)
}

func TestRetryEngine_HandleFailure_ExceedMaxRetries(t *testing.T) {
	log := logger.NewNop()
	backoff := retry.NewExponentialBackoff(1*time.Second, 10*time.Second)
	engine := retry.NewRetryEngine(backoff, log)

	job := &domain.Job{
		ID:           "test-job-dlq",
		Status:       domain.StatusRunning,
		MaxRetries:   2,
		RetriesCount: 2, // Retries count is already at max
	}

	ctx := context.Background()
	errReason := errors.New("fatal database error")

	// Third failure: attempt count becomes 3 > max retries (2) -> should return false (route to DLQ)
	delay, shouldRetry, err := engine.HandleFailure(ctx, job, errReason)
	require.NoError(t, err)
	assert.False(t, shouldRetry)
	assert.Equal(t, time.Duration(0), delay)
	assert.Equal(t, 3, job.RetriesCount)
	assert.Equal(t, "fatal database error", job.ErrorMessage)
}
