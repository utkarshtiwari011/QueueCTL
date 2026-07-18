package cli

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var statusJobID string

// statusCmd represents the status command
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status health of job queues or view job details",
	Long: `Display the aggregated health statistics for all active queues.
Optionally, lookup full parameters of a job by passing its UUID.`,
	Example: `  # Display health statistics metrics for all queues
  queuectl status

  # Lookup specific details of a job by ID
  queuectl status --id a65d4c9f-3d84-4fe1-ba91-ef17d4a22c54`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Case 1: Lookup specific job ID
		if statusJobID != "" {
			job, err := Svc.GetJob(ctx, statusJobID)
			if err != nil {
				return fmt.Errorf("failed to retrieve job details: %w", err)
			}

			fmt.Printf("Job Details:\n")
			fmt.Printf("  ID:            %s\n", job.ID)
			fmt.Printf("  Type:          %s\n", job.Type)
			fmt.Printf("  Payload:       %s\n", job.Payload)
			fmt.Printf("  Queue:         %s\n", job.Queue)
			fmt.Printf("  Status:        %s\n", job.Status)
			fmt.Printf("  Max Retries:   %d\n", job.MaxRetries)
			fmt.Printf("  Retries Run:   %d\n", job.RetriesCount)
			fmt.Printf("  Scheduled Run: %s\n", job.RunAt.Format(time.RFC3339))
			fmt.Printf("  Created At:    %s\n", job.CreatedAt.Format(time.RFC3339))
			fmt.Printf("  Updated At:    %s\n", job.UpdatedAt.Format(time.RFC3339))
			if job.ErrorMessage != "" {
				fmt.Printf("  Last Error:    %s\n", job.ErrorMessage)
			}
			return nil
		}

		// Case 2: Render aggregated queue stats
		stats, err := Svc.GetStats(ctx)
		if err != nil {
			return fmt.Errorf("failed to query queue metrics: %w", err)
		}

		if len(stats) == 0 {
			fmt.Println("No jobs or queues currently exist in the database.")
			return nil
		}

		fmt.Println("Queue Metrics Summary:")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 4, ' ', 0)
		fmt.Fprintln(w, "QUEUE NAME\tPENDING\tRUNNING\tCOMPLETED\tFAILED\tDLQ (DEAD LETTER)")

		for queue, m := range stats {
			// Pull status counts
			fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\n",
				queue,
				m["pending"],
				m["running"],
				m["completed"],
				m["failed"],
				m["dead_letter"],
			)
		}
		return w.Flush()
	},
}

func init() {
	statusCmd.Flags().StringVar(&statusJobID, "id", "", "Find and print full attributes of a job by ID")

	RootCmd.AddCommand(statusCmd)
}
