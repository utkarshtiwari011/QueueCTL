# QueueCTL Command Line Interface (CLI) Usage Guide

QueueCTL provides a powerful command-line interface based on Cobra to manage queues, start worker daemons, and administer the Dead Letter Queue.

---

## 🌐 Global Flags

| Flag | Description | Default |
| :--- | :--- | :--- |
| `--config` | Path to the configuration file (YAML/JSON) | Searches for `config.yaml` or `config.json` in the current directory. |

---

## 🛠️ Commands Reference

### 1. `worker start`
Starts the worker daemon process to claim and process jobs from a target queue.
```bash
queuectl worker start [flags]
```
#### Flags:
*   `-q, --queue string`: Target queue to poll for jobs. (Default: `"default"`)
*   `--pid-file string`: Path to write the worker PID file. (Default: `"worker.pid"`)
*   `-c, --concurrency int`: Maximum concurrent jobs to process. (Overrides configuration)
*   `-p, --poll-interval duration`: Database polling interval when empty. (Overrides configuration)

#### Example:
```bash
queuectl worker start --queue high_priority --concurrency 10 --poll-interval 500ms
```

---

### 2. `worker stop`
Gracefully stops a running worker pool process by reading its PID from the pid-file.
```bash
queuectl worker stop [flags]
```
#### Flags:
*   `--pid-file string`: Path to read the worker PID file. (Default: `"worker.pid"`)

#### Example:
```bash
queuectl worker stop
```

---

### 3. `enqueue`
Schedules a new background job. The payload argument must be a valid JSON string.
```bash
queuectl enqueue [job-type] [payload] [flags]
```
#### Flags:
*   `-q, --queue string`: Target queue name. (Default: `"default"`)
*   `-p, --priority int`: Job execution priority (higher values run first). (Default: `0`)
*   `-d, --delay duration`: Postpones execution by a duration (e.g. `10s`, `5m`, `1h`).
*   `--max-retries int`: Max retries before routing to DLQ (0 defaults to configuration limit).

#### Example:
```bash
queuectl enqueue resize_image '{"id": 48102, "width": 800}' --queue images --priority 10 --delay 30s --max-retries 5
```

---

### 4. `list`
Queries and lists jobs from the database matching the filters.
```bash
queuectl list [flags]
```
#### Flags:
*   `-q, --queue string`: Filter jobs by queue name.
*   `-s, --status string`: Filter jobs by status (`pending`, `running`, `completed`, `failed`, `dead_letter`).
*   `-l, --limit int`: Maximum number of jobs to return (Max: `500`). (Default: `20`)
*   `--search string`: Search payloads, job types, or error strings.

#### Example:
```bash
queuectl list --queue main --status failed --limit 50 --search "timeout"
```

---

### 5. `status`
Checks the overall health of job queues or retrieves details of a specific job by UUID.
```bash
queuectl status [flags]
```
#### Flags:
*   `--id string`: Find and print full attributes of a specific job by ID.

#### Example (Aggregated Queue Stats):
```bash
queuectl status
```
*Output:*
```text
QUEUE NAME      PENDING     RUNNING     COMPLETED     FAILED     DLQ (DEAD LETTER)
default         12          3           145           4          0
images          2           0           89            1          1
```

#### Example (Job Details Lookup):
```bash
queuectl status --id a65d4c9f-3d84-4fe1-ba91-ef17d4a22c54
```
*Output:*
```text
Job Details:
  ID:            a65d4c9f-3d84-4fe1-ba91-ef17d4a22c54
  Type:          send_email
  Payload:       {"to": "user@example.com"}
  Queue:         default
  Status:        failed
  Max Retries:   3
  Retries Run:   1
  Scheduled Run: 2026-07-18T00:55:00Z
  Created At:    2026-07-18T00:54:00Z
  Updated At:    2026-07-18T00:55:00Z
  Last Error:    dial tcp: lookup smtp.gmail.com: no such host
```

---

### 6. `purge`
Permanently deletes completed jobs from the database that are older than the specified duration.
```bash
queuectl purge [flags]
```
#### Flags:
*   `--older-than duration`: Age threshold duration. (Default: `24h`)

#### Example:
```bash
queuectl purge --older-than 7d
```

---

### 7. `metrics`
Displays QueueCTL system telemetry and metrics.
```bash
queuectl metrics
```
*Output:*
```text
============================================================
                     QueueCTL Telemetry Metrics             
============================================================
Job States:
  Pending Jobs:         14
  Running Jobs:         3
  Completed Jobs:       234
  Failed Jobs:          5
  Dead Letter Queue:    1
	
Execution Performance:
  Total Reschedule Retries:     12
  Average Execution Runtime:    1.42s
  Attempt Success Rate:         95.12%
	
Worker Telemetry:
  Registered Worker Nodes:      2
  Active Workers:               2
  Worker Node Utilization:      100.00%
============================================================
```

---

### 8. `config`
Prints the current active configurations resolved from all inputs.
```bash
queuectl config [flags]
```
#### Flags:
*   `-f, --format string`: Configuration output format (`yaml` or `json`). (Default: `"yaml"`)

#### Example:
```bash
queuectl config --format json
```

---

### 9. `dlq` Command Group
Manage and recover failed jobs stored in the Dead Letter Queue.

*   **List DLQ Jobs**:
    ```bash
    queuectl dlq list --queue default --search timeout
    ```
*   **Retry DLQ Job (Re-queue to pending)**:
    ```bash
    # Retry specific job
    queuectl dlq retry --id <job-id>

    # Retry all DLQ jobs
    queuectl dlq retry --all
    ```
*   **Delete DLQ Job**:
    ```bash
    # Delete specific job
    queuectl dlq delete --id <job-id>

    # Delete all DLQ jobs
    queuectl dlq delete --all
    ```
*   **Restore DLQ Job to a Different Queue**:
    ```bash
    queuectl dlq restore --id <job-id> --queue fallback_processing
    ```
*   **View DLQ Stats**:
    ```bash
    queuectl dlq stats
    ```
