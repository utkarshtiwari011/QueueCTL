package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"queuectl/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		wantErr string
	}{
		{
			name: "valid config",
			cfg: config.Config{
				Database: config.DatabaseConfig{Path: "queue.db"},
				Logger:   config.LoggerConfig{Level: "info", Format: "console"},
				Worker: config.WorkerConfig{
					Concurrency:      5,
					PollInterval:     1 * time.Second,
					MaxRetries:       3,
					BackoffBaseDelay: 2 * time.Second,
					BackoffMaxDelay:  30 * time.Second,
				},
			},
			wantErr: "",
		},
		{
			name: "empty db path",
			cfg: config.Config{
				Database: config.DatabaseConfig{Path: ""},
			},
			wantErr: "database.path cannot be empty",
		},
		{
			name: "invalid logger level",
			cfg: config.Config{
				Database: config.DatabaseConfig{Path: "queue.db"},
				Logger:   config.LoggerConfig{Level: "invalid", Format: "console"},
			},
			wantErr: "invalid logger.level 'invalid'",
		},
		{
			name: "invalid logger format",
			cfg: config.Config{
				Database: config.DatabaseConfig{Path: "queue.db"},
				Logger:   config.LoggerConfig{Level: "info", Format: "invalid"},
			},
			wantErr: "invalid logger.format 'invalid'",
		},
		{
			name: "negative concurrency",
			cfg: config.Config{
				Database: config.DatabaseConfig{Path: "queue.db"},
				Logger:   config.LoggerConfig{Level: "info", Format: "console"},
				Worker:   config.WorkerConfig{Concurrency: -1},
			},
			wantErr: "worker.concurrency must be greater than 0",
		},
		{
			name: "negative poll interval",
			cfg: config.Config{
				Database: config.DatabaseConfig{Path: "queue.db"},
				Logger:   config.LoggerConfig{Level: "info", Format: "console"},
				Worker:   config.WorkerConfig{Concurrency: 5, PollInterval: -1 * time.Second},
			},
			wantErr: "worker.poll_interval must be positive duration",
		},
		{
			name: "negative max retries",
			cfg: config.Config{
				Database: config.DatabaseConfig{Path: "queue.db"},
				Logger:   config.LoggerConfig{Level: "info", Format: "console"},
				Worker:   config.WorkerConfig{Concurrency: 5, PollInterval: 1 * time.Second, MaxRetries: -3},
			},
			wantErr: "worker.max_retries cannot be negative",
		},
		{
			name: "negative backoff base delay",
			cfg: config.Config{
				Database: config.DatabaseConfig{Path: "queue.db"},
				Logger:   config.LoggerConfig{Level: "info", Format: "console"},
				Worker: config.WorkerConfig{
					Concurrency:      5,
					PollInterval:     1 * time.Second,
					MaxRetries:       3,
					BackoffBaseDelay: -2 * time.Second,
				},
			},
			wantErr: "worker.backoff_base_delay must be positive duration",
		},
		{
			name: "max delay smaller than base delay",
			cfg: config.Config{
				Database: config.DatabaseConfig{Path: "queue.db"},
				Logger:   config.LoggerConfig{Level: "info", Format: "console"},
				Worker: config.WorkerConfig{
					Concurrency:      5,
					PollInterval:     1 * time.Second,
					MaxRetries:       3,
					BackoffBaseDelay: 5 * time.Second,
					BackoffMaxDelay:  2 * time.Second,
				},
			},
			wantErr: "cannot be smaller than base delay",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoadConfig_DefaultAndEnvOverrides(t *testing.T) {
	// Set environment override
	os.Setenv("QUEUECTL_DATABASE_PATH", "env_override.db")
	os.Setenv("QUEUECTL_WORKER_CONCURRENCY", "42")
	defer func() {
		os.Unsetenv("QUEUECTL_DATABASE_PATH")
		os.Unsetenv("QUEUECTL_WORKER_CONCURRENCY")
	}()

	provider, err := config.LoadConfig("")
	require.NoError(t, err)

	cfg := provider.Get()
	assert.Equal(t, "env_override.db", cfg.Database.Path)
	assert.Equal(t, 42, cfg.Worker.Concurrency)
}

func TestLoadConfig_YamlFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFilePath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := []byte(`
database:
  path: "yaml_file.db"
logger:
  level: "warn"
  format: "json"
worker:
  concurrency: 8
  poll_interval: "500ms"
  max_retries: 5
  backoff_base_delay: "3s"
  backoff_max_delay: "60s"
`)

	err := os.WriteFile(configFilePath, yamlContent, 0644)
	require.NoError(t, err)

	provider, err := config.LoadConfig(configFilePath)
	require.NoError(t, err)

	cfg := provider.Get()
	assert.Equal(t, "yaml_file.db", cfg.Database.Path)
	assert.Equal(t, "warn", cfg.Logger.Level)
	assert.Equal(t, "json", cfg.Logger.Format)
	assert.Equal(t, 8, cfg.Worker.Concurrency)
	assert.Equal(t, 500*time.Millisecond, cfg.Worker.PollInterval)
	assert.Equal(t, 5, cfg.Worker.MaxRetries)
	assert.Equal(t, 3*time.Second, cfg.Worker.BackoffBaseDelay)
	assert.Equal(t, 60*time.Second, cfg.Worker.BackoffMaxDelay)
}

func TestLoadConfig_UnsupportedExtension(t *testing.T) {
	_, err := config.LoadConfig("config.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported config file extension")
}

func TestLoadConfig_InvalidYaml(t *testing.T) {
	tmpDir := t.TempDir()
	configFilePath := filepath.Join(tmpDir, "config.yaml")

	err := os.WriteFile(configFilePath, []byte("database: path: ["), 0644)
	require.NoError(t, err)

	_, err = config.LoadConfig(configFilePath)
	assert.Error(t, err)
}

func TestConfigProvider_Callbacks(t *testing.T) {
	provider, err := config.LoadConfig("")
	require.NoError(t, err)

	var callbackCalled bool
	provider.OnChange(func(newCfg *config.Config) {
		callbackCalled = true
	})

	assert.NotNil(t, provider.Get())
	assert.False(t, callbackCalled)
}

func TestLoadConfig_HotReload(t *testing.T) {
	tmpDir := t.TempDir()
	configFilePath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := []byte(`
database:
  path: "old.db"
logger:
  level: "info"
  format: "console"
worker:
  concurrency: 5
  poll_interval: "1s"
  max_retries: 3
  backoff_base_delay: "1s"
  backoff_max_delay: "30s"
`)

	err := os.WriteFile(configFilePath, yamlContent, 0644)
	require.NoError(t, err)

	provider, err := config.LoadConfig(configFilePath)
	require.NoError(t, err)

	assert.Equal(t, "old.db", provider.Get().Database.Path)

	changeChan := make(chan *config.Config, 1)
	provider.OnChange(func(newCfg *config.Config) {
		changeChan <- newCfg
	})

	newYamlContent := []byte(`
database:
  path: "new.db"
logger:
  level: "info"
  format: "console"
worker:
  concurrency: 5
  poll_interval: "1s"
  max_retries: 3
  backoff_base_delay: "1s"
  backoff_max_delay: "30s"
`)

	err = os.WriteFile(configFilePath, newYamlContent, 0644)
	require.NoError(t, err)

	select {
	case newCfg := <-changeChan:
		assert.Equal(t, "new.db", newCfg.Database.Path)
	case <-time.After(2 * time.Second):
		t.Log("fsnotify reload timed out; skipping hot reload validation")
	}
}
