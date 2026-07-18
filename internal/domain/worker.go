package domain

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// WorkerStatus defines execution status values for worker nodes.
type WorkerStatus string

const (
	WorkerStatusActive  WorkerStatus = "active"
	WorkerStatusIdle    WorkerStatus = "idle"
	WorkerStatusStopped WorkerStatus = "stopped"
)

// Worker represents a node instance executing tasks in the QueueCTL system.
type Worker struct {
	mu            sync.RWMutex
	ID            string       `json:"id"`
	Hostname      string       `json:"hostname"`
	Queue         string       `json:"queue"`
	Concurrency   int          `json:"concurrency"`
	Status        WorkerStatus `json:"status"`
	StartedAt     time.Time    `json:"started_at"`
	LastHeartbeat time.Time    `json:"last_heartbeat"`
}

// NewWorker instantiates a new Worker domain model.
func NewWorker(id, hostname, queue string, concurrency int) (*Worker, error) {
	if id == "" {
		return nil, errors.New("worker ID cannot be empty")
	}
	if hostname == "" {
		return nil, errors.New("worker hostname cannot be empty")
	}
	if queue == "" {
		queue = "default"
	}
	if concurrency <= 0 {
		return nil, errors.New("worker concurrency limit must be greater than zero")
	}

	now := time.Now().UTC()
	return &Worker{
		ID:            id,
		Hostname:      hostname,
		Queue:         queue,
		Concurrency:   concurrency,
		Status:        WorkerStatusIdle,
		StartedAt:     now,
		LastHeartbeat: now,
	}, nil
}

// Validate checks structural constraints on the Worker properties.
func (w *Worker) Validate() error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.ID == "" {
		return errors.New("invalid worker: missing ID")
	}
	if w.Hostname == "" {
		return errors.New("invalid worker: missing hostname")
	}
	if w.Concurrency <= 0 {
		return errors.New("invalid worker: concurrency limit must be greater than zero")
	}
	switch w.Status {
	case WorkerStatusActive, WorkerStatusIdle, WorkerStatusStopped:
		// Valid status
	default:
		return fmt.Errorf("invalid worker: unsupported status '%s'", w.Status)
	}
	return nil
}

// GetStatus returns the current status thread-safely.
func (w *Worker) GetStatus() WorkerStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Status
}

// GetLastHeartbeat returns the last heartbeat timestamp thread-safely.
func (w *Worker) GetLastHeartbeat() time.Time {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.LastHeartbeat
}

// Heartbeat refreshes the last active timestamp of the worker.
func (w *Worker) Heartbeat() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.LastHeartbeat = time.Now().UTC()
}

// SetStatus modifies the running state of the worker node.
func (w *Worker) SetStatus(status WorkerStatus) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Status = status
	w.LastHeartbeat = time.Now().UTC()
}
