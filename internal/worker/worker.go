package worker

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"queuectl/internal/config"
	"queuectl/internal/domain"
	"queuectl/internal/logger"
	"queuectl/internal/repository"
	"queuectl/internal/scheduler"
	"queuectl/internal/service"

	"github.com/google/uuid"
)

// WorkerPool orchestrates a set of concurrent worker goroutines that poll and process jobs.
type WorkerPool interface {
	// Start begins polling the database and running jobs.
	Start(ctx context.Context) error

	// Stop triggers a graceful shutdown of the worker pool.
	Stop()
}

type workerPool struct {
	id       string
	repo     repository.JobRepository
	svc      service.JobService
	sched    scheduler.Scheduler
	cfg      *config.Config
	logger   logger.Logger
	queue    string
	stopChan chan struct{}
	wg       sync.WaitGroup
	sem      chan struct{} // semaphore to limit concurrency
	once     sync.Once
}

// NewWorkerPool instantiates a concurrency-safe WorkerPool.
func NewWorkerPool(repo repository.JobRepository, svc service.JobService, sched scheduler.Scheduler, cfg *config.Config, logger logger.Logger, queue string) WorkerPool {
	if queue == "" {
		queue = "default"
	}
	return &workerPool{
		id:       uuid.New().String(),
		repo:     repo,
		svc:      svc,
		sched:    sched,
		cfg:      cfg,
		logger:   logger,
		queue:    queue,
		stopChan: make(chan struct{}),
		sem:      make(chan struct{}, cfg.Worker.Concurrency),
	}
}

// Start begins polling and execution loop.
func (w *workerPool) Start(ctx context.Context) error {
	w.logger.Info("starting worker pool",
		logger.String("worker_id", w.id),
		logger.String("queue", w.queue),
		logger.Int("concurrency", w.cfg.Worker.Concurrency),
		logger.Duration("poll_interval", w.cfg.Worker.PollInterval),
	)

	// 1. Register worker node
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}

	workerNode, err := domain.NewWorker(w.id, hostname, w.queue, w.cfg.Worker.Concurrency)
	if err != nil {
		return fmt.Errorf("failed to initialize worker registration info: %w", err)
	}

	workerNode.SetStatus(domain.WorkerStatusIdle)
	if err := w.svc.RegisterWorker(ctx, workerNode); err != nil {
		return fmt.Errorf("failed to register worker node at startup: %w", err)
	}

	// Immediate cleanup of orphaned jobs on startup
	if err := w.svc.ReclaimOrphanedJobs(ctx); err != nil {
		w.logger.Error("failed to reclaim orphaned jobs at startup", logger.Error(err))
	}

	// 2. Start heartbeat goroutine
	heartbeatTicker := time.NewTicker(5 * time.Second)
	defer heartbeatTicker.Stop()

	var heartbeatWg sync.WaitGroup
	heartbeatWg.Add(1)
	go func() {
		defer heartbeatWg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.stopChan:
				return
			case <-heartbeatTicker.C:
				workerNode.Heartbeat()
				hbCtx, hbCancel := context.WithTimeout(context.Background(), 3*time.Second)
				_ = w.svc.RegisterWorker(hbCtx, workerNode)
				hbCancel()
			}
		}
	}()

	// 3. Start database polling loop
	ticker := time.NewTicker(w.cfg.Worker.PollInterval)
	defer ticker.Stop()

	// 4. Start periodic cleanup of orphaned jobs (every 30 seconds)
	reclaimTicker := time.NewTicker(30 * time.Second)
	defer reclaimTicker.Stop()

	defer func() {
		// Wait for heartbeat goroutine to stop first before unregistering
		heartbeatWg.Wait()

		// Clean up worker registration on shutdown
		w.logger.Info("unregistering worker node", logger.String("worker_id", w.id))
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		_ = w.svc.UnregisterWorker(cleanupCtx, w.id)
	}()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("worker pool received cancellation signal, shutting down...")
			w.Stop()
			return ctx.Err()

		case <-w.stopChan:
			w.logger.Info("worker pool received stop signal, shutting down...")
			return nil

		case <-ticker.C:
			// Poll for work if there are available execution slots
			w.pollAndDispatch(ctx, workerNode)

		case <-reclaimTicker.C:
			// Reclaim jobs left in running state by crashed/dead workers
			_ = w.svc.ReclaimOrphanedJobs(ctx)

		case <-w.sched.NotifyChan():
			// Wake up instantly!
			w.logger.Debug("worker pool woke up by scheduler notification event")
			w.pollAndDispatch(ctx, workerNode)
		}
	}
}

// pollAndDispatch attempts to acquire an execution slot, polls for a job, and executes it.
func (w *workerPool) pollAndDispatch(ctx context.Context, node *domain.Worker) {
	// Check if we can acquire a concurrency slot without blocking
	select {
	case w.sem <- struct{}{}:
		// Slot acquired, now poll for a job (calling Svc instead of Repo directly)
		job, err := w.svc.FetchNextJob(ctx, w.queue, w.id)
		if err != nil {
			// Release slot
			<-w.sem
			w.logger.Error("failed to acquire next pending job", logger.Error(err))
			return
		}

		if job == nil {
			// No job found, release slot immediately
			<-w.sem
			return
		}

		// Update worker status to Active
		if node.GetStatus() != domain.WorkerStatusActive {
			node.SetStatus(domain.WorkerStatusActive)
			_ = w.svc.RegisterWorker(ctx, node)
		}

		// Job acquired! Dispatch to a worker goroutine
		w.wg.Add(1)
		go w.executeWorker(ctx, job, node)

	default:
		// All worker slots are busy
		w.logger.Debug("all worker slots are busy, throttling poll")
	}
}

// executeWorker processes the job and handles outcomes.
func (w *workerPool) executeWorker(ctx context.Context, job *domain.Job, node *domain.Worker) {
	defer func() {
		// Recover from any panics inside job handlers to prevent crashing the worker pool
		if r := recover(); r != nil {
			w.logger.Error("panic recovered during job execution",
				logger.String("job_id", job.ID),
				logger.Any("panic", r),
			)
			// Handle the panic as an execution failure
			panicErr := fmt.Errorf("handler panic: %v", r)
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := w.svc.HandleJobFailure(cleanupCtx, job, w.id, panicErr); err != nil {
				w.logger.Error("failed to record job failure after panic recovery", logger.Error(err))
			}
			cleanupCancel()
		}

		// Release the concurrency slot and waitgroup count
		<-w.sem
		w.wg.Done()

		// If no more jobs in queue and no slots occupied, update status to Idle
		if len(w.sem) == 0 && node.GetStatus() != domain.WorkerStatusIdle {
			node.SetStatus(domain.WorkerStatusIdle)
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = w.svc.RegisterWorker(cleanupCtx, node)
			cleanupCancel()
		}
	}()

	w.logger.Info("processing job",
		logger.String("job_id", job.ID),
		logger.String("type", job.Type),
	)

	// Execute job handler
	err := w.svc.ExecuteJob(ctx, job)
	if err != nil {
		w.logger.Error("job execution failed",
			logger.String("job_id", job.ID),
			logger.Error(err),
		)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if updateErr := w.svc.HandleJobFailure(cleanupCtx, job, w.id, err); updateErr != nil {
			w.logger.Error("failed to record job execution failure", logger.Error(updateErr))
		}
		cleanupCancel()
		return
	}

	// Success
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if updateErr := w.svc.HandleJobSuccess(cleanupCtx, job, w.id); updateErr != nil {
		w.logger.Error("failed to record job execution success", logger.Error(updateErr))
	}
	cleanupCancel()
}

// Stop gracefully shuts down the worker pool.
func (w *workerPool) Stop() {
	w.once.Do(func() {
		close(w.stopChan)
		w.logger.Info("graceful shutdown initiated, waiting for active jobs to complete...")

		// Wait for all active worker goroutines to finish
		w.wg.Wait()
		w.logger.Info("all active jobs completed, worker pool stopped successfully")
	})
}
