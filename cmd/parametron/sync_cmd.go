package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Create or refresh parametron.lock.json from a project entrypoint",
		RunE: func(cmd *cobra.Command, args []string) error {
			configureCommandLogging(debugMode, false)
			entryPath, err := resolveProjectCommandEntrypoint(dslFilePath, projectPath)
			if err != nil {
				return err
			}

			lockPath, err := syncProjectLock(entryPath)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Lock file written: %s\n", lockPath)
			return err
		},
	}
}
