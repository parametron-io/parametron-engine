package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

type snapshotDescriptor struct {
	SchemaVersion  string         `json:"schemaVersion"`
	DSLFile        string         `json:"dslFile"`
	DSLHash        string         `json:"dslHash"`
	PlanHash       string         `json:"planHash"`
	Profile        string         `json:"profile,omitempty"`
	Inputs         map[string]any `json:"inputs"`
	RunRoot        string         `json:"runRoot"`
	GeneratedFiles []string       `json:"generatedFiles"`
}

func newSnapshotCmd() *cobra.Command {
	var inputsPath string

	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Execute one deterministic case and capture a stable snapshot package",
		RunE: func(cmd *cobra.Command, args []string) error {
			configureCommandLogging(debugMode, false)
			entryPath, err := resolveExecutionEntrypoint(dslFilePath, projectPath)
			if err != nil {
				return err
			}
			if inputsPath == "" {
				return fmt.Errorf("required flag(s) \"inputs\" not set")
			}
			if strings.TrimSpace(outputDir) == "" {
				return fmt.Errorf("required flag(s) \"out\" not set")
			}

			inputs, overrides, err := loadJSONOverridesFile(inputsPath)
			if err != nil {
				return err
			}
			if err := ensureDirectoryEmpty(outputDir); err != nil {
				return err
			}

			planned, err := loadPlannedRun(entryPath, overrides, args)
			if err != nil {
				return err
			}
			if err := writeJSONFile(filepath.Join(outputDir, "inputs.json"), inputs); err != nil {
				return err
			}
			if err := writeJSONFile(filepath.Join(outputDir, "plan.json"), planned.Plan); err != nil {
				return err
			}

			result, execErr := executePlanRun(executionOptions{
				Planned:         planned,
				ExplicitRunRoot: outputDir,
				UseCache:        false,
			})
			files, filesErr := sortedFileList(outputDir)
			if filesErr != nil {
				return filesErr
			}

			descriptor := snapshotDescriptor{
				SchemaVersion:  harnessSchemaVersion,
				DSLFile:        entryPath,
				DSLHash:        planned.DSLHash,
				PlanHash:       planned.PlanHash,
				Profile:        planned.ProfileName,
				Inputs:         inputs,
				RunRoot:        outputDir,
				GeneratedFiles: files,
			}
			if err := writeJSONFile(filepath.Join(outputDir, "snapshot.json"), descriptor); err != nil {
				return err
			}
			if execErr != nil {
				return execErr
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Snapshot created at %s\n", result.RunRoot)
			return err
		},
	}

	cmd.Flags().StringVar(&inputsPath, "inputs", "", "Path to a JSON object with parameter overrides")
	return cmd
}
