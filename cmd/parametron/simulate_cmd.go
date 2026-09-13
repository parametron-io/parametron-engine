package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

type simulateCaseSummary struct {
	Index    int            `json:"index"`
	Inputs   map[string]any `json:"inputs"`
	Status   string         `json:"status"`
	PlanHash string         `json:"planHash,omitempty"`
	RunRoot  string         `json:"runRoot,omitempty"`
	Error    string         `json:"error,omitempty"`
}

type simulateReport struct {
	SchemaVersion string                `json:"schemaVersion"`
	File          string                `json:"file"`
	TotalCases    int                   `json:"totalCases"`
	PassedCases   int                   `json:"passedCases"`
	FailedCases   int                   `json:"failedCases"`
	Cases         []simulateCaseSummary `json:"cases"`
}

func newSimulateCmd() *cobra.Command {
	var inputsPath string
	var failFast bool
	var maxErrors int

	cmd := &cobra.Command{
		Use:   "simulate",
		Short: "Execute multiple deterministic input cases through the runtime path",
		RunE: func(cmd *cobra.Command, args []string) error {
			configureCommandLogging(debugMode, false)
			entryPath, err := resolveExecutionEntrypoint(dslFilePath, projectPath)
			if err != nil {
				return err
			}
			if inputsPath == "" {
				return fmt.Errorf("required flag(s) \"inputs\" not set")
			}

			rawCases, caseOverrides, err := loadJSONCaseArray(inputsPath)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return err
			}

			summary := simulateReport{
				SchemaVersion: harnessSchemaVersion,
				File:          entryPath,
				TotalCases:    len(rawCases),
				Cases:         make([]simulateCaseSummary, 0, len(rawCases)),
			}

			failures := 0
			stopped := false
			for index := range rawCases {
				entry := simulateCaseSummary{
					Index:  index,
					Inputs: rawCases[index],
					Status: "skipped",
				}
				if stopped {
					summary.Cases = append(summary.Cases, entry)
					continue
				}

				planned, err := loadPlannedRun(entryPath, caseOverrides[index], args)
				if err != nil {
					entry.Status = "failed"
					entry.Error = err.Error()
					summary.FailedCases++
					failures++
				} else {
					entry.PlanHash = planned.PlanHash
					caseRunRoot := filepath.Join(outputDir, fmt.Sprintf("case-%03d", index))
					runResult, execErr := executePlanRun(executionOptions{
						Planned:         planned,
						ExplicitRunRoot: caseRunRoot,
						UseCache:        false,
					})
					entry.RunRoot = caseRunRoot
					if execErr != nil {
						entry.Status = "failed"
						entry.Error = execErr.Error()
						summary.FailedCases++
						failures++
					} else {
						entry.Status = "passed"
						entry.PlanHash = planned.PlanHash
						entry.RunRoot = runResult.RunRoot
						summary.PassedCases++
					}
				}
				summary.Cases = append(summary.Cases, entry)

				if failFast && failures > 0 {
					stopped = true
				}
				if maxErrors > 0 && failures >= maxErrors {
					stopped = true
				}
			}

			if err := writeJSONFile(filepath.Join(outputDir, "simulate_report.json"), summary); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "total cases: %d\npassed: %d\nfailed: %d\n", summary.TotalCases, summary.PassedCases, summary.FailedCases)
			return err
		},
	}

	cmd.Flags().StringVar(&inputsPath, "inputs", "", "Path to a JSON array of flat input objects")
	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "Stop after the first failed case")
	cmd.Flags().IntVar(&maxErrors, "max-errors", 0, "Stop after reaching this many failed cases")
	return cmd
}
