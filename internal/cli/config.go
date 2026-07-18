package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configFormat string

// configCmd represents the config command
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Print the current active configuration",
	Long:  `Print the resolved configurations loaded from environment overrides, configuration files, and default values.`,
	Example: `  # Print configuration in YAML format (default)
  queuectl config

  # Print configuration in JSON format
  queuectl config --format json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format := strings.ToLower(configFormat)

		// 1. Validation
		if format != "yaml" && format != "json" {
			return fmt.Errorf("invalid config format '%s'. Must be either 'yaml' or 'json'", configFormat)
		}

		// 2. Marshalling
		var output []byte
		var err error
		if format == "json" {
			output, err = json.MarshalIndent(AppConfig, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal config to JSON: %w", err)
			}
		} else {
			output, err = yaml.Marshal(AppConfig)
			if err != nil {
				return fmt.Errorf("failed to marshal config to YAML: %w", err)
			}
		}

		// 3. Print output
		fmt.Println(string(output))
		return nil
	},
}

func init() {
	configCmd.Flags().StringVarP(&configFormat, "format", "f", "yaml", "Configuration output format (yaml, json)")

	RootCmd.AddCommand(configCmd)
}
