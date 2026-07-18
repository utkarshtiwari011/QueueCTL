package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// JobStatus defines the valid states a Job can traverse.
type JobStatus string

const (
	StatusPending    JobStatus = "pending"
	StatusRunning    JobStatus = "running"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
	StatusDeadLetter JobStatus = "dead_letter"
)

// Job represents a background execution task in QueueCTL.
type Job struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Payload      string    `json:"payload"`
	Queue        string    `json:"queue"`
	Status       JobStatus `json:"status"`
	Priority     int       `json:"priority"` // Higher priority executed first
	MaxRetries   int       `json:"max_retries"`
	RetriesCount int       `json:"retries_count"`
	ErrorMessage string    `json:"error_message,omitempty"`
	RunAt        time.Time `json:"run_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NewJob is a constructor that instantiates a Job with sane defaults and validations.
func NewJob(id, jobType, payload, queue string, priority int, maxRetries int, runAt time.Time) (*Job, error) {
	if id == "" {
		return nil, errors.New("job ID cannot be empty")
	}
	if jobType == "" {
		return nil, errors.New("job type cannot be empty")
	}
	if queue == "" {
		queue = "default"
	}
	if runAt.IsZero() {
		runAt = time.Now()
	}
	runAt = runAt.UTC()

	if maxRetries < 0 {
		return nil, errors.New("max retries cannot be negative")
	}

	now := time.Now().UTC()
	return &Job{
		ID:           id,
		Type:         jobType,
		Payload:      payload,
		Queue:        queue,
		Status:       StatusPending,
		Priority:     priority,
		MaxRetries:   maxRetries,
		RetriesCount: 0,
		ErrorMessage: "",
		RunAt:        runAt,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// Validate checks the structural integrity of the Job.
func (j *Job) Validate() error {
	if j.ID == "" {
		return errors.New("invalid job: missing ID")
	}
	if j.Type == "" {
		return errors.New("invalid job: missing type")
	}
	if j.Queue == "" {
		return errors.New("invalid job: missing queue name")
	}
	switch j.Status {
	case StatusPending, StatusRunning, StatusCompleted, StatusFailed, StatusDeadLetter:
		// Valid status
	default:
		return fmt.Errorf("invalid job: unsupported status '%s'", j.Status)
	}
	if j.RetriesCount < 0 {
		return errors.New("invalid job: retries count cannot be negative")
	}
	if j.MaxRetries < 0 {
		return errors.New("invalid job: max retries cannot be negative")
	}
	return nil
}

// IsRetryable determines if a job can be rescheduled for execution.
func (j *Job) IsRetryable() bool {
	return j.RetriesCount < j.MaxRetries
}

// HasFailed determines if a job execution failed.
func (j *Job) HasFailed() bool {
	return j.Status == StatusFailed || j.Status == StatusDeadLetter
}

// ScheduleNextRun transitions the job to pending and schedules the next execution run timestamp.
func (j *Job) ScheduleNextRun(backoff time.Duration, errMsg string) {
	j.Status = StatusPending
	j.ErrorMessage = errMsg
	j.RunAt = time.Now().Add(backoff).UTC()
}

// JobHandler defines the function signature for executing a specific type of background job.
type JobHandler func(ctx context.Context, payload string) error
