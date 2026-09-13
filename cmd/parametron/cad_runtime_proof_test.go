package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"parametron/internal/authoring/planner"
)

// Task 14 permanent regression coverage for the CLI's absolute-run-root
// correction: executePlanRun (cli_helpers.go) always resolves the run root
// through filepath.Abs before any aligned working-copy directory is derived
// from it. Unlike the Task 13 controlled runtime used elsewhere in this
// package (which tolerates relative paths by calling os.path.abspath
// itself), the strict controlled proof runtime
// (scripts/cad_runtime_proof_runtime.py) hard-fails execution with
// "all aligned paths must be absolute" if any --working-copy, --manifest,
// --result, --output-dir, --observation-request, or
// --reference-traversal-request argument is not already
// absolute. Driving the real CLI through that strict runtime from a relative
// working directory and relative --out argument is therefore a genuine,
// distinct proof that the CLI itself supplies canonical absolute paths.

type task14ProofInvocation struct {
	AttemptID       string `json:"attemptID"`
	WorkingCopy     string `json:"workingCopy"`
	Manifest        string `json:"manifest"`
	Result          string `json:"result"`
	OutputDirectory string `json:"outputDirectory"`
}

// strictProofRuntimePath resolves the absolute path of the strict controlled
// proof runtime script relative to this package's source directory. It must
// be called before any test changes the process working directory (e.g. via
// chdirToTemp), since it depends on os.Getwd() reflecting the package dir.
func strictProofRuntimePath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path, err := filepath.Abs(filepath.Join(wd, "..", "..", "scripts", "cad_runtime_proof_runtime.py"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("strict controlled proof runtime not found at %s: %v", path, err)
	}
	return path
}

func installStrictProofRuntime(t *testing.T, script string) {
	t.Helper()
	stateDir := t.TempDir()
	t.Setenv("PARAMETRON_FREECAD_RUNTIME", script)
	t.Setenv("PARAMETRON_CAD_PROOF_MODE", "success")
	t.Setenv("PARAMETRON_CAD_PROOF_STATE_DIR", stateDir)
	t.Setenv("PARAMETRON_CAD_PROOF_INVOCATION_LOG", filepath.Join(stateDir, "invocations.jsonl"))
}

func readProofInvocations(t *testing.T, logPath string) []task14ProofInvocation {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	var got []task14ProofInvocation
	for _, line := range splitNonEmptyLines(string(data)) {
		var entry task14ProofInvocation
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode invocation record %q: %v", line, err)
		}
		got = append(got, entry)
	}
	return got
}

func splitNonEmptyLines(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			if line := text[start:i]; line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

func TestCLI_AlignedRunRootIsCanonicalAbsolute(t *testing.T) {
	resetGlobals()
	script := strictProofRuntimePath(t)
	installStrictProofRuntime(t, script)
	logPath := os.Getenv("PARAMETRON_CAD_PROOF_INVOCATION_LOG")

	chdirToTemp(t)
	setPathWithoutFreeCAD(t)
	projectDir := writeProjectExecutionFixture(t)

	// A relative --out argument, from a relative working directory: if the
	// CLI passed this straight through, the strict runtime would reject the
	// run outright.
	outDir := "relative-out"
	if _, err := executeRootCommand(t, []string{
		"--project", projectDir,
		"--out", outDir,
	}); err != nil {
		t.Fatalf("expected the aligned run to succeed through the strict absolute-path proof runtime: %v", err)
	}

	invocations := readProofInvocations(t, logPath)
	if len(invocations) != 1 {
		t.Fatalf("expected exactly one strict-runtime invocation, got %d", len(invocations))
	}
	inv := invocations[0]
	if inv.AttemptID == "" {
		t.Fatal("expected a non-empty attempt identity")
	}
	for name, path := range map[string]string{
		"workingCopy": inv.WorkingCopy, "manifest": inv.Manifest,
		"result": inv.Result, "outputDirectory": inv.OutputDirectory,
	} {
		if !filepath.IsAbs(path) {
			t.Fatalf("%s=%q is not an absolute path", name, path)
		}
		if filepath.Clean(path) != path {
			t.Fatalf("%s=%q is not canonical (filepath.Clean changes it)", name, path)
		}
	}
}

func TestCLI_EquivalentRelativeAndAbsoluteOutputRootsPreserveLogicalIdentity(t *testing.T) {
	script := strictProofRuntimePath(t)

	// The same project fixture (an absolute, cwd-independent path) is reused
	// for both runs: only the --out argument's shape (relative vs. absolute)
	// and the active working directory vary, isolating that as the sole
	// variable under test. A fresh fixture per run would also vary the
	// resolved absolute source-model path baked into the plan, which
	// legitimately changes the plan hash for reasons unrelated to this test.
	resetGlobals()
	projectDir := writeProjectExecutionFixture(t)

	runOnce := func(useAbsoluteOut bool) (planHash, jobID string) {
		resetGlobals()
		installStrictProofRuntime(t, script)
		chdirToTemp(t)
		setPathWithoutFreeCAD(t)

		outDir := "out"
		if useAbsoluteOut {
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			outDir = filepath.Join(cwd, "out")
		}
		if _, err := executeRootCommand(t, []string{
			"--project", projectDir,
			"--out", outDir,
		}); err != nil {
			t.Fatalf("expected aligned run to succeed (absolute=%v): %v", useAbsoluteOut, err)
		}

		planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		// Mirrors executePlanRun's own runRoot derivation exactly (base
		// output dir joined with the plan hash, then made absolute), so this
		// reconstruction is valid regardless of whether outDir started
		// relative or absolute.
		runRoot, err := filepath.Abs(planner.BuildRunRoot(outDir, planned.PlanHash))
		if err != nil {
			t.Fatal(err)
		}
		reportBytes, err := os.ReadFile(filepath.Join(runRoot, "report.json"))
		if err != nil {
			t.Fatalf("read report.json (absolute=%v): %v", useAbsoluteOut, err)
		}
		var decoded struct {
			PlanHash string `json:"planHash"`
			Jobs     []struct {
				JobID string `json:"jobId"`
			} `json:"jobs"`
		}
		if err := json.Unmarshal(reportBytes, &decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded.Jobs) != 1 {
			t.Fatalf("expected exactly one job report, got %d", len(decoded.Jobs))
		}
		return decoded.PlanHash, decoded.Jobs[0].JobID
	}

	relPlanHash, relJobID := runOnce(false)
	absPlanHash, absJobID := runOnce(true)

	if relPlanHash == "" || relPlanHash != absPlanHash {
		t.Fatalf("plan hash identity differs: relative=%q absolute=%q", relPlanHash, absPlanHash)
	}
	if relJobID == "" || relJobID != absJobID {
		t.Fatalf("job ID identity differs: relative=%q absolute=%q", relJobID, absJobID)
	}
}
