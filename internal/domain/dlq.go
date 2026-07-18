package domain

import (
	"errors"
	"time"
)

// DLQEnvelope represents metadata for a job that has been isolated in the Dead Letter Queue.
type DLQEnvelope struct {
	JobID         string    `json:"job_id"`
	OriginalQueue string    `json:"original_queue"`
	Payload       string    `json:"payload"`
	FailedAt      time.Time `json:"failed_at"`
	ErrorMessage  string    `json:"error_message"`
}

// NewDLQEnvelope wraps a failed job into a DLQEnvelope structure for debugging storage.
func NewDLQEnvelope(job *Job, errMsg string) (*DLQEnvelope, error) {
	if job == nil {
		return nil, errors.New("cannot create DLQ envelope from nil job")
	}

	return &DLQEnvelope{
		JobID:         job.ID,
		OriginalQueue: job.Queue,
		Payload:       job.Payload,
		FailedAt:      time.Now(),
		ErrorMessage:  errMsg,
	}, nil
}

// Validate verifies constraints on DLQ metadata.
func (e *DLQEnvelope) Validate() error {
	if e.JobID == "" {
		return errors.New("invalid DLQ envelope: missing job ID")
	}
	if e.OriginalQueue == "" {
		return errors.New("invalid DLQ envelope: missing original queue name")
	}
	if e.FailedAt.IsZero() {
		return errors.New("invalid DLQ envelope: missing failed timestamp")
	}
	return nil
}
