package scheduler

import (
	"context"
	"time"

	"queuectl/internal/logger"
)

// Scheduler coordinates polling schedules and instant worker execution triggers.
type Scheduler interface {
	// Notify triggers an immediate worker polling event.
	Notify()

	// NotifyChan returns the receive-only channel carrying wake-up events.
	NotifyChan() <-chan struct{}

	// Start begins a background ticker that regularly dispatches polling triggers.
	Start(ctx context.Context, pollInterval time.Duration)
}

type eventScheduler struct {
	notifyChan chan struct{}
	logger     logger.Logger
}

// NewScheduler instantiates a concurrency-safe event-driven Scheduler.
func NewScheduler(log logger.Logger) Scheduler {
	return &eventScheduler{
		notifyChan: make(chan struct{}, 1),
		logger:     log,
	}
}

// Notify writes a non-blocking wake event to notify workers.
func (s *eventScheduler) Notify() {
	select {
	case s.notifyChan <- struct{}{}:
		s.logger.Debug("scheduler notification dispatched to worker pool")
	default:
		// Notification channel buffer is full, worker is already waking or processing.
	}
}

// NotifyChan returns the communication channel.
func (s *eventScheduler) NotifyChan() <-chan struct{} {
	return s.notifyChan
}

// Start runs the periodic ticker loop.
func (s *eventScheduler) Start(ctx context.Context, pollInterval time.Duration) {
	s.logger.Info("starting periodic scheduler ticker loop", logger.Duration("poll_interval", pollInterval))
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("scheduler ticker loop stopped")
			return
		case <-ticker.C:
			s.Notify()
		}
	}
}
