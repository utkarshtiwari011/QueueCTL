package cli

import (
	"fmt"
	"time"

	"queuectl/internal/utils"

	"github.com/spf13/cobra"
)

var (
	enqueueQueue      string
	enqueueDelay      time.Duration
	enqueueMaxRetries int
	enqueuePriority   int
)

// enqueueCmd represents the enqueue command
var enqueueCmd = &cobra.Command{
	Use:   "enqueue [job-type] [payload]",
	Short: "Enqueue a new background job",
	Long: `Enqueue a new background job into the SQLite storage queue.
The job will be scheduled for execution based on the queue name, priority, and delay duration flags.`,
	Example: `  # Enqueue an email job immediately
  queuectl enqueue email '{"to": "user@example.com", "body": "Hello!"}'

  # Enqueue a high priority resizing job with 30s delay on the "processing" queue
  queuectl enqueue image_resize '{"image_id": "12345"}' --queue processing --priority 10 --delay 30s

  # Enqueue a sync job with custom max-retries limit
  queuectl enqueue data_sync '{}' --max-retries 5`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobType := args[0]
		payload := args[1]

		// 1. Argument validation
		if jobType == "" {
			return fmt.Errorf("job-type cannot be empty")
		}

		// Validate that the payload is valid JSON (using our utility helper)
		if err := utils.ValidateJSON(payload); err != nil {
			return fmt.Errorf("invalid payload format: %w", err)
		}

		if enqueueMaxRetries < 0 {
			return fmt.Errorf("max-retries limit cannot be negative")
		}

		var runAt time.Time
		if enqueueDelay > 0 {
			runAt = time.Now().Add(enqueueDelay)
		} else {
			runAt = time.Now()
		}

		// 2. Delegate to service layer
		ctx := cmd.Context()
		job, err := Svc.Enqueue(ctx, jobType, payload, enqueueQueue, enqueuePriority, runAt, enqueueMaxRetries)
		if err != nil {
			return fmt.Errorf("failed to enqueue job: %w", err)
		}

		// 3. Print output
		fmt.Printf("Job enqueued successfully:\n")
		fmt.Printf("  ID:           %s\n", job.ID)
		fmt.Printf("  Type:         %s\n", job.Type)
		fmt.Printf("  Queue:        %s\n", job.Queue)
		fmt.Printf("  Status:       %s\n", job.Status)
		fmt.Printf("  Priority:     %d\n", job.Priority)
		fmt.Printf("  Max Retries:  %d\n", job.MaxRetries)
		fmt.Printf("  Scheduled At: %s\n", job.RunAt.Format(time.RFC3339))

		return nil
	},
}

func init() {
	enqueueCmd.Flags().StringVarP(&enqueueQueue, "queue", "q", "default", "Target queue name")
	enqueueCmd.Flags().DurationVarP(&enqueueDelay, "delay", "d", 0, "Execution delay duration (e.g. 10s, 5m, 1h)")
	enqueueCmd.Flags().IntVar(&enqueueMaxRetries, "max-retries", 0, "Max retry limit before job is sent to DLQ (0 defaults to configuration value)")
	enqueueCmd.Flags().IntVarP(&enqueuePriority, "priority", "p", 0, "Job priority (higher values executed first)")

	RootCmd.AddCommand(enqueueCmd)
}
