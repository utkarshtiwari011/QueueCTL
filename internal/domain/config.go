package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// SystemConfig represents the system configuration parameters as a domain model.
type SystemConfig struct {
	DatabasePath     string        `json:"database_path"`
	LogLevel         string        `json:"log_level"`
	LogFormat        string        `json:"log_format"`
	Concurrency      int           `json:"concurrency"`
	PollInterval     time.Duration `json:"poll_interval"`
	MaxRetries       int           `json:"max_retries"`
	BackoffBaseDelay time.Duration `json:"backoff_base_delay"`
	BackoffMaxDelay  time.Duration `json:"backoff_max_delay"`
}

// NewSystemConfig instantiates a validated SystemConfig domain model.
func NewSystemConfig(
	dbPath, logLevel, logFormat string,
	concurrency int,
	pollInterval time.Duration,
	maxRetries int,
	baseDelay, maxDelay time.Duration,
) (*SystemConfig, error) {
	cfg := &SystemConfig{
		DatabasePath:     dbPath,
		LogLevel:         logLevel,
		LogFormat:        logFormat,
		Concurrency:      concurrency,
		PollInterval:     pollInterval,
		MaxRetries:       maxRetries,
		BackoffBaseDelay: baseDelay,
		BackoffMaxDelay:  maxDelay,
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks constraints on configuration fields.
func (c *SystemConfig) Validate() error {
	if c.DatabasePath == "" {
		return errors.New("database path cannot be empty")
	}

	lvl := strings.ToLower(c.LogLevel)
	if lvl != "debug" && lvl != "info" && lvl != "warn" && lvl != "error" {
		return fmt.Errorf("invalid log level '%s': must be debug, info, warn, or error", c.LogLevel)
	}

	fmtStr := strings.ToLower(c.LogFormat)
	if fmtStr != "console" && fmtStr != "json" {
		return fmt.Errorf("invalid log format '%s': must be console or json", c.LogFormat)
	}

	if c.Concurrency <= 0 {
		return fmt.Errorf("concurrency limit must be greater than zero, got %d", c.Concurrency)
	}

	if c.PollInterval <= 0 {
		return fmt.Errorf("poll interval must be a positive duration, got %s", c.PollInterval)
	}

	if c.MaxRetries < 0 {
		return fmt.Errorf("max retries cannot be negative, got %d", c.MaxRetries)
	}

	if c.BackoffBaseDelay <= 0 {
		return fmt.Errorf("backoff base delay must be positive, got %s", c.BackoffBaseDelay)
	}

	if c.BackoffMaxDelay < c.BackoffBaseDelay {
		return fmt.Errorf("backoff max delay (%s) cannot be smaller than base delay (%s)", c.BackoffMaxDelay, c.BackoffBaseDelay)
	}

	return nil
}
