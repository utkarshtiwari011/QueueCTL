package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// Config represents the master configuration struct for QueueCTL.
type Config struct {
	Database DatabaseConfig `mapstructure:"database" json:"database" yaml:"database"`
	Logger   LoggerConfig   `mapstructure:"logger" json:"logger" yaml:"logger"`
	Worker   WorkerConfig   `mapstructure:"worker" json:"worker" yaml:"worker"`
}

// DatabaseConfig holds connection settings for SQLite.
type DatabaseConfig struct {
	Path string `mapstructure:"path" json:"path" yaml:"path"`
}

// LoggerConfig holds configurations for structured telemetry.
type LoggerConfig struct {
	Level  string `mapstructure:"level" json:"level" yaml:"level"`
	Format string `mapstructure:"format" json:"format" yaml:"format"`
}

// WorkerConfig holds execution limits for background processing.
type WorkerConfig struct {
	Concurrency      int           `mapstructure:"concurrency" json:"concurrency" yaml:"concurrency"`
	PollInterval     time.Duration `mapstructure:"poll_interval" json:"poll_interval" yaml:"poll_interval"`
	MaxRetries       int           `mapstructure:"max_retries" json:"max_retries" yaml:"max_retries"`
	BackoffBaseDelay time.Duration `mapstructure:"backoff_base_delay" json:"backoff_base_delay" yaml:"backoff_base_delay"`
	BackoffMaxDelay  time.Duration `mapstructure:"backoff_max_delay" json:"backoff_max_delay" yaml:"backoff_max_delay"`
}

// Validate checks configuration rules, returning error on breach.
func (c *Config) Validate() error {
	if c.Database.Path == "" {
		return errors.New("database.path cannot be empty")
	}

	lvl := strings.ToLower(c.Logger.Level)
	if lvl != "debug" && lvl != "info" && lvl != "warn" && lvl != "error" {
		return fmt.Errorf("invalid logger.level '%s': must be debug, info, warn, or error", c.Logger.Level)
	}

	loggerFormat := strings.ToLower(c.Logger.Format)
	if loggerFormat != "console" && loggerFormat != "json" {
		return fmt.Errorf("invalid logger.format '%s': must be console or json", c.Logger.Format)
	}

	if c.Worker.Concurrency <= 0 {
		return fmt.Errorf("worker.concurrency must be greater than 0, got %d", c.Worker.Concurrency)
	}

	if c.Worker.PollInterval <= 0 {
		return fmt.Errorf("worker.poll_interval must be positive duration, got %s", c.Worker.PollInterval)
	}

	if c.Worker.MaxRetries < 0 {
		return fmt.Errorf("worker.max_retries cannot be negative, got %d", c.Worker.MaxRetries)
	}

	if c.Worker.BackoffBaseDelay <= 0 {
		return fmt.Errorf("worker.backoff_base_delay must be positive duration, got %s", c.Worker.BackoffBaseDelay)
	}

	if c.Worker.BackoffMaxDelay < c.Worker.BackoffBaseDelay {
		return fmt.Errorf("worker.backoff_max_delay (%s) cannot be smaller than base delay (%s)",
			c.Worker.BackoffMaxDelay, c.Worker.BackoffBaseDelay)
	}

	return nil
}

// ConfigProvider provides thread-safe access to dynamic, hot-reloading configurations.
type ConfigProvider interface {
	// Get returns the currently active configuration.
	Get() *Config

	// OnChange registers a callback to trigger when configuration reloads.
	OnChange(callback func(*Config))
}

type configProvider struct {
	mu        sync.RWMutex
	cfg       *Config
	v         *viper.Viper
	callbacks []func(*Config)
}

func (p *configProvider) Get() *Config {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cfg
}

func (p *configProvider) OnChange(callback func(*Config)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callbacks = append(p.callbacks, callback)
}

func (p *configProvider) triggerCallbacks(cfg *Config) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, cb := range p.callbacks {
		go cb(cfg) // Execute asynchronously to prevent blocking the reload event
	}
}

// LoadConfig reads the configuration file or defaults, sets env bindings, validates parameters,
// and configures hot reloading if a configuration file is used.
func LoadConfig(configPath string) (ConfigProvider, error) {
	v := viper.New()

	// 1. Establish Default Sane Values
	v.SetDefault("database.path", "queue.db")
	v.SetDefault("logger.level", "info")
	v.SetDefault("logger.format", "console")
	v.SetDefault("worker.concurrency", 5)
	v.SetDefault("worker.poll_interval", 1*time.Second)
	v.SetDefault("worker.max_retries", 3)
	v.SetDefault("worker.backoff_base_delay", 2*time.Second)
	v.SetDefault("worker.backoff_max_delay", 30*time.Second)

	// 2. Configure Environment Variable Mapping
	v.SetEnvPrefix("QUEUECTL")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 3. Read configuration file (YAML or JSON)
	var usingFile bool
	if configPath != "" {
		ext := strings.ToLower(filepath.Ext(configPath))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil, fmt.Errorf("unsupported config file extension '%s' (only yaml/json supported)", ext)
		}
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		usingFile = true
	} else {
		// Attempt to load standard "config.yaml" or "config.json" from local directory
		v.AddConfigPath(".")
		v.SetConfigName("config")
		v.SetConfigType("yaml") // Will try yaml suffix first
		if err := v.ReadInConfig(); err == nil {
			usingFile = true
		} else {
			// Try fallback to json config file format
			v.SetConfigType("json")
			if err := v.ReadInConfig(); err == nil {
				usingFile = true
			}
		}
	}

	// 4. Initial Unmarshalling and Validation
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse configuration structure: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	provider := &configProvider{
		cfg: &cfg,
		v:   v,
	}

	// 5. Setup Hot-Reload Dynamic Watcher
	if usingFile {
		v.WatchConfig()
		v.OnConfigChange(func(e fsnotify.Event) {
			var newCfg Config
			if err := v.Unmarshal(&newCfg); err != nil {
				// Log to stdout since logger isn't dynamically connected inside config package
				fmt.Printf("[Config Warning] dynamic reload failed (parse error): %v\n", err)
				return
			}

			// Validate reloaded config before activating
			if err := newCfg.Validate(); err != nil {
				fmt.Printf("[Config Warning] dynamic reload rejected (validation error): %v\n", err)
				return
			}

			provider.mu.Lock()
			provider.cfg = &newCfg
			provider.mu.Unlock()

			provider.triggerCallbacks(&newCfg)
		})
	}

	return provider, nil
}
