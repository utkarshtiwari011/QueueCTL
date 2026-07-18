# QueueCTL Project Roadmap

This document outlines the planned developmental milestones and future direction of the QueueCTL background job engine.

---

## 🏁 Milestone 1: Core Engine & Resilience (Completed)
Focuses on establishing a production-grade, single-node transactional job queue.
*   [x] **SQLite Transactional Storage**: Clean database schema with indices for high concurrency.
*   [x] **WAL & Lock Upgrades**: Write-Ahead Logging (WAL) and lock-upgrading SQL queries to eliminate `database is locked` deadlocks.
*   [x] **Semaphore Throttling**: Channel-based concurrency controls inside worker pools.
*   [x] **Orphaned Job Recovery**: Automatic reclamation of stuck tasks from crashed workers.
*   [x] **Structured Retry & DLQ**: Exponential backoffs and administrator isolation tools (DLQ).
*   [x] **Structured Logging & Telemetry**: Zap-based logs and telemetry metrics.
*   [x] **Docker Integration**: Production-ready Docker and Compose scripts.
*   [x] **Comprehensive Test Suite**: Concurrency stress, worker crashes, and database rollback integration tests.

---

## 📈 Milestone 2: Management & Observability (Planned - Q3 2026)
Focuses on making QueueCTL easier to monitor and administer.
*   [ ] **Single-Page Web Console**:
    *   A web-based dashboard displaying active workers, pending/failed queues, and system utilization charts.
    *   Real-time telemetry updates using WebSockets.
    *   Actions to retry, pause, or purge jobs from the DLQ directly through the web UI.
*   [ ] **Native Cron Support**:
    *   Ability to schedule recurring background jobs using standard cron expressions (e.g. `0 0 * * *` for daily backups).
*   [ ] **Shell Autocompletion**:
    *   Shell completion scripts for Bash, Zsh, and PowerShell to make CLI command execution easier.

---

## 🚀 Milestone 3: Scale & Distribution (Future - Q1 2027)
Focuses on scaling QueueCTL horizontally to support higher workloads.
*   [ ] **SQLite Database Sharding**:
    *   Distribute jobs across separate SQLite database files (shards) based on queue names or hash keys to bypass single-writer limits.
*   [ ] **HTTP/gRPC Gateway**:
    *   Expose a lightweight API gateway allowing client applications written in other languages (Python, Node.js, Rust) to enqueue jobs and fetch metrics.
*   [ ] **Prometheus Integration**:
    *   Expose a `/metrics` HTTP endpoint for Prometheus scraping.
