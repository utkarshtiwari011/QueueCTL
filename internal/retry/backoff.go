package retry

import (
	"math"
	"time"
)

// Backoff defines the contract for calculating retry delays.
type Backoff interface {
	// Calculate returns the duration to wait before attempting the next retry.
	Calculate(retriesCount int) time.Duration
}

type exponentialBackoff struct {
	baseDelay time.Duration
	maxDelay  time.Duration
}

// NewExponentialBackoff instantiates an exponential backoff calculator.
func NewExponentialBackoff(baseDelay, maxDelay time.Duration) Backoff {
	if baseDelay <= 0 {
		baseDelay = 2 * time.Second
	}
	if maxDelay <= 0 {
		maxDelay = 30 * time.Second
	}
	return &exponentialBackoff{
		baseDelay: baseDelay,
		maxDelay:  maxDelay,
	}
}

// Calculate computes: baseDelay * 2^(retriesCount - 1), capped at maxDelay.
func (b *exponentialBackoff) Calculate(retriesCount int) time.Duration {
	if retriesCount <= 0 {
		return 0
	}

	multiplier := math.Pow(2, float64(retriesCount-1))
	delay := time.Duration(float64(b.baseDelay) * multiplier)

	if delay > b.maxDelay {
		return b.maxDelay
	}
	return delay
}
