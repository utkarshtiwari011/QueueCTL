package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"queuectl/internal/domain"

	"github.com/spf13/cobra"
)

var (
	listQueue  string
	listStatus string
	listLimit  int
	listSearch string
)

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List background jobs with filtering options",
	Long: `List jobs scheduled or executed in the SQLite database.
Supports filtering by target queue name, job execution state, and result limits.`,
	Example: `  # List the oldest 20 jobs of all statuses
  queuectl list

  # Search jobs containing "user_signup"
  queuectl list --search user_signup

  # List the oldest 50 pending jobs in the "high_priority" queue
  queuectl list --queue high_priority --status pending --limit 50`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Argument validations
		if listLimit < 0 {
			return fmt.Errorf("limit count cannot be negative")
		}
		if listLimit > 500 {
			return fmt.Errorf("limit count cannot exceed 500 records")
		}
		if listLimit == 0 {
			listLimit = 20
		}

		if listStatus != "" {
			switch domain.JobStatus(listStatus) {
			case domain.StatusPending, domain.StatusRunning, domain.StatusCompleted, domain.StatusFailed, domain.StatusDeadLetter:
				// Valid status
			default:
				return fmt.Errorf("invalid status filter '%s'. Must be one of: pending, running, completed, failed, dead_letter", listStatus)
			}
		}

		// 2. Query service layer
		ctx := cmd.Context()
		jobs, err := Svc.ListJobs(ctx, listQueue, domain.JobStatus(listStatus), listSearch, listLimit)
		if err != nil {
			return fmt.Errorf("failed to query jobs from database: %w", err)
		}

		if len(jobs) == 0 {
			fmt.Println("No jobs found matching the specified filters.")
			return nil
		}

		// 3. Render output via tabwriter
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "JOB ID\tTYPE\tQUEUE\tSTATUS\tRETRIES\tSCHEDULED RUN\tLAST ERROR")
		for _, job := range jobs {
			errStr := "-"
			if job.ErrorMessage != "" {
				errStr = job.ErrorMessage
				if len(errStr) > 30 {
					errStr = errStr[:27] + "..."
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d/%d\t%s\t%s\n",
				job.ID,
				job.Type,
				job.Queue,
				job.Status,
				job.RetriesCount,
				job.MaxRetries,
				job.RunAt.Format("2006-01-02 15:04:05"),
				errStr,
			)
		}

		return w.Flush()
	},
}

func init() {
	listCmd.Flags().StringVarP(&listQueue, "queue", "q", "", "Filter jobs by queue name")
	listCmd.Flags().StringVarP(&listStatus, "status", "s", "", "Filter jobs by status (pending, running, completed, failed, dead_letter)")
	listCmd.Flags().IntVarP(&listLimit, "limit", "l", 20, "Maximum number of jobs to list (default 20, max 500)")
	listCmd.Flags().StringVar(&listSearch, "search", "", "Search payloads, job types, or error strings")

	RootCmd.AddCommand(listCmd)
}
