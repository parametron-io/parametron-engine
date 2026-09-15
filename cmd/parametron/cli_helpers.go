package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cache"
	"parametron/internal/engine/cad"
	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/metadata"
	"parametron/internal/engine/projectinput"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/report"
	"parametron/internal/engine/runtimecap"
	"parametron/internal/engine/scheduler"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
	"parametron/internal/engine/table"
	"parametron/internal/engine/tableloader"
	"parametron/internal/shared/config"
	"parametron/internal/shared/projectlock"
	"parametron/internal/shared/projectmap"
)

const harnessSchemaVersion = "1.0"

type plannedRun struct {
	AST             *dsl.AST
	Plan            *planner.ExecutionPlan
	PlanHash        string
	DSLHash         string
	DSLFile         string
	ProfileName     string
	ProfileSettings map[string]any
	TableInputs     []metadata.TableInputMetadata
	ProjectInputs   *projectinput.CapturedResources
}

type executionOptions struct {
	Planned         *plannedRun
	OutputDir       string
	ExplicitRunRoot string
	ModelHash       string
	UseCache        bool
}

type executionResult struct {
	RunRoot        string
	Execution      scheduler.ExecutionResult
	Report         report.Report
	Cached         bool
	LayerKeys      map[cache.CacheLayer]string
	ExecutionError error
}

type resolvedProjectInput = projectinput.Resolved

func configureCommandLogging(debug bool, quiet bool) {
	if quiet {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		return
	}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
	if debug {
		slog.Debug("Debug mode enabled")
	}
}

func requireDSLFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("required flag(s) \"file\" not set")
	}
	return nil
}

func requireExclusiveEntrypoint(filePath string, projectPath string) error {
	fileSet := strings.TrimSpace(filePath) != ""
	projectSet := strings.TrimSpace(projectPath) != ""

	switch {
	case fileSet && projectSet:
		return fmt.Errorf("flags --file and --project are mutually exclusive")
	case !fileSet && !projectSet:
		return fmt.Errorf("required: exactly one of --file or --project must be set")
	default:
		return nil
	}
}

func resolveExecutionEntrypoint(filePath string, projectPath string) (string, error) {
	if err := requireExclusiveEntrypoint(filePath, projectPath); err != nil {
		return "", err
	}
	if strings.TrimSpace(projectPath) == "" {
		return filePath, nil
	}
	if err := requireProjectFlagPath(projectPath); err != nil {
		return "", err
	}
	return projectPath, nil
}

func requireProjectFlagPath(path string) error {
	projectInput, err := resolveProjectInput(path)
	if err != nil {
		return err
	}
	if projectInput == nil {
		return fmt.Errorf("--project must reference a project directory or %s path", projectmap.FileName)
	}
	return nil
}

func resolveProjectCommandEntrypoint(filePath string, projectPath string) (string, error) {
	entryPath, err := resolveExecutionEntrypoint(filePath, projectPath)
	if err != nil {
		return "", err
	}

	projectInput, err := resolveProjectInput(entryPath)
	if err != nil {
		return "", err
	}
	if projectInput == nil {
		return "", fmt.Errorf("sync requires a project directory or %s path; standalone DSL files are not supported", projectmap.FileName)
	}

	return entryPath, nil
}

func loadConfiguration() error {
	slog.Debug("Loading configuration")
	_, err := config.Load()
	return err
}

func parseOverrideList(raw []string) (map[string]string, error) {
	overrides := make(map[string]string, len(raw))
	for _, override := range raw {
		parts := strings.SplitN(override, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Errorf("invalid parameter override format: '%s'. Expected 'key=value'", override)
		}
		key := strings.TrimSpace(parts[0])
		overrides[key] = strings.TrimSpace(parts[1])
	}
	return overrides, nil
}

func parseTableInputList(raw []string) (map[string]string, error) {
	tables := make(map[string]string, len(raw))
	for _, value := range raw {
		logicalID, path, ok := strings.Cut(value, "=")
		if !ok || logicalID == "" || path == "" {
			return nil, fmt.Errorf("invalid --table value %q. Expected 'logical-id=path'", value)
		}
		if _, exists := tables[logicalID]; exists {
			return nil, fmt.Errorf("duplicate --table for %q", logicalID)
		}
		tables[logicalID] = path
	}
	return tables, nil
}

func loadPlannerTables(raw []string) (map[string]*table.Table, error) {
	inputs, err := parseTableInputList(raw)
	if err != nil {
		return nil, err
	}
	return tableloader.LoadFiles(inputs)
}

func loadPlannedRun(dslPath string, overrides map[string]string, args []string) (*plannedRun, error) {
	if err := loadConfiguration(); err != nil {
		slog.Error("Failed to load configuration", "error", err)
		return nil, err
	}

	projectInput, err := resolveProjectInput(dslPath)
	if err != nil {
		slog.Error("Failed to resolve project input", "path", dslPath, "error", err)
		return nil, err
	}
	if projectInput != nil {
		dslPath = projectInput.DSLPath
	}

	slog.Info("Parsing DSL file", "path", dslPath)
	ast, err := dsl.Parse(dslPath)
	if err != nil {
		slog.Error("Failed to parse DSL", "path", dslPath, "error", err)
		if shouldSuggestQuotedFilePath(err, dslPath) {
			return nil, fmt.Errorf("%w\nIf the file path contains spaces, wrap it in quotes:\nparametron --file %q", err, strings.Join(append([]string{dslPath}, args...), " "))
		}
		return nil, err
	}

	loadedTables, err := loadPlannerTables(tableInputs)
	if err != nil {
		slog.Error("Failed to load tables", "error", err)
		return nil, err
	}
	if projectInput != nil {
		projectTables, err := projectinput.LoadProjectTables(projectInput)
		if err != nil {
			slog.Error("Failed to load project tables", "project", projectInput.ProjectFile, "error", err)
			return nil, err
		}
		loadedTables, err = projectinput.MergeTables(projectTables, loadedTables)
		if err != nil {
			slog.Error("Failed to merge project tables", "project", projectInput.ProjectFile, "error", err)
			return nil, err
		}
	}

	projectResources, err := captureProjectResources(projectInput)
	if err != nil {
		slog.Error("Failed to capture project resources", "path", dslPath, "error", err)
		return nil, err
	}

	if len(loadedTables) == 0 {
		slog.Info("Validating AST")
		if err := dsl.Validate(ast); err != nil {
			slog.Error("AST validation failed", "error", err)
			return nil, err
		}
	} else {
		slog.Info("Validating AST with loaded tables", "count", len(loadedTables))
		if err := dsl.ValidateWithTables(ast, loadedTables); err != nil {
			slog.Error("AST validation failed", "error", err)
			return nil, err
		}
	}

	captureContract, err := loadProjectCaptureContract(projectInput)
	if err != nil {
		slog.Error("Failed to load project capture contract", "error", err)
		return nil, err
	}
	var semanticModel *semantic.Model
	var semanticMapContract *semanticmap.SemanticMap
	if captureContract != nil {
		semanticMapContract, err = loadProjectSemanticMap(projectInput)
		if err != nil {
			slog.Error("Failed to load project semantic map", "error", err)
			return nil, err
		}
		semanticModel, err = buildCaptureBackedSemanticModel(captureContract, ast)
		if err != nil {
			slog.Error("Capture-backed semantic planning preparation failed", "error", err)
			return nil, err
		}
	}

	if projectInput != nil {
		if err := applyProjectModelMappings(ast, projectInput); err != nil {
			slog.Error("Failed to resolve project model mappings", "project", projectInput.ProjectFile, "error", err)
			return nil, err
		}
	}

	tableMetadata, err := buildTableInputMetadata(loadedTables)
	if err != nil {
		slog.Error("Failed to fingerprint loaded tables", "error", err)
		return nil, err
	}

	var plan *planner.ExecutionPlan
	if semanticModel != nil {
		slog.Info("Generating execution plan through semantic model boundary", "tables", len(loadedTables))
		plan, err = planner.CreatePlanWithTablesAndSemanticModel(ast, overrides, loadedTables, semanticModel, semanticMapContract)
		if err != nil {
			slog.Error("Failed to create execution plan", "error", err)
			return nil, err
		}
	} else if len(loadedTables) == 0 {
		slog.Info("Generating execution plan")
		plan, err = planner.CreatePlan(ast, overrides)
		if err != nil {
			slog.Error("Failed to create execution plan", "error", err)
			return nil, err
		}
	} else {
		slog.Info("Generating execution plan with loaded tables", "count", len(loadedTables))
		plan, err = planner.CreatePlanWithTables(ast, overrides, loadedTables)
		if err != nil {
			slog.Error("Failed to create execution plan", "error", err)
			return nil, err
		}
	}

	planHash, err := planner.ComputePlanHash(plan, ast)
	if err != nil {
		return nil, err
	}
	dslHash := ""
	if projectResources != nil {
		dslHash = projectResources.DSL.Signature
	} else {
		dslHash, err = computeFileSHA256(dslPath)
		if err != nil {
			return nil, err
		}
	}

	profileName := ""
	profileSettings := map[string]any{}
	if ast.ActiveProfileName != nil {
		profileName = *ast.ActiveProfileName
		resolved, err := planner.ResolveActiveProfileSettings(ast)
		if err != nil {
			slog.Warn("Failed to resolve profile settings for metadata/report", "error", err)
		} else {
			profileSettings = resolved
		}
	}

	return &plannedRun{
		AST:             ast,
		Plan:            plan,
		PlanHash:        planHash,
		DSLHash:         dslHash,
		DSLFile:         dslPath,
		ProfileName:     profileName,
		ProfileSettings: profileSettings,
		TableInputs:     tableMetadata,
		ProjectInputs:   projectResources,
	}, nil
}

func executePlanRun(opts executionOptions) (*executionResult, error) {
	if opts.Planned == nil {
		return nil, fmt.Errorf("planned run is required")
	}

	runRoot := opts.ExplicitRunRoot
	if strings.TrimSpace(runRoot) == "" {
		baseOutputDir, err := planner.ResolveBaseOutputDir(opts.Planned.AST, opts.OutputDir)
		if err != nil {
			return nil, err
		}
		runRoot = planner.BuildRunRoot(baseOutputDir, opts.Planned.PlanHash)
	}
	absoluteRunRoot, err := filepath.Abs(runRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute run root: %w", err)
	}
	runRoot = filepath.Clean(absoluteRunRoot)

	var layerKeys map[cache.CacheLayer]string
	if opts.UseCache {
		keys, err := computeRunLayerKeys(opts.Planned.Plan, opts.Planned.AST, strings.TrimSpace(opts.ModelHash), opts.Planned.TableInputs, opts.Planned.ProjectInputs, &cache.SignatureV2Strategy{})
		if err != nil {
			slog.Warn("Failed to build cache context", "error", err)
		} else {
			layerKeys = keys
			exists, err := allRunLayersCached(keys)
			if err != nil {
				slog.Warn("Failed to check cache", "error", err)
			} else if exists {
				return &executionResult{
					RunRoot:   runRoot,
					Cached:    true,
					LayerKeys: layerKeys,
				}, nil
			}
		}
	}

	store := artifact.NewFileSystemStore(runRoot)
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	resolver, err := runtimecap.NewExecutableResolver(cfg.CADRuntimeCommands())
	if err != nil {
		return nil, err
	}
	capability := runtimecap.NewExternalCapability()
	ctx := context.Background()
	slog.Info("Executing plan", "run_root", runRoot)
	s := scheduler.NewWithProductDirs(runRoot, func(productDir string) *executor.Executor {
		adp := freecad.NewFreeCADAdapter(productDir)
		aligned, wrapErr := cadruntime.NewFreeCADRuntimeAdapter(adp, capability)
		if wrapErr != nil {
			panic(wrapErr)
		}
		return executor.NewWithArtifactsAndCADRuntimeConsumption(aligned, store, resolver, productDir)
	})

	startedAt := time.Now().UTC()
	schedulerResult, executionErr := s.ExecuteWithResult(ctx, opts.Planned.Plan)
	endedAt := time.Now().UTC()

	artifacts, err := store.List()
	if err != nil {
		slog.Warn("Failed to list artifacts for metadata/report", "error", err)
		artifacts = nil
	}

	runReport, reportErr := report.Build(report.BuildInput{
		PlanHash:           opts.Planned.PlanHash,
		DSLHash:            opts.Planned.DSLHash,
		ProfileName:        opts.Planned.ProfileName,
		ProfileSettings:    opts.Planned.ProfileSettings,
		Execution:          schedulerResult,
		Artifacts:          artifacts,
		StartedAt:          startedAt,
		EndedAt:            endedAt,
		WorkerCount:        scheduler.EffectiveWorkerCount(opts.Planned.Plan),
		StepTimeoutSeconds: int(executor.DefaultStepTimeout / time.Second),
		MaxRetries:         executor.DefaultMaxRetries,
		Err:                executionErr,
	})
	if reportErr != nil {
		slog.Warn("Failed to build execution report", "error", reportErr)
	} else if err := report.Write(runRoot, runReport); err != nil {
		slog.Warn("Failed to write "+report.FileName, "error", err)
	}

	var runMetadata *metadata.Metadata
	if executionErr == nil {
		if err := store.WriteManifest(); err != nil {
			slog.Warn("Failed to write artifact manifest", "error", err)
		}

		artifacts, err = store.List()
		if err != nil {
			slog.Warn("Failed to list artifacts for metadata", "error", err)
			artifacts = nil
		}

		builtMetadata := metadata.Build(metadata.BuildInput{
			PlanHash:          opts.Planned.PlanHash,
			DSLHash:           opts.Planned.DSLHash,
			ProfileName:       opts.Planned.ProfileName,
			ProfileSettings:   opts.Planned.ProfileSettings,
			Tables:            opts.Planned.TableInputs,
			ProjectInputs:     opts.Planned.ProjectInputs,
			Artifacts:         artifacts,
			StartedAt:         startedAt,
			EndedAt:           endedAt,
			WorkerCount:       scheduler.EffectiveWorkerCount(opts.Planned.Plan),
			MaxRetries:        executor.DefaultMaxRetries,
			StepTimeoutSecs:   int(executor.DefaultStepTimeout / time.Second),
			ParametronVersion: parametronVersion,
			GoVersion:         runtime.Version(),
		})
		runMetadata = &builtMetadata
		if err := metadata.Write(runRoot, builtMetadata); err != nil {
			slog.Warn("Failed to write "+metadata.FileName, "error", err)
		}
	}

	var packageEmitErr error
	if reportErr == nil {
		emitInput := recordemit.RunEmitInput{
			RunRoot:            runRoot,
			PlanHash:           opts.Planned.PlanHash,
			Report:             runReport,
			Metadata:           runMetadata,
			ReferenceTraversal: referenceTraversalRunEvidence(schedulerResult, executionErr == nil),
		}
		emitInput.CADRuntime, packageEmitErr = cadRuntimeRunEvidence(schedulerResult, executionErr == nil)
		if packageEmitErr == nil {
			packageEmitErr = recordemit.EmitRunPackage(emitInput)
		}
	}

	result := &executionResult{
		RunRoot:        runRoot,
		Execution:      schedulerResult,
		Report:         runReport,
		LayerKeys:      layerKeys,
		ExecutionError: executionErr,
	}

	if executionErr != nil {
		if packageEmitErr != nil {
			slog.Error("Failed to emit record package", "error", packageEmitErr)
		}
		return result, executionErr
	}
	if packageEmitErr != nil {
		return result, fmt.Errorf("emit record package: %w", packageEmitErr)
	}

	if opts.UseCache {
		if layerKeys != nil {
			if err := markRunLayersDone(layerKeys); err != nil {
				slog.Warn("Failed to update cache", "error", err)
			}
		} else {
			slog.Warn("Failed to update cache", "error", "cache context unavailable")
		}
	}

	return result, nil
}

func cadRuntimeRunEvidence(execution scheduler.ExecutionResult, overallExecutionSucceeded bool) (*recordemit.CADRuntimeRunEvidence, error) {
	if !overallExecutionSucceeded {
		return nil, nil
	}
	var candidate *executor.CADRuntimeOutcome
	for _, jobExecution := range execution.Jobs {
		outcome := jobExecution.CADRuntimeOutcome
		if jobExecution.Err != nil || outcome == nil || outcome.Verification != artifact.VerificationOutcomePassed {
			continue
		}
		// Singular package destinations cannot represent multiple attempts.
		if candidate != nil {
			return nil, nil
		}
		candidate = outcome
	}
	if candidate == nil {
		return nil, nil
	}
	evidence := &recordemit.CADRuntimeRunEvidence{}
	for _, source := range []struct {
		path    string
		content *[]byte
	}{
		{candidate.ResultPath, &evidence.Result},
		{candidate.ObservationRequestPath, &evidence.Verification},
		{candidate.ObservedPath, &evidence.Observed},
	} {
		content, err := cadruntime.ReadOptionalFreeCADRuntimeEvidence(candidate.WorkingCopyDir, source.path)
		if err != nil {
			return nil, fmt.Errorf("read CAD runtime evidence %q: %w", source.path, err)
		}
		*source.content = content
	}
	return evidence, nil
}

func referenceTraversalRunEvidence(execution scheduler.ExecutionResult, overallExecutionSucceeded bool) *recordemit.ReferenceTraversalRunEvidence {
	if !overallExecutionSucceeded {
		return nil
	}
	var candidate *recordemit.ReferenceTraversalRunEvidence
	for _, jobExecution := range execution.Jobs {
		outcome := jobExecution.CADRuntimeOutcome
		if jobExecution.Err != nil || outcome == nil || outcome.Verification != artifact.VerificationOutcomePassed || len(outcome.ReferenceTraversalJSON) == 0 ||
			!isCanonicalExecutionLink(outcome.JobID) || !isCanonicalExecutionLink(outcome.ProductKey) || !isCanonicalExecutionLink(outcome.StepID) {
			continue
		}
		if candidate != nil {
			return nil
		}
		candidate = &recordemit.ReferenceTraversalRunEvidence{
			Content: append([]byte(nil), outcome.ReferenceTraversalJSON...),
			JobID:   outcome.JobID, ProductKey: outcome.ProductKey, StepRef: outcome.StepID,
		}
	}
	return candidate
}

func isCanonicalExecutionLink(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsRune(value, '\x00')
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".json-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func loadJSONOverridesFile(path string) (map[string]any, map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var values map[string]any
	if err := decoder.Decode(&values); err != nil {
		return nil, nil, err
	}
	if values == nil {
		return nil, nil, fmt.Errorf("inputs JSON must be an object")
	}
	if decoder.More() {
		return nil, nil, fmt.Errorf("inputs JSON must contain a single object")
	}

	overrides := make(map[string]string, len(values))
	for key, value := range values {
		override, err := jsonScalarToOverrideString(value)
		if err != nil {
			return nil, nil, fmt.Errorf("input %q: %w", key, err)
		}
		overrides[key] = override
	}
	return values, overrides, nil
}

func loadJSONCaseArray(path string) ([]map[string]any, []map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var cases []map[string]any
	if err := decoder.Decode(&cases); err != nil {
		return nil, nil, err
	}
	if cases == nil {
		return nil, nil, fmt.Errorf("inputs JSON must be an array of objects")
	}

	overrides := make([]map[string]string, 0, len(cases))
	for index, oneCase := range cases {
		if oneCase == nil {
			return nil, nil, fmt.Errorf("case %d must be an object", index)
		}
		caseOverrides := make(map[string]string, len(oneCase))
		for key, value := range oneCase {
			override, err := jsonScalarToOverrideString(value)
			if err != nil {
				return nil, nil, fmt.Errorf("case %d input %q: %w", index, key, err)
			}
			caseOverrides[key] = override
		}
		overrides = append(overrides, caseOverrides)
	}
	return cases, overrides, nil
}

func jsonScalarToOverrideString(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case json.Number:
		return v.String(), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case nil:
		return "", fmt.Errorf("null is not supported")
	default:
		return "", fmt.Errorf("must be a scalar JSON value")
	}
}

func resolveProjectInput(inputPath string) (*resolvedProjectInput, error) {
	return projectinput.ResolveProject(inputPath)
}

func applyProjectModelMappings(ast *dsl.AST, projectInput *resolvedProjectInput) error {
	return projectinput.ApplyModelMappings(ast, projectInput)
}

func captureProjectResources(projectInput *resolvedProjectInput) (*projectinput.CapturedResources, error) {
	return projectinput.CaptureResources(projectInput)
}

func loadProjectCaptureContract(projectInput *resolvedProjectInput) (*cad.CADContract, error) {
	return projectinput.LoadCaptureContract(projectInput)
}

func loadProjectSemanticMap(projectInput *resolvedProjectInput) (*semanticmap.SemanticMap, error) {
	return projectinput.LoadSemanticMap(projectInput)
}

func buildCaptureBackedSemanticModel(contract *cad.CADContract, ast *dsl.AST) (*semantic.Model, error) {
	model, err := semantic.BuildFromCapture(contract)
	if err != nil {
		return nil, err
	}
	return semantic.InjectDSLIntent(model, ast)
}

func syncProjectLock(inputPath string) (string, error) {
	projectInput, err := resolveProjectInput(inputPath)
	if err != nil {
		return "", err
	}
	if projectInput == nil {
		return "", fmt.Errorf("sync requires a project directory or %s path; standalone DSL files are not supported", projectmap.FileName)
	}

	project, err := projectmap.Load(projectInput.ProjectFile)
	if err != nil {
		return "", err
	}

	captured, err := captureProjectResources(projectInput)
	if err != nil {
		return "", err
	}

	lock, err := projectlock.Build(project, projectInput.ProjectFile, captured)
	if err != nil {
		return "", err
	}

	lockPath := filepath.Join(projectInput.ProjectRoot, projectlock.FileName)
	if err := projectlock.Write(lockPath, lock); err != nil {
		return "", err
	}

	return lockPath, nil
}

func shouldSuggestQuotedFilePath(err error, filePath string) bool {
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		return false
	}
	if !os.IsNotExist(pathErr.Err) {
		return false
	}

	return strings.TrimSpace(filePath) != ""
}

func computeFileSHA256(path string) (string, error) {
	captured, err := projectinput.CaptureResources(&projectinput.Resolved{DSLPath: path})
	if err != nil {
		return "", err
	}
	return captured.DSL.Signature, nil
}

func buildTableInputMetadata(tables map[string]*table.Table) ([]metadata.TableInputMetadata, error) {
	if len(tables) == 0 {
		return nil, nil
	}

	logicalIDs := make([]string, 0, len(tables))
	for logicalID := range tables {
		logicalIDs = append(logicalIDs, logicalID)
	}
	slices.Sort(logicalIDs)

	out := make([]metadata.TableInputMetadata, 0, len(logicalIDs))
	for _, logicalID := range logicalIDs {
		loadedTable := tables[logicalID]
		fingerprint, err := loadedTable.Fingerprint()
		if err != nil {
			return nil, fmt.Errorf("fingerprint table %q: %w", logicalID, err)
		}
		out = append(out, metadata.TableInputMetadata{
			LogicalID:   logicalID,
			Name:        loadedTable.Name,
			Fingerprint: fingerprint,
		})
	}
	return out, nil
}

func normalizeTableInputMetadata(tableInputs []metadata.TableInputMetadata) []metadata.TableInputMetadata {
	if len(tableInputs) == 0 {
		return nil
	}

	out := make([]metadata.TableInputMetadata, len(tableInputs))
	copy(out, tableInputs)
	slices.SortFunc(out, func(a, b metadata.TableInputMetadata) int {
		if a.LogicalID != b.LogicalID {
			return strings.Compare(a.LogicalID, b.LogicalID)
		}
		if a.Name != b.Name {
			return strings.Compare(a.Name, b.Name)
		}
		return strings.Compare(a.Fingerprint, b.Fingerprint)
	})
	return out
}

func computeRunLayerKeys(plan *planner.ExecutionPlan, ast *dsl.AST, trimmedModelHash string, tableInputs []metadata.TableInputMetadata, projectInputs *projectinput.CapturedResources, strategy cache.CacheKeyStrategy) (map[cache.CacheLayer]string, error) {
	ctx, err := planner.ComputeCacheKeyContext(plan, ast)
	if err != nil {
		return nil, err
	}
	ctx.ModelHash = trimmedModelHash
	if ctx.ModelHash == "" {
		projectModelHash, err := projectinput.CombinedModelSignature(projectInputs)
		if err != nil {
			return nil, err
		}
		ctx.ModelHash = projectModelHash
	}
	if normalizedTables := normalizeTableInputMetadata(tableInputs); len(normalizedTables) > 0 {
		ctx.Inputs["tables"] = normalizedTables
	}

	if strategy == nil {
		strategy = &cache.SignatureV2Strategy{}
	}

	return map[cache.CacheLayer]string{
		cache.CacheGeometry: strategy.ComputeKey(cache.CacheGeometry, ctx),
		cache.CacheArtifact: strategy.ComputeKey(cache.CacheArtifact, ctx),
		cache.CacheMetadata: strategy.ComputeKey(cache.CacheMetadata, ctx),
	}, nil
}

func allRunLayersCached(layerKeys map[cache.CacheLayer]string) (bool, error) {
	for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
		key := layerKeys[layer]
		exists, err := cache.ExistsLayer(layer, key)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
	}
	return true, nil
}

func normalizeJSONBytes(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	normalized, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(normalized, '\n'), nil
}

func firstDiffLineCol(a, b []byte) (int, int, int) {
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	line := 1
	col := 1
	for i := 0; i < limit; i++ {
		if a[i] != b[i] {
			return i, line, col
		}
		if a[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	if len(a) != len(b) {
		return limit, line, col
	}
	return -1, 0, 0
}

func sortedFileList(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	return files, nil
}

func ensureDirectoryEmpty(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(path, 0755)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("output directory must be empty: %s", path)
	}
	return nil
}

func findProduct(ast *dsl.AST, name string) *dsl.ProductNode {
	for _, product := range ast.Products {
		if product.Name == name {
			return product
		}
	}
	return nil
}
