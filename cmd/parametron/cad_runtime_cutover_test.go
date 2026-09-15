package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/report"
	"parametron/internal/shared/config"
)

// ===================== Part I: normal CLI aligned path =====================

func TestCLI_NormalFreeCADProjectUsesAlignedRuntime(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")

	run := runRootProjectExecution(t, projectDir, outDir)

	cadKeys := runCADRuntimeProductKeys(run.planned.Plan)
	if len(cadKeys) != 1 || cadKeys[0] != "Widget" {
		t.Fatalf("expected planner to emit RunCADRuntime for Widget, got %v", cadKeys)
	}
	if run.report.Status != report.StatusSuccess {
		t.Fatalf("expected successful report status, got %q", run.report.Status)
	}
	if len(run.report.Jobs) != 1 {
		t.Fatalf("expected exactly one job report, got %d", len(run.report.Jobs))
	}
	job := run.report.Jobs[0]
	if len(job.Steps) == 0 || job.Steps[len(job.Steps)-1].Type != planner.StepRunCADRuntime {
		t.Fatalf("expected the final reported step to be RunCADRuntime, got %+v", job.Steps)
	}
	if job.Steps[len(job.Steps)-1].Status != report.StepStatusSuccess {
		t.Fatalf("expected the RunCADRuntime step to report success, got %q", job.Steps[len(job.Steps)-1].Status)
	}

	stepMatches, err := filepath.Glob(filepath.Join(run.result.RunRoot, "products", "Widget", "_working", "*", "outputs", "Widget.step"))
	if err != nil || len(stepMatches) != 1 {
		t.Fatalf("expected exactly one controlled-runtime STEP artifact, matches=%v err=%v", stepMatches, err)
	}
}

func TestCLI_AlignedRuntimeRegistersExpectedArtifacts(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	run := runRootProjectExecution(t, projectDir, outDir)

	byFilenameSuffix := func(suffix string) *report.ArtifactInfo {
		for i := range run.report.Artifacts {
			if strings.HasSuffix(run.report.Artifacts[i].Filename, suffix) {
				return &run.report.Artifacts[i]
			}
		}
		return nil
	}

	csv := byFilenameSuffix("Widget.csv")
	if csv == nil || csv.Type != "csv" {
		t.Fatalf("expected root CSV artifact registered, got %+v", run.report.Artifacts)
	}
	manifest := byFilenameSuffix(planner.ExportManifestFilename)
	if manifest == nil || manifest.Type != "json" {
		t.Fatalf("expected root export manifest artifact registered, got %+v", run.report.Artifacts)
	}
	result := byFilenameSuffix(planner.FreeCADRuntimeResultFilename)
	if result == nil || result.Type != "json" {
		t.Fatalf("expected aligned %s artifact registered, got %+v", planner.FreeCADRuntimeResultFilename, run.report.Artifacts)
	}
	step := byFilenameSuffix("Widget.step")
	if step == nil || step.Type != "step" {
		t.Fatalf("expected declared STEP output artifact registered, got %+v", run.report.Artifacts)
	}
}

func TestCLI_AlignedRuntimeExcludesRawEvidence(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	run := runRootProjectExecution(t, projectDir, outDir)

	forbidden := []string{
		"prm.observed.json",
		"native_manifest.json",
		"observation_request.json",
	}
	for _, a := range run.report.Artifacts {
		for _, name := range forbidden {
			if strings.HasSuffix(a.Filename, name) {
				t.Fatalf("did not expect raw evidence file %q to be registered as an artifact: %+v", name, a)
			}
		}
	}

	// The raw evidence must still exist on disk (Task 10 owns writing it) -
	// it is excluded from *registration*, not from the working copy.
	observedMatches, err := filepath.Glob(filepath.Join(run.result.RunRoot, "products", "Widget", "_working", "*", "outputs", "prm.observed.json"))
	if err != nil || len(observedMatches) != 1 {
		t.Fatalf("expected the raw observed evidence file to exist on disk (unregistered), matches=%v err=%v", observedMatches, err)
	}
}

func TestCLI_AlignedRuntimePreservesProjectTableAndCacheBehavior(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")

	first := runCLIExecutionForProject(t, projectDir, outDir)
	if first.result.Cached {
		t.Fatal("expected the first aligned project run to execute, not hit cache")
	}
	if first.metadata.ProjectInputs == nil {
		t.Fatal("expected project input capture to be preserved for the aligned path")
	}

	cached := runCLIExecutionForProject(t, projectDir, outDir)
	if !cached.result.Cached {
		t.Fatal("expected the second identical aligned project run to be a deterministic cache hit")
	}

	modelPath := filepath.Join(projectDir, "models", "box.FCStd")
	if err := os.WriteFile(modelPath, []byte("model-a-modified"), 0o644); err != nil {
		t.Fatalf("failed to modify source model: %v", err)
	}
	invalidated := runCLIExecutionForProject(t, projectDir, outDir)
	if invalidated.result.Cached {
		t.Fatal("expected a source-model change to invalidate the aligned cache")
	}
}

func TestCLI_AlignedRuntimeFailureProducesDeterministicReportAndRecords(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")

	run := runFailedCLIExecutionForProject(t, projectDir, outDir)
	if run.err == nil {
		t.Fatal("expected the controlled aligned runtime failure to propagate")
	}
	if !strings.Contains(run.err.Error(), "controlled_failure") {
		t.Fatalf("expected the runtime-native failure code to be visible, got: %v", run.err)
	}
	if run.result == nil || run.result.Report.Status != report.StatusFailed {
		t.Fatalf("expected a deterministic failed report, got %+v", run.result)
	}
	for _, a := range run.result.Report.Artifacts {
		if strings.HasSuffix(a.Filename, "result.json") || a.Type == "step" {
			t.Fatalf("did not expect any aligned runtime artifact to be accepted after failure: %+v", a)
		}
	}
	if _, err := os.Stat(filepath.Join(run.result.RunRoot, report.FileName)); err != nil {
		t.Fatalf("expected %s to exist deterministically after failure: %v", report.FileName, err)
	}
}

func TestCLI_AlignedRuntimeExecutableResolution(t *testing.T) {
	t.Run("configured executable", func(t *testing.T) {
		resetGlobals()
		chdirToTemp(t)
		setPathWithoutFreeCAD(t)
		projectDir := writeProjectExecutionFixture(t)
		installControlledAlignedRuntime(t, "success")
		run := runFailedOrSucceededProject(t, projectDir)
		if run.err != nil {
			t.Fatalf("expected success with a configured executable: %v", run.err)
		}
	})

	t.Run("default PATH executable", func(t *testing.T) {
		resetGlobals()
		chdirToTemp(t)
		setPathWithoutFreeCAD(t)
		projectDir := writeProjectExecutionFixture(t)

		// installControlledAlignedRuntime sets PARAMETRON_FREECAD_RUNTIME;
		// unset it (rather than setting it empty, which config.Load treats
		// as an explicitly configured-but-empty command) so resolution must
		// fall through to the default PATH executable.
		runtimePath := installControlledAlignedRuntime(t, "success")
		unsetEnvForTest(t, config.FreeCADRuntimeEnv)
		pathDir := filepath.Dir(runtimePath)
		defaultPath := filepath.Join(pathDir, "parametron-freecad")
		if runtimePath != defaultPath {
			if err := os.Symlink(runtimePath, defaultPath); err != nil {
				t.Fatalf("failed to install default-PATH runtime shim: %v", err)
			}
		}
		t.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		run := runFailedOrSucceededProject(t, projectDir)
		if run.err != nil {
			t.Fatalf("expected success resolving the default PATH executable: %v", run.err)
		}
	})

	t.Run("invalid configured executable", func(t *testing.T) {
		resetGlobals()
		chdirToTemp(t)
		setPathWithoutFreeCAD(t)
		projectDir := writeProjectExecutionFixture(t)
		t.Setenv(config.FreeCADRuntimeEnv, filepath.Join(t.TempDir(), "does-not-exist"))

		run := runFailedOrSucceededProject(t, projectDir)
		if run.err == nil {
			t.Fatal("expected a deterministic executable-resolution failure")
		}
	})

	t.Run("non-CAD project with no executable", func(t *testing.T) {
		resetGlobals()
		chdirToTemp(t)
		setPathWithoutFreeCAD(t)
		unsetEnvForTest(t, config.FreeCADRuntimeEnv)

		dslPath := writeNoAdapterOnlyProject(t)
		planned, err := loadPlannedRun(dslPath, map[string]string{}, nil)
		if err != nil {
			t.Fatalf("loadPlannedRun: %v", err)
		}
		if _, err := executePlanRun(executionOptions{Planned: planned, OutputDir: filepath.Join(t.TempDir(), "out"), UseCache: true}); err != nil {
			t.Fatalf("expected a non-CAD project to succeed without any configured executable: %v", err)
		}
	})

}

// --- Part I helpers ---

// runFailedOrSucceededProject runs the aligned project fixture and reports
// success/failure without failing the test on execution error, so table
// cases can assert either outcome.
func runFailedOrSucceededProject(t *testing.T, projectPath string) failedCLIProjectExecution {
	t.Helper()
	resetGlobals()

	planned, err := loadPlannedRun(projectPath, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun failed: %v", err)
	}
	result, execErr := executePlanRun(executionOptions{
		Planned:   planned,
		OutputDir: filepath.Join(t.TempDir(), "out"),
		UseCache:  true,
	})
	return failedCLIProjectExecution{result: result, planned: planned, err: execErr}
}

func writeNoAdapterOnlyProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dslPath := filepath.Join(dir, "project.dsl")
	// No adapter/source_model/outputs declaration at all: this product does
	// not require CAD execution and the planner never resolves a CAD-runtime
	// executable for it.
	if err := os.WriteFile(dslPath, []byte(withVersionHeader(`
product Summary {
    param label: string = "summary"
}
`)), 0o644); err != nil {
		t.Fatalf("failed to write no-adapter DSL: %v", err)
	}
	return dslPath
}

// unsetEnvForTest removes an environment variable for the duration of the
// test, restoring its prior value (or absence) on cleanup. Unlike
// t.Setenv(key, ""), this makes the variable genuinely unset so
// config.Load's os.LookupEnv-based detection falls through to defaults.
func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	original, hadOriginal := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("failed to unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if hadOriginal {
			os.Setenv(key, original)
		} else {
			os.Unsetenv(key)
		}
	})
}
