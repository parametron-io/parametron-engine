package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/cache"
)

var (
	parametronVersion = "Dev 0.7.0"
	dslFilePath       string
	projectPath       string
	outputDir         string
	modelHash         string
	paramOverrides    []string
	tableInputs       []string
	debugMode         bool
	dryRun            bool
	printPlan         bool
	jsonPlan          bool
	printAST          bool
)

func main() {
	// Initialize structured logger with default settings
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)
	slog.SetDefault(logger)

	rootCmd := selectCommand(os.Args[1:])

	// Execute root command
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "parametron",
		Short: "Parametron Engine - deterministic engineering execution",
		Long: `Parametron Engine parses DSL definitions, validates logic, creates execution plans,
and executes requested engineering work through adapter-backed runtime capabilities.`,
		RunE: run,
		// Silence usage on error so we don't print help when logic fails
		SilenceUsage: true,
	}
	bindPersistentFlags(rootCmd)
	return rootCmd
}

func newHarnessRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "parametron",
		Short: "Parametron Engine - deterministic engineering execution",
		Long: `Parametron Engine parses DSL definitions, validates logic, creates execution plans,
and executes requested engineering work through adapter-backed runtime capabilities.`,
		RunE:         run,
		SilenceUsage: true,
	}
	bindPersistentFlags(rootCmd)
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newSimulateCmd())
	rootCmd.AddCommand(newSweepCmd())
	rootCmd.AddCommand(newSnapshotCmd())
	rootCmd.AddCommand(newDiffCmd())
	rootCmd.AddCommand(newSyncCmd())
	return rootCmd
}

func bindPersistentFlags(rootCmd *cobra.Command) {

	// Define flags
	rootCmd.PersistentFlags().StringVarP(&dslFilePath, "file", "f", "", "Path to the .dsl file (required)")
	rootCmd.PersistentFlags().StringVar(&projectPath, "project", "", "Path to a project directory or parametron.project.json")
	rootCmd.PersistentFlags().StringVarP(&outputDir, "out", "o", "./output", "Output directory for artifacts")
	rootCmd.PersistentFlags().StringVar(&modelHash, "model-hash", "", "Optional model hash for cache signature v2")
	rootCmd.PersistentFlags().BoolVarP(&debugMode, "debug", "d", false, "Enable debug logging")
	rootCmd.PersistentFlags().StringSliceVar(&paramOverrides, "set", []string{}, "Set/override a parameter value (e.g., --set length=1500)")
	rootCmd.PersistentFlags().StringSliceVar(&tableInputs, "table", []string{}, "Load a planner-visible table as logical-id=path")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Simulate execution and print resolved parameters")
	rootCmd.PersistentFlags().BoolVar(&printPlan, "print-plan", false, "Print the execution plan and exit")
	rootCmd.PersistentFlags().BoolVar(&jsonPlan, "json-plan", false, "Output execution plan as JSON")
	rootCmd.PersistentFlags().BoolVar(&printAST, "print-ast", false, "Print the parsed AST as JSON and exit")
}

func selectCommand(args []string) *cobra.Command {
	if shouldUseHarnessCommandTree(args) {
		return newHarnessRootCmd()
	}
	return newRootCmd()
}

func shouldUseHarnessCommandTree(args []string) bool {
	firstArg := firstNonFlagToken(args)
	switch firstArg {
	case "validate", "simulate", "sweep", "snapshot", "diff", "sync", "help":
		return true
	default:
		return false
	}
}

func firstNonFlagToken(args []string) string {
	valueFlags := map[string]struct{}{
		"--file":       {},
		"-f":           {},
		"--project":    {},
		"--out":        {},
		"-o":           {},
		"--model-hash": {},
		"--set":        {},
		"--table":      {},
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if _, ok := valueFlags[arg]; ok {
			i++
			continue
		}
		if strings.HasPrefix(arg, "--") {
			if strings.Contains(arg, "=") {
				continue
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return ""
}

func run(cmd *cobra.Command, args []string) error {
	configureCommandLogging(debugMode, jsonPlan || printAST)

	entryPath, err := resolveExecutionEntrypoint(dslFilePath, projectPath)
	if err != nil {
		return err
	}

	overridesMap, err := parseOverrideList(paramOverrides)
	if err != nil {
		return err
	}
	planned, err := loadPlannedRun(entryPath, overridesMap, args)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if printAST {
		return writeJSON(out, planned.AST)
	}
	if jsonPlan {
		return writeJSON(out, struct {
			Plan interface{} `json:"plan"`
			Hash string      `json:"hash"`
		}{
			Plan: planned.Plan,
			Hash: planned.PlanHash,
		})
	}
	if printPlan {
		prettyPrintPlan(out, planned.Plan)
		_, err := io.WriteString(out, "Plan Hash: "+planned.PlanHash+"\n")
		return err
	}
	if dryRun {
		prettyPrintPlan(out, planned.Plan)
		if _, err := io.WriteString(out, "Plan Hash: "+planned.PlanHash+"\n"); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Dry run mode: no execution performed.\n")
		return err
	}

	result, err := executePlanRun(executionOptions{
		Planned:   planned,
		OutputDir: outputDir,
		ModelHash: modelHash,
		UseCache:  true,
	})
	if err != nil {
		return err
	}
	if result.Cached {
		slog.Info("Plan execution skipped (cached)", "hash", planned.PlanHash)
		return nil
	}

	slog.Info("Parametron execution completed successfully")
	return nil
}

func prettyPrintPlan(w io.Writer, plan *planner.ExecutionPlan) {
	fmt.Fprintln(w, "Execution Plan:")
	for i, step := range plan.Steps {
		fmt.Fprintf(w, "Step %d: %s\n", i+1, step.Type)
		payloadJSON, err := json.MarshalIndent(step.Payload, "  ", "  ")
		if err != nil {
			fmt.Fprintf(w, "  Payload: %+v\n", step.Payload)
		} else {
			fmt.Fprintf(w, "  Payload:\n  %s\n", string(payloadJSON))
		}
		fmt.Fprintln(w)
	}
}

func markRunLayersDone(layerKeys map[cache.CacheLayer]string) error {
	for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
		key := layerKeys[layer]
		if err := cache.MarkDoneLayer(layer, key); err != nil {
			return err
		}
	}
	return nil
}
