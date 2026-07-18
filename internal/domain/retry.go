package domain

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// RetryPolicy defines rules governing failed job rescheduling.
type RetryPolicy struct {
	MaxRetries       int           `json:"max_retries"`
	BackoffBaseDelay time.Duration `json:"backoff_base_delay"`
	BackoffMaxDelay  time.Duration `json:"backoff_max_delay"`
}

// NewRetryPolicy instantiates a RetryPolicy with validations.
func NewRetryPolicy(maxRetries int, baseDelay, maxDelay time.Duration) (*RetryPolicy, error) {
	if maxRetries < 0 {
		return nil, errors.New("max retries count cannot be negative")
	}
	if baseDelay <= 0 {
		return nil, errors.New("backoff base delay must be a positive duration")
	}
	if maxDelay < baseDelay {
		return nil, fmt.Errorf("backoff max delay (%s) cannot be smaller than base delay (%s)", maxDelay, baseDelay)
	}

	return &RetryPolicy{
		MaxRetries:       maxRetries,
		BackoffBaseDelay: baseDelay,
		BackoffMaxDelay:  maxDelay,
	}, nil
}

// Validate checks internal consistency of the policy parameters.
func (p *RetryPolicy) Validate() error {
	if p.MaxRetries < 0 {
		return errors.New("invalid policy: negative max retries limit")
	}
	if p.BackoffBaseDelay <= 0 {
		return errors.New("invalid policy: base delay must be positive")
	}
	if p.BackoffMaxDelay < p.BackoffBaseDelay {
		return fmt.Errorf("invalid policy: max delay (%s) is smaller than base delay (%s)", p.BackoffMaxDelay, p.BackoffBaseDelay)
	}
	return nil
}

// CalculateBackoff computes the delay duration: baseDelay * 2^(attempt - 1), capped at maxDelay.
func (p *RetryPolicy) CalculateBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	multiplier := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(p.BackoffBaseDelay) * multiplier)

	if delay > p.BackoffMaxDelay {
		return p.BackoffMaxDelay
	}
	return delay
}
