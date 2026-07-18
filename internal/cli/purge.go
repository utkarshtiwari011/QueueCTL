package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var purgeOlderThan time.Duration

// purgeCmd represents the purge command
var purgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Purge completed jobs from the database",
	Long:  `Delete successfully completed jobs from the database that are older than the specified duration.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		count, err := Svc.PurgeCompleted(ctx, purgeOlderThan)
		if err != nil {
			return err
		}

		fmt.Printf("Purge operation completed successfully. Deleted %d completed jobs older than %s.\n", count, purgeOlderThan)
		return nil
	},
}

func init() {
	purgeCmd.Flags().DurationVar(&purgeOlderThan, "older-than", 24*time.Hour, "Delete completed jobs older than this duration (e.g. 1h, 12h, 7d)")

	RootCmd.AddCommand(purgeCmd)
}
