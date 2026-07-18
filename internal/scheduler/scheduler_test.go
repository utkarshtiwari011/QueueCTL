package scheduler_test

import (
	"context"
	"testing"
	"time"

	"queuectl/internal/logger"
	"queuectl/internal/scheduler"

	"github.com/stretchr/testify/assert"
)

func TestScheduler_Notify(t *testing.T) {
	log := logger.NewNop()
	sched := scheduler.NewScheduler(log)

	// Verify channel returns
	ch := sched.NotifyChan()
	assert.NotNil(t, ch)

	// Call Notify. Since buffer is 1, it should succeed non-blockingly.
	sched.Notify()

	// Verify we can read from the channel
	select {
	case <-ch:
		// Success
	default:
		t.Fatal("expected notification on channel")
	}

	// Double Notify should not block (default case covers buffer saturation)
	sched.Notify()
	sched.Notify()
}

func TestScheduler_Start(t *testing.T) {
	log := logger.NewNop()
	sched := scheduler.NewScheduler(log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start scheduler in background
	go sched.Start(ctx, 10*time.Millisecond)

	// Wait for periodic ticker ticks
	ch := sched.NotifyChan()
	ticks := 0
	for i := 0; i < 3; i++ {
		select {
		case <-ch:
			ticks++
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timed out waiting for tick %d", i)
		}
	}
	assert.Equal(t, 3, ticks)

	// Cancel context to stop scheduler
	cancel()
	time.Sleep(20 * time.Millisecond)

	// Drain any remaining ticks
	select {
	case <-ch:
	default:
	}

	// Verify no more notifications are generated after stop
	select {
	case <-ch:
		t.Fatal("unexpected notification after scheduler was stopped")
	case <-time.After(50 * time.Millisecond):
		// Success
	}
}
