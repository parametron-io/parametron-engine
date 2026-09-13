package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	var inputsPath string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Parse, validate, and optionally plan a DSL file with inputs",
		RunE: func(cmd *cobra.Command, args []string) error {
			configureCommandLogging(debugMode, false)
			entryPath, err := resolveExecutionEntrypoint(dslFilePath, projectPath)
			if err != nil {
				return err
			}

			overrides, err := parseOverrideList(paramOverrides)
			if err != nil {
				return err
			}
			if inputsPath != "" {
				_, inputOverrides, err := loadJSONOverridesFile(inputsPath)
				if err != nil {
					return err
				}
				for key, value := range inputOverrides {
					overrides[key] = value
				}
			}

			if _, err := loadPlannedRun(entryPath, overrides, args); err != nil {
				return err
			}

			if inputsPath != "" {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Validation succeeded for %s with inputs %s\n", entryPath, inputsPath)
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Validation succeeded for %s\n", entryPath)
			return err
		},
	}

	cmd.Flags().StringVar(&inputsPath, "inputs", "", "Path to a JSON object with parameter overrides")
	return cmd
}
