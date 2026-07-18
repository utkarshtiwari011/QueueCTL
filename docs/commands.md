# QueueCTL Command Documentation

Below is a reference guide for all CLI commands exposed by the `queuectl` tool.

---

## Global Flags

These flags can be appended to any command:

*   `--config <path>`: Specifies a custom YAML configuration file (defaults to looking for `./config.yaml`).
*   `--db-path <path>`: Overrides the SQLite database file path.
*   `-v`, `--verbose`: Toggles debug-level logging.

---

## Commands

### 1. `enqueue`
Queues a task in SQLite.

**Syntax:**
```bash
queuectl enqueue <job-type> <payload> [flags]
```

**Arguments:**
*   `job-type`: String name identifier representing task handler (e.g., `email`, `image_resize`).
*   `payload`: Data parameters string (recommended: JSON format).

**Flags:**
*   `-q`, `--queue`: Target queue name (default: `default`).
*   `-d`, `--delay`: Execution offset duration (e.g., `10s`, `15m`, `2h`).
*   `--max-retries`: Custom limit before job is sent to DLQ (defaults to config limit).

---

### 2. `worker`
Starts the execution listener loop.

**Syntax:**
```bash
queuectl worker [flags]
```

**Flags:**
*   `-q`, `--queue`: Poll targeted queue (default: `default`).
*   `-c`, `--concurrency`: Max parallel task slots (overrides config).
*   `-p`, `--poll-interval`: Database poll rate (overrides config).

---

### 3. `status`
Renders stats overview tables or prints raw job values.

**Syntax:**
```bash
queuectl status [flags]
```

**Flags:**
*   `--id`: Retrieve specific job detail attributes by UUID.
*   `-q`, `--queue`: Filter job listings by queue name.
*   `-s`, `--status`: Filter job listings by status (`pending`, `running`, `completed`, `failed`, `dead_letter`).
*   `-l`, `--limit`: Limit returned rows (default: `20`).

---

### 4. `purge`
Deletes successfully completed jobs.

**Syntax:**
```bash
queuectl purge [flags]
```

**Flags:**
*   `--older-than`: Purges completed records older than this duration (default: `24h`).
