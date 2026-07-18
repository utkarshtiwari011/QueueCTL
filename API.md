# QueueCTL Go API Specifications

This document details the core Go interfaces, entities, and service signatures of QueueCTL.

---

## 📦 Domain Models

Located in [internal/domain](file:///c:/Users/utkar/Downloads/QueueCTL/internal/domain).

### 1. Job Struct
Represents a task scheduled for execution.
```go
type Job struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Payload      string    `json:"payload"`
	Queue        string    `json:"queue"`
	Status       JobStatus `json:"status"`
	Priority     int       `json:"priority"`
	MaxRetries   int       `json:"max_retries"`
	RetriesCount int       `json:"retries_count"`
	ErrorMessage string    `json:"error_message"`
	RunAt        time.Time `json:"run_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
```
*   `Payload`: Must be a valid JSON string (validated upon enqueue).
*   `Status`: One of `pending`, `running`, `completed`, `failed`, `dead_letter`.
*   `Priority`: An integer value. Jobs with higher priorities are claimed first.

### 2. Worker Struct
Represents an active or idle worker pool instance.
```go
type Worker struct {
	ID            string       `json:"id"`
	Hostname      string       `json:"hostname"`
	Queue         string       `json:"queue"`
	Concurrency   int          `json:"concurrency"`
	Status        WorkerStatus `json:"status"`
	StartedAt     time.Time    `json:"started_at"`
	LastHeartbeat time.Time    `json:"last_heartbeat"`
}
```
*   `Status`: One of `idle`, `active`, `stopped`.

### 3. ExecutionLog Struct
Tracks attempts to execute a specific job.
```go
type ExecutionLog struct {
	ID           string          `json:"id"`
	JobID        string          `json:"job_id"`
	WorkerID     string          `json:"worker_id"`
	Attempt      int             `json:"attempt"`
	Status       ExecutionStatus `json:"status"`
	StartedAt    time.Time       `json:"started_at"`
	FinishedAt   time.Time       `json:"finished_at"`
	ErrorMessage string          `json:"error_message"`
}
```
*   `Status`: One of `running`, `success`, `failed`.

---

## 🛠️ Service Interfaces

### 1. JobService
Orchestrates job queues, worker status changes, and execution results. Located in [internal/service/job_service.go](file:///c:/Users/utkar/Downloads/QueueCTL/internal/service/job_service.go).

```go
type JobService interface {
	// Enqueue adds a new background job. Returns the created Job.
	Enqueue(ctx context.Context, jobType string, payload string, queue string, priority int, runAt time.Time, maxRetries int) (*domain.Job, error)

	// GetJob retrieves a job's metadata by its unique UUID.
	GetJob(ctx context.Context, id string) (*domain.Job, error)

	// ListJobs queries jobs matching queue, status, search keyword, and limits.
	ListJobs(ctx context.Context, queue string, status domain.JobStatus, search string, limit int) ([]*domain.Job, error)

	// GetStats aggregates job status counts grouped by queue.
	GetStats(ctx context.Context) (map[string]map[domain.JobStatus]int, error)

	// PurgeCompleted deletes completed jobs older than the specified duration.
	PurgeCompleted(ctx context.Context, olderThan time.Duration) (int64, error)

	// RegisterHandler maps a job type to a processing handler function.
	RegisterHandler(jobType string, handler domain.JobHandler) error

	// ExecuteJob fetches and invokes the handler associated with the job's type.
	ExecuteJob(ctx context.Context, job *domain.Job) error

	// FetchNextJob claims the next pending job inside a write transaction.
	FetchNextJob(ctx context.Context, queue string, workerID string) (*domain.Job, error)

	// RegisterWorker inserts or updates a worker process record in the database.
	RegisterWorker(ctx context.Context, w *domain.Worker) error

	// UnregisterWorker deletes the worker's registry record.
	UnregisterWorker(ctx context.Context, workerID string) error

	// ReclaimOrphanedJobs scans and recovers jobs from crashed worker nodes.
	ReclaimOrphanedJobs(ctx context.Context) error

	// HandleJobSuccess transitions a job to completed and saves execution logs.
	HandleJobSuccess(ctx context.Context, job *domain.Job, workerID string) error

	// HandleJobFailure evaluates retries, reschedules, or routes to DLQ.
	HandleJobFailure(ctx context.Context, job *domain.Job, workerID string, err error) error
}
```

### 2. DLQ Router
Manages dead letter job routing and administration. Located in [internal/dlq/dlq.go](file:///c:/Users/utkar/Downloads/QueueCTL/internal/dlq/dlq.go).

```go
type Router interface {
	// Route moves a failed job into the DLQ by setting status to 'dead_letter'.
	Route(ctx context.Context, job *domain.Job, reason error) error

	// List queries jobs in 'dead_letter' status.
	List(ctx context.Context, queue string, search string, limit int) ([]*domain.Job, error)

	// Retry re-enqueues a DLQ job back to pending.
	Retry(ctx context.Context, jobID string) error

	// Delete permanently deletes a DLQ job from the database.
	Delete(ctx context.Context, jobID string) error

	// Restore re-enqueues a DLQ job and overrides its target queue.
	Restore(ctx context.Context, jobID string, queue string) error

	// GetStats counts dead letter jobs grouped by queue.
	GetStats(ctx context.Context) (map[string]int64, error)
}
```

### 3. Retry Engine
Determines backoff delays on failure. Located in [internal/retry/retry.go](file:///c:/Users/utkar/Downloads/QueueCTL/internal/retry/retry.go).

```go
type Engine interface {
	// HandleFailure evaluates whether a job should be retried.
	// Updates status to pending and sets run_at to backoff time if retrying.
	HandleFailure(ctx context.Context, job *domain.Job, reason error) (nextAttempt int, shouldRetry bool, err error)
}
```

---

## 💡 Code Integration Example

Here is how you can integrate the `JobService` to register custom handlers and process jobs in your Go application:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"queuectl/internal/config"
	"queuectl/internal/database"
	"queuectl/internal/dlq"
	"queuectl/internal/logger"
	"queuectl/internal/repository/sqlite"
	"queuectl/internal/retry"
	"queuectl/internal/scheduler"
	"queuectl/internal/service"
)

func main() {
	ctx := context.Background()
	log := logger.NewNop()

	// 1. Establish SQLite connection
	db, err := database.Connect(":memory:")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	if err := database.RunMigrations(ctx, db); err != nil {
		panic(err)
	}

	// 2. Initialize repositories
	jobRepo := sqlite.NewSQLiteJobRepository(db)
	workerRepo := sqlite.NewSQLiteWorkerRepository(db)
	execLogRepo := sqlite.NewSQLiteExecutionLogRepository(db)

	// 3. Initialize auxiliary engines
	cfg := &config.Config{}
	cfg.Worker.BackoffBaseDelay = 1 * time.Second
	cfg.Worker.BackoffMaxDelay = 10 * time.Second

	backoff := retry.NewExponentialBackoff(cfg.Worker.BackoffBaseDelay, cfg.Worker.BackoffMaxDelay)
	retryEngine := retry.NewRetryEngine(backoff, log)
	dlqRouter := dlq.NewRouter(jobRepo)
	sched := scheduler.NewScheduler(log)

	// 4. Instantiate JobService
	svc := service.NewJobService(
		jobRepo,
		workerRepo,
		execLogRepo,
		cfg,
		log,
		retryEngine,
		dlqRouter,
		sched,
	)

	// 5. Register custom job handlers
	err = svc.RegisterHandler("send_sms", func(ctx context.Context, payload string) error {
		fmt.Printf("Processing SMS task: %s\n", payload)
		return nil
	})
	if err != nil {
		panic(err)
	}

	// 6. Schedule a job
	job, err := svc.Enqueue(ctx, "send_sms", `{"to": "+12345678", "msg": "Hi!"}`, "default", 1, time.Now(), 3)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Scheduled Job: %s\n", job.ID)
}
```
