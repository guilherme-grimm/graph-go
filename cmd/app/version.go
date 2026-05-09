package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	Commit    = ""
	BuildDate = ""
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, and build date.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), versionString(Version, Commit, BuildDate)); err != nil {
				return fmt.Errorf("write version output: %w", err)
			}

			return nil
		},
	}
}

func versionString(version, commit, date string) string {
	if version == "" {
		version = "(devel)"
	}
	if commit == "" {
		commit = "(unknown)"
	}
	if date == "" {
		date = "(unknown)"
	}

	return fmt.Sprintf("graph-go %s\ncommit: %s\nbuilt:  %s", version, commit, date)
}
