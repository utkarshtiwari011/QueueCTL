# QueueCTL Design Document

This document outlines the detailed system design, architectural patterns, concurrency models, and engineering tradeoffs implemented in **QueueCTL**.

---

## 1. Clean Architecture Layers

QueueCTL strictly separates core business logic from database drivers, frameworks, and CLI layers:

```
  +-------------------------------------------------------------+
  |                   CLI Layer (internal/cli)                  |
  |   - Parse flags & inputs (Cobra)                            |
  |   - Dependency Injection wire-up (root.go)                  |
  +------------------------------+------------------------------+
                                 |
                                 v
  +-------------------------------------------------------------+
  |              Concurrency Layer (internal/worker)            |
  |   - WorkerPool, Semaphores, WaitGroups                      |
  |   - Graceful shutdown traps, panic recovery                 |
  +------------------------------+------------------------------+
                                 |
                                 v
  +-------------------------------------------------------------+
  |                 Service Layer (internal/service)            |
  |   - Use Cases (Enqueue, Execute, Success, Failure)          |
  |   - Coordinator for repository/DLQ/retry integrations       |
  +------------------------------+------------------------------+
                                 |
                                 v
  +-------------------------------------------------------------+
  |             Persistence Layer (internal/repository)         |
  |   - CRUD sql operations, immediate transaction scopes       |
  |   - Optimistic concurrency control (OCC) checks             |
  +------------------------------+------------------------------+
                                 |
                                 v
  +-------------------------------------------------------------+
  |                Domain Layer (internal/domain)               |
  |   - Plain Go structs (Job, Worker, Retry, ExecutionLog)     |
  |   - Sane constructors, validation rules, zero dependencies  |
  +-------------------------------------------------------------+
```

---

## 2. Concurrency & Worker Engine Design

### Throttling & Resource Protection
Worker threads process tasks concurrently using Go channels as semaphores:
*   **Buffered Channel Semaphore**: Created with a capacity equal to `Worker.Concurrency`. Waking threads must write to the channel (`w.sem <- struct{}{}`) before checking or running jobs. This limits active goroutines, preventing CPU/RAM exhaustion under heavy backlog loads.
*   **Active Job Tracking**: Increments a `sync.WaitGroup` upon dispatching a task, decrementing (`wg.Done()`) inside a deferred completion hook.
*   **Panic Recovery**: deferred recovery blocks capture panics during handler execution. Panics are logged, converted into failed execution logs, and the semaphore slot is freed.

---

## 3. Database Design & Locking Strategies

SQLite requires optimized parameters to sustain concurrent read/write locks in a serverless format:

### 1. Write-Ahead Logging (WAL)
Enabled via `PRAGMA journal_mode=WAL;`. WAL allows reader processes to query stats while writer processes commit state transitions, preventing lockups during polling.

### 2. Pessimistic Acquisition Locks
Acquiring a pending job runs inside a `BEGIN IMMEDIATE` transaction block. SQLite blocks other write transactions immediately. This ensures that:
*   Only one thread selects the oldest pending job.
*   The job status is updated to `running` atomically.
This guarantees **exactly-once execution** (no duplicate worker runs).

### 3. Optimistic Concurrency Control (OCC)
To prevent write conflicts (e.g. if two daemons update the same job model):
```sql
UPDATE jobs
SET status = ?, retries_count = ?, error_message = ?, run_at = ?, updated_at = ?
WHERE id = ? AND updated_at = ?
```
If the job's `updated_at` timestamp changed in the database, the query fails with `repository.ErrConcurrencyConflict`, alerting the caller to abort stale modifications.

---

## 4. Event-Driven Scheduler Design

Traditional database queues rely on polling tickers, which introduces latency (e.g. up to a 5-second delay before checking new work). QueueCTL uses a hybrid event-driven scheduler:
*   **Immediate Notifications**: When `Enqueue` completes, it writes a non-blocking wake signal to the scheduler's buffered channel:
    ```go
    select {
    case s.notifyChan <- struct{}{}:
    default:
    }
    ```
*   **Instant Wake**: The worker pool selects on this channel, triggering an immediate database poll.
*   **Backup Ticker**: A periodic ticker (e.g. every 5 seconds) runs in the background to handle delayed jobs and retries.

---

## 5. Engineering Tradeoffs

### 1. SQLite vs. Redis/PostgreSQL
*   **Pros**: SQLite requires zero server administration, runs embedded in-process, and stores data in a single file, making it perfect for lightweight CLI configurations.
*   **Cons**: Lacks network transport support. Multiple workers must mount the same physical file.
*   **Mitigation**: WAL mode and busy timeouts are configured to sustain concurrent locks. The clean repository interface allows shifting to Postgres/Redis easily.

### 2. In-Memory Event Channels vs. Network Message Brokers
*   **Pros**: Go channels require zero dependencies and operate at memory speeds.
*   **Cons**: Notifications are lost if the CLI process crashes.
*   **Mitigation**: The background ticker handles missed notifications, ensuring eventual consistency.
