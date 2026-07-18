# QueueCTL Test Plan

This document outlines the testing strategy, test suites, execution commands, expected outputs, and pass criteria for the QueueCTL validation suite.

---

## 1. Test Suites Structure

The QueueCTL validation suite is partitioned into:

*   **Unit Tests**: Validate individual package components (DLQ, SQLite repository, retry calculation, worker loops, job services).
*   **Integration Tests**: Validate end-to-end job enqueuing, worker execution, state transitions, and context propagation.
*   **Concurrency Tests**: Verify execution safety across multiple worker nodes to prevent duplicate runs under high concurrency stress.
*   **Stress Tests**: Measure throughput, latency, memory, and goroutine performance metrics under high job volumes (100, 1000, and 5000 jobs).
*   **Failure Tests**: Verify system stability and recovery during panic events, context timeouts, and stale worker crashes.
*   **Restart Tests**: Verify that running jobs orphaned by terminated workers are automatically recovered and rescheduled upon worker restart.
*   **Benchmarks**: Measure micro-level latency metrics for critical scheduling, database, and retry code blocks.

---

## 2. Test Execution & Pass Criteria

### Standard Unit & Integration Suite
Executes all unit, integration, failure, and restart tests under the project.
*   **Command**:
    ```bash
    go test -v ./...
    ```
*   **Expected Output**:
    All packages report `ok` with details on individual test pass status.
*   **Pass Criteria**:
    100% of the tests must pass successfully with exit code `0`.

---

### Non-Cached Verification Run
Forces Go to ignore the cache and run all tests afresh.
*   **Command**:
    ```bash
    go test -count=1 ./...
    ```
*   **Expected Output**:
    All tests execute sequentially, showing fresh runtime logs.
*   **Pass Criteria**:
    All tests pass without timeouts or transaction deadlocks.

---

### High-Volume Stress Tests
Executes the high-volume stress suite for 100, 1000, and 5000 jobs, outputting metrics.
*   **Command**:
    ```bash
    go test -v -run=TestStress_HighVolume ./tests
    ```
*   **Expected Output**:
    ```text
    === Metrics for Volume 100 ===
    Throughput:     150.00 jobs/sec
    Avg Latency:    6.67 ms/job
    Memory Alloc:   1.20 MB
    Goroutine Delta: 5 -> 5
    ```
*   **Pass Criteria**:
    All volumes complete without timing out, achieving perfect job completion.

---

### Micro-Benchmarks
Measures execution speed, memory allocations, and CPU throughput.
*   **Command**:
    ```bash
    go test -bench=. -benchmem ./tests
    ```
*   **Expected Output**:
    Lists operations per second (`op/s`), nanoseconds per operation (`ns/op`), and memory bytes allocated.
*   **Pass Criteria**:
    Zero panics or compiler failures during execution.

---

## 3. Data Integrity & Invariants Checked
*   **Zero Orphaned Jobs**: Any running job whose worker dies must be marked as pending or sent to the DLQ.
*   **No Duplicate Runs**: A single job must never be picked up by multiple workers concurrently.
*   **State Machine Validity**: Only jobs in `StatusRunning` can transition to completion or fail. Invalid transition attempts return explicit errors.
