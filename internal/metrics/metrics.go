package metrics

import (
	"context"
	"fmt"
	"time"

	"queuectl/internal/domain"
	"queuectl/internal/repository"
)

// QueueMetrics encapsulates aggregated telemetry and execution metrics for QueueCTL.
type QueueMetrics struct {
	PendingCount       int           `json:"pending_count"`
	RunningCount       int           `json:"running_count"`
	CompletedCount     int           `json:"completed_count"`
	FailedCount        int           `json:"failed_count"`
	DLQCount           int           `json:"dlq_count"`
	TotalRetries       int           `json:"total_retries"`
	AverageRuntime     time.Duration `json:"average_runtime"`
	SuccessRate        float64       `json:"success_rate_percentage"`
	ActiveWorkersCount int           `json:"active_workers_count"`
	TotalWorkersCount  int           `json:"total_workers_count"`
	WorkerUtilization  float64       `json:"worker_utilization_percentage"`
}

// Service aggregates statistics across jobs, workers, and execution logs.
type Service interface {
	// GetMetrics gathers system statistics and telemetry values.
	GetMetrics(ctx context.Context) (*QueueMetrics, error)
}

type metricsService struct {
	jobRepo     repository.JobRepository
	workerRepo  repository.WorkerRepository
	execLogRepo repository.ExecutionLogRepository
}

// NewMetricsService instantiates a concrete metrics aggregation Service.
func NewMetricsService(
	jobRepo repository.JobRepository,
	workerRepo repository.WorkerRepository,
	execLogRepo repository.ExecutionLogRepository,
) Service {
	return &metricsService{
		jobRepo:     jobRepo,
		workerRepo:  workerRepo,
		execLogRepo: execLogRepo,
	}
}

// GetMetrics returns aggregated queue statistics.
func (s *metricsService) GetMetrics(ctx context.Context) (*QueueMetrics, error) {
	// Prune stale workers first (inactivity threshold of 30 seconds)
	_, _ = s.workerRepo.PruneStale(ctx, 30*time.Second)

	// 1. Fetch Job Queue counts
	jobStats, err := s.jobRepo.GetQueueStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve job stats: %w", err)
	}

	var pending, running, completed, failed, dlq int
	for _, statusCounts := range jobStats {
		pending += statusCounts[domain.StatusPending]
		running += statusCounts[domain.StatusRunning]
		completed += statusCounts[domain.StatusCompleted]
		failed += statusCounts[domain.StatusFailed]
		dlq += statusCounts[domain.StatusDeadLetter]
	}

	// 2. Fetch Average Runtime
	avgRuntime, err := s.execLogRepo.GetAverageRuntime(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve average execution duration: %w", err)
	}

	// 3. Fetch Execution Success Rate from Execution Logs
	successLogs, failedLogs, err := s.execLogRepo.GetStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve execution stats: %w", err)
	}

	var successRate float64
	totalExecutions := successLogs + failedLogs
	if totalExecutions > 0 {
		successRate = (float64(successLogs) / float64(totalExecutions)) * 100
	} else {
		successRate = 100.0 // Default to 100% success if no executions occurred
	}

	// 4. Fetch Worker Utilization
	workers, err := s.workerRepo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve active workers list: %w", err)
	}

	totalWorkers := len(workers)
	activeWorkerIDs := make(map[string]bool)
	runningJobs, err := s.jobRepo.ListJobs(ctx, "", domain.StatusRunning, "", 1000)
	if err == nil {
		for _, job := range runningJobs {
			logs, err := s.execLogRepo.GetByJobID(ctx, job.ID)
			if err == nil && len(logs) > 0 {
				latestLog := logs[len(logs)-1]
				if latestLog.FinishedAt.IsZero() {
					activeWorkerIDs[latestLog.WorkerID] = true
				}
			}
		}
	}

	activeWorkers := 0
	for _, w := range workers {
		if w.Status == domain.WorkerStatusActive || activeWorkerIDs[w.ID] {
			activeWorkers++
		}
	}

	var workerUtil float64
	if totalWorkers > 0 {
		workerUtil = (float64(activeWorkers) / float64(totalWorkers)) * 100
	}

	// 5. Aggregate total retries count by scanning the jobs database (limit 1000)
	allJobs, err := s.jobRepo.ListJobs(ctx, "", "", "", 1000)
	totalRetries := 0
	if err == nil {
		for _, j := range allJobs {
			totalRetries += j.RetriesCount
		}
	}

	return &QueueMetrics{
		PendingCount:       pending,
		RunningCount:       running,
		CompletedCount:     completed,
		FailedCount:        failed,
		DLQCount:           dlq,
		TotalRetries:       totalRetries,
		AverageRuntime:     avgRuntime,
		SuccessRate:        successRate,
		ActiveWorkersCount: activeWorkers,
		TotalWorkersCount:  totalWorkers,
		WorkerUtilization:  workerUtil,
	}, nil
}
