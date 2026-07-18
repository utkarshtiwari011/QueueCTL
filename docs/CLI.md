# QueueCTL Command Line Interface (CLI) Manual

This document provides complete, exhaustive operational documentation for every CLI command exposed by the QueueCTL binary.

---

## Global Options

The following flags can be prefixed or appended to any QueueCTL command:

| Flag | Shorthand | Type | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `--config` | - | `string` | `""` | Custom configuration file path (looks for `./config.yaml` if omitted). |
| `--db-path` | - | `string` | `""` | Overrides the database file path (e.g. `--db-path /var/lib/queue.db`). |
| `--verbose` | `-v` | `bool` | `false` | Enables real-time DEBUG level logging output. |

---

## 1. `enqueue`

Submits a new background job into a specified queue for asynchronous execution.

### Syntax
```bash
queuectl enqueue [job-type] [payload] [flags]
```

### Parameters
*   `job-type` (Required): The identifier of the task (e.g., `email`, `image_resize`). Must match a registered worker handler.
*   `payload` (Required): A valid JSON string containing the job parameters (e.g. `'{"to": "user@example.com"}'`).

### Flags
| Flag | Shorthand | Type | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `--queue` | `-q` | `string` | `"default"` | Name of the queue where the job will be routed. |
| `--priority` | `-p` | `int` | `0` | Execution priority. Higher priority jobs are fetched first. |
| `--delay` | `-d` | `duration` | `0s` | Delay execution by a specific duration (e.g., `30s`, `15m`, `2h`). |
| `--max-retries` | - | `int` | `0` | Max execution failures before moving to the DLQ. (0 defaults to config). |

### Examples & Expected Outputs
#### immediate enqueue:
```bash
queuectl enqueue email '{"to": "hello@world.com"}' --priority 10
```
*Expected Output:*
```text
Job enqueued successfully:
  ID:           e3a2419a-9e12-4211-9a2c-f604312019a1
  Type:         email
  Queue:        default
  Status:       pending
  Priority:     10
  Max Retries:  3
  Scheduled At: 2026-07-18T05:50:00Z
```

#### Delayed enqueue:
```bash
queuectl enqueue image_resize '{"image_id": "9982"}' --delay 5m
```
*Expected Output:*
```text
Job enqueued successfully:
  ID:           cb320982-f38b-4a3d-bcfd-e6b810985f39
  Type:         image_resize
  Queue:        default
  Status:       pending
  Priority:     0
  Max Retries:  3
  Scheduled At: 2026-07-18T05:55:00Z
```

### Notes
*   If `--delay` is specified, the scheduler updates the job's `run_at` timestamp into the future. Idle workers will ignore the job until that timestamp is reached.

### Common Mistakes
*   **Invalid JSON syntax**: Enqueueing a payload with bad JSON (e.g. `'{"to":}'`) will fail.
*   **Shell quoting conflicts**: On Windows PowerShell, single quotes are processed differently. Use escaped double quotes: `queuectl enqueue email "{\"to\": \"user@example.com\"}"`.

---

## 2. `worker`

Controls the background worker execution daemon.

### Syntax
```bash
queuectl worker [subcommand] [flags]
```

### Subcommands
*   `start`: Launches the polling and execution daemon.
*   `stop`: Signals a graceful termination to the running daemon.

### `worker start` Flags
| Flag | Shorthand | Type | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `--queue` | `-q` | `string` | `"default"` | Target queue queue name to poll. |
| `--concurrency` | `-c` | `int` | `0` | Concurrency limit (overrides config). |
| `--poll-interval` | `-p` | `duration` | `0s` | Database poll frequency (overrides config). |
| `--pid-file` | - | `string` | `"worker.pid"` | Path to write the worker's process ID. |

### Examples & Expected Outputs
#### Start Worker:
```bash
queuectl worker start --queue default --concurrency 5
```
*Expected Output:*
```text
2026-07-18T05:50:00.123Z  INFO  worker process started  {"pid": 12891, "pid_file": "worker.pid"}
2026-07-18T05:50:00.124Z  INFO  starting worker pool  {"worker_id": "5f9b1c", "queue": "default", "concurrency": 5}
```

#### Stop Worker:
```bash
queuectl worker stop --pid-file worker.pid
```
*Expected Output:*
```text
Stopping worker process (PID: 12891)...
Worker process stopped gracefully.
```

### Notes
*   `worker start` writes its OS PID to the designated `--pid-file` which is read by `worker stop` to send `SIGINT` (on supported OS).

### Common Mistakes
*   **Attempting `worker stop` on Windows**: Sending signals using process handles is not natively supported on Windows. Terminate the process using `Ctrl+C` or `taskkill`.

---

## 3. `list`

Queries and lists historical and current jobs stored in the database.

### Syntax
```bash
queuectl list [flags]
```

### Flags
| Flag | Shorthand | Type | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `--queue` | `-q` | `string` | `""` | Filter by queue name. |
| `--status` | `-s` | `string` | `""` | Filter by status (`pending`, `running`, `completed`, `failed`, `dead_letter`). |
| `--search` | - | `string` | `""` | Search substring match in payload, type, or error message. |
| `--limit` | `-l` | `int` | `50` | Maximum number of rows to retrieve. |

### Examples & Expected Outputs
#### List pending jobs in the tasks queue:
```bash
queuectl list --queue tasks --status pending --limit 2
```
*Expected Output:*
```text
JOB ID                                TYPE        QUEUE    STATUS   RETRIES  SCHEDULED RUN        LAST ERROR
c26e1072-2fea-41c3-a636-3d03a2a070b0  email       tasks    pending  0/3      2026-07-18 05:50:00  -
a1683b7c-923a-4096-a18a-29b24d0b7da5  sync_data   tasks    pending  1/3      2026-07-18 05:52:12  network timeout
```

### Common Mistakes
*   **Omitting limits**: Listing millions of completed jobs can exhaust terminal memory buffers. Use `--limit` or query only non-completed states.

---

## 4. `metrics`

Collects, aggregates, and renders real-time telemetry metrics.

### Syntax
```bash
queuectl metrics [flags]
```

### Examples & Expected Outputs
```bash
queuectl metrics
```
*Expected Output:*
```text
============================================================
                     QueueCTL Telemetry Metrics             
============================================================
Job States:
  Pending Jobs:       1
  Running Jobs:       0
  Completed Jobs:     432
  Failed Jobs:        12
  Dead Letter Queue:  2
                      
Execution Performance:
  Total Reschedule Retries:   18
  Average Execution Runtime:  1.45s
  Attempt Success Rate:       96.88%
                              
Worker Telemetry:
  Registered Worker Nodes:  2
  Active Workers:           1
  Worker Node Utilization:  50.00%
============================================================
```

---

## 5. `dlq`

Administrative subcommands to inspect, replay, or purge jobs isolated in the Dead Letter Queue.

### Syntax
```bash
queuectl dlq [subcommand] [flags]
```

### Subcommands
*   `list`: Lists isolated dead letter jobs.
*   `replay`: Re-injects a job back into execution status.
*   `purge`: Permanently deletes a job from the DLQ.

### Flags
| Flag | Subcommand | Type | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `--id` | `replay`, `purge` | `string` | `""` | Target Job ID. |
| `--all` | `replay`, `purge` | `bool` | `false` | Apply the operation to ALL jobs in the DLQ. |
| `--queue` | `replay` | `string` | `""` | Route the job to a different queue (defaults to its original queue). |

### Examples & Expected Outputs
#### List DLQ jobs:
```bash
queuectl dlq list
```
*Expected Output:*
```text
JOB ID                                TYPE        ORIGINAL QUEUE  EXHAUSTED AT         ERROR MESSAGE
e98a10-b32c-4f81-ba23-9082cb1920a2  data_sync   default         2026-07-18 05:12:00  fatal DB lock
```

#### Replay job:
```bash
queuectl dlq replay --id e98a10-b32c-4f81-ba23-9082cb1920a2 --queue processing
```
*Expected Output:*
```text
Successfully replayed job e98a10-b32c-4f81-ba23-9082cb1920a2 back to queue 'processing'.
```

#### Purge all DLQ jobs:
```bash
queuectl dlq purge --all
```
*Expected Output:*
```text
Successfully purged 1 dead letter jobs from the database.
```

---

## 6. `purge`

Cleans up successfully completed job historical entries to prevent database file bloat.

### Syntax
```bash
queuectl purge [flags]
```

### Flags
| Flag | Shorthand | Type | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `--older-than` | - | `duration` | `24h` | Delete completed jobs older than this duration (e.g. `12h`, `7d`). |

### Examples & Expected Outputs
```bash
queuectl purge --older-than 7d
```
*Expected Output:*
```text
Purge operation completed successfully. Deleted 120 completed jobs older than 168h0m0s.
```

---

## 7. `config`

Renders the resolved system configuration values.

### Syntax
```bash
queuectl config [flags]
```

### Flags
| Flag | Shorthand | Type | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `--format` | `-f` | `string` | `"yaml"` | Output layout format (`yaml` or `json`). |

### Examples & Expected Outputs
```bash
queuectl config --format json
```
*Expected Output:*
```json
{
  "database": {
    "path": "./queue.db"
  },
  "worker": {
    "concurrency": 5,
    "poll_interval": 1000000000,
    "max_retries": 3,
    "backoff_base_delay": 1000000000,
    "backoff_max_delay": 30000000000
  },
  "logger": {
    "level": "info",
    "format": "console"
  }
}
```

---

## 8. `status`

Provides a quick summary layout of queue lengths.

### Syntax
```bash
queuectl status
```

### Examples & Expected Outputs
```bash
queuectl status
```
*Expected Output:*
```text
Queue Metrics Summary:
QUEUE NAME    PENDING    RUNNING    COMPLETED    FAILED    DLQ (DEAD LETTER)
default       2          0          12           0         1
processing    0          1          94           1         0
```
