package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var (
	dlqListQueue  string
	dlqListLimit  int
	dlqListSearch string

	dlqRetryID  string
	dlqRetryAll bool

	dlqDeleteID  string
	dlqDeleteAll bool

	dlqRestoreID    string
	dlqRestoreQueue string
)

// dlqCmd represents the parent dlq command
var dlqCmd = &cobra.Command{
	Use:   "dlq",
	Short: "Manage jobs in the Dead Letter Queue (DLQ)",
	Long:  `Manage and recover jobs that failed persistently and were sent to the Dead Letter Queue.`,
}

// dlqListCmd represents the subcommand to view DLQ jobs
var dlqListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all jobs in the Dead Letter Queue",
	Long:  `List jobs currently in 'dead_letter' status, showing the failure reason and scheduling context.`,
	Example: `  # List the oldest 20 dead letter jobs
  queuectl dlq list

  # Search dead letter jobs containing "timeout"
  queuectl dlq list --search timeout

  # List the oldest 50 dead letter jobs in the "high_priority" queue
  queuectl dlq list --queue high_priority --limit 50`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if dlqListLimit < 0 {
			return fmt.Errorf("limit count cannot be negative")
		}
		if dlqListLimit == 0 {
			dlqListLimit = 20
		}

		ctx := cmd.Context()
		jobs, err := DLQSvc.List(ctx, dlqListQueue, dlqListSearch, dlqListLimit)
		if err != nil {
			return fmt.Errorf("failed to query DLQ jobs: %w", err)
		}

		if len(jobs) == 0 {
			fmt.Println("No jobs found in the Dead Letter Queue.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "JOB ID\tTYPE\tQUEUE\tRETRIES\tFAILED AT\tERROR REASON")
		for _, job := range jobs {
			errStr := job.ErrorMessage
			if len(errStr) > 40 {
				errStr = errStr[:37] + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d\t%s\t%s\n",
				job.ID,
				job.Type,
				job.Queue,
				job.RetriesCount,
				job.MaxRetries,
				job.UpdatedAt.Format("2006-01-02 15:04:05"),
				errStr,
			)
		}

		return w.Flush()
	},
}

// dlqRetryCmd represents the subcommand to retry DLQ jobs
var dlqRetryCmd = &cobra.Command{
	Use:   "retry",
	Short: "Re-queue jobs from the Dead Letter Queue",
	Long: `Re-queue dead letter jobs by resetting their status to 'pending' and retries count to '0'.
You must specify either a specific job ID via --id, or target all dead letter jobs via --all.`,
	Example: `  # Retry a specific dead letter job by ID
  queuectl dlq retry --id a65d4c9f-3d84-4fe1-ba91-ef17d4a22c54

  # Retry all dead letter jobs in the queue database
  queuectl dlq retry --all`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if dlqRetryID == "" && !dlqRetryAll {
			return fmt.Errorf("must specify either a specific job --id or --all")
		}
		if dlqRetryID != "" && dlqRetryAll {
			return fmt.Errorf("cannot specify both a specific job --id and --all")
		}

		ctx := cmd.Context()

		if dlqRetryID != "" {
			if err := DLQSvc.Retry(ctx, dlqRetryID); err != nil {
				return fmt.Errorf("failed to retry job: %w", err)
			}
			fmt.Printf("Job %s successfully re-queued to pending state.\n", dlqRetryID)
			return nil
		}

		// Retry all
		jobs, err := DLQSvc.List(ctx, "", "", 1000)
		if err != nil {
			return fmt.Errorf("failed to query DLQ jobs: %w", err)
		}

		if len(jobs) == 0 {
			fmt.Println("No dead letter jobs found to retry.")
			return nil
		}

		fmt.Printf("Re-queueing %d jobs from Dead Letter Queue...\n", len(jobs))
		successCount := 0
		for _, job := range jobs {
			if err := DLQSvc.Retry(ctx, job.ID); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to retry job %s: %v\n", job.ID, err)
				continue
			}
			successCount++
		}

		fmt.Printf("Successfully re-queued %d out of %d dead letter jobs.\n", successCount, len(jobs))
		return nil
	},
}

// dlqDeleteCmd represents the subcommand to delete DLQ jobs
var dlqDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Permanently delete jobs from the Dead Letter Queue",
	Long: `Remove failed jobs permanently from the SQLite database.
You must specify either a specific job ID via --id, or target all dead letter jobs via --all.`,
	Example: `  # Delete a specific job by ID
  queuectl dlq delete --id a65d4c9f-3d84-4fe1-ba91-ef17d4a22c54

  # Delete all jobs in the Dead Letter Queue
  queuectl dlq delete --all`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if dlqDeleteID == "" && !dlqDeleteAll {
			return fmt.Errorf("must specify either a specific job --id or --all")
		}
		if dlqDeleteID != "" && dlqDeleteAll {
			return fmt.Errorf("cannot specify both a specific job --id and --all")
		}

		ctx := cmd.Context()

		if dlqDeleteID != "" {
			if err := DLQSvc.Delete(ctx, dlqDeleteID); err != nil {
				return fmt.Errorf("failed to delete job: %w", err)
			}
			fmt.Printf("Job %s permanently deleted from DLQ.\n", dlqDeleteID)
			return nil
		}

		// Delete all
		jobs, err := DLQSvc.List(ctx, "", "", 1000)
		if err != nil {
			return fmt.Errorf("failed to query DLQ jobs: %w", err)
		}

		if len(jobs) == 0 {
			fmt.Println("No dead letter jobs found to delete.")
			return nil
		}

		fmt.Printf("Deleting %d jobs from Dead Letter Queue...\n", len(jobs))
		successCount := 0
		for _, job := range jobs {
			if err := DLQSvc.Delete(ctx, job.ID); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to delete job %s: %v\n", job.ID, err)
				continue
			}
			successCount++
		}

		fmt.Printf("Successfully deleted %d out of %d dead letter jobs.\n", successCount, len(jobs))
		return nil
	},
}

// dlqRestoreCmd represents the subcommand to restore jobs to another queue
var dlqRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore DLQ jobs to a different queue",
	Long:  `Restore dead letter jobs back to pending state and assign them to a different queue.`,
	Example: `  # Restore a DLQ job to the "default" queue
  queuectl dlq restore --id a65d4c9f-3d84-4fe1-ba91-ef17d4a22c54 --queue default`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if dlqRestoreID == "" {
			return fmt.Errorf("must specify a job --id to restore")
		}
		if dlqRestoreQueue == "" {
			return fmt.Errorf("must specify a target --queue name")
		}

		ctx := cmd.Context()
		if err := DLQSvc.Restore(ctx, dlqRestoreID, dlqRestoreQueue); err != nil {
			return fmt.Errorf("failed to restore job: %w", err)
		}

		fmt.Printf("Job %s successfully restored to '%s' queue as pending.\n", dlqRestoreID, dlqRestoreQueue)
		return nil
	},
}

// dlqStatsCmd represents the subcommand to view DLQ statistics
var dlqStatsCmd = &cobra.Command{
	Use:     "stats",
	Short:   "View Dead Letter Queue statistics",
	Long:    `Display job counts currently isolated in the Dead Letter Queue, grouped by queue name.`,
	Example: `  queuectl dlq stats`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		stats, err := DLQSvc.GetStats(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch DLQ statistics: %w", err)
		}

		if len(stats) == 0 {
			fmt.Println("Dead Letter Queue is empty.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "QUEUE NAME\tDEAD LETTER JOBS COUNT")
		for qName, count := range stats {
			fmt.Fprintf(w, "%s\t%d\n", qName, count)
		}

		return w.Flush()
	},
}

func init() {
	dlqListCmd.Flags().StringVarP(&dlqListQueue, "queue", "q", "", "Filter DLQ jobs by queue name")
	dlqListCmd.Flags().IntVarP(&dlqListLimit, "limit", "l", 20, "Maximum number of DLQ jobs to list")
	dlqListCmd.Flags().StringVar(&dlqListSearch, "search", "", "Search payloads or error strings")

	dlqRetryCmd.Flags().StringVar(&dlqRetryID, "id", "", "ID of the specific job to retry")
	dlqRetryCmd.Flags().BoolVar(&dlqRetryAll, "all", false, "Retry all jobs in the Dead Letter Queue")

	dlqDeleteCmd.Flags().StringVar(&dlqDeleteID, "id", "", "ID of the specific job to delete")
	dlqDeleteCmd.Flags().BoolVar(&dlqDeleteAll, "all", false, "Delete all jobs in the Dead Letter Queue")

	dlqRestoreCmd.Flags().StringVar(&dlqRestoreID, "id", "", "ID of the job to restore")
	dlqRestoreCmd.Flags().StringVarP(&dlqRestoreQueue, "queue", "q", "", "Target queue name")

	dlqCmd.AddCommand(dlqListCmd)
	dlqCmd.AddCommand(dlqRetryCmd)
	dlqCmd.AddCommand(dlqDeleteCmd)
	dlqCmd.AddCommand(dlqRestoreCmd)
	dlqCmd.AddCommand(dlqStatsCmd)

	RootCmd.AddCommand(dlqCmd)
}
