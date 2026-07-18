package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"queuectl/internal/config"
	"queuectl/internal/database"
	"queuectl/internal/dlq"
	"queuectl/internal/logger"
	"queuectl/internal/metrics"
	"queuectl/internal/repository"
	sqliteRepo "queuectl/internal/repository/sqlite"
	"queuectl/internal/retry"
	"queuectl/internal/scheduler"
	"queuectl/internal/service"

	"github.com/spf13/cobra"
)

var (
	cfgFile string
	dbPath  string
	verbose bool

	// Shared resources initialized in PersistentPreRunE
	AppConfig  *config.Config
	Logger     logger.Logger
	DB         *sql.DB
	Repo       repository.JobRepository
	Svc        service.JobService
	DLQSvc     dlq.Service
	Sched      scheduler.Scheduler
	MetricsSvc metrics.Service
)

// RootCmd represents the base command when called without any subcommands.
var RootCmd = &cobra.Command{
	Use:   "queuectl",
	Short: "QueueCTL is a production-grade background job queue CLI",
	Long: `QueueCTL is a lightweight, reliable, and production-grade background job queue CLI.
It leverages SQLite for storage, Viper for configurations, Zap for logging,
and provides Clean Architecture implementation of job scheduling, worker execution,
and Dead Letter Queue (DLQ) support.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// 1. Load configuration provider
		provider, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}
		AppConfig = provider.Get()

		// 2. Override configurations via CLI flags if provided
		if dbPath != "" {
			AppConfig.Database.Path = dbPath
		}
		if verbose {
			AppConfig.Logger.Level = "debug"
		}

		// 3. Initialize logger
		log, err := logger.New(AppConfig.Logger)
		if err != nil {
			return fmt.Errorf("failed to initialize logger: %w", err)
		}
		Logger = log

		// Register hot reload callbacks to dynamically updates config parameters
		provider.OnChange(func(newCfg *config.Config) {
			if Logger != nil {
				Logger.Info("configuration file changed; hot reloaded dynamic settings",
					logger.Int("concurrency", newCfg.Worker.Concurrency),
					logger.Duration("poll_interval", newCfg.Worker.PollInterval),
				)
			}
			AppConfig = newCfg
		})

		// 4. Initialize database connection
		db, err := database.Connect(AppConfig.Database.Path)
		if err != nil {
			return err
		}
		DB = db

		// 5. Run migrations
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		if err := database.RunMigrations(ctx, DB); err != nil {
			return fmt.Errorf("database migrations failed: %w", err)
		}

		// 6. Initialize repository and service
		Repo = sqliteRepo.NewSQLiteJobRepository(DB)
		workerRepo := sqliteRepo.NewSQLiteWorkerRepository(DB)
		execLogRepo := sqliteRepo.NewSQLiteExecutionLogRepository(DB)

		Sched = scheduler.NewScheduler(Logger)
		// Start the scheduler background ticker
		go Sched.Start(cmd.Context(), AppConfig.Worker.PollInterval)

		backoffCalculator := retry.NewExponentialBackoff(AppConfig.Worker.BackoffBaseDelay, AppConfig.Worker.BackoffMaxDelay)
		retryEngine := retry.NewRetryEngine(backoffCalculator, Logger)
		dlqRouter := dlq.NewRouter(Repo)
		Svc = service.NewJobService(Repo, workerRepo, execLogRepo, AppConfig, Logger, retryEngine, dlqRouter, Sched)
		DLQSvc = dlq.NewDLQService(Repo, Logger)
		MetricsSvc = metrics.NewMetricsService(Repo, workerRepo, execLogRepo)

		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		// Cleanup database connections and flush loggers
		if DB != nil {
			_ = DB.Close()
		}
		if Logger != nil {
			_ = Logger.Sync()
		}
	},
}

func Execute() {
	RootCmd.SetOut(os.Stdout)
	RootCmd.SetErr(os.Stderr)

	if err := RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	RootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path (default is ./config.yaml)")
	RootCmd.PersistentFlags().StringVar(&dbPath, "db-path", "", "sqlite database file path")
	RootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging mode")
}
