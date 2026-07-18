package cli

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// metricsCmd represents the metrics command
var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Display QueueCTL system metrics and execution telemetry",
	Long: `Retrieve aggregated metrics across jobs, workers, and execution logs.
Includes job states, total retries, execution times, success rates, and worker utilization.`,
	Example: `  # Get real-time queue metrics and telemetry statistics
  queuectl metrics`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		m, err := MetricsSvc.GetMetrics(ctx)
		if err != nil {
			return fmt.Errorf("failed to retrieve metrics: %w", err)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "============================================================")
		fmt.Fprintln(w, "                     QueueCTL Telemetry Metrics             ")
		fmt.Fprintln(w, "============================================================")
		fmt.Fprintf(w, "Job States:\n")
		fmt.Fprintf(w, "  Pending Jobs:\t%d\n", m.PendingCount)
		fmt.Fprintf(w, "  Running Jobs:\t%d\n", m.RunningCount)
		fmt.Fprintf(w, "  Completed Jobs:\t%d\n", m.CompletedCount)
		fmt.Fprintf(w, "  Failed Jobs:\t%d\n", m.FailedCount)
		fmt.Fprintf(w, "  Dead Letter Queue:\t%d\n", m.DLQCount)
		fmt.Fprintln(w, "\t")

		fmt.Fprintf(w, "Execution Performance:\n")
		fmt.Fprintf(w, "  Total Reschedule Retries:\t%d\n", m.TotalRetries)
		fmt.Fprintf(w, "  Average Execution Runtime:\t%s\n", m.AverageRuntime.Round(time.Millisecond))
		fmt.Fprintf(w, "  Attempt Success Rate:\t%.2f%%\n", m.SuccessRate)
		fmt.Fprintln(w, "\t")

		fmt.Fprintf(w, "Worker Telemetry:\n")
		fmt.Fprintf(w, "  Registered Worker Nodes:\t%d\n", m.TotalWorkersCount)
		fmt.Fprintf(w, "  Active Workers:\t%d\n", m.ActiveWorkersCount)
		fmt.Fprintf(w, "  Worker Node Utilization:\t%.2f%%\n", m.WorkerUtilization)
		fmt.Fprintln(w, "============================================================")

		return w.Flush()
	},
}

func init() {
	RootCmd.AddCommand(metricsCmd)
}
