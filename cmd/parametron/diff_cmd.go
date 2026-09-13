package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"
)

type diffSummary struct {
	SchemaVersion string   `json:"schemaVersion"`
	Mode          string   `json:"mode"`
	Equal         bool     `json:"equal"`
	Summary       string   `json:"summary"`
	Differences   []string `json:"differences,omitempty"`
	FirstDiffByte int      `json:"firstDiffByte,omitempty"`
	FirstDiffLine int      `json:"firstDiffLine,omitempty"`
	FirstDiffCol  int      `json:"firstDiffColumn,omitempty"`
}

func newDiffCmd() *cobra.Command {
	var planPaths []string
	var snapshotPaths []string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Compare two plan JSON files or two snapshot directories",
		RunE: func(cmd *cobra.Command, args []string) error {
			configureCommandLogging(debugMode, jsonOutput)

			var summary diffSummary
			switch {
			case len(planPaths) == 2 && len(snapshotPaths) == 0:
				result, err := diffPlanFiles(planPaths[0], planPaths[1])
				if err != nil {
					return err
				}
				summary = result
			case len(snapshotPaths) == 2 && len(planPaths) == 0:
				result, err := diffSnapshotDirs(snapshotPaths[0], snapshotPaths[1])
				if err != nil {
					return err
				}
				summary = result
			default:
				return fmt.Errorf("provide either two --plan flags or two --snapshot flags")
			}

			if jsonOutput {
				return writeJSON(cmd.OutOrStdout(), summary)
			}
			if summary.Equal {
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: equal\n", summary.Mode)
				return err
			}
			if summary.FirstDiffByte > 0 {
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: different\nfirst difference at byte %d (line %d, column %d)\n", summary.Mode, summary.FirstDiffByte, summary.FirstDiffLine, summary.FirstDiffCol)
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: different\n%s\n", summary.Mode, summary.Summary)
			return err
		},
	}

	cmd.Flags().StringArrayVar(&planPaths, "plan", nil, "Path to a plan JSON file")
	cmd.Flags().StringArrayVar(&snapshotPaths, "snapshot", nil, "Path to a snapshot directory")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit structured diff output")
	return cmd
}

func diffPlanFiles(aPath, bPath string) (diffSummary, error) {
	aRaw, err := os.ReadFile(aPath)
	if err != nil {
		return diffSummary{}, err
	}
	bRaw, err := os.ReadFile(bPath)
	if err != nil {
		return diffSummary{}, err
	}
	aNorm, err := normalizeJSONBytes(aRaw)
	if err != nil {
		return diffSummary{}, err
	}
	bNorm, err := normalizeJSONBytes(bRaw)
	if err != nil {
		return diffSummary{}, err
	}
	if string(aNorm) == string(bNorm) {
		return diffSummary{
			SchemaVersion: harnessSchemaVersion,
			Mode:          "plan",
			Equal:         true,
			Summary:       "plans are equal",
		}, nil
	}
	offset, line, col := firstDiffLineCol(aNorm, bNorm)
	return diffSummary{
		SchemaVersion: harnessSchemaVersion,
		Mode:          "plan",
		Equal:         false,
		Summary:       "plans differ",
		FirstDiffByte: offset + 1,
		FirstDiffLine: line,
		FirstDiffCol:  col,
	}, nil
}

func diffSnapshotDirs(aDir, bDir string) (diffSummary, error) {
	differences := []string{}

	aSnapshot, err := readSnapshotDescriptor(filepath.Join(aDir, "snapshot.json"))
	if err != nil {
		return diffSummary{}, err
	}
	bSnapshot, err := readSnapshotDescriptor(filepath.Join(bDir, "snapshot.json"))
	if err != nil {
		return diffSummary{}, err
	}

	if aSnapshot.PlanHash != bSnapshot.PlanHash {
		differences = append(differences, "planHash differs")
	}
	if aSnapshot.DSLHash != bSnapshot.DSLHash {
		differences = append(differences, "dslHash differs")
	}
	if !jsonEqual(aSnapshot.Inputs, bSnapshot.Inputs) {
		differences = append(differences, "inputs differ")
	}
	if !slices.Equal(aSnapshot.GeneratedFiles, bSnapshot.GeneratedFiles) {
		differences = append(differences, "generatedFiles differ")
	}

	for _, fileName := range []string{"report.json", "metadata.json", "manifest.json"} {
		aExists := fileExists(filepath.Join(aDir, fileName))
		bExists := fileExists(filepath.Join(bDir, fileName))
		if aExists != bExists {
			differences = append(differences, fileName+" presence differs")
		}
	}

	if fileExists(filepath.Join(aDir, "report.json")) && fileExists(filepath.Join(bDir, "report.json")) {
		aStatus, err := readReportStatus(filepath.Join(aDir, "report.json"))
		if err != nil {
			return diffSummary{}, err
		}
		bStatus, err := readReportStatus(filepath.Join(bDir, "report.json"))
		if err != nil {
			return diffSummary{}, err
		}
		if aStatus != bStatus {
			differences = append(differences, "report status differs")
		}
	}

	if len(differences) == 0 {
		return diffSummary{
			SchemaVersion: harnessSchemaVersion,
			Mode:          "snapshot",
			Equal:         true,
			Summary:       "snapshots are equal",
		}, nil
	}
	return diffSummary{
		SchemaVersion: harnessSchemaVersion,
		Mode:          "snapshot",
		Equal:         false,
		Summary:       "snapshots differ",
		Differences:   differences,
	}, nil
}

func readSnapshotDescriptor(path string) (snapshotDescriptor, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return snapshotDescriptor{}, err
	}
	var descriptor snapshotDescriptor
	if err := json.Unmarshal(raw, &descriptor); err != nil {
		return snapshotDescriptor{}, err
	}
	return descriptor, nil
}

func readReportStatus(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", err
	}
	return payload.Status, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func jsonEqual(a, b any) bool {
	aBytes, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bBytes, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(aBytes) == string(bBytes)
}
