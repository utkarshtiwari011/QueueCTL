# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [1.0.0] - 2026-07-18

### Added
*   **Structured Retry Engine**: Exponential backoff rescheduling with random jitter.
*   **Dead Letter Queue (DLQ)**: Admin routing, retry, delete, restore, and queue-wide metrics.
*   **Orphaned Job Recovery**: Background daemon thread that automatically reclaims active tasks from unresponsive workers (>30s silence).
*   **Viper Configuration Manager**: Supports YAML/JSON config files, env overrides, and live hot-reloading.
*   **Worker Graceful Shutdown**: Intercepts `SIGINT` / `SIGTERM` signals to drain running tasks safely before shutting down.
*   **Low-Overhead Event Scheduler**: Uses non-blocking wake channels to alert workers instantly upon enqueuing, avoiding database polling when idle.
*   **Telemetry Telemetry**: Tracks active worker nodes, utilization percentage, average runtimes, and success rates.
*   **Docker Support**: Multi-stage `Dockerfile`, along with development and production `docker-compose` setups.
*   **Automated E2E Test Suite**: Concurrency stress tests, reboot resilience tests, and benchmarks.

### Fixed
*   **SQLite julianday Timezone Defect**: Wrapped datetime fields in `substr(..., 1, 19)` to strip timezone suffixes (`+0000 UTC` from Go's sql driver) that caused SQLite's `julianday` function to return `NULL`.
*   **Worker Heartbeat Re-registration Race**: Added waitgroups inside the unregistration block to ensure the heartbeat loop terminates before unregistering the worker.
*   **Timezone Query Range Collisions**: Standardized all database time values to UTC format strings.
*   **Windows Test Clock Precision**: Standardized timestamp truncation to millisecond/second boundaries to prevent test assertions from failing due to low clock precision on Windows.
