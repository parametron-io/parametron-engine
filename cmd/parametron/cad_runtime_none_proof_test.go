package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/report"
)

// writeNativeOnlyProjectExecutionFixture mirrors writeProjectExecutionFixture
// (cli_test.go) but declares outputs=["none"] instead of a derived output,
// and drops the table dependency to keep the fixture minimal. It is a
// programmatically generated project input, not the committed cube
// rehearsal fixture, per the Stage 2 constraint against modifying
// testdata/projects/freecad/rehearsal/cube.
func writeNativeOnlyProjectExecutionFixture(t *testing.T) string {
	t.Helper()

	projectDir := t.TempDir()
	modelDir := filepath.Join(projectDir, "models")
	for _, dir := range []string{modelDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create fixture dir %s: %v", dir, err)
		}
	}

	if err := os.WriteFile(filepath.Join(modelDir, "box.FCStd"), []byte("model-a"), 0o644); err != nil {
		t.Fatalf("failed to write box model: %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "project.dsl"), []byte(withVersionHeader(`
const PRIMARY_MODEL = "box_model"

product Widget {
    adapter = "freecad"
    source_model = PRIMARY_MODEL
    outputs = ["none"]

    param label: string = "native-only"
}
`)), 0o644); err != nil {
		t.Fatalf("failed to write project DSL: %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "parametron.project.json"), []byte(`{
  "version": "1.0",
  "projectId": "project-native-only-run-fixture",
  "dsl": "./project.dsl",
  "resources": {
    "models": {
      "box_model": "./models/box.FCStd"
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("failed to write project map: %v", err)
	}

	return projectDir
}

type nativeOnlyRunResult struct {
	planHash    string
	jobID       string
	packageKey  string
	report      nativeOnlyReport
	invocations int
}

type nativeOnlyReport struct {
	Status    string `json:"status"`
	PlanHash  string `json:"planHash"`
	Artifacts []struct {
		Type     string `json:"type"`
		Filename string `json:"filename"`
	} `json:"artifacts"`
	Jobs []struct {
		JobID             string   `json:"jobId"`
		ExpectedArtifacts []string `json:"expectedArtifacts"`
		Steps             []struct {
			Type   planner.StepType `json:"type"`
			Status string           `json:"status"`
		} `json:"steps"`
	} `json:"jobs"`
}

// runControlledNativeOnly drives the real CLI through the strict controlled
// proof runtime (the same convention used by the Task 14 aligned run-root
// proofs in cad_runtime_proof_test.go) against outputs=["none"]. It returns
// the decoded report plus the plan hash/job identity, so both a single-run
// proof and a repeated-run determinism proof can reuse it. script must be
// resolved by the caller via strictProofRuntimePath, and projectDir
// must be a single fixture reused across repeated calls, both
// before any call in the same test changes the process working directory
// or varies the resolved absolute source-model path baked into the plan.
func runControlledNativeOnly(t *testing.T, script, projectDir string) nativeOnlyRunResult {
	t.Helper()

	resetGlobals()
	installStrictProofRuntime(t, script)
	logPath := os.Getenv("PARAMETRON_CAD_PROOF_INVOCATION_LOG")

	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	outDir := "out"
	if _, err := executeRootCommand(t, []string{
		"--project", projectDir,
		"--out", outDir,
	}); err != nil {
		t.Fatalf("expected the native-only aligned run to succeed: %v", err)
	}

	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	runRoot, err := filepath.Abs(planner.BuildRunRoot(outDir, planned.PlanHash))
	if err != nil {
		t.Fatal(err)
	}
	reportBytes, err := os.ReadFile(filepath.Join(runRoot, report.FileName))
	if err != nil {
		t.Fatalf("read %s: %v", report.FileName, err)
	}
	var decoded nativeOnlyReport
	if err := json.Unmarshal(reportBytes, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Jobs) != 1 {
		t.Fatalf("expected exactly one job report, got %d", len(decoded.Jobs))
	}

	invocations := readProofInvocations(t, logPath)

	return nativeOnlyRunResult{
		planHash:    decoded.PlanHash,
		jobID:       decoded.Jobs[0].JobID,
		packageKey:  planned.PlanHash + "|" + decoded.Jobs[0].JobID,
		report:      decoded,
		invocations: len(invocations),
	}
}

// TestCLI_NativeOnlyControlledRunProducesZeroArtifactsAndSucceeds is the
// Stage 2 controlled native-only proof required by section 19: it exercises
// project loading -> DSL outputs=["none"] -> planner -> handoff -> aligned
// controlled CADRuntime -> zero result artifacts -> verification success ->
// normal report/package completion, without touching the cube rehearsal
// fixture.
func TestCLI_NativeOnlyControlledRunProducesZeroArtifactsAndSucceeds(t *testing.T) {
	script := strictProofRuntimePath(t)
	resetGlobals()
	projectDir := writeNativeOnlyProjectExecutionFixture(t)
	result := runControlledNativeOnly(t, script, projectDir)

	if result.invocations != 1 {
		t.Fatalf("expected exactly one aligned RunCADRuntime invocation, got %d", result.invocations)
	}

	report := result.report
	if report.Status != "success" {
		t.Fatalf("expected overall report status success, got %q", report.Status)
	}
	job := report.Jobs[0]

	var sawRunCADRuntime bool
	for _, step := range job.Steps {
		if step.Type == planner.StepRunCADRuntime {
			sawRunCADRuntime = true
			if step.Status != "success" {
				t.Fatalf("expected RunCADRuntime step status success, got %q", step.Status)
			}
		}
	}
	if !sawRunCADRuntime {
		t.Fatal("expected the aligned RunCADRuntime step to have executed")
	}

	// prm.result.json remains an eligible execution output; zero derived
	// artifacts are added; no native CAD document is registered.
	var sawResultJSON bool
	for _, a := range report.Artifacts {
		if a.Filename == planner.FreeCADRuntimeResultFilename {
			sawResultJSON = true
			continue
		}
		if a.Filename == "Widget.csv" || a.Filename == planner.ExportManifestFilename {
			continue
		}
		t.Fatalf("expected no other registered artifacts for native-only run, found %+v", a)
	}
	if !sawResultJSON {
		t.Fatal("expected prm.result.json to remain a registered execution output")
	}
	for _, filename := range job.ExpectedArtifacts {
		if filename == "" {
			continue
		}
		if filepath.Ext(filename) == ".FCStd" {
			t.Fatalf("expected no native CAD document in ExpectedArtifacts, found %q", filename)
		}
	}
}

// TestCLI_NativeOnlyRepeatedRunIsPlanAndJobIdentityDeterministic proves
// section 20's repeated-run determinism requirement for the surfaces whose
// contract requires stability: two fresh equivalent native-only controlled
// runs produce a stable plan hash and stable job/package identity.
func TestCLI_NativeOnlyRepeatedRunIsPlanAndJobIdentityDeterministic(t *testing.T) {
	script := strictProofRuntimePath(t)
	resetGlobals()
	projectDir := writeNativeOnlyProjectExecutionFixture(t)

	first := runControlledNativeOnly(t, script, projectDir)
	second := runControlledNativeOnly(t, script, projectDir)

	if first.planHash == "" || first.planHash != second.planHash {
		t.Fatalf("plan hash identity differs across repeated native-only runs: %q vs %q", first.planHash, second.planHash)
	}
	if first.jobID == "" || first.jobID != second.jobID {
		t.Fatalf("job identity differs across repeated native-only runs: %q vs %q", first.jobID, second.jobID)
	}
	if first.packageKey != second.packageKey {
		t.Fatalf("package key differs across repeated native-only runs: %q vs %q", first.packageKey, second.packageKey)
	}
	if len(first.report.Artifacts) != len(second.report.Artifacts) {
		t.Fatalf("artifact inventory shape differs across repeated native-only runs: %d vs %d", len(first.report.Artifacts), len(second.report.Artifacts))
	}
}
