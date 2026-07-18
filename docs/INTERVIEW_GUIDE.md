# QueueCTL Technical Interview Study Guide

This document prepares you for technical interviews by walking through system design considerations, engineering challenges, concurrency models, and common interview questions with answers based on the QueueCTL project.

---

## 🏗️ System Design: The SQLite Queue Broker

### Q1: Why would you choose SQLite for a queue broker instead of Redis or RabbitMQ?
**Answer**: 
Choosing SQLite is about **minimizing operational overhead** and **avoiding network boundaries** for specific deployment topologies (such as edge computing, local microservices, or embedded applications). 
*   Redis and RabbitMQ require standalone servers, network configurations, credential management, security patches, and backup schedules.
*   SQLite runs **in-process**, requiring zero external dependencies or ports. Data is stored in a single local file, making it exceptionally fast for single-host jobs, lightweight to deploy, and transactional out-of-the-box.
*   However, we trade off horizontal scalability across multiple hosts.

### Q2: SQLite is a single-writer database. How does QueueCTL handle high concurrent write contention without failing?
**Answer**:
We solve SQLite write contention using a three-tier architecture:
1.  **Connection Level**: We limit database writers to exactly one connection (`MaxOpenConns(1)`). This queues concurrent write operations inside Go's in-memory connection pool, preventing database-level locking conflicts.
2.  **Storage Level**: We enable **Write-Ahead Logging (WAL) Mode** (`PRAGMA journal_mode=WAL;`). This allows concurrent readers to query statistics and fetch jobs without blocking the single active write connection.
3.  **Transaction Level**: We execute an empty write query (`UPDATE jobs SET id = id WHERE 1=0`) at the very start of our transactions. This upgrades the database lock to `IMMEDIATE` status instantly, preventing concurrent transactions from obtaining read locks and subsequently deadlocking during lock upgrades.

---

## ⚡ Concurrency & Resiliency Design Patterns

### Q3: Explain Optimistic Concurrency Control (OCC) and how you applied it in QueueCTL.
**Answer**:
Optimistic Concurrency Control assumes that conflicts are rare, allowing multiple transactions to read and edit data without acquiring heavy lock structures. When saving, we verify that the record hasn't changed since we read it.
In QueueCTL, we apply OCC to **Worker Heartbeats**:
*   When a worker node updates its heartbeat, we issue a query:
    ```sql
    UPDATE workers SET last_heartbeat = ? WHERE id = ? AND last_heartbeat = ?
    ```
*   If another thread (or the reclaimer) has already updated or stopped the worker, the `last_heartbeat` check fails, the query affects `0` rows, and we gracefully abort or handle the conflict rather than writing stale states.

### Q4: How does QueueCTL handle graceful shutdowns and prevent job loss when a worker receives a termination signal?
**Answer**:
1.  We intercept `SIGINT`/`SIGTERM` signals using a dedicated signal channel.
2.  The worker pool instantly disables the database polling loop, stopping new jobs from being claimed.
3.  We use a `sync.WaitGroup` to wait for all currently executing job goroutines to finish.
4.  Once the active jobs finish and commit their outcomes (success or retry), the worker daemon stops the heartbeat loop, unregisters itself from the database, and exits cleanly.
5.  If a job hangs, we enforce a 30-second context timeout before triggering a forced exit.

### Q5: Describe the background crash recovery flow when a worker node crashes abruptly.
**Answer**:
If a worker process crashes (e.g., `kill -9` or hardware failure), it cannot run its graceful shutdown unregistration. The jobs it claimed remain stuck in the `running` state.
*   **Reclaimer Engine**: Spawns every 30 seconds to query the `workers` table.
*   **Stale Detection**: Identifies any worker whose `last_heartbeat` timestamp is older than 30 seconds.
*   **Orphan Clean-up**: Marks the worker as `stopped`, queries all associated `running` jobs, increments their retry counts, and moves them back to `pending` status (or `dead_letter` if they have exceeded `max_retries`).
*   This recovery flow is idempotent and serialized using write transaction locks.
