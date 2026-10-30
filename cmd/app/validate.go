package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/guilherme-grimm/graph-go/internal/config"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "validate",
		Short:        "Check the config file and exit; no server is started.",
		Long:         "validate parses the config file and reports every problem it finds: missing required fields, unknown connection types, and unknown keys (which serve silently ignores). Exits non-zero when the config is invalid. The report goes to stderr.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			return runValidate(cmd, configPath)
		},
	}
}

func runValidate(cmd *cobra.Command, configPath string) error {
	problems, err := config.Check(configPath)
	if err != nil {
		return err
	}

	stderr := cmd.ErrOrStderr()
	if len(problems) == 0 {
		fmt.Fprintf(stderr, "✓ %s is valid\n", configPath)
		return nil
	}

	fmt.Fprintf(stderr, "✗ %s has %d problem(s):\n", configPath, len(problems))
	for _, p := range problems {
		fmt.Fprintf(stderr, "  - %s\n", p)
	}
	return fmt.Errorf("invalid config: %s", configPath)
}
