package domain

import (
	"errors"
	"fmt"
	"time"
)

// ExecutionStatus defines state outcomes for an individual job execution run.
type ExecutionStatus string

const (
	ExecutionSuccess ExecutionStatus = "success"
	ExecutionFailed  ExecutionStatus = "failed"
)

// ExecutionLog records execution metrics and history for debugging runs.
type ExecutionLog struct {
	ID           string          `json:"id"`
	JobID        string          `json:"job_id"`
	WorkerID     string          `json:"worker_id"`
	Attempt      int             `json:"attempt"`
	Status       ExecutionStatus `json:"status"`
	StartedAt    time.Time       `json:"started_at"`
	FinishedAt   time.Time       `json:"finished_at"`
	ErrorMessage string          `json:"error_message,omitempty"`
}

// NewExecutionLog instantiates a new ExecutionLog instance.
func NewExecutionLog(id, jobID, workerID string, attempt int, startedAt time.Time) (*ExecutionLog, error) {
	if id == "" {
		return nil, errors.New("execution log ID cannot be empty")
	}
	if jobID == "" {
		return nil, errors.New("associated job ID cannot be empty")
	}
	if workerID == "" {
		return nil, errors.New("associated worker ID cannot be empty")
	}
	if attempt <= 0 {
		return nil, errors.New("attempt count must be greater than zero")
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	startedAt = startedAt.UTC()

	return &ExecutionLog{
		ID:           id,
		JobID:        jobID,
		WorkerID:     workerID,
		Attempt:      attempt,
		Status:       ExecutionFailed, // Default to failed until marked complete
		StartedAt:    startedAt,
		FinishedAt:   time.Time{},
		ErrorMessage: "",
	}, nil
}

// Validate checks internal consistency of the execution log attributes.
func (l *ExecutionLog) Validate() error {
	if l.ID == "" {
		return errors.New("invalid execution log: missing ID")
	}
	if l.JobID == "" {
		return errors.New("invalid execution log: missing job ID")
	}
	if l.WorkerID == "" {
		return errors.New("invalid execution log: missing worker ID")
	}
	if l.Attempt <= 0 {
		return errors.New("invalid execution log: attempt count must be greater than zero")
	}
	if l.StartedAt.IsZero() {
		return errors.New("invalid execution log: missing started timestamp")
	}
	return nil
}

// Complete updates execution status, completion timestamp, and error reasons.
func (l *ExecutionLog) Complete(status ExecutionStatus, finishedAt time.Time, errMsg string) error {
	switch status {
	case ExecutionSuccess, ExecutionFailed:
		l.Status = status
	default:
		return fmt.Errorf("invalid execution status '%s'", status)
	}

	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	finishedAt = finishedAt.UTC()
	startedAtUTC := l.StartedAt.UTC()

	if finishedAt.Before(startedAtUTC) {
		return errors.New("finished timestamp cannot be before started timestamp")
	}

	l.FinishedAt = finishedAt
	l.StartedAt = startedAtUTC
	l.ErrorMessage = errMsg
	return nil
}

// Duration returns the total running duration of this execution attempt.
func (l *ExecutionLog) Duration() time.Duration {
	if l.FinishedAt.IsZero() {
		return time.Since(l.StartedAt)
	}
	return l.FinishedAt.Sub(l.StartedAt)
}
