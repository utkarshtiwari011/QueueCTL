# Contributing to QueueCTL

Thank you for your interest in contributing to QueueCTL! We welcome community contributions to help improve this background job queue engine.

---

## 1. Code of Conduct

All contributors must adhere to our **[Code of Conduct](./CODE_OF_CONDUCT.md)** to ensure a welcoming and inclusive environment for everyone.

---

## 2. Developer Workspace Setup

### Prerequisites
*   **Go 1.20** or newer installed.
*   **golangci-lint** static analysis tool installed locally.
*   A C compiler (optional; only needed if you configure CGO SQLite, though QueueCTL defaults to the pure-Go modernc.org/sqlite).

### Local Setup
1.  Fork the repository on GitHub and clone your fork locally:
    ```bash
    git clone https://github.com/<your-username>/QueueCTL.git
    cd QueueCTL
    ```
2.  Install dependencies:
    ```bash
    go mod download
    ```
3.  Verify the environment by compiling and running tests:
    ```bash
    go test -v ./...
    ```

---

## 3. Coding Guidelines & Standards

To maintain a high level of code quality, please adhere to these guidelines:

### Formatting & Style
*   Run `gofmt` to format all Go source code files:
    ```bash
    gofmt -w .
    ```
*   Keep functions focused, small, and follow **Clean Architecture** patterns.
*   Preserve all existing documentation, docstrings, and comments unless they are directly related to your changes.

### Static Analysis
Before opening a Pull Request, run the Go vet tool and local linters:
```bash
# Run vet analysis
go vet ./...

# Run golangci-lint
golangci-lint run
```

### Testing Standards
All new features or bug fixes must include corresponding tests:
*   **Unit Tests**: Isolated using mock repositories. Mock objects are available in the test files (e.g. `mockJobRepository`, `mockWorkerRepository`).
*   **Integration Tests**: Run against an isolated SQLite database file or in-memory storage (`:memory:`). Ensure all resources are closed and cleaned up using `t.Cleanup` or defer statements.
*   **Race Conditions**: Always run the test suite with the race detector enabled to verify thread-safety:
    ```bash
    go test -race -v ./...
    ```

---

## 4. Commit Message Guidelines

We follow the standard **Conventional Commits** specification. Commit messages should have a structured prefix:

*   `feat: ...` for a new feature (e.g. `feat: add native cron scheduling`)
*   `fix: ...` for a bug fix (e.g. `fix: resolve worker heartbeat race condition`)
*   `docs: ...` for documentation updates
*   `test: ...` for adding or improving test coverage
*   `refactor: ...` for code changes that neither fix a bug nor add a feature
*   `chore: ...` for updates to build scripts, CI pipelines, or package dependencies

---

## 5. Pull Request Process

1.  Create a descriptive branch name from `main`:
    ```bash
    git checkout -b feature/your-feature-name
    # OR
    git checkout -b fix/bug-description
    ```
2.  Implement your changes, adding comments to non-obvious algorithms and documenting new fields.
3.  Write tests that demonstrate the bug fix or validate the new feature under concurrency loads.
4.  Ensure that the entire test suite passes successfully.
5.  Commit your changes following the Commit Message Guidelines.
6.  Push your branch to your GitHub fork and open a Pull Request against the upstream repository.
7.  Provide a clear description in the PR containing:
    *   The goal/problem being addressed.
    *   The technical approach taken.
    *   The specific tests run to validate the changes.
