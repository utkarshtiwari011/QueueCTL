# Release Notes v1.0.0 - QueueCTL Official Release

We are proud to announce the **QueueCTL v1.0.0** release. This release brings production-grade stability, concurrency controls, stale crash recovery, and rich telemetry to our standalone SQLite-based Go background job queue.

---

## Key Highlights

### 1. SQLite Storage Layer with WAL and OCC
*   **Write-Ahead Logging**: Configures SQLite database pools to support parallel reads during write locks.
*   **Optimistic Concurrency Control (OCC)**: Leverages atomic version matching (`updated_at` checks) to reject double updates.

### 2. Concurrency, Heartbeats, & Crash Recovery
*   **Semaphore-Throttled Workers**: Gracefully limits the capacity of concurrent worker nodes.
*   **Heartbeat Nodes Registry**: Workers send active heartbeats to the database every 5s.
*   **Auto-Reclaimer**: Automatically detects dead/stale workers (>30s silence) and reschedules or routes their running jobs to the DLQ.

### 3. Retry Engine & Dead Letter Queue (DLQ)
*   **Exponential Backoffs**: Automatically reschedules failed tasks with customizable multiplier delays.
*   **DLQ Support**: Safely isolates persistently failing jobs, with command support to list, delete, or retry them.

### 4. Dynamic Configurations & Telemetry Metrics
*   **YAML Settings**: Powered by Viper with dynamic live-reloading.
*   **Aggregated Telemetry**: Queries queue capacity, worker counts, and node utilization stats.

---

## Downloads

Official cross-compiled binaries are available for:
*   `queuectl-linux-amd64`
*   `queuectl-darwin-amd64`
*   `queuectl-windows-amd64.exe`
