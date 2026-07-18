# QueueCTL Production Folder Structure Explanation

This document explains the organization of QueueCTL's codebase. It describes why each folder exists and how it contributes to maintaining Clean Architecture and SOLID principles.

---

## Complete Folder Tree

```
QueueCTL/
├── .github/
│   └── workflows/
│       └── ci.yml             # GitHub Actions Workflows
├── cmd/
│   └── queuectl/
│       └── main.go            # Entrypoints
├── docs/
│   ├── commands.md            # CLI manuals
│   └── folder_explanation.md  # Folder structure reference
├── internal/                  # Private application code
│   ├── cli/                   # User interface (Cobra command controllers)
│   ├── config/                # Environment config binding (Viper)
│   ├── database/              # DB connection and migrations (SQLite pragmas)
│   ├── dlq/                   # Dead letter queue logic
│   ├── domain/                # Enterprise business models (pure Go)
│   ├── logger/                # Structured logging configurations (Zap)
│   ├── repository/            # Storage abstractions & SQLite implementation
│   ├── retry/                 # Exponential backoff calculations
│   ├── scheduler/             # Scheduled offset execution
│   ├── service/               # App business rules (handler registry, state machine)
│   ├── utils/                 # General helpers (JSON validators)
│   └── worker/                # Background concurrency engines
├── scripts/
│   └── seed_jobs.sh           # Test database seeds
├── tests/
│   └── integration_test.go    # E2E integration test suite
├── Dockerfile                 # Multi-stage production container build
├── docker-compose.yml         # Compose local run config
├── config.yaml                # Default YAML parameters
└── go.mod                     # Dependency definitions
```

---

## Detailed Directory Explanations

### 1. `cmd`
*   **Why it exists**: Idiomatic Go project layout convention. It separates execution entry points from the rest of the application. The `cmd/queuectl/main.go` file contains only a minimal `main` function that executes the CLI bootstrap layer. It prevents business logic or database setups from polluting the initialization path.

### 2. `internal`
*   **Why it exists**: Enforces Go's internal package visibility rules. Code inside `internal/` cannot be imported by external packages. This ensures that QueueCTL's implementation details remain encapsulated, preventing external systems from developing tight coupling with internal database repositories or worker pool structures.

### 3. `internal/repository`
*   **Why it exists**: Implements the **Repository Pattern**. It decouples the data persistence layer from the business logic layer. The repository interface defines CRUD operations, atomic locks, statistics, and purging. The concrete implementation (`internal/repository/sqlite`) isolates SQLite-specific raw query statements and transaction logic.

### 4. `internal/service`
*   **Why it exists**: Implements the **Service Layer** (Use Cases). It acts as the coordinator of business rules. It contains the logic for transitioning job states, registering task handler callbacks, executing jobs within timeouts, and triggering error recovery or completion hooks. It depends only on domain entities and repository interfaces, ensuring high testability.

### 5. `internal/worker`
*   **Why it exists**: Handles background task processing concurrency. It controls how goroutines poll the database, fetch pending jobs, and invoke service executors. Isolating this to its own folder prevents concurrency orchestration mechanics (semaphores, waitgroups, channel selects, panic recoveries) from cluttering the business layer.

### 6. `internal/scheduler`
*   **Why it exists**: Isolates scheduling parameter calculations. It calculates future execution timestamps (`run_at`) based on delay parameters (e.g. `10s`, `5m`) before committing jobs to the repository. This encapsulates timing and scheduling from the core service layer.

### 7. `internal/retry`
*   **Why it exists**: Manages exponential backoff rules. It isolates the mathematical formula used to compute retry delays:
    $$\text{delay} = \min(\text{BaseDelay} \times 2^{\text{RetriesCount}-1}, \text{MaxDelay})$$
    Separating this logic into its own package makes it easier to modify, swap, or test backoff policies independently of job state updates.

### 8. `internal/dlq`
*   **Why it exists**: Manages **Dead Letter Queue** routing. When a job exceeds its maximum retry threshold, this package handles updating its state to `dead_letter` and attaching the final stack error trace. Isolating this logic provides a clear boundary for monitoring failed tasks.

### 9. `internal/config`
*   **Why it exists**: Manages configuration parsing and injection. It uses Viper to read YAML settings and bind environment variables dynamically. This isolates setup configuration logic, keeping other layers stateless.

### 10. `internal/database`
*   **Why it exists**: Manages SQLite connections and migrations. It configures optimal performance pragmas, such as Write-Ahead Logging (WAL) and busy timeout limits. Isolating this logic prevents the repository layer from having to manage raw database connection pools.

### 11. `internal/logger`
*   **Why it exists**: Wraps and configures structured logging. It initializes Uber Zap with custom formatting configurations, outputting colored logs in development and structured JSON in production.

### 12. `internal/utils`
*   **Why it exists**: Contains general helper utilities, such as a JSON payload validator. Keeping these helpers separate prevents other core packages from accumulating utility function bloat.

### 13. `tests`
*   **Why it exists**: Holds end-to-end integration tests. Placing these tests outside of `internal/` allows them to compile and execute against the public interface of the application, simulating how a developer or script would interact with the CLI and database components.

### 14. `docs`
*   **Why it exists**: Houses markdown manuals, command line reference manuals, and architecture guides, keeping documentation close to the codebase.

### 15. `scripts`
*   **Why it exists**: Contains developer helper scripts, such as database seed scripts. This keeps script utilities organized and separate from application logic.

### 16. `Docker` & `GitHub Actions`
*   **Why they exist**:
    *   `Dockerfile` & `docker-compose.yml` (Docker support): Enables containerization, allowing developers to run background workers with persistent data volumes on any system.
    *   `.github/workflows/ci.yml` (GitHub Actions): Automates formatting checks, static analysis (`go vet`), race-detector tests, and Docker compilation to ensure pull requests meet quality standards.
