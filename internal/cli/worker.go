package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"queuectl/internal/logger"
	"queuectl/internal/worker"

	"github.com/spf13/cobra"
)

var (
	workerQueue        string
	workerConcurrency  int
	workerPollInterval time.Duration
	pidFileName        string
)

// workerCmd represents the parent worker command group
var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Manage background job processing worker pools",
	Long:  `Manage background job processing workers. Supports starting the daemon process or stopping running daemons.`,
}

// workerStartCmd represents the command to start the worker daemon
var workerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a background job processing worker pool",
	Long: `Start a background job processing worker pool listening to a specific queue.
It writes the running process ID (PID) to a file to allow stop control.`,
	Example: `  # Start worker polling the "default" queue
  queuectl worker start

  # Start worker with 10 concurrency limits and 500ms database poll intervals
  queuectl worker start --queue high_priority --concurrency 10 --poll-interval 500ms`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Argument overrides & validation
		if workerConcurrency < 0 {
			return fmt.Errorf("concurrency limit cannot be negative")
		}
		if workerPollInterval < 0 {
			return fmt.Errorf("poll-interval duration cannot be negative")
		}

		if workerConcurrency > 0 {
			AppConfig.Worker.Concurrency = workerConcurrency
		}
		if workerPollInterval > 0 {
			AppConfig.Worker.PollInterval = workerPollInterval
		}

		// Register standard mock handlers
		registerDemoHandlers()

		// 2. Initialize PID file
		pidPath := filepath.Clean(pidFileName)
		pid := os.Getpid()
		if err := writePIDFile(pidPath, pid); err != nil {
			return fmt.Errorf("failed to create worker pid file: %w", err)
		}
		defer func() {
			_ = os.Remove(pidPath)
		}()

		// 3. Setup worker pool execution
		pool := worker.NewWorkerPool(Repo, Svc, Sched, AppConfig, Logger, workerQueue)

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		go func() {
			sig := <-sigChan
			Logger.Info("system signal received, initiating graceful shutdown", logger.String("signal", sig.String()))
			cancel()

			// Fallback force-exit if active tasks hang during graceful shutdown
			<-time.After(30 * time.Second)
			Logger.Fatal("graceful shutdown timed out, force exiting process")
			os.Exit(1)
		}()

		Logger.Info("worker process started", logger.Int("pid", pid), logger.String("pid_file", pidPath))
		if err := pool.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("worker pool terminated unexpectedly: %w", err)
		}

		Logger.Info("worker process stopped gracefully")
		return nil
	},
}

// workerStopCmd represents the command to terminate a running worker daemon
var workerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a running worker pool process",
	Long: `Reads the PID of the worker process from the pid file and sends a termination signal.
Supports graceful stops on Unix (SIGTERM) and fallback kills.`,
	Example: `  # Stop the running worker process
  queuectl worker stop`,
	RunE: func(cmd *cobra.Command, args []string) error {
		pidPath := filepath.Clean(pidFileName)

		// 1. Read the PID file
		pid, err := readPIDFile(pidPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("no running worker process found (pid file '%s' does not exist)", pidPath)
			}
			return fmt.Errorf("failed to read pid file: %w", err)
		}

		// 2. Find the process
		proc, err := os.FindProcess(pid)
		if err != nil {
			// Clean up orphaned pid file
			_ = os.Remove(pidPath)
			return fmt.Errorf("could not find process with PID %d: %w", pid, err)
		}

		// 3. Send termination signal (cross-platform compatible signal)
		fmt.Printf("Stopping worker process (PID: %d)...\n", pid)

		// Try sending Interrupt (SIGINT) first
		err = proc.Signal(os.Interrupt)
		if err != nil {
			// If sending signal fails (e.g. process is dead but PID file remains)
			_ = os.Remove(pidPath)
			return fmt.Errorf("failed to send shutdown signal to worker: %w", err)
		}

		// Verify process exit by waiting in a loop, fallback to kill if unresponsive
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.After(5 * time.Second)

		for {
			select {
			case <-timeout:
				fmt.Println("Worker unresponsive to graceful stop. Forcing shutdown...")
				_ = proc.Kill()
				_ = os.Remove(pidPath)
				fmt.Println("Worker terminated.")
				return nil
			case <-ticker.C:
				// On Unix/Windows, sending signal 0 checks if process is alive
				if err := proc.Signal(syscall.Signal(0)); err != nil {
					// Process is no longer running
					_ = os.Remove(pidPath)
					fmt.Println("Worker stopped successfully.")
					return nil
				}
			}
		}
	},
}

// writePIDFile saves the current PID to file.
func writePIDFile(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0600)
}

// readPIDFile retrieves the PID from file.
func readPIDFile(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return 0, err
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PID data: %w", err)
	}

	return pid, nil
}

// registerDemoHandlers registers mock handlers
func registerDemoHandlers() {
	_ = Svc.RegisterHandler("email", func(ctx context.Context, payload string) error {
		Logger.Info("simulating email delivery...", logger.String("payload", payload))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
		Logger.Info("email delivered successfully")
		return nil
	})

	_ = Svc.RegisterHandler("image_resize", func(ctx context.Context, payload string) error {
		Logger.Info("simulating image resizing...", logger.String("payload", payload))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
		Logger.Info("image resized successfully")
		return nil
	})

	_ = Svc.RegisterHandler("error_demo", func(ctx context.Context, payload string) error {
		Logger.Warn("error_demo handler invoked: simulating task failure", logger.String("payload", payload))
		return errors.New("simulated network connection timeout (temporary error)")
	})

	_ = Svc.RegisterHandler("panic_demo", func(ctx context.Context, payload string) error {
		Logger.Warn("panic_demo handler invoked: simulating runtime panic", logger.String("payload", payload))
		panic("nil pointer dereference or index out of range simulated in user handler")
	})

	// 5. Shell command execution handler
	_ = Svc.RegisterHandler("exec_command", func(ctx context.Context, payload string) error {
		type CommandPayload struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
			Timeout int      `json:"timeout_seconds"`
		}

		var cmdPayload CommandPayload
		if err := json.Unmarshal([]byte(payload), &cmdPayload); err != nil {
			return fmt.Errorf("failed to parse command payload: %w", err)
		}

		if cmdPayload.Command == "" {
			return errors.New("command parameter cannot be empty")
		}

		// Apply custom timeout context if configured
		runCtx := ctx
		if cmdPayload.Timeout > 0 {
			var cancel context.CancelFunc
			runCtx, cancel = context.WithTimeout(ctx, time.Duration(cmdPayload.Timeout)*time.Second)
			defer cancel()
		}

		Logger.Info("executing OS command",
			logger.String("command", cmdPayload.Command),
			logger.Any("args", cmdPayload.Args),
		)

		startTime := time.Now()
		osCmd := exec.CommandContext(runCtx, cmdPayload.Command, cmdPayload.Args...)

		var stdoutBuf, stderrBuf bytes.Buffer
		osCmd.Stdout = &stdoutBuf
		osCmd.Stderr = &stderrBuf

		err := osCmd.Run()
		executionDuration := time.Since(startTime)

		stdoutStr := stdoutBuf.String()
		stderrStr := stderrBuf.String()

		// Structured telemetry log fields
		logFields := []logger.Field{
			logger.String("command", cmdPayload.Command),
			logger.ExecutionTime(executionDuration),
			logger.String("stdout", stdoutStr),
			logger.String("stderr", stderrStr),
		}

		if err != nil {
			logFields = append(logFields, logger.Error(err))
			Logger.Error("OS command execution failed", logFields...)
			return fmt.Errorf("command execution failed: %v, stderr: %s", err, stderrStr)
		}

		Logger.Info("OS command executed successfully", logFields...)
		return nil
	})

	_ = Svc.RegisterHandler("data_sync", func(ctx context.Context, payload string) error {
		Logger.Info("starting data synchronization task...", logger.String("payload", payload))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		Logger.Info("data synchronization completed successfully")
		return nil
	})
}

func init() {
	workerCmd.PersistentFlags().StringVarP(&workerQueue, "queue", "q", "default", "Queue name to poll for jobs")
	workerCmd.PersistentFlags().StringVar(&pidFileName, "pid-file", "worker.pid", "Path to write the worker PID file")

	workerStartCmd.Flags().IntVarP(&workerConcurrency, "concurrency", "c", 0, "Number of concurrent job execution workers (overrides config)")
	workerStartCmd.Flags().DurationVarP(&workerPollInterval, "poll-interval", "p", 0, "Database polling frequency (overrides config)")

	workerCmd.AddCommand(workerStartCmd)
	workerCmd.AddCommand(workerStopCmd)
	RootCmd.AddCommand(workerCmd)
}
