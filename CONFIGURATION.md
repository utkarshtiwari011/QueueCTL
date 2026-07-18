# QueueCTL Configuration Guide

QueueCTL resolves configuration settings from three sources, ordered by increasing precedence:
1.  **Default values** (embedded in code)
2.  **Configuration file** (`config.yaml` or `config.json`)
3.  **Environment variables** (prefixed with `QUEUECTL_`)

---

## ⚙️ Configuration Schema & Parameters

Below is a complete description of all available configuration parameters, their data types, defaults, and descriptions.

### 1. Database Configuration (`database`)

Parameters to manage the SQLite connection.

| Parameter | Type | Default | Environment Override | Description |
| :--- | :--- | :--- | :--- | :--- |
| `database.path` | `string` | `"queue.db"` | `QUEUECTL_DATABASE_PATH` | Path to the SQLite database file. Supports `:memory:` or `file::memory:` for transient setups. |

### 2. Logger Configuration (`logger`)

Settings for telemetry output stream logs.

| Parameter | Type | Default | Environment Override | Description |
| :--- | :--- | :--- | :--- | :--- |
| `logger.level` | `string` | `"info"` | `QUEUECTL_LOGGER_LEVEL` | Minimum log level. Must be one of: `debug`, `info`, `warn`, `error`. |
| `logger.format` | `string` | `"console"` | `QUEUECTL_LOGGER_FORMAT` | Rendering format. Must be one of: `console` (human-readable) or `json` (for log shippers). |

### 3. Worker Configuration (`worker`)

Controls concurrency thresholds, databases polling frequency, and job retry intervals.

| Parameter | Type | Default | Environment Override | Description |
| :--- | :--- | :--- | :--- | :--- |
| `worker.concurrency` | `integer` | `5` | `QUEUECTL_WORKER_CONCURRENCY` | Maximum number of concurrent worker goroutines allowed to process jobs. Must be `> 0`. |
| `worker.poll_interval` | `duration` | `"1s"` | `QUEUECTL_WORKER_POLL_INTERVAL` | The fallback polling interval when the queue database is empty. (e.g. `200ms`, `1s`, `5s`). |
| `worker.max_retries` | `integer` | `3` | `QUEUECTL_WORKER_MAX_RETRIES` | Max retries allowed for a failing job before routing it to the Dead Letter Queue. |
| `worker.backoff_base_delay`| `duration` | `"2s"` | `QUEUECTL_WORKER_BACKOFF_BASE_DELAY`| Base delay duration used for exponential backoff retry calculations. |
| `worker.backoff_max_delay` | `duration` | `"30s"` | `QUEUECTL_WORKER_BACKOFF_MAX_DELAY` | Max retry backoff delay duration cap. |

---

## 📄 Configuration File Examples

### YAML Format (`config.yaml`)
```yaml
database:
  path: "./data/queue.db"

logger:
  level: "debug"
  format: "console"

worker:
  concurrency: 10
  poll_interval: "250ms"
  max_retries: 5
  backoff_base_delay: "1s"
  backoff_max_delay: "15s"
```

### JSON Format (`config.json`)
```json
{
  "database": {
    "path": "./data/queue.db"
  },
  "logger": {
    "level": "info",
    "format": "json"
  },
  "worker": {
    "concurrency": 8,
    "poll_interval": "500ms",
    "max_retries": 3,
    "backoff_base_delay": "2s",
    "backoff_max_delay": "30s"
  }
}
```

---

## 🔄 Dynamic Hot-Reloading

QueueCTL supports **zero-downtime config hot-reloading**:
*   Viper monitors the config file (`config.yaml` or `config.json`) for changes using filesystem events (`fsnotify`).
*   When changes are detected, the configuration is parsed and validated in the background.
*   If validation succeeds, the new settings are swapped atomically in memory without interrupting active job executions.
*   If validation fails (e.g. invalid logger level or negative concurrency limits), the change is **rejected**, a warning is printed to stdout, and the daemon continues running on the last valid configuration.

---

## 🌍 Environment Variable Overrides

Any configuration parameter can be overridden at runtime using environment variables. The mapping rules are:
1.  Variable names must be prefixed with `QUEUECTL_`.
2.  Nested parameters use underscores (`_`) instead of dots (`.`).
3.  All letters must be uppercase.

### Examples:
```bash
# Override SQLite database path
export QUEUECTL_DATABASE_PATH="/var/run/queuectl/queue.db"

# Override worker concurrency
export QUEUECTL_WORKER_CONCURRENCY=20

# Run queuectl daemon with overrides
./queuectl worker start
```
