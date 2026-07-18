# QueueCTL Architectural Design Decisions & Trade-Offs

This document captures the core design decisions, trade-offs, challenges, and lessons learned during the development of QueueCTL.

---

## 🏛️ 1. SQLite as a Queue Broker (The Trade-Off)

### Decision
Utilize SQLite as the single storage engine and broker, rather than relying on standard external network dependencies (such as Redis, RabbitMQ, or PostgreSQL).

### Rationale
*   **Operational Simplicity**: Zero-dependency deployments. Running a queue engine on a single SQLite file eliminates configuration overhead, credentials management, network latency, and backup orchestration complexity for small-to-medium deployments.
*   **Embedded Performance**: For microservices, edge deployments, and single-host applications, SQLite reads/writes are performed in-process via file descriptors, matching or exceeding typical networked Redis round-trip latency.
*   **ACID Compliance**: SQLite supports full ACID transactions, ensuring that job state transitions (e.g. pending $\to$ running) and execution logging are atomic.

### Trade-Offs
*   **Horizontal Scaling Limits**: SQLite is a single-host, single-writer database. It cannot scale write throughput horizontally across multiple instances without adding distributed layer coordination (like dqlite or rqlite).
*   **Write Contention**: High concurrent write throughput (exceeding 1,500-2,000 writes/second) will saturate SQLite's single-writer thread pool, queuing client requests in-memory.

---

## 🔄 2. Event-Driven Wakeups vs. Polling

### Decision
Implement an in-memory event-driven wakeup channel (`NotifyChan`) combined with a fallback database polling ticker.

### Rationale
*   **Efficiency**: If a queue is idle, constant database querying ("polling") wastes CPU cycles, increases disk IOPS, and blocks connection locks. By broadcasting job-enqueue events on a Go channel, idle workers wake up instantly with sub-millisecond response times.
*   **Low CPU Consumption**: When the queue is empty, the daemon CPU usage drops to near 0%.

### Trade-Offs
*   **In-Memory Scope**: wake-up channels only notify workers running in the *same* process. For multi-process/multi-host CLI installations, workers fall back to the periodic database polling ticker (`poll_interval`). This is a necessary concession to maintain simple local SQLite setups.

---

## 🔒 3. Pure Go SQLite Driver vs. CGO-based `go-sqlite3`

### Decision
Use `modernc.org/sqlite` (a pure Go port of SQLite) instead of `github.com/mattn/go-sqlite3` (which requires CGO and GCC compilation).

### Rationale
*   **Cross-Compilation**: Go's greatest strength is compiling static binaries for other platforms (e.g., cross-compiling for Linux on macOS). Using CGO breaks standard `go build` cross-compilation, requiring complex toolchains (e.g. `xgo` or Docker cross-compilers).
*   **Minimal Image Footprints**: A static pure-Go binary can be copied directly into a minimal scratch/Alpine Docker image without requiring shared C libraries or compiler headers.

### Trade-Offs
*   **CPU Performance**: A pure-Go port of C-code generated via transpilers is roughly 1.5x-2x slower for pure raw computational queries compared to native C-compiled binary execution. However, database performance is bound by disk I/O, which mitigates this CPU penalty.

---

## 🛡️ 4. Active Heartbeats & Crash Recovery

### Decision
Implement active heartbeats on worker nodes in the database, with a decoupled reclaimer engine executing every 30 seconds.

### Rationale
*   **Resiliency**: If a worker process is force-killed or crashes due to hardware failure, its claimed jobs would remain stuck in `running` status permanently. Heartbeats allow the system to detect dead nodes and automatically reschedule stuck jobs without human intervention.

### Trade-Offs
*   **Database Writes**: Heartbeats require worker nodes to write to the database every 5 seconds, increasing baseline write overhead. This is mitigated by WAL mode separating read and write structures.
