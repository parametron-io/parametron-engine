package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/cache"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/metadata"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/report"
	"parametron/internal/engine/scheduler"
)

func withVersionHeader(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "dsl v1.0") || strings.HasPrefix(trimmed, "dsl 1.0") {
		return content
	}
	return "dsl v1.0\n" + content
}

func TestCLI(t *testing.T) {
	// Helper to create temp DSL file
	createDSL := func(t *testing.T, content string) string {
		t.Helper()
		f, err := os.CreateTemp("", "test_*.dsl")
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		defer f.Close()
		if _, err := f.Write([]byte(withVersionHeader(content))); err != nil {
			t.Fatalf("Failed to write to temp file: %v", err)
		}
		return f.Name()
	}

	validDSL := `
product Test {
    param width: number = 100
}
`

	tests := []struct {
		name           string
		args           []string
		dslContent     string
		expectError    bool
		validateOutput func(t *testing.T, stdout string)
		validateError  func(t *testing.T, err error)
	}{
		{
			name:       "Print Plan",
			dslContent: validDSL,
			args:       []string{"--print-plan"},
			validateOutput: func(t *testing.T, stdout string) {
				if !strings.Contains(stdout, "Execution Plan:") {
					t.Error("Expected 'Execution Plan:' in output")
				}
				if !strings.Contains(stdout, "Plan Hash:") {
					t.Error("Expected 'Plan Hash:' in output")
				}
			},
		},
		{
			name:       "JSON Plan",
			dslContent: validDSL,
			args:       []string{"--json-plan"},
			validateOutput: func(t *testing.T, stdout string) {
				var out struct {
					Plan interface{} `json:"plan"`
					Hash string      `json:"hash"`
				}
				if err := json.Unmarshal([]byte(stdout), &out); err != nil {
					t.Errorf("Failed to parse JSON output: %v\nOutput: %s", err, stdout)
				}
				if out.Hash == "" {
					t.Error("Expected hash in JSON output")
				}
			},
		},
		{
			name:       "Print AST",
			dslContent: validDSL,
			args:       []string{"--print-ast"},
			validateOutput: func(t *testing.T, stdout string) {
				var out struct {
					DSLVersion string        `json:"DSLVersion"`
					Products   []interface{} `json:"Products"`
				}
				if err := json.Unmarshal([]byte(stdout), &out); err != nil {
					t.Errorf("Failed to parse JSON output: %v\nOutput: %s", err, stdout)
				}
				if out.DSLVersion != "1.0" {
					t.Errorf("Expected DSLVersion=1.0, got %q", out.DSLVersion)
				}
				if len(out.Products) != 1 {
					t.Errorf("Expected 1 product, got %d", len(out.Products))
				}
			},
		},
		{
			name: "Print AST With Comments",
			dslContent: `
// global line
product Test {
    param width: number = 100/*inline*/
    param half: number = width / 2 // trailing
}
`,
			args: []string{"--print-ast"},
			validateOutput: func(t *testing.T, stdout string) {
				if !strings.Contains(stdout, "\"Name\": \"Test\"") {
					t.Error("Expected product name in AST output")
				}
			},
		},
		{
			name: "Print Plan With Comments",
			dslContent: `
profile Dev { output_dir = "out" }
use profile Dev // select profile
product Test {
    param width: number = 100
    param half: number = width /*block*/ / 2
}
`,
			args: []string{"--print-plan"},
			validateOutput: func(t *testing.T, stdout string) {
				if !strings.Contains(stdout, "Execution Plan:") {
					t.Error("Expected 'Execution Plan:' in output")
				}
			},
		},
		{
			name: "Unclosed Block Comment Is Lex Error",
			dslContent: `
product Test {
    /* comment
`,
			args:        []string{"--print-ast"},
			expectError: true,
			validateError: func(t *testing.T, err error) {
				if !strings.Contains(err.Error(), "lex error: unclosed block comment") {
					t.Fatalf("expected lex error, got: %v", err)
				}
			},
		},
		{
			name:        "Invalid Override",
			dslContent:  validDSL,
			args:        []string{"--set", "invalid_param=10"},
			expectError: true,
		},
		{
			name:        "Missing File",
			args:        []string{}, // No file arg
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global flags to defaults to ensure determinism
			resetGlobals()

			cmd := newRootCmd()
			var bufOut, bufErr bytes.Buffer
			cmd.SetOut(&bufOut)
			cmd.SetErr(&bufErr)

			args := tt.args
			if tt.dslContent != "" {
				path := createDSL(t, tt.dslContent)
				defer os.Remove(path)
				// Append file arg
				args = append(args, "--file", path)
			}

			cmd.SetArgs(args)

			err := cmd.Execute()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, but got nil")
				} else if tt.validateError != nil {
					tt.validateError(t, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tt.validateOutput != nil {
				tt.validateOutput(t, bufOut.String())
			}
		})
	}
}

func resetGlobals() {
	dslFilePath = ""
	projectPath = ""
	outputDir = "./output"
	modelHash = ""
	paramOverrides = []string{}
	tableInputs = []string{}
	debugMode = false
	dryRun = false
	printPlan = false
	jsonPlan = false
	printAST = false
}

func executeRootCommand(t *testing.T, args []string) (string, error) {
	t.Helper()
	resetGlobals()

	cmd := newRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return stdout.String(), err
}

func executeRootCommandWithVisibleError(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	resetGlobals()

	cmd := newRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()
	visibleError := stderr.String()
	if err != nil {
		if visibleError == "" {
			visibleError = err.Error()
		} else {
			visibleError += err.Error()
		}
	}

	return stdout.String(), visibleError, err
}

func runRootProjectExecution(t *testing.T, entryPath string, outDir string, extraArgs ...string) cliProjectExecution {
	t.Helper()
	installControlledAlignedRuntime(t, "success")

	args := []string{
		"--project", entryPath,
		"--out", outDir,
	}
	args = append(args, extraArgs...)

	stdout, err := executeRootCommand(t, args)
	if err != nil {
		t.Fatalf("root command failed: %v", err)
	}

	planned, err := loadPlannedRun(entryPath, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun failed: %v", err)
	}

	runRoot := planner.BuildRunRoot(outDir, planned.PlanHash)
	metadataBytes, err := os.ReadFile(filepath.Join(runRoot, metadata.FileName))
	if err != nil {
		t.Fatalf("failed to read %s: %v", metadata.FileName, err)
	}

	var runMetadata metadata.Metadata
	if err := json.Unmarshal(metadataBytes, &runMetadata); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", metadata.FileName, err)
	}

	reportBytes, err := os.ReadFile(filepath.Join(runRoot, report.FileName))
	if err != nil {
		t.Fatalf("failed to read %s: %v", report.FileName, err)
	}

	var runReport report.Report
	if err := json.Unmarshal(reportBytes, &runReport); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", report.FileName, err)
	}

	layerKeys, err := computeRunLayerKeys(planned.Plan, planned.AST, "", planned.TableInputs, planned.ProjectInputs, &cache.SignatureV2Strategy{})
	if err != nil {
		t.Fatalf("computeRunLayerKeys failed: %v", err)
	}

	return cliProjectExecution{
		result: &executionResult{
			RunRoot:   runRoot,
			Cached:    strings.Contains(stdout, "Plan execution skipped (cached)"),
			LayerKeys: layerKeys,
		},
		planned:       planned,
		metadata:      runMetadata,
		metadataBytes: metadataBytes,
		report:        runReport,
		reportBytes:   reportBytes,
		stdout:        stdout,
	}
}

func allLayersCached(t *testing.T, layerKeys map[cache.CacheLayer]string) bool {
	t.Helper()

	for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
		path := filepath.Join(".cache", string(layer), layerKeys[layer]+".done")
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return false
			}
			t.Fatalf("failed to stat cache marker %s: %v", path, err)
		}
	}

	return true
}

func TestCLI_ExecuteWithoutAdapter_SkipsCADRunnerAndProducesCoreArtifacts(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)

	dslFile, err := os.CreateTemp("", "no_adapter_execute_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp dsl file: %v", err)
	}
	defer os.Remove(dslFile.Name())
	dslContent := withVersionHeader(`
product Widget {
    param width: number = 120
}
`)
	if _, err := dslFile.Write([]byte(dslContent)); err != nil {
		t.Fatalf("failed to write temp dsl file: %v", err)
	}
	if err := dslFile.Close(); err != nil {
		t.Fatalf("failed to close temp dsl file: %v", err)
	}

	outDir := t.TempDir()
	cmd := newRootCmd()
	var bufOut, bufErr bytes.Buffer
	cmd.SetOut(&bufOut)
	cmd.SetErr(&bufErr)
	cmd.SetArgs([]string{"--file", dslFile.Name(), "--out", outDir, "--model-hash", outDir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("failed to read output dir: %v", err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("expected exactly one run directory in %q, got %d entries", outDir, len(entries))
	}

	runRoot := filepath.Join(outDir, entries[0].Name())
	productDir := filepath.Join(runRoot, "products", "Widget")

	if _, err := os.Stat(filepath.Join(runRoot, metadata.FileName)); err != nil {
		t.Fatalf("expected %s in run root: %v", metadata.FileName, err)
	}
	if _, err := os.Stat(filepath.Join(runRoot, report.FileName)); err != nil {
		t.Fatalf("expected %s in run root: %v", report.FileName, err)
	}
	if _, err := os.Stat(filepath.Join(productDir, "Widget.csv")); err != nil {
		t.Fatalf("expected Widget.csv artifact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(productDir, planner.ExportManifestFilename)); err != nil {
		t.Fatalf("expected %s artifact: %v", planner.ExportManifestFilename, err)
	}
	if _, err := os.Stat(filepath.Join(productDir, "Widget.step")); !os.IsNotExist(err) {
		t.Fatalf("expected no STEP output without freecad adapter, got err=%v", err)
	}

	reportBytes, err := os.ReadFile(filepath.Join(runRoot, report.FileName))
	if err != nil {
		t.Fatalf("failed to read %s: %v", report.FileName, err)
	}
	if !strings.Contains(string(reportBytes), `"schemaVersion": "1.0"`) {
		t.Fatalf("expected schemaVersion in %s, got:\n%s", report.FileName, string(reportBytes))
	}
	if !strings.Contains(string(reportBytes), `"status": "success"`) {
		t.Fatalf("expected success status in %s, got:\n%s", report.FileName, string(reportBytes))
	}
}

func TestCLI_TableBackedRun_WritesTableMetadata(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)

	tablePath := writeTableFixture(t, `{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8}
  ]
}`)
	dslPath := writeDSLFixture(t, `
product Widget {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
}
`)

	outDir := t.TempDir()
	cmd := newRootCmd()
	var bufOut, bufErr bytes.Buffer
	cmd.SetOut(&bufOut)
	cmd.SetErr(&bufErr)
	cmd.SetArgs([]string{"--file", dslPath, "--out", outDir, "--table", "fasteners=" + tablePath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("failed to read output dir: %v", err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("expected exactly one run directory in %q, got %d entries", outDir, len(entries))
	}

	data, err := os.ReadFile(filepath.Join(outDir, entries[0].Name(), metadata.FileName))
	if err != nil {
		t.Fatalf("failed to read %s: %v", metadata.FileName, err)
	}

	var got metadata.Metadata
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", metadata.FileName, err)
	}
	if len(got.Tables) != 1 {
		t.Fatalf("expected 1 table entry, got %d", len(got.Tables))
	}
	if got.Tables[0].LogicalID != "fasteners" || got.Tables[0].Name != "fastener_catalog" || got.Tables[0].Fingerprint == "" {
		t.Fatalf("unexpected table metadata entry: %+v", got.Tables[0])
	}
}

func TestCLI_TableFlagOrderingDoesNotAffectPlan(t *testing.T) {
	fastenersPath := writeTableFixture(t, `{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8}
  ]
}`)
	labelsPath := writeTableFixture(t, `{
  "schemaVersion": "1.0",
  "name": "label_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "label", "type": "string", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "label": "M8 bolt"}
  ]
}`)
	dslPath := writeDSLFixture(t, `
product Widget {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
    param label: string = table_cell("labels", sku, "label")
}
`)

	first, err := executeRootCommand(t, []string{
		"--file", dslPath,
		"--json-plan",
		"--table", "fasteners=" + fastenersPath,
		"--table", "labels=" + labelsPath,
	})
	if err != nil {
		t.Fatalf("first command returned error: %v", err)
	}

	second, err := executeRootCommand(t, []string{
		"--file", dslPath,
		"--json-plan",
		"--table", "labels=" + labelsPath,
		"--table", "fasteners=" + fastenersPath,
	})
	if err != nil {
		t.Fatalf("second command returned error: %v", err)
	}

	if first != second {
		t.Fatalf("expected deterministic JSON plan output across flag orderings\nfirst: %s\nsecond: %s", first, second)
	}
}

func TestCLI_RuntimeFailure_WritesReportJSON(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)

	// Create DSL that requires freecad adapter
	dslFile, err := os.CreateTemp(t.TempDir(), "failure_test_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp dsl file: %v", err)
	}
	defer os.Remove(dslFile.Name())

	// We use source_model to satisfy the requirement for adapter="freecad".
	// Supply source bytes so execution reaches the controlled external runtime.
	if err := os.WriteFile(filepath.Join(filepath.Dir(dslFile.Name()), "dummy.FCStd"), []byte("source"), 0o644); err != nil {
		t.Fatalf("write source model: %v", err)
	}
	dslContent := withVersionHeader(`
profile Fail {
}
use profile Fail
product P {
    adapter = "freecad"
    source_model = "dummy.FCStd"

    param label: string = "P"
}
`)
	if _, err := dslFile.Write([]byte(dslContent)); err != nil {
		t.Fatalf("failed to write dsl: %v", err)
	}
	if err := dslFile.Close(); err != nil {
		t.Fatalf("failed to close dsl file: %v", err)
	}

	outDir := t.TempDir()
	installControlledAlignedRuntime(t, "failure")

	cmd := newRootCmd()
	var bufOut, bufErr bytes.Buffer
	cmd.SetOut(&bufOut)
	cmd.SetErr(&bufErr)
	// Passing a non-empty model-hash ensures we aren't hitting legacy cache paths.
	cmd.SetArgs([]string{
		"--file", dslFile.Name(),
		"--out", outDir,
		"--model-hash", "hash",
	})

	// Execution should fail because the external runtime reports failure.
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected CLI execution to fail due to runner error")
	}

	// Verify report.FileName exists
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("failed to read output dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 run directory, got %d", len(entries))
	}
	runRoot := filepath.Join(outDir, entries[0].Name())
	reportPath := filepath.Join(runRoot, report.FileName)

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", report.FileName, err)
	}

	var report struct {
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("failed to unmarshal report: %v", err)
	}

	if report.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", report.Status)
	}
	if report.Error == nil {
		t.Fatal("expected error object in report, got nil")
	}
	if !strings.Contains(report.Error.Message, "intentional controlled aligned runtime failure") {
		t.Errorf("expected controlled external runtime failure, got %q", report.Error.Message)
	}
}

func TestCLI_RootProjectFlag_AcceptsDirectoryAndProjectFileEntrypoints(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	projectFile := filepath.Join(projectDir, "parametron.project.json")

	fromDir := runRootProjectExecution(t, projectDir, filepath.Join(t.TempDir(), "out-dir"))
	if err := os.RemoveAll(".cache"); err != nil {
		t.Fatalf("failed to clear cache markers between equivalent project runs: %v", err)
	}
	fromFile := runRootProjectExecution(t, projectFile, filepath.Join(t.TempDir(), "out-file"))

	if fromDir.metadata.ProjectInputs == nil {
		t.Fatal("expected project inputs in metadata for project directory entrypoint")
	}
	if fromFile.metadata.ProjectInputs == nil {
		t.Fatal("expected project inputs in metadata for project file entrypoint")
	}
	if fromDir.planned.PlanHash != fromFile.planned.PlanHash {
		t.Fatalf("expected identical plan hashes across project entrypoints, got %q and %q", fromDir.planned.PlanHash, fromFile.planned.PlanHash)
	}

	dirJSON, err := json.Marshal(fromDir.metadata.ProjectInputs)
	if err != nil {
		t.Fatalf("marshal directory project inputs failed: %v", err)
	}
	fileJSON, err := json.Marshal(fromFile.metadata.ProjectInputs)
	if err != nil {
		t.Fatalf("marshal project file inputs failed: %v", err)
	}
	if !bytes.Equal(dirJSON, fileJSON) {
		t.Fatalf("expected identical captured project inputs across entrypoints\ndir:\n%s\nfile:\n%s", string(dirJSON), string(fileJSON))
	}
}

func TestCLI_RootProjectFlag_RejectsMissingBothFileAndProject(t *testing.T) {
	_, err := executeRootCommand(t, []string{"--print-plan"})
	if err == nil {
		t.Fatal("expected missing execution entrypoint to fail")
	}
	if err.Error() != "required: exactly one of --file or --project must be set" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCLI_RootProjectFlag_RejectsFileAndProjectTogether(t *testing.T) {
	projectDir := writeProjectExecutionFixture(t)
	dslPath := writeDSLFixture(t, `
product Widget {
    param width: number = 100
}
`)

	_, err := executeRootCommand(t, []string{
		"--file", dslPath,
		"--project", projectDir,
		"--print-plan",
	})
	if err == nil {
		t.Fatal("expected mutually exclusive entrypoints to fail")
	}
	if err.Error() != "flags --file and --project are mutually exclusive" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCLI_RootProjectFlag_RejectsStandaloneDSLPath(t *testing.T) {
	dslPath := writeDSLFixture(t, `
product Widget {
    param width: number = 100
}
`)

	_, err := executeRootCommand(t, []string{
		"--project", dslPath,
		"--print-plan",
	})
	if err == nil {
		t.Fatal("expected standalone DSL passed to --project to fail")
	}
	if err.Error() != "--project must reference a project directory or parametron.project.json path" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCLI_RootProjectFlag_PreservesProjectCaptureAndModelCacheBehavior(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")

	first := runRootProjectExecution(t, projectDir, outDir)
	if first.metadata.ProjectInputs == nil {
		t.Fatal("expected project inputs in metadata")
	}
	if len(first.metadata.ProjectInputs.Models) != 2 {
		t.Fatalf("expected 2 captured model resources, got %d", len(first.metadata.ProjectInputs.Models))
	}
	if len(first.metadata.ProjectInputs.Tables) != 2 {
		t.Fatalf("expected 2 captured table resources, got %d", len(first.metadata.ProjectInputs.Tables))
	}

	modelPath := filepath.Join(projectDir, "models", "box.FCStd")
	if err := os.WriteFile(modelPath, []byte("model-a-updated"), 0o644); err != nil {
		t.Fatalf("failed to update model fixture: %v", err)
	}

	second := runRootProjectExecution(t, projectDir, outDir)
	if bytes.Equal(first.metadataBytes, second.metadataBytes) {
		t.Fatalf("expected metadata to change after model update\nbefore:\n%s\nafter:\n%s", string(first.metadataBytes), string(second.metadataBytes))
	}
	if first.result.LayerKeys[cache.CacheGeometry] == second.result.LayerKeys[cache.CacheGeometry] {
		t.Fatal("expected cache keys to change after project model capture changes")
	}
}

func TestCLI_RootProjectFlag_PreservesProjectTableBehavior(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")

	run := runRootProjectExecution(t, projectDir, outDir)
	if run.metadata.ProjectInputs == nil || len(run.metadata.ProjectInputs.Tables) != 2 {
		t.Fatalf("expected auto-loaded project tables in metadata, got %#v", run.metadata.ProjectInputs)
	}

	tablePath := writeTableFixture(t, `{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8}
  ]
}`)

	_, err := executeRootCommand(t, []string{
		"--project", projectDir,
		"--json-plan",
		"--table", "fasteners=" + tablePath,
	})
	if err == nil {
		t.Fatal("expected duplicate logical table IDs across project and --table to fail")
	}
	if !strings.Contains(err.Error(), `duplicate table logical ID "fasteners" across project mapping and --table`) {
		t.Fatalf("unexpected duplicate table error: %v", err)
	}
}

func TestCLI_RootFileFlag_RemainsSupported(t *testing.T) {
	dslPath := writeDSLFixture(t, `
product Widget {
    param width: number = 100
}
`)

	stdout, err := executeRootCommand(t, []string{
		"--file", dslPath,
		"--json-plan",
	})
	if err != nil {
		t.Fatalf("expected standalone --file execution to succeed: %v", err)
	}
	if !strings.Contains(stdout, `"hash"`) {
		t.Fatalf("expected JSON plan output, got %q", stdout)
	}
}

func TestCLI_CommentExamples(t *testing.T) {
	resetGlobals()
	smokePath := filepath.Join("..", "..", "testdata", "dsl", "smoke", "smoke_comments.dsl")

	t.Run("smoke comments print ast", func(t *testing.T) {
		cmd := newRootCmd()
		var bufOut, bufErr bytes.Buffer
		cmd.SetOut(&bufOut)
		cmd.SetErr(&bufErr)
		cmd.SetArgs([]string{"--file", smokePath, "--print-ast"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(bufOut.String(), "\"Name\": \"Commented\"") {
			t.Fatalf("expected Commented product in AST output")
		}
	})

	resetGlobals()
	t.Run("smoke comments print plan", func(t *testing.T) {
		cmd := newRootCmd()
		var bufOut, bufErr bytes.Buffer
		cmd.SetOut(&bufOut)
		cmd.SetErr(&bufErr)
		cmd.SetArgs([]string{"--file", smokePath, "--print-plan"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out := bufOut.String()
		if !strings.Contains(out, "Execution Plan:") || !strings.Contains(out, "Plan Hash:") {
			t.Fatalf("expected execution plan output, got: %s", out)
		}
	})
}

func TestCLI_UnclosedBlockCommentExample(t *testing.T) {
	resetGlobals()
	breakPath := filepath.Join("..", "..", "testdata", "dsl", "break", "break_unclosed_block_comment.dsl")

	cmd := newRootCmd()
	var bufOut, bufErr bytes.Buffer
	cmd.SetOut(&bufOut)
	cmd.SetErr(&bufErr)
	cmd.SetArgs([]string{"--file", breakPath, "--print-ast"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "lex error: unclosed block comment") {
		t.Fatalf("expected lex error for unclosed block comment, got: %v", err)
	}
}

func TestCLI_BooleanEqualitySmokeExample(t *testing.T) {
	resetGlobals()
	smokePath := filepath.Join("..", "..", "testdata", "dsl", "smoke", "smoke_boolean_equality.dsl")

	cmd := newRootCmd()
	var bufOut, bufErr bytes.Buffer
	cmd.SetOut(&bufOut)
	cmd.SetErr(&bufErr)
	cmd.SetArgs([]string{"--file", smokePath, "--print-plan"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := bufOut.String()
	if !strings.Contains(out, "Execution Plan:") || !strings.Contains(out, "BoolComparison.csv") {
		t.Fatalf("expected execution plan for smoke boolean equality example, got: %s", out)
	}
}

func TestCLI_BooleanEqualityTypeMismatchBreakExample(t *testing.T) {
	resetGlobals()
	breakPath := filepath.Join("..", "..", "testdata", "dsl", "break", "break_equality_type_mismatch.dsl")

	cmd := newRootCmd()
	var bufOut, bufErr bytes.Buffer
	cmd.SetOut(&bufOut)
	cmd.SetErr(&bufErr)
	cmd.SetArgs([]string{"--file", breakPath, "--print-plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "right side of comparison '=='") {
		t.Fatalf("expected equality type mismatch validation error, got: %v", err)
	}
}

func TestCLI_StringConcatenationSmokeExample(t *testing.T) {
	resetGlobals()
	smokePath := filepath.Join("..", "..", "testdata", "dsl", "smoke", "smoke_string_concat.dsl")

	cmd := newRootCmd()
	var bufOut, bufErr bytes.Buffer
	cmd.SetOut(&bufOut)
	cmd.SetErr(&bufErr)
	cmd.SetArgs([]string{"--file", smokePath, "--print-ast"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := bufOut.String()
	if !strings.Contains(out, "\"Name\": \"StringConcat\"") || !strings.Contains(out, "\"Operator\": \"+\"") {
		t.Fatalf("expected string concatenation expression in AST output, got: %s", out)
	}
}

func TestCLI_StringConcatenationTypeMismatchBreakExample(t *testing.T) {
	resetGlobals()
	breakPath := filepath.Join("..", "..", "testdata", "dsl", "break", "break_string_concat_type_mismatch.dsl")

	cmd := newRootCmd()
	var bufOut, bufErr bytes.Buffer
	cmd.SetOut(&bufOut)
	cmd.SetErr(&bufErr)
	cmd.SetArgs([]string{"--file", breakPath, "--print-plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "right side of operator '+'") {
		t.Fatalf("expected operator '+' type mismatch validation error, got: %v", err)
	}
}

func TestCLI_StringInterpolationSmokeExample(t *testing.T) {
	resetGlobals()
	smokePath := filepath.Join("..", "..", "testdata", "dsl", "smoke", "smoke_string_interpolation.dsl")

	cmd := newRootCmd()
	var bufOut, bufErr bytes.Buffer
	cmd.SetOut(&bufOut)
	cmd.SetErr(&bufErr)
	cmd.SetArgs([]string{"--file", smokePath, "--print-plan"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := bufOut.String()
	if !strings.Contains(out, "Interpolation.csv") || !strings.Contains(out, "ENG-Ada-Lovelace") {
		t.Fatalf("expected interpolated string value in plan output, got: %s", out)
	}
}

func TestCLI_StringInterpolationTypeMismatchBreakExample(t *testing.T) {
	resetGlobals()
	breakPath := filepath.Join("..", "..", "testdata", "dsl", "break", "break_string_interpolation_type_mismatch.dsl")

	cmd := newRootCmd()
	var bufOut, bufErr bytes.Buffer
	cmd.SetOut(&bufOut)
	cmd.SetErr(&bufErr)
	cmd.SetArgs([]string{"--file", breakPath, "--print-plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "interpolation binding 'length' has type 'number', but expected 'string'") {
		t.Fatalf("expected interpolation type mismatch validation error, got: %v", err)
	}
}

func TestCLI_SuggestsQuotedFilePathWhenSplitBySpaces(t *testing.T) {
	resetGlobals()

	cmd := newRootCmd()
	var bufOut, bufErr bytes.Buffer
	cmd.SetOut(&bufOut)
	cmd.SetErr(&bufErr)
	cmd.SetArgs([]string{"--file", "examples/sample", "copy.dsl", "--print-ast"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "failed to read DSL file") {
		t.Fatalf("expected file read error, got: %v", err)
	}
	if !strings.Contains(msg, "If the file path contains spaces, wrap it in quotes:") {
		t.Fatalf("expected quoted-path suggestion, got: %v", err)
	}
	if !strings.Contains(msg, `parametron --file "examples/sample copy.dsl"`) {
		t.Fatalf("expected quoted command example, got: %v", err)
	}
}

func TestCLIModelHashEmpty_ReusesSignatureV2LayerKeys(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)

	ast, plan := parsePlanForCacheTest(t, `
product Test {
    param width: number = 100
}
`)

	planHash, err := planner.ComputePlanHash(plan, ast)
	if err != nil {
		t.Fatalf("ComputePlanHash failed: %v", err)
	}

	keys, err := computeRunLayerKeys(plan, ast, "", nil, nil, &cache.SignatureV2Strategy{})
	if err != nil {
		t.Fatalf("computeRunLayerKeys failed: %v", err)
	}

	for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
		if keys[layer] == planHash {
			t.Fatalf("expected Signature V2 key to differ from plan hash for layer %s", layer)
		}
	}

	hit, err := allRunLayersCached(keys)
	if err != nil {
		t.Fatalf("allRunLayersCached failed: %v", err)
	}
	if hit {
		t.Fatal("expected cache miss before marking new keys done")
	}
	if err := markRunLayersDone(keys); err != nil {
		t.Fatalf("markRunLayersDone failed: %v", err)
	}
	repeatedKeys, err := computeRunLayerKeys(plan, ast, "", nil, nil, &cache.SignatureV2Strategy{})
	if err != nil {
		t.Fatalf("computeRunLayerKeys failed: %v", err)
	}
	for layer, key := range keys {
		if repeatedKeys[layer] != key {
			t.Fatalf("expected deterministic key for layer %s", layer)
		}
	}
	hit, err = allRunLayersCached(repeatedKeys)
	if err != nil {
		t.Fatalf("allRunLayersCached failed: %v", err)
	}
	if !hit {
		t.Fatal("expected full run cache hit with repeated Signature V2 keys")
	}
}

func TestCLIModelHashChange_InvalidatesCache(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)

	ast, plan := parsePlanForCacheTest(t, `
product Test {
    param width: number = 100
}
`)

	keysA, err := computeRunLayerKeys(plan, ast, "A", nil, nil, &cache.SignatureV2Strategy{})
	if err != nil {
		t.Fatalf("computeRunLayerKeys(A) failed: %v", err)
	}
	if err := markRunLayersDone(keysA); err != nil {
		t.Fatalf("markRunLayersDone(A) failed: %v", err)
	}

	keysB, err := computeRunLayerKeys(plan, ast, "B", nil, nil, &cache.SignatureV2Strategy{})
	if err != nil {
		t.Fatalf("computeRunLayerKeys(B) failed: %v", err)
	}

	if keysA[cache.CacheGeometry] == keysB[cache.CacheGeometry] {
		t.Fatal("expected different keys for different model hashes")
	}

	hit, err := allRunLayersCached(keysB)
	if err != nil {
		t.Fatalf("allRunLayersCached failed: %v", err)
	}
	if hit {
		t.Fatal("expected cache miss after model hash change")
	}
}

func TestCLITableFingerprints_EmptyModelHash_UsesHashedLayerKeys(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)

	ast, plan := parsePlanForCacheTest(t, `
product Test {
    param width: number = 100
}
`)

	tableInputs := []metadata.TableInputMetadata{
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "fp-1"},
	}

	keys, err := computeRunLayerKeys(plan, ast, "", tableInputs, nil, &cache.SignatureV2Strategy{})
	if err != nil {
		t.Fatalf("computeRunLayerKeys failed: %v", err)
	}

	planHash, err := planner.ComputePlanHash(plan, ast)
	if err != nil {
		t.Fatalf("ComputePlanHash failed: %v", err)
	}
	for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
		if keys[layer] == planHash {
			t.Fatalf("expected non-legacy hashed key for layer %s when table fingerprints are present", layer)
		}
	}
}

func TestCLITableFingerprints_ChangeInvalidatesCache(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)

	ast, plan := parsePlanForCacheTest(t, `
product Test {
    param width: number = 100
}
`)

	keysA, err := computeRunLayerKeys(plan, ast, "", []metadata.TableInputMetadata{
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "fp-a"},
	}, nil, &cache.SignatureV2Strategy{})
	if err != nil {
		t.Fatalf("computeRunLayerKeys(A) failed: %v", err)
	}
	if err := markRunLayersDone(keysA); err != nil {
		t.Fatalf("markRunLayersDone(A) failed: %v", err)
	}

	keysB, err := computeRunLayerKeys(plan, ast, "", []metadata.TableInputMetadata{
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "fp-b"},
	}, nil, &cache.SignatureV2Strategy{})
	if err != nil {
		t.Fatalf("computeRunLayerKeys(B) failed: %v", err)
	}

	if keysA[cache.CacheGeometry] == keysB[cache.CacheGeometry] {
		t.Fatal("expected different keys when table fingerprint changes")
	}

	hit, err := allRunLayersCached(keysB)
	if err != nil {
		t.Fatalf("allRunLayersCached failed: %v", err)
	}
	if hit {
		t.Fatal("expected cache miss after table fingerprint change")
	}
}

func TestCLITableFingerprints_InsertionOrderDoesNotAffectLayerKeys(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)

	ast, plan := parsePlanForCacheTest(t, `
product Test {
    param width: number = 100
}
`)

	keysA, err := computeRunLayerKeys(plan, ast, "", []metadata.TableInputMetadata{
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "fp-a"},
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "fp-b"},
	}, nil, &cache.SignatureV2Strategy{})
	if err != nil {
		t.Fatalf("computeRunLayerKeys(A) failed: %v", err)
	}
	keysB, err := computeRunLayerKeys(plan, ast, "", []metadata.TableInputMetadata{
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "fp-b"},
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "fp-a"},
	}, nil, &cache.SignatureV2Strategy{})
	if err != nil {
		t.Fatalf("computeRunLayerKeys(B) failed: %v", err)
	}

	for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
		if keysA[layer] != keysB[layer] {
			t.Fatalf("expected identical keys for layer %s across table input orderings: %q vs %q", layer, keysA[layer], keysB[layer])
		}
	}
}

func TestCLIRun_TableInputs_DeterministicMetadataAndCache(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)

	fastenersPath := writeTableFixture(t, `{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8}
  ]
}`)
	labelsPath := writeTableFixture(t, `{
  "schemaVersion": "1.0",
  "name": "label_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "label", "type": "string", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "label": "M8 bolt"}
  ]
}`)
	dslPath := writeDSLFixture(t, `
product Widget {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
    param label: string = table_cell("labels", sku, "label")
}
`)

	outDir := filepath.Join(t.TempDir(), "out")

	first := runCLIExecutionWithTables(t, dslPath, outDir, []string{
		"labels=" + labelsPath,
		"fasteners=" + fastenersPath,
	})
	if first.result.Cached {
		t.Fatal("expected first run to execute, but it was cached")
	}
	assertLayerMarkersExist(t, first.result.LayerKeys)

	second := runCLIExecutionWithTables(t, dslPath, outDir, []string{
		"labels=" + labelsPath,
		"fasteners=" + fastenersPath,
	})
	if !second.result.Cached {
		t.Fatal("expected second identical run to hit cache")
	}
	if !bytes.Equal(first.metadataBytes, second.metadataBytes) {
		t.Fatalf("expected identical metadata bytes across identical runs\nfirst:\n%s\nsecond:\n%s", string(first.metadataBytes), string(second.metadataBytes))
	}
	assertLayerKeysEqual(t, first.result.LayerKeys, second.result.LayerKeys)

	reordered := runCLIExecutionWithTables(t, dslPath, outDir, []string{
		"fasteners=" + fastenersPath,
		"labels=" + labelsPath,
	})
	if !reordered.result.Cached {
		t.Fatal("expected reordered table flags to hit cache")
	}
	if !bytes.Equal(first.metadataBytes, reordered.metadataBytes) {
		t.Fatalf("expected identical metadata bytes across table flag orderings\nfirst:\n%s\nreordered:\n%s", string(first.metadataBytes), string(reordered.metadataBytes))
	}
	assertLayerKeysEqual(t, first.result.LayerKeys, reordered.result.LayerKeys)

	if err := os.WriteFile(labelsPath, []byte(`{
  "schemaVersion": "1.0",
  "name": "label_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "label", "type": "string", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "label": "M8 bolt updated"}
  ]
}`), 0o644); err != nil {
		t.Fatalf("failed to update table fixture: %v", err)
	}

	modified := runCLIExecutionWithTables(t, dslPath, outDir, []string{
		"labels=" + labelsPath,
		"fasteners=" + fastenersPath,
	})
	if modified.result.Cached {
		t.Fatal("expected cache miss after table change")
	}
	if bytes.Equal(first.metadataBytes, modified.metadataBytes) {
		t.Fatalf("expected metadata to change after table fingerprint change\nbefore:\n%s\nafter:\n%s", string(first.metadataBytes), string(modified.metadataBytes))
	}
	if modified.result.LayerKeys[cache.CacheGeometry] == first.result.LayerKeys[cache.CacheGeometry] {
		t.Fatal("expected layer keys to change after table fingerprint change")
	}
	assertLayerMarkersExist(t, modified.result.LayerKeys)
}

func TestCLIProjectRun_CapturesProjectInputsAndInvalidatesOnModelChange(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")

	first := runCLIExecutionForProject(t, projectDir, outDir)
	if first.result.Cached {
		t.Fatal("expected first project run to execute")
	}
	if first.metadata.ProjectInputs == nil {
		t.Fatal("expected project inputs in metadata")
	}
	if len(first.metadata.ProjectInputs.Models) != 2 {
		t.Fatalf("expected 2 captured model resources, got %d", len(first.metadata.ProjectInputs.Models))
	}
	if len(first.metadata.ProjectInputs.Tables) != 2 {
		t.Fatalf("expected 2 captured table resources, got %d", len(first.metadata.ProjectInputs.Tables))
	}
	if first.metadata.ProjectInputs.DSL.Signature == "" {
		t.Fatal("expected captured DSL signature")
	}

	second := runCLIExecutionForProject(t, projectDir, outDir)
	if !second.result.Cached {
		t.Fatal("expected identical project run to hit cache")
	}
	if !bytes.Equal(first.metadataBytes, second.metadataBytes) {
		t.Fatalf("expected identical metadata bytes across identical project runs\nfirst:\n%s\nsecond:\n%s", string(first.metadataBytes), string(second.metadataBytes))
	}
	assertLayerKeysEqual(t, first.result.LayerKeys, second.result.LayerKeys)

	modelPath := filepath.Join(projectDir, "models", "box.FCStd")
	if err := os.WriteFile(modelPath, []byte("model-a-updated"), 0o644); err != nil {
		t.Fatalf("failed to update model fixture: %v", err)
	}

	modifiedModel := runCLIExecutionForProject(t, projectDir, outDir)
	if modifiedModel.result.Cached {
		t.Fatal("expected cache miss after model change")
	}
	if bytes.Equal(first.metadataBytes, modifiedModel.metadataBytes) {
		t.Fatalf("expected metadata to change after model update\nbefore:\n%s\nafter:\n%s", string(first.metadataBytes), string(modifiedModel.metadataBytes))
	}
	if first.result.LayerKeys[cache.CacheGeometry] == modifiedModel.result.LayerKeys[cache.CacheGeometry] {
		t.Fatal("expected layer keys to change after model update")
	}
	if first.metadata.ProjectInputs.Models[0].Signature == modifiedModel.metadata.ProjectInputs.Models[0].Signature {
		t.Fatal("expected captured model signature to change after model update")
	}

	tablePath := filepath.Join(projectDir, "tables", "labels.json")
	if err := os.WriteFile(tablePath, []byte(`{
  "schemaVersion": "1.0",
  "name": "label_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "label", "type": "string", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "label": "M8 bolt updated"}
  ]
}`), 0o644); err != nil {
		t.Fatalf("failed to update table fixture: %v", err)
	}

	modifiedTable := runCLIExecutionForProject(t, projectDir, outDir)
	if modifiedTable.result.Cached {
		t.Fatal("expected cache miss after table change")
	}
	if modifiedTable.metadata.ProjectInputs.Tables[1].Signature == modifiedModel.metadata.ProjectInputs.Tables[1].Signature {
		t.Fatal("expected captured table signature to change after table update")
	}
}

func TestCLIProjectRun_DirectoryAndProjectFileEntrypointsProduceSameCapture(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	projectFile := filepath.Join(projectDir, "parametron.project.json")

	fromDir, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun(directory) failed: %v", err)
	}
	fromFile, err := loadPlannedRun(projectFile, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun(file) failed: %v", err)
	}

	firstJSON, err := json.Marshal(fromDir.ProjectInputs)
	if err != nil {
		t.Fatalf("marshal directory capture failed: %v", err)
	}
	secondJSON, err := json.Marshal(fromFile.ProjectInputs)
	if err != nil {
		t.Fatalf("marshal file capture failed: %v", err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("expected identical project capture across entrypoints\ndir:\n%s\nfile:\n%s", string(firstJSON), string(secondJSON))
	}
}

func TestCLIProjectRun_CacheKeysUseCapturedModelInputsWithoutManualModelHash(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)

	first, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun(first) failed: %v", err)
	}
	keysA, err := computeRunLayerKeys(first.Plan, first.AST, "", first.TableInputs, first.ProjectInputs, &cache.SignatureV2Strategy{})
	if err != nil {
		t.Fatalf("computeRunLayerKeys(first) failed: %v", err)
	}

	modelPath := filepath.Join(projectDir, "models", "box.FCStd")
	if err := os.WriteFile(modelPath, []byte("model-a-updated"), 0o644); err != nil {
		t.Fatalf("failed to update model fixture: %v", err)
	}

	second, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun(second) failed: %v", err)
	}
	keysB, err := computeRunLayerKeys(second.Plan, second.AST, "", second.TableInputs, second.ProjectInputs, &cache.SignatureV2Strategy{})
	if err != nil {
		t.Fatalf("computeRunLayerKeys(second) failed: %v", err)
	}

	if keysA[cache.CacheGeometry] == keysB[cache.CacheGeometry] {
		t.Fatal("expected project model capture to change cache keys without manual model hash")
	}
}

func TestCLIProjectRun_EmitsRecordPackageFromNormalRun(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	run := runCLIExecutionForProject(t, projectDir, filepath.Join(t.TempDir(), "out"))
	if run.result.Cached {
		t.Fatal("expected normal non-cached project run")
	}

	packageRoot := recordPackageRoot(run.result.RunRoot)
	for _, contractPath := range []string{
		recordpackage.PackageManifestContractPath(),
		recordpackage.MustRecordContractPath("execution"),
		recordpackage.RawReportContractPath(),
		recordpackage.RawMetadataContractPath(),
		recordpackage.RawArtifactStoreManifestContractPath(),
		recordpackage.RawObservedContractPath(),
		recordpackage.RawVerificationContractPath(),
		recordpackage.RawRuntimeResultContractPath(),
	} {
		assertPackageFileExists(t, packageRoot, contractPath)
	}

	manifest := readCLIRecordPackageManifest(t, packageRoot)
	wantPackageKey := "engine-run:" + run.planned.PlanHash
	if manifest.PackageKey != wantPackageKey {
		t.Fatalf("packageKey = %q, want %q", manifest.PackageKey, wantPackageKey)
	}
	assertManifestHasRecord(t, manifest, "execution", recordpackage.MustRecordContractPath("execution"), wantPackageKey)
	assertManifestRawEvidencePaths(t, manifest, []string{
		recordpackage.RawReportContractPath(),
		recordpackage.RawMetadataContractPath(),
		recordpackage.RawArtifactStoreManifestContractPath(),
		recordpackage.RawObservedContractPath(),
		recordpackage.RawVerificationContractPath(),
		recordpackage.RawRuntimeResultContractPath(),
	})
	assertManifestOrdering(t, manifest)

	// The single successful, verification-passed CAD outcome from this run
	// must supply real attempt evidence, not merely present-but-empty files.
	for _, contractPath := range []string{
		recordpackage.RawObservedContractPath(),
		recordpackage.RawVerificationContractPath(),
		recordpackage.RawRuntimeResultContractPath(),
	} {
		data, err := os.ReadFile(filepath.Join(packageRoot, filepath.FromSlash(contractPath)))
		if err != nil {
			t.Fatalf("read package file %q: %v", contractPath, err)
		}
		if len(data) == 0 {
			t.Fatalf("package file %q is empty, want real attempt evidence bytes", contractPath)
		}
	}
}

// validReferenceTraversalJSONFixture is a deliberately non-canonically
// formatted schema-2 traversal payload with a single resolved internal
// reference edge, used to drive the controlled aligned runtime's optional
// reference-traversal output.
func validReferenceTraversalJSONFixture() []byte {
	return []byte(`{
  "schemaVersion":  "2.0",
  "kind": "reference-traversal",
  "boundary": "internal",
  "operation": "resolve",
  "status": "succeeded",
  "sourceDocument": "Widget.FCStd",
  "nodes": [
    {"sequence": 0, "id": "n1", "kind": "document", "state": "resolved", "documentPath": "Widget.FCStd"},
    {"sequence": 1, "id": "n2", "kind": "object", "state": "resolved", "documentPath": "Widget.FCStd", "objectName": "Body"}
  ],
  "edges": [
    {"sequence": 0, "source": "n1", "target": "n2", "kind": "document_internal_reference", "state": "resolved"}
  ],
  "diagnostics": []
}
`)
}

// runCLIExecutionForProjectWithReferenceTraversal mirrors
// runCLIExecutionForProject, additionally driving the controlled aligned
// runtime to write the supplied bytes as its reference-traversal output (or
// no traversal output at all when traversalJSON is nil).
func runCLIExecutionForProjectWithReferenceTraversal(t *testing.T, projectPath, outDir string, traversalJSON []byte) cliProjectExecution {
	t.Helper()
	installControlledAlignedRuntime(t, "success")
	if traversalJSON != nil {
		t.Setenv("PARAMETRON_TASK13_RUNTIME_TRAVERSAL_JSON", string(traversalJSON))
	}

	resetGlobals()

	planned, err := loadPlannedRun(projectPath, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun failed: %v", err)
	}

	result, err := executePlanRun(executionOptions{
		Planned:   planned,
		OutputDir: outDir,
		UseCache:  true,
	})
	if err != nil {
		t.Fatalf("executePlanRun failed: %v", err)
	}

	metadataBytes, err := os.ReadFile(filepath.Join(result.RunRoot, metadata.FileName))
	if err != nil {
		t.Fatalf("failed to read %s: %v", metadata.FileName, err)
	}
	var runMetadata metadata.Metadata
	if err := json.Unmarshal(metadataBytes, &runMetadata); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", metadata.FileName, err)
	}

	reportBytes, err := os.ReadFile(filepath.Join(result.RunRoot, report.FileName))
	if err != nil {
		t.Fatalf("failed to read %s: %v", report.FileName, err)
	}
	var runReport report.Report
	if err := json.Unmarshal(reportBytes, &runReport); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", report.FileName, err)
	}

	return cliProjectExecution{
		result: result, planned: planned, metadata: runMetadata,
		metadataBytes: metadataBytes, report: runReport, reportBytes: reportBytes,
	}
}

// TestCLIProjectRun_EmitsReferenceRecordFromRuntimeTraversal is the primary
// Stage 2 integration proof: a successful aligned execution through the
// controlled/fake aligned runtime, producing one verified reference-traversal
// output, must flow through the normal record package into canonical raw
// traversal evidence plus a validated normalized ReferenceRecord. This does
// not require real FreeCAD and does not simulate recordemit directly.
func TestCLIProjectRun_EmitsReferenceRecordFromRuntimeTraversal(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	traversal := validReferenceTraversalJSONFixture()
	run := runCLIExecutionForProjectWithReferenceTraversal(t, projectDir, filepath.Join(t.TempDir(), "out"), traversal)
	if run.result.Cached {
		t.Fatal("expected a normal non-cached project run")
	}

	packageRoot := recordPackageRoot(run.result.RunRoot)
	assertPackageFileExists(t, packageRoot, recordpackage.RawRuntimeReferenceTraversalContractPath())
	rawBytes, err := os.ReadFile(filepath.Join(packageRoot, filepath.FromSlash(recordpackage.RawRuntimeReferenceTraversalContractPath())))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rawBytes, traversal) {
		t.Fatalf("raw traversal evidence bytes differ\nwant:\n%s\ngot:\n%s", traversal, rawBytes)
	}

	assertPackageFileExists(t, packageRoot, recordpackage.MustRecordContractPath("reference"))
	referenceBytes, err := os.ReadFile(filepath.Join(packageRoot, filepath.FromSlash(recordpackage.MustRecordContractPath("reference"))))
	if err != nil {
		t.Fatal(err)
	}
	var referenceRecord recordcontract.ReferenceRecord
	if err := json.Unmarshal(referenceBytes, &referenceRecord); err != nil {
		t.Fatalf("unmarshal reference record: %v\n%s", err, string(referenceBytes))
	}
	if err := recordcontract.ValidateReferenceRecord(referenceRecord); err != nil {
		t.Fatalf("ValidateReferenceRecord(emitted) returned error: %v", err)
	}
	if len(referenceRecord.Reference.Edges) != 1 {
		t.Fatalf("expected exactly one normalized edge, got %#v", referenceRecord.Reference.Edges)
	}

	manifest := readCLIRecordPackageManifest(t, packageRoot)
	wantPackageKey := "engine-run:" + run.planned.PlanHash
	wantReferenceKey := wantPackageKey + ":reference"
	if referenceRecord.RecordKey != wantReferenceKey {
		t.Fatalf("reference recordKey = %q, want %q", referenceRecord.RecordKey, wantReferenceKey)
	}
	assertManifestHasRecord(t, manifest, "reference", recordpackage.MustRecordContractPath("reference"), wantReferenceKey)
	assertManifestRawEvidencePaths(t, manifest, []string{
		recordpackage.RawReportContractPath(),
		recordpackage.RawMetadataContractPath(),
		recordpackage.RawArtifactStoreManifestContractPath(),
		recordpackage.RawObservedContractPath(),
		recordpackage.RawVerificationContractPath(),
		recordpackage.RawRuntimeResultContractPath(),
		recordpackage.RawRuntimeReferenceTraversalContractPath(),
	})

	// Traversal integration is additive: existing normal package surfaces remain.
	assertManifestHasRecord(t, manifest, "execution", recordpackage.MustRecordContractPath("execution"), wantPackageKey)
	assertPackageFileExists(t, packageRoot, recordpackage.RawReportContractPath())
}

func TestCLIProjectRun_RepeatedEquivalentNormalRunsEmitDeterministicReferenceRecord(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	traversal := validReferenceTraversalJSONFixture()
	first := runCLIExecutionForProjectWithReferenceTraversal(t, projectDir, filepath.Join(t.TempDir(), "out-a"), traversal)
	if first.result.Cached {
		t.Fatal("expected first normal run to execute")
	}
	if err := os.RemoveAll(".cache"); err != nil {
		t.Fatalf("failed to clear cache before equivalent rerun: %v", err)
	}
	second := runCLIExecutionForProjectWithReferenceTraversal(t, projectDir, filepath.Join(t.TempDir(), "out-b"), traversal)
	if second.result.Cached {
		t.Fatal("expected second normal run to execute")
	}
	if first.result.RunRoot == second.result.RunRoot {
		t.Fatalf("expected fresh run roots, both runs used %q", first.result.RunRoot)
	}

	assertExactlyOneTraversalCandidate := func(label string, run cliProjectExecution) {
		t.Helper()
		eligible := 0
		for _, job := range run.result.Execution.Jobs {
			outcome := job.CADRuntimeOutcome
			if job.Err == nil && outcome != nil && outcome.Verification == artifact.VerificationOutcomePassed && len(outcome.ReferenceTraversalJSON) > 0 {
				eligible++
			}
		}
		if eligible != 1 {
			t.Fatalf("%s run eligible verified traversal candidates = %d, want exactly 1", label, eligible)
		}
	}
	assertExactlyOneTraversalCandidate("first", first)
	assertExactlyOneTraversalCandidate("second", second)

	referencePath := recordpackage.MustRecordContractPath("reference")
	rawPath := recordpackage.RawRuntimeReferenceTraversalContractPath()
	readSurfaces := func(label string, run cliProjectExecution) ([]byte, []byte, recordcontract.ReferenceRecord, cliRecordPackageManifest, cliRecordPackageManifestRecord) {
		t.Helper()
		packageRoot := recordPackageRoot(run.result.RunRoot)
		assertPackageFileExists(t, packageRoot, referencePath)
		assertPackageFileExists(t, packageRoot, rawPath)

		referenceBytes, err := os.ReadFile(filepath.Join(packageRoot, filepath.FromSlash(referencePath)))
		if err != nil {
			t.Fatalf("read %s reference record: %v", label, err)
		}
		var record recordcontract.ReferenceRecord
		if err := json.Unmarshal(referenceBytes, &record); err != nil {
			t.Fatalf("unmarshal %s reference record: %v\n%s", label, err, referenceBytes)
		}
		if err := recordcontract.ValidateReferenceRecord(record); err != nil {
			t.Fatalf("validate %s reference record: %v", label, err)
		}
		if len(record.Reference.Edges) < 1 {
			t.Fatalf("%s reference record has no normalized edges", label)
		}
		hasInternalComponentEdge := false
		for _, edge := range record.Reference.Edges {
			if edge.Kind == recordcontract.ReferenceKindComponent {
				hasInternalComponentEdge = true
				break
			}
		}
		if !hasInternalComponentEdge {
			t.Fatalf("%s reference record has no normalized internal/component edge: %#v", label, record.Reference.Edges)
		}

		rawBytes, err := os.ReadFile(filepath.Join(packageRoot, filepath.FromSlash(rawPath)))
		if err != nil {
			t.Fatalf("read %s raw traversal evidence: %v", label, err)
		}
		manifest := readCLIRecordPackageManifest(t, packageRoot)
		var entry cliRecordPackageManifestRecord
		matches := 0
		for _, candidate := range manifest.Records {
			if candidate.Family == "reference" {
				entry = candidate
				matches++
			}
		}
		if matches != 1 {
			t.Fatalf("%s manifest reference-family entries = %d, want exactly 1: %#v", label, matches, manifest.Records)
		}
		if entry.ContractPath != referencePath || entry.RecordKey != record.RecordKey || entry.IdentityID != record.Identity.ID {
			t.Fatalf("%s manifest reference entry does not index emitted record: entry=%#v record=%#v", label, entry, record)
		}
		return referenceBytes, rawBytes, record, manifest, entry
	}

	firstReferenceBytes, firstRawBytes, firstRecord, firstManifest, firstEntry := readSurfaces("first", first)
	secondReferenceBytes, secondRawBytes, secondRecord, secondManifest, secondEntry := readSurfaces("second", second)
	assertPackageFileBytesEqual(t, referencePath, firstReferenceBytes, secondReferenceBytes)
	if firstRecord.Identity != secondRecord.Identity {
		t.Fatalf("reference identities differ across equivalent runs: %#v vs %#v", firstRecord.Identity, secondRecord.Identity)
	}
	if firstEntry != secondEntry {
		t.Fatalf("reference manifest entries differ across equivalent runs: %#v vs %#v", firstEntry, secondEntry)
	}
	if firstManifest.PackageKey != secondManifest.PackageKey || firstManifest.PackageKey != "engine-run:"+first.planned.PlanHash {
		t.Fatalf("reference package keys differ or are unexpected: %q vs %q", firstManifest.PackageKey, secondManifest.PackageKey)
	}
	if !bytes.Equal(firstRawBytes, secondRawBytes) || !bytes.Equal(firstRawBytes, traversal) {
		t.Fatalf("canonical raw traversal evidence differs across equivalent runs or from runtime bytes")
	}
	wantDigest := sha256.Sum256(traversal)
	for label, record := range map[string]recordcontract.ReferenceRecord{"first": firstRecord, "second": secondRecord} {
		for _, edge := range record.Reference.Edges {
			if edge.Evidence.DigestSHA256 != fmt.Sprintf("%x", wantDigest) {
				t.Fatalf("%s normalized edge evidence digest = %q, want %x", label, edge.Evidence.DigestSHA256, wantDigest)
			}
		}
	}
}

// TestCLIProjectRun_NoReferenceTraversalOutputIsCompatible proves the same
// normal aligned CLI path succeeds without inventing traversal-specific
// package surfaces when the runtime produces no traversal output at all.
func TestCLIProjectRun_NoReferenceTraversalOutputIsCompatible(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	run := runCLIExecutionForProject(t, projectDir, filepath.Join(t.TempDir(), "out"))
	if run.result.Cached {
		t.Fatal("expected a normal non-cached project run")
	}

	packageRoot := recordPackageRoot(run.result.RunRoot)
	if _, err := os.Stat(filepath.Join(packageRoot, filepath.FromSlash(recordpackage.RawRuntimeReferenceTraversalContractPath()))); !os.IsNotExist(err) {
		t.Fatalf("did not expect raw traversal evidence without a traversal output, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(packageRoot, filepath.FromSlash(recordpackage.MustRecordContractPath("reference")))); !os.IsNotExist(err) {
		t.Fatalf("did not expect a reference record without a traversal output, stat err=%v", err)
	}
	manifest := readCLIRecordPackageManifest(t, packageRoot)
	for _, record := range manifest.Records {
		if record.Family == "reference" {
			t.Fatalf("manifest indexes a reference record without a traversal output: %#v", record)
		}
	}
	for _, evidence := range manifest.RawEvidence {
		if evidence.ContractPath == recordpackage.RawRuntimeReferenceTraversalContractPath() {
			t.Fatalf("manifest indexes raw traversal evidence without a traversal output: %#v", evidence)
		}
	}
}

// TestCLIProjectRun_InvalidReferenceTraversalFailsPackageEmissionAndCache
// proves that an authoritative but mapper-invalid traversal output fails
// package emission closed and prevents cache completion, reusing the same
// cache-marker helper used by the existing failed-run package test.
func TestCLIProjectRun_InvalidReferenceTraversalFailsPackageEmissionAndCache(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	installControlledAlignedRuntime(t, "success")
	// Unsupported schema version: syntactically valid, strictly-decodable
	// JSON that nonetheless violates the mapper contract.
	invalidTraversal := []byte(`{"schemaVersion":"1.0","kind":"reference-traversal","boundary":"internal","operation":"resolve","status":"succeeded","sourceDocument":"Widget.FCStd","nodes":[],"edges":[],"diagnostics":[]}`)
	t.Setenv("PARAMETRON_TASK13_RUNTIME_TRAVERSAL_JSON", string(invalidTraversal))

	resetGlobals()
	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun failed: %v", err)
	}
	result, execErr := executePlanRun(executionOptions{
		Planned:   planned,
		OutputDir: filepath.Join(t.TempDir(), "out"),
		UseCache:  true,
	})
	if execErr == nil {
		t.Fatal("expected package emission to fail closed for a mapper-invalid traversal output")
	}
	if !strings.Contains(execErr.Error(), "emit record package") {
		t.Fatalf("execution error = %v, want a wrapped record-package emission failure", execErr)
	}
	if result == nil {
		t.Fatal("expected a non-nil result even though package emission failed")
	}
	if allLayersCached(t, result.LayerKeys) {
		t.Fatal("package emission failure must prevent cache completion")
	}
	// The underlying execution itself succeeded; only package emission failed.
	if _, err := os.Stat(filepath.Join(result.RunRoot, report.FileName)); err != nil {
		t.Fatalf("expected %s to exist despite the package emission failure: %v", report.FileName, err)
	}
}

func TestCLIProjectRun_RepeatedEquivalentNormalRunsEmitDeterministicRecordPackageSurfaces(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)

	first := runCLIExecutionForProject(t, projectDir, filepath.Join(t.TempDir(), "out-a"))
	if first.result.Cached {
		t.Fatal("expected first run to execute")
	}
	if err := os.RemoveAll(".cache"); err != nil {
		t.Fatalf("failed to clear cache before equivalent rerun: %v", err)
	}
	second := runCLIExecutionForProject(t, projectDir, filepath.Join(t.TempDir(), "out-b"))
	if second.result.Cached {
		t.Fatal("expected second run to execute")
	}

	firstFiles := readRecordPackageFiles(t, recordPackageRoot(first.result.RunRoot))
	secondFiles := readRecordPackageFiles(t, recordPackageRoot(second.result.RunRoot))
	assertSamePackageFileSet(t, firstFiles, secondFiles)

	stablePaths := deterministicRecordPackagePaths(t, firstFiles)
	for _, path := range stablePaths {
		assertPackageFileBytesEqual(t, path, firstFiles[path], secondFiles[path])
	}

	firstManifest := readCLIRecordPackageManifest(t, recordPackageRoot(first.result.RunRoot))
	secondManifest := readCLIRecordPackageManifest(t, recordPackageRoot(second.result.RunRoot))
	if firstManifest.PackageKey != secondManifest.PackageKey {
		t.Fatalf("package keys differ across equivalent runs: %q vs %q", firstManifest.PackageKey, secondManifest.PackageKey)
	}
	if firstManifest.PackageKey != "engine-run:"+first.planned.PlanHash {
		t.Fatalf("packageKey = %q, want engine-run:%s", firstManifest.PackageKey, first.planned.PlanHash)
	}
	if first.planned.PlanHash != second.planned.PlanHash {
		t.Fatalf("plan hashes differ across equivalent runs: %q vs %q", first.planned.PlanHash, second.planned.PlanHash)
	}
	assertManifestOrdering(t, firstManifest)
	assertManifestOrdering(t, secondManifest)
}

func TestRecordEmitRunPackage_ReemissionIntoSameRunRootIsIdempotent(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	run := runCLIExecutionForProject(t, projectDir, filepath.Join(t.TempDir(), "out"))
	if run.result.Cached {
		t.Fatal("expected normal non-cached project run")
	}

	packageRoot := recordPackageRoot(run.result.RunRoot)
	sentinelPath := filepath.Join(packageRoot, "local-note.txt")
	if err := os.WriteFile(sentinelPath, []byte("preserve me\n"), 0o644); err != nil {
		t.Fatalf("failed to write package sentinel: %v", err)
	}
	before := readRecordPackageFiles(t, packageRoot)

	// Re-emission does not reconstruct CAD runtime evidence from run-root
	// paths; a caller reproducing an equivalent package must resupply the
	// same attempt evidence bytes it already holds.
	cadRuntimeEvidence := &recordemit.CADRuntimeRunEvidence{
		Result:       before[recordpackage.RawRuntimeResultContractPath()],
		Verification: before[recordpackage.RawVerificationContractPath()],
		Observed:     before[recordpackage.RawObservedContractPath()],
	}

	if err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot:    run.result.RunRoot,
		PlanHash:   run.planned.PlanHash,
		Report:     run.report,
		Metadata:   &run.metadata,
		CADRuntime: cadRuntimeEvidence,
	}); err != nil {
		t.Fatalf("EmitRunPackage(second) failed: %v", err)
	}

	after := readRecordPackageFiles(t, packageRoot)
	assertSamePackageFileSet(t, before, after)
	for _, path := range sortedPackagePaths(before) {
		assertPackageFileBytesEqual(t, path, before[path], after[path])
	}

	manifest := readCLIRecordPackageManifest(t, packageRoot)
	assertManifestOrdering(t, manifest)
}

func TestCLIProjectRun_CachedRunDoesNotInventOrTouchRecordPackage(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")

	first := runCLIExecutionForProject(t, projectDir, outDir)
	if first.result.Cached {
		t.Fatal("expected first run to execute")
	}
	packageRoot := recordPackageRoot(first.result.RunRoot)
	before := readRecordPackageFiles(t, packageRoot)

	cached := runCLIExecutionForProject(t, projectDir, outDir)
	if !cached.result.Cached {
		t.Fatal("expected second run to use cached early return")
	}
	after := readRecordPackageFiles(t, packageRoot)
	assertSamePackageFileSet(t, before, after)
	for _, path := range sortedPackagePaths(before) {
		assertPackageFileBytesEqual(t, path, before[path], after[path])
	}

	if err := os.RemoveAll(packageRoot); err != nil {
		t.Fatalf("failed to remove package before cached run: %v", err)
	}
	missingPackageCached := runCLIExecutionForProject(t, projectDir, outDir)
	if !missingPackageCached.result.Cached {
		t.Fatal("expected cached early return after removing package")
	}
	if _, err := os.Stat(packageRoot); !os.IsNotExist(err) {
		t.Fatalf("cached run should not invent a missing record package, stat err = %v", err)
	}
}

func TestCLIProjectRun_FailedNormalRunEmitsFailureRecordPackage(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)

	run := runFailedCLIExecutionForProject(t, projectDir, filepath.Join(t.TempDir(), "out"))
	if run.err == nil {
		t.Fatal("expected failed project execution")
	}
	if strings.Contains(run.err.Error(), "emit record package") {
		t.Fatalf("execution error = %v, want original execution failure", run.err)
	}
	if !strings.Contains(run.err.Error(), "controlled_failure") {
		t.Fatalf("execution error = %v, want controlled aligned runtime failure", run.err)
	}
	if allLayersCached(t, run.result.LayerKeys) {
		t.Fatal("failed execution should not complete cache markers")
	}

	reportPath := filepath.Join(run.result.RunRoot, report.FileName)
	assertRegularFile(t, reportPath)
	packageRoot := recordPackageRoot(run.result.RunRoot)
	for _, contractPath := range []string{
		recordpackage.PackageManifestContractPath(),
		recordpackage.MustRecordContractPath("execution"),
		recordpackage.MustRecordContractPath("failure"),
		recordpackage.RawReportContractPath(),
	} {
		assertPackageFileExists(t, packageRoot, contractPath)
	}

	manifest := readCLIRecordPackageManifest(t, packageRoot)
	wantPackageKey := "engine-run:" + run.planned.PlanHash
	wantFailureKey := wantPackageKey + ":failure"
	if manifest.PackageKey != wantPackageKey {
		t.Fatalf("packageKey = %q, want %q", manifest.PackageKey, wantPackageKey)
	}
	assertManifestHasRecord(t, manifest, "execution", recordpackage.MustRecordContractPath("execution"), wantPackageKey)
	assertManifestHasRecord(t, manifest, "failure", recordpackage.MustRecordContractPath("failure"), wantFailureKey)
	assertManifestRawEvidencePaths(t, manifest, []string{recordpackage.RawReportContractPath()})

	failure := readCLIFailureRecord(t, packageRoot)
	if err := recordcontract.ValidateFailureRecord(failure); err != nil {
		t.Fatalf("ValidateFailureRecord(emitted) returned error: %v", err)
	}
	if failure.Family != recordcontract.FamilyFailure {
		t.Fatalf("failure family = %q, want %q", failure.Family, recordcontract.FamilyFailure)
	}
	if failure.Version != recordcontract.CurrentVersion {
		t.Fatalf("failure version = %q, want %q", failure.Version, recordcontract.CurrentVersion)
	}
	if failure.RecordKey != wantFailureKey {
		t.Fatalf("failure recordKey = %q, want %q", failure.RecordKey, wantFailureKey)
	}
	if failure.Failure.Class == "" || failure.Failure.Stage == "" || failure.Failure.Severity == "" || failure.Failure.Message == "" {
		t.Fatalf("failure summary missing required normalized material: %#v", failure.Failure)
	}
	if !hasFailureRecordEvidence(failure, "report", recordpackage.RawReportContractPath()) {
		t.Fatalf("failure evidence = %#v, want raw report evidence", failure.Failure.Evidence)
	}
	if failure.Provenance.Plan.PlanHash != run.planned.PlanHash {
		t.Fatalf("failure provenance plan hash = %q, want %q", failure.Provenance.Plan.PlanHash, run.planned.PlanHash)
	}
	if failure.Failure.Linkage.ProductKey == "" || failure.Failure.Linkage.StepRef == "" {
		t.Fatalf("failure linkage = %#v, want product and step linkage", failure.Failure.Linkage)
	}
}

func TestCLIProjectRun_RepeatedEquivalentFailuresEmitDeterministicRecordPackageSurfaces(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)

	first := runFailedCLIExecutionForProject(t, projectDir, filepath.Join(t.TempDir(), "out-a"))
	if first.err == nil {
		t.Fatal("expected first run to fail")
	}
	if err := os.RemoveAll(".cache"); err != nil {
		t.Fatalf("failed to clear cache before equivalent failed rerun: %v", err)
	}
	second := runFailedCLIExecutionForProject(t, projectDir, filepath.Join(t.TempDir(), "out-b"))
	if second.err == nil {
		t.Fatal("expected second run to fail")
	}

	firstFiles := readRecordPackageFiles(t, recordPackageRoot(first.result.RunRoot))
	secondFiles := readRecordPackageFiles(t, recordPackageRoot(second.result.RunRoot))
	assertSamePackageFileSet(t, firstFiles, secondFiles)

	for _, path := range []string{
		recordpackage.PackageManifestContractPath(),
		recordpackage.MustRecordContractPath("execution"),
		recordpackage.MustRecordContractPath("failure"),
	} {
		assertPackageFileBytesEqual(t, path, firstFiles[path], secondFiles[path])
	}
	if _, ok := firstFiles[recordpackage.RawReportContractPath()]; !ok {
		t.Fatalf("first package missing raw report evidence in %#v", sortedPackagePaths(firstFiles))
	}
	if _, ok := secondFiles[recordpackage.RawReportContractPath()]; !ok {
		t.Fatalf("second package missing raw report evidence in %#v", sortedPackagePaths(secondFiles))
	}

	firstManifest := readCLIRecordPackageManifest(t, recordPackageRoot(first.result.RunRoot))
	secondManifest := readCLIRecordPackageManifest(t, recordPackageRoot(second.result.RunRoot))
	if firstManifest.PackageKey != secondManifest.PackageKey {
		t.Fatalf("package keys differ across equivalent failed runs: %q vs %q", firstManifest.PackageKey, secondManifest.PackageKey)
	}
	if firstManifest.PackageKey != "engine-run:"+first.planned.PlanHash {
		t.Fatalf("packageKey = %q, want engine-run:%s", firstManifest.PackageKey, first.planned.PlanHash)
	}
	firstFailure := readCLIFailureRecord(t, recordPackageRoot(first.result.RunRoot))
	secondFailure := readCLIFailureRecord(t, recordPackageRoot(second.result.RunRoot))
	if firstFailure.RecordKey != secondFailure.RecordKey {
		t.Fatalf("failure record keys differ: %q vs %q", firstFailure.RecordKey, secondFailure.RecordKey)
	}
	if !strings.HasSuffix(firstFailure.RecordKey, ":failure") {
		t.Fatalf("failure record key = %q, want :failure suffix", firstFailure.RecordKey)
	}
}

// --- referenceTraversalRunEvidence candidate cardinality (helper-level) ---

func eligibleCADRuntimeOutcome(jobID, productKey, stepID string, traversal []byte) *executor.CADRuntimeOutcome {
	return &executor.CADRuntimeOutcome{
		JobID: jobID, ProductKey: productKey, StepID: stepID,
		Verification:           artifact.VerificationOutcomePassed,
		ReferenceTraversalJSON: traversal,
	}
}

func TestReferenceTraversalRunEvidence_ZeroEligibleCandidates(t *testing.T) {
	cases := map[string]struct {
		succeeded bool
		jobs      []scheduler.JobExecution
	}{
		"no CADRuntimeOutcome": {true, []scheduler.JobExecution{{CADRuntimeOutcome: nil}}},
		"verification not pass": {true, []scheduler.JobExecution{{CADRuntimeOutcome: &executor.CADRuntimeOutcome{
			JobID: "job-1", ProductKey: "widget", StepID: "2",
			Verification: artifact.VerificationOutcomeFailed, ReferenceTraversalJSON: []byte("x"),
		}}}},
		"empty traversal bytes": {true, []scheduler.JobExecution{{CADRuntimeOutcome: eligibleCADRuntimeOutcome("job-1", "widget", "2", nil)}}},
		"job error": {true, []scheduler.JobExecution{{
			Err: errors.New("boom"), CADRuntimeOutcome: eligibleCADRuntimeOutcome("job-1", "widget", "2", []byte("x")),
		}}},
		"overall execution failure": {false, []scheduler.JobExecution{{CADRuntimeOutcome: eligibleCADRuntimeOutcome("job-1", "widget", "2", []byte("x"))}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := referenceTraversalRunEvidence(scheduler.ExecutionResult{Jobs: tc.jobs}, tc.succeeded)
			if got != nil {
				t.Fatalf("expected nil evidence, got %#v", got)
			}
		})
	}
}

func TestReferenceTraversalRunEvidence_ExactlyOneEligibleCandidate(t *testing.T) {
	traversal := []byte(`{"schemaVersion":"2.0"}`)
	original := append([]byte(nil), traversal...)
	outcome := eligibleCADRuntimeOutcome("job-1", "widget", "2", traversal)
	execution := scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{CADRuntimeOutcome: outcome}}}

	got := referenceTraversalRunEvidence(execution, true)
	if got == nil {
		t.Fatal("expected non-nil evidence for exactly one eligible candidate")
	}
	if got.JobID != "job-1" || got.ProductKey != "widget" || got.StepRef != "2" || !bytes.Equal(got.Content, original) {
		t.Fatalf("evidence=%#v", got)
	}

	// Copy safety: mutating the source outcome after projection must not
	// affect the already-projected evidence.
	outcome.ReferenceTraversalJSON[0] = 'X'
	outcome.JobID, outcome.ProductKey, outcome.StepID = "mutated", "mutated", "mutated"
	if !bytes.Equal(got.Content, original) || got.JobID != "job-1" || got.ProductKey != "widget" || got.StepRef != "2" {
		t.Fatalf("projected evidence changed after mutating its source: %#v", got)
	}
}

func TestReferenceTraversalRunEvidence_MultipleEligibleCandidatesOmitProjection(t *testing.T) {
	execution := scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{
		{CADRuntimeOutcome: eligibleCADRuntimeOutcome("job-1", "widget-a", "2", []byte("a"))},
		{CADRuntimeOutcome: eligibleCADRuntimeOutcome("job-1", "widget-b", "2", []byte("b"))},
	}}
	if got := referenceTraversalRunEvidence(execution, true); got != nil {
		t.Fatalf("expected nil projection for multiple eligible candidates (no arbitrary selection), got %#v", got)
	}
}

// --- cadRuntimeRunEvidence candidate cardinality and raw evidence reading (helper-level) ---

// cadRuntimeEvidenceOutcome builds an eligible CADRuntimeOutcome backed by
// real files under a fresh working copy directory, so cadRuntimeRunEvidence
// exercises its actual guarded filesystem reads rather than opaque struct
// plumbing. A nil bytes value omits that evidence family's source file/path
// entirely, matching how an attempt with no such optional output behaves.
func cadRuntimeEvidenceOutcome(t *testing.T, jobID, productKey, stepID string, result, verification, observed []byte) *executor.CADRuntimeOutcome {
	t.Helper()
	working := t.TempDir()
	outcome := &executor.CADRuntimeOutcome{
		JobID: jobID, ProductKey: productKey, StepID: stepID,
		Verification:   artifact.VerificationOutcomePassed,
		WorkingCopyDir: working,
	}
	if result != nil {
		path := filepath.Join(working, "prm.result.json")
		if err := os.WriteFile(path, result, 0o600); err != nil {
			t.Fatal(err)
		}
		outcome.ResultPath = path
	}
	if verification != nil {
		path := filepath.Join(working, "prm.verification.json")
		if err := os.WriteFile(path, verification, 0o600); err != nil {
			t.Fatal(err)
		}
		outcome.ObservationRequestPath = path
	}
	if observed != nil {
		dir := filepath.Join(working, "outputs")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "prm.observed.json")
		if err := os.WriteFile(path, observed, 0o600); err != nil {
			t.Fatal(err)
		}
		outcome.ObservedPath = path
	}
	return outcome
}

func TestCADRuntimeRunEvidence_ZeroEligibleCandidates(t *testing.T) {
	cases := map[string]struct {
		succeeded bool
		jobs      []scheduler.JobExecution
	}{
		"no CADRuntimeOutcome": {true, []scheduler.JobExecution{{CADRuntimeOutcome: nil}}},
		"verification not pass": {true, []scheduler.JobExecution{{CADRuntimeOutcome: &executor.CADRuntimeOutcome{
			JobID: "job-1", ProductKey: "widget", StepID: "2",
			Verification: artifact.VerificationOutcomeFailed,
		}}}},
		"job error": {true, []scheduler.JobExecution{{
			Err:               errors.New("boom"),
			CADRuntimeOutcome: cadRuntimeEvidenceOutcome(t, "job-1", "widget", "2", []byte("r"), []byte("v"), []byte("o")),
		}}},
		"overall execution failure": {false, []scheduler.JobExecution{{
			CADRuntimeOutcome: cadRuntimeEvidenceOutcome(t, "job-1", "widget", "2", []byte("r"), []byte("v"), []byte("o")),
		}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := cadRuntimeRunEvidence(scheduler.ExecutionResult{Jobs: tc.jobs}, tc.succeeded)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != nil {
				t.Fatalf("expected nil evidence, got %#v", got)
			}
		})
	}
}

func TestCADRuntimeRunEvidence_ExactlyOneEligibleCandidate_CapturesExactBytes(t *testing.T) {
	// Deliberately non-canonical formatting (whitespace/indentation/key
	// order) so a re-serializing implementation would fail this comparison.
	result := []byte("{\n  \"status\":   \"succeeded\",\n \"schemaVersion\": \"1.0\"\n}\n")
	verification := []byte("{\"expected\":{\n\"parameters\":[]  }}\n")
	observed := []byte("{  \"workingCopy\":{\"path\":\"x\"} }")
	outcome := cadRuntimeEvidenceOutcome(t, "job-1", "widget", "2", result, verification, observed)
	execution := scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{CADRuntimeOutcome: outcome}}}

	got, err := cadRuntimeRunEvidence(execution, true)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil evidence for exactly one eligible candidate")
	}
	if !bytes.Equal(got.Result, result) {
		t.Fatalf("Result bytes differ\nwant: %q\ngot:  %q", result, got.Result)
	}
	if !bytes.Equal(got.Verification, verification) {
		t.Fatalf("Verification bytes differ\nwant: %q\ngot:  %q", verification, got.Verification)
	}
	if !bytes.Equal(got.Observed, observed) {
		t.Fatalf("Observed bytes differ\nwant: %q\ngot:  %q", observed, got.Observed)
	}
}

func TestCADRuntimeRunEvidence_AbsentOptionalEvidenceOmitted(t *testing.T) {
	outcome := cadRuntimeEvidenceOutcome(t, "job-1", "widget", "2", []byte("result-only"), nil, nil)
	execution := scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{CADRuntimeOutcome: outcome}}}

	got, err := cadRuntimeRunEvidence(execution, true)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !bytes.Equal(got.Result, []byte("result-only")) {
		t.Fatalf("expected result evidence, got %#v", got)
	}
	if len(got.Verification) != 0 || len(got.Observed) != 0 {
		t.Fatalf("expected absent optional evidence to stay empty, got verification=%q observed=%q", got.Verification, got.Observed)
	}
}

func TestCADRuntimeRunEvidence_MultipleEligibleCandidatesOmitProjection(t *testing.T) {
	execution := scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{
		{CADRuntimeOutcome: cadRuntimeEvidenceOutcome(t, "job-1", "widget-a", "2", []byte("a"), []byte("a"), []byte("a"))},
		{CADRuntimeOutcome: cadRuntimeEvidenceOutcome(t, "job-1", "widget-b", "2", []byte("b"), []byte("b"), []byte("b"))},
	}}
	got, err := cadRuntimeRunEvidence(execution, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil projection for multiple eligible candidates (no arbitrary selection), got %#v", got)
	}
}

func TestCADRuntimeRunEvidence_PropagatesPathSafetyErrors(t *testing.T) {
	outcome := cadRuntimeEvidenceOutcome(t, "job-1", "widget", "2", []byte("r"), nil, nil)
	// Point ResultPath outside the declared working copy: the guarded reader
	// must reject this rather than silently reading across the boundary.
	outside := filepath.Join(t.TempDir(), "prm.result.json")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome.ResultPath = outside
	execution := scheduler.ExecutionResult{Jobs: []scheduler.JobExecution{{CADRuntimeOutcome: outcome}}}

	got, err := cadRuntimeRunEvidence(execution, true)
	if err == nil || got != nil {
		t.Fatalf("got=%#v err=%v, want a path-safety error and no evidence", got, err)
	}
}

func parsePlanForCacheTest(t *testing.T, dslContent string) (*dsl.AST, *planner.ExecutionPlan) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "cache_test.dsl")
	if err := os.WriteFile(path, []byte(withVersionHeader(dslContent)), 0644); err != nil {
		t.Fatalf("failed to write DSL: %v", err)
	}

	ast, err := dsl.Parse(path)
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("failed to validate DSL: %v", err)
	}

	plan, err := planner.CreatePlan(ast, map[string]string{})
	if err != nil {
		t.Fatalf("failed to create plan: %v", err)
	}
	return ast, plan
}

func chdirToTemp(t *testing.T) {
	t.Helper()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("failed to chdir to temp: %v", err)
	}

	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})
}

type cliExecutionWithTables struct {
	result        *executionResult
	planned       *plannedRun
	metadataBytes []byte
}

type cliProjectExecution struct {
	result        *executionResult
	planned       *plannedRun
	metadata      metadata.Metadata
	metadataBytes []byte
	report        report.Report
	reportBytes   []byte
	stdout        string
}

type failedCLIProjectExecution struct {
	result  *executionResult
	planned *plannedRun
	err     error
}

func runCLIExecutionWithTables(t *testing.T, dslPath string, outDir string, tables []string) cliExecutionWithTables {
	t.Helper()

	resetGlobals()
	tableInputs = append([]string(nil), tables...)

	planned, err := loadPlannedRun(dslPath, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun failed: %v", err)
	}

	result, err := executePlanRun(executionOptions{
		Planned:   planned,
		OutputDir: outDir,
		UseCache:  true,
	})
	if err != nil {
		t.Fatalf("executePlanRun failed: %v", err)
	}

	metadataBytes, err := os.ReadFile(filepath.Join(result.RunRoot, metadata.FileName))
	if err != nil {
		t.Fatalf("failed to read %s: %v", metadata.FileName, err)
	}

	return cliExecutionWithTables{
		result:        result,
		planned:       planned,
		metadataBytes: metadataBytes,
	}
}

func runCLIExecutionForProject(t *testing.T, projectPath string, outDir string) cliProjectExecution {
	t.Helper()
	installControlledAlignedRuntime(t, "success")

	resetGlobals()

	planned, err := loadPlannedRun(projectPath, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun failed: %v", err)
	}

	result, err := executePlanRun(executionOptions{
		Planned:   planned,
		OutputDir: outDir,
		UseCache:  true,
	})
	if err != nil {
		t.Fatalf("executePlanRun failed: %v", err)
	}

	metadataBytes, err := os.ReadFile(filepath.Join(result.RunRoot, metadata.FileName))
	if err != nil {
		t.Fatalf("failed to read %s: %v", metadata.FileName, err)
	}

	var runMetadata metadata.Metadata
	if err := json.Unmarshal(metadataBytes, &runMetadata); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", metadata.FileName, err)
	}

	reportBytes, err := os.ReadFile(filepath.Join(result.RunRoot, report.FileName))
	if err != nil {
		t.Fatalf("failed to read %s: %v", report.FileName, err)
	}

	var runReport report.Report
	if err := json.Unmarshal(reportBytes, &runReport); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", report.FileName, err)
	}

	return cliProjectExecution{
		result:        result,
		planned:       planned,
		metadata:      runMetadata,
		metadataBytes: metadataBytes,
		report:        runReport,
		reportBytes:   reportBytes,
	}
}

func runFailedCLIExecutionForProject(t *testing.T, projectPath string, outDir string) failedCLIProjectExecution {
	t.Helper()
	installControlledAlignedRuntime(t, "failure")

	resetGlobals()

	planned, err := loadPlannedRun(projectPath, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun failed: %v", err)
	}

	result, err := executePlanRun(executionOptions{
		Planned:   planned,
		OutputDir: outDir,
		UseCache:  true,
	})
	if result == nil {
		t.Fatalf("executePlanRun failed without result: %v", err)
	}

	return failedCLIProjectExecution{
		result:  result,
		planned: planned,
		err:     err,
	}
}

func assertLayerKeysEqual(t *testing.T, want map[cache.CacheLayer]string, got map[cache.CacheLayer]string) {
	t.Helper()

	for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
		if want[layer] != got[layer] {
			t.Fatalf("expected identical key for layer %s: %q vs %q", layer, want[layer], got[layer])
		}
	}
}

func assertLayerMarkersExist(t *testing.T, layerKeys map[cache.CacheLayer]string) {
	t.Helper()

	for _, layer := range []cache.CacheLayer{cache.CacheGeometry, cache.CacheArtifact, cache.CacheMetadata} {
		path := filepath.Join(".cache", string(layer), layerKeys[layer]+".done")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected cache marker for layer %s at %s: %v", layer, path, err)
		}
	}
}

type cliRecordPackageManifest struct {
	PackageKey  string `json:"packageKey"`
	Records     []cliRecordPackageManifestRecord
	RawEvidence []cliRecordPackageManifestRawEntry `json:"rawEvidence,omitempty"`
}

type cliRecordPackageManifestRecord struct {
	Family       string `json:"family"`
	ContractPath string `json:"contractPath"`
	RecordKey    string `json:"recordKey"`
	IdentityID   string `json:"identityId"`
}

type cliRecordPackageManifestRawEntry struct {
	ContractPath string `json:"contractPath"`
}

func recordPackageRoot(runRoot string) string {
	return filepath.Join(runRoot, recordpackage.PackageDirectoryName)
}

func assertPackageFileExists(t *testing.T, packageRoot string, contractPath string) {
	t.Helper()

	info, err := os.Stat(filepath.Join(packageRoot, filepath.FromSlash(contractPath)))
	if err != nil {
		t.Fatalf("expected package file %q: %v", contractPath, err)
	}
	if info.IsDir() {
		t.Fatalf("expected package path %q to be a file", contractPath)
	}
}

func assertRegularFile(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected file %q: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("expected %q to be a file", path)
	}
}

func readCLIRecordPackageManifest(t *testing.T, packageRoot string) cliRecordPackageManifest {
	t.Helper()

	var manifest cliRecordPackageManifest
	data, err := os.ReadFile(filepath.Join(packageRoot, filepath.FromSlash(recordpackage.PackageManifestContractPath())))
	if err != nil {
		t.Fatalf("failed to read package manifest: %v", err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("failed to unmarshal package manifest: %v\n%s", err, string(data))
	}
	return manifest
}

func readCLIFailureRecord(t *testing.T, packageRoot string) recordcontract.FailureRecord {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(packageRoot, filepath.FromSlash(recordpackage.MustRecordContractPath("failure"))))
	if err != nil {
		t.Fatalf("failed to read failure record: %v", err)
	}
	var failure recordcontract.FailureRecord
	if err := json.Unmarshal(data, &failure); err != nil {
		t.Fatalf("failed to unmarshal failure record: %v\n%s", err, string(data))
	}
	return failure
}

func assertManifestHasRecord(t *testing.T, manifest cliRecordPackageManifest, family string, contractPath string, recordKey string) {
	t.Helper()

	for _, record := range manifest.Records {
		if record.Family == family && record.ContractPath == contractPath && record.RecordKey == recordKey && record.IdentityID != "" {
			return
		}
	}
	t.Fatalf("manifest missing record family=%q contractPath=%q recordKey=%q in %#v", family, contractPath, recordKey, manifest.Records)
}

func hasFailureRecordEvidence(record recordcontract.FailureRecord, sourceKind string, sourceRef string) bool {
	for _, evidence := range record.Failure.Evidence {
		if evidence.SourceKind == sourceKind && evidence.SourceRef == sourceRef {
			return true
		}
	}
	return false
}

func assertManifestRawEvidencePaths(t *testing.T, manifest cliRecordPackageManifest, want []string) {
	t.Helper()

	got := make([]string, len(manifest.RawEvidence))
	for i, item := range manifest.RawEvidence {
		got[i] = item.ContractPath
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("raw evidence manifest paths = %#v, want %#v", got, want)
	}
}

func assertManifestOrdering(t *testing.T, manifest cliRecordPackageManifest) {
	t.Helper()

	for i := 1; i < len(manifest.Records); i++ {
		if manifest.Records[i-1].ContractPath > manifest.Records[i].ContractPath {
			t.Fatalf("manifest record entries are not deterministic by contract path: %#v", manifest.Records)
		}
	}

	wantRawPrefix := []string{
		recordpackage.RawReportContractPath(),
		recordpackage.RawMetadataContractPath(),
		recordpackage.RawArtifactStoreManifestContractPath(),
	}
	for i, want := range wantRawPrefix {
		if i >= len(manifest.RawEvidence) {
			t.Fatalf("raw evidence manifest paths missing %q in %#v", want, manifest.RawEvidence)
		}
		if manifest.RawEvidence[i].ContractPath != want {
			t.Fatalf("raw evidence path %d = %q, want %q", i, manifest.RawEvidence[i].ContractPath, want)
		}
	}
}

func readRecordPackageFiles(t *testing.T, packageRoot string) map[string][]byte {
	t.Helper()

	files := map[string][]byte{}
	err := filepath.WalkDir(packageRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(packageRoot, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatalf("failed to read record package tree %q: %v", packageRoot, err)
	}
	return files
}

func deterministicRecordPackagePaths(t *testing.T, files map[string][]byte) []string {
	t.Helper()

	paths := make([]string, 0, len(files))
	for path := range files {
		if path == recordpackage.PackageManifestContractPath() || strings.HasPrefix(path, recordpackage.RecordsDirectoryContractPath()+"/") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) < 2 {
		t.Fatalf("expected manifest and at least one normalized record in package files, got %#v", sortedPackagePaths(files))
	}
	return paths
}

func assertSamePackageFileSet(t *testing.T, first map[string][]byte, second map[string][]byte) {
	t.Helper()

	firstPaths := sortedPackagePaths(first)
	secondPaths := sortedPackagePaths(second)
	if !reflect.DeepEqual(firstPaths, secondPaths) {
		t.Fatalf("record package file sets differ\nfirst only: %#v\nsecond only: %#v", difference(firstPaths, secondPaths), difference(secondPaths, firstPaths))
	}
}

func sortedPackagePaths(files map[string][]byte) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func difference(left []string, right []string) []string {
	rightSet := make(map[string]struct{}, len(right))
	for _, path := range right {
		rightSet[path] = struct{}{}
	}
	var out []string
	for _, path := range left {
		if _, ok := rightSet[path]; !ok {
			out = append(out, path)
		}
	}
	return out
}

func assertPackageFileBytesEqual(t *testing.T, contractPath string, first []byte, second []byte) {
	t.Helper()

	if bytes.Equal(first, second) {
		return
	}
	if strings.HasSuffix(contractPath, ".json") {
		firstNormalized, firstErr := normalizeJSONBytes(first)
		secondNormalized, secondErr := normalizeJSONBytes(second)
		if firstErr == nil && secondErr == nil {
			t.Fatalf("record package file %q bytes differ\nfirst:\n%s\nsecond:\n%s", contractPath, string(firstNormalized), string(secondNormalized))
		}
	}
	t.Fatalf("record package file %q bytes differ: first %d bytes, second %d bytes", contractPath, len(first), len(second))
}

func writeProjectExecutionFixture(t *testing.T) string {
	t.Helper()

	projectDir := t.TempDir()
	modelDir := filepath.Join(projectDir, "models")
	tableDir := filepath.Join(projectDir, "tables")
	for _, dir := range []string{modelDir, tableDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create fixture dir %s: %v", dir, err)
		}
	}

	if err := os.WriteFile(filepath.Join(modelDir, "box.FCStd"), []byte("model-a"), 0o644); err != nil {
		t.Fatalf("failed to write box model: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "cover.FCStd"), []byte("model-b"), 0o644); err != nil {
		t.Fatalf("failed to write cover model: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tableDir, "fasteners.json"), []byte(`{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true},
    {"name": "label", "type": "string", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8, "label": "M8 bolt"}
  ]
}`), 0o644); err != nil {
		t.Fatalf("failed to write fasteners table: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tableDir, "labels.json"), []byte(`{
  "schemaVersion": "1.0",
  "name": "label_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "label", "type": "string", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "label": "M8 bolt"}
  ]
}`), 0o644); err != nil {
		t.Fatalf("failed to write labels table: %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "project.dsl"), []byte(withVersionHeader(`
const PRIMARY_MODEL = "box_model"

product Widget {
    adapter = "freecad"
    source_model = PRIMARY_MODEL
    outputs = ["step"]

    param sku: string = "M8x20"
    param fastener_label: string = table_cell("fasteners", sku, "label")
    param label: string = table_cell("labels", sku, "label")
}
`)), 0o644); err != nil {
		t.Fatalf("failed to write project DSL: %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "parametron.project.json"), []byte(`{
  "version": "1.0",
  "projectId": "project-run-fixture",
  "dsl": "./project.dsl",
  "resources": {
    "models": {
      "cover_model": "./models/cover.FCStd",
      "box_model": "./models/box.FCStd"
    }
  },
  "tables": {
    "labels": "./tables/labels.json",
    "fasteners": "./tables/fasteners.json"
  }
}`), 0o644); err != nil {
		t.Fatalf("failed to write project map: %v", err)
	}

	return projectDir
}

func setPathWithoutFreeCAD(t *testing.T) {
	t.Helper()

	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("failed to resolve python3 before isolating PATH: %v", err)
	}
	binDir := t.TempDir()
	if err := os.Symlink(pythonPath, filepath.Join(binDir, "python3")); err != nil {
		t.Fatalf("failed to create isolated python3 shim: %v", err)
	}

	t.Setenv("PATH", binDir)
}

func installControlledAlignedRuntime(t *testing.T, mode string) string {
	t.Helper()
	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("resolve python3 for controlled aligned runtime: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "parametron-freecad")
	script := `#!` + pythonPath + `
import hashlib
import json
import os
import sys

def arg(name):
    i = sys.argv.index(name)
    return sys.argv[i + 1]

working = os.path.abspath(arg("--working-copy"))
manifest_path = arg("--manifest")
result_path = arg("--result")
output_dir = arg("--output-dir")
request_path = arg("--observation-request")
mode = os.environ.get("PARAMETRON_TASK13_RUNTIME_MODE", "success")

with open(manifest_path, "r", encoding="utf-8") as handle:
    manifest = json.load(handle)
with open(request_path, "r", encoding="utf-8") as handle:
    contract = json.load(handle)

if mode == "blocking":
    import signal
    signal.pause()

if mode == "malformed_result":
    with open(result_path, "w", encoding="utf-8") as handle:
        handle.write("{")
    sys.exit(0)

if mode == "failure":
    result = {
        "schemaVersion": "1.0",
        "status": "failed",
        "failure": {
            "boundary": "freecad",
            "category": "runtime",
            "code": "controlled_failure",
            "message": "intentional controlled aligned runtime failure",
            "stage": "execute"
        }
    }
    with open(result_path, "w", encoding="utf-8") as handle:
        json.dump(result, handle, sort_keys=True, separators=(",", ":"))
        handle.write("\n")
    sys.exit(17)

artifacts = []
for output in manifest.get("outputs", []):
    relative = output["path"]
    if mode == "artifact_missing" and not artifacts:
        artifacts.append({"id": output["id"], "format": output["format"], "path": relative})
        continue
    target = os.path.join(working, *relative.split("/"))
    os.makedirs(os.path.dirname(target), exist_ok=True)
    with open(target, "w", encoding="utf-8") as handle:
        handle.write(output["format"] + "\n")
    artifacts.append({"id": output["id"], "format": output["format"], "path": relative})

source = manifest["sourceDocument"]
source_path = source if os.path.isabs(source) else os.path.join(working, source)
with open(source_path, "rb") as handle:
    digest = hashlib.sha256(handle.read()).hexdigest()

expected = contract.get("expected", {})
bindings = {item["id"]: item for item in contract.get("observationContext", {}).get("parameters", [])}
parameters = []
for item in expected.get("parameters", []):
    binding = bindings.get(item["id"], {})
    parameters.append({
        "id": item["id"],
        "name": binding.get("name", item.get("name", item["id"])),
        "value": item["value"],
        "valueKind": "number"
    })
metadata = []
for item in expected.get("metadata", []):
    metadata.append({
        "id": item.get("id", item["key"]),
        "key": item["key"],
        "value": digest if item["key"] == "working_copy_sha256" else item["value"],
        "valueKind": item.get("valueKind", "string")
    })
references = [{"kind": item["kind"], "name": item["name"]} for item in expected.get("references", [])]
components = [{"id": item["id"], "kind": item["kind"], "name": item["name"]} for item in expected.get("components", [])]
observed = {
    "schemaVersion": "1.0",
    "workingCopy": {"path": working, "sha256": digest},
    "observation": {
        "parameters": parameters,
        "metadata": metadata,
        "references": references,
        "components": components
    }
}
if mode == "observed_mismatch":
    observed["workingCopy"]["sha256"] = "0" * 64
with open(os.path.join(output_dir, "prm.observed.json"), "w", encoding="utf-8") as handle:
    json.dump(observed, handle, sort_keys=True, separators=(",", ":"))
    handle.write("\n")

traversal_json = os.environ.get("PARAMETRON_TASK13_RUNTIME_TRAVERSAL_JSON", "")
if traversal_json:
    with open(os.path.join(output_dir, "prm.reference-traversal.json"), "w", encoding="utf-8") as handle:
        handle.write(traversal_json)

result = {"schemaVersion": "1.0", "status": "succeeded", "artifacts": artifacts}
with open(result_path, "w", encoding="utf-8") as handle:
    json.dump(result, handle, sort_keys=True, separators=(",", ":"))
    handle.write("\n")
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write controlled aligned runtime: %v", err)
	}
	t.Setenv("PARAMETRON_FREECAD_RUNTIME", path)
	t.Setenv("PARAMETRON_TASK13_RUNTIME_MODE", mode)
	return path
}

func assertProjectTableIDs(t *testing.T, got []metadata.TableInputMetadata, want ...string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("expected %d project tables in metadata, got %d", len(want), len(got))
	}

	for i, logicalID := range want {
		if got[i].LogicalID != logicalID {
			t.Fatalf("expected project table %d logical ID %q, got %q", i, logicalID, got[i].LogicalID)
		}
		if got[i].Fingerprint == "" {
			t.Fatalf("expected non-empty fingerprint for project table %q", logicalID)
		}
	}
}

func TestCLI_ProjectBasedExecution_RehearsalPass(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	projectFile := filepath.Join(projectDir, "parametron.project.json")
	lockPath := filepath.Join(projectDir, "parametron.lock.json")
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected fixture project to start without lock file, got err=%v", err)
	}

	dirRun := runRootProjectExecution(t, projectDir, filepath.Join(t.TempDir(), "dir-out"))
	if dirRun.result.Cached {
		t.Fatal("expected first root CLI project run to execute, but it reported a cache hit")
	}
	if dirRun.report.Status != report.StatusSuccess {
		t.Fatalf("expected successful report status, got %q", dirRun.report.Status)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected root CLI project run not to require or create %s, got err=%v", filepath.Base(lockPath), err)
	}

	for _, path := range []string{
		filepath.Join(dirRun.result.RunRoot, report.FileName),
		filepath.Join(dirRun.result.RunRoot, metadata.FileName),
		filepath.Join(dirRun.result.RunRoot, "manifest.json"),
		filepath.Join(dirRun.result.RunRoot, "products", "Widget", planner.ExportManifestFilename),
		filepath.Join(dirRun.result.RunRoot, "products", "Widget", "Widget.csv"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected rehearsal artifact %s: %v", path, err)
		}
	}
	stepMatches, err := filepath.Glob(filepath.Join(dirRun.result.RunRoot, "products", "Widget", "_working", "*", "outputs", "Widget.step"))
	if err != nil || len(stepMatches) != 1 {
		t.Fatalf("expected one aligned rehearsal STEP artifact, matches=%v err=%v", stepMatches, err)
	}
	if !strings.Contains(string(dirRun.reportBytes), `"status": "success"`) {
		t.Fatalf("expected success status in %s, got:\n%s", report.FileName, string(dirRun.reportBytes))
	}
	if dirRun.metadata.ProjectInputs == nil {
		t.Fatal("expected project-derived captured inputs in metadata")
	}
	if dirRun.metadata.ProjectInputs.DSL.Signature == "" {
		t.Fatal("expected captured project DSL signature in metadata")
	}
	if len(dirRun.metadata.ProjectInputs.Models) != 2 {
		t.Fatalf("expected 2 captured project models, got %d", len(dirRun.metadata.ProjectInputs.Models))
	}
	if len(dirRun.metadata.ProjectInputs.Tables) != 2 {
		t.Fatalf("expected 2 captured project tables, got %d", len(dirRun.metadata.ProjectInputs.Tables))
	}
	assertProjectTableIDs(t, dirRun.metadata.Tables, "fasteners", "labels")
	assertLayerMarkersExist(t, dirRun.result.LayerKeys)

	metadataInfoBefore, err := os.Stat(filepath.Join(dirRun.result.RunRoot, metadata.FileName))
	if err != nil {
		t.Fatalf("failed to stat %s before cache reuse check: %v", metadata.FileName, err)
	}
	reportInfoBefore, err := os.Stat(filepath.Join(dirRun.result.RunRoot, report.FileName))
	if err != nil {
		t.Fatalf("failed to stat %s before cache reuse check: %v", report.FileName, err)
	}

	cacheRepeat := runRootProjectExecution(t, projectDir, filepath.Dir(dirRun.result.RunRoot))
	metadataInfoAfter, err := os.Stat(filepath.Join(cacheRepeat.result.RunRoot, metadata.FileName))
	if err != nil {
		t.Fatalf("failed to stat %s after cache reuse check: %v", metadata.FileName, err)
	}
	reportInfoAfter, err := os.Stat(filepath.Join(cacheRepeat.result.RunRoot, report.FileName))
	if err != nil {
		t.Fatalf("failed to stat %s after cache reuse check: %v", report.FileName, err)
	}
	if !metadataInfoAfter.ModTime().Equal(metadataInfoBefore.ModTime()) {
		t.Fatalf("expected cache reuse to leave %s untouched: before=%s after=%s", metadata.FileName, metadataInfoBefore.ModTime(), metadataInfoAfter.ModTime())
	}
	if !reportInfoAfter.ModTime().Equal(reportInfoBefore.ModTime()) {
		t.Fatalf("expected cache reuse to leave %s untouched: before=%s after=%s", report.FileName, reportInfoBefore.ModTime(), reportInfoAfter.ModTime())
	}
	if !bytes.Equal(dirRun.metadataBytes, cacheRepeat.metadataBytes) {
		t.Fatalf("expected identical metadata bytes across cached project runs\nfirst:\n%s\nsecond:\n%s", string(dirRun.metadataBytes), string(cacheRepeat.metadataBytes))
	}
	assertLayerKeysEqual(t, dirRun.result.LayerKeys, cacheRepeat.result.LayerKeys)

	if err := os.RemoveAll(".cache"); err != nil {
		t.Fatalf("failed to clear cache before file-entrypoint equivalence run: %v", err)
	}
	fileRun := runRootProjectExecution(t, projectFile, filepath.Join(t.TempDir(), "file-out"))
	if fileRun.report.Status != report.StatusSuccess {
		t.Fatalf("expected successful report status via project file entrypoint, got %q", fileRun.report.Status)
	}
	if dirRun.planned.PlanHash != fileRun.planned.PlanHash {
		t.Fatalf("expected identical plan hashes across project entrypoints, got %q and %q", dirRun.planned.PlanHash, fileRun.planned.PlanHash)
	}
	dirCapture, err := json.Marshal(dirRun.metadata.ProjectInputs)
	if err != nil {
		t.Fatalf("marshal directory project inputs failed: %v", err)
	}
	fileCapture, err := json.Marshal(fileRun.metadata.ProjectInputs)
	if err != nil {
		t.Fatalf("marshal project file inputs failed: %v", err)
	}
	if !bytes.Equal(dirCapture, fileCapture) {
		t.Fatalf("expected identical captured project inputs across --project entrypoints\ndir:\n%s\nfile:\n%s", string(dirCapture), string(fileCapture))
	}

	if err := os.RemoveAll(".cache"); err != nil {
		t.Fatalf("failed to clear cache before invalidation checks: %v", err)
	}
	invalidationBase := runRootProjectExecution(t, projectDir, filepath.Join(t.TempDir(), "invalidate-out"))
	modelPath := filepath.Join(projectDir, "models", "box.FCStd")
	if err := os.WriteFile(modelPath, []byte("model-a-updated"), 0o644); err != nil {
		t.Fatalf("failed to update model fixture: %v", err)
	}
	modelChanged := runRootProjectExecution(t, projectDir, filepath.Join(t.TempDir(), "invalidate-out"))
	if invalidationBase.result.LayerKeys[cache.CacheGeometry] == modelChanged.result.LayerKeys[cache.CacheGeometry] {
		t.Fatal("expected project-mode geometry cache key to change after mapped model update")
	}
	if bytes.Equal(invalidationBase.metadataBytes, modelChanged.metadataBytes) {
		t.Fatalf("expected metadata to change after mapped model update\nbefore:\n%s\nafter:\n%s", string(invalidationBase.metadataBytes), string(modelChanged.metadataBytes))
	}

	tablePath := filepath.Join(projectDir, "tables", "labels.json")
	if err := os.WriteFile(tablePath, []byte(`{
  "schemaVersion": "1.0",
  "name": "label_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "label", "type": "string", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "label": "M8 bolt updated"}
  ]
}`), 0o644); err != nil {
		t.Fatalf("failed to update table fixture: %v", err)
	}
	tableChanged := runRootProjectExecution(t, projectDir, filepath.Join(t.TempDir(), "invalidate-out"))
	if modelChanged.result.LayerKeys[cache.CacheGeometry] == tableChanged.result.LayerKeys[cache.CacheGeometry] {
		t.Fatal("expected project-mode geometry cache key to change after mapped table update")
	}
	if bytes.Equal(modelChanged.metadataBytes, tableChanged.metadataBytes) {
		t.Fatalf("expected metadata to change after mapped table update\nbefore:\n%s\nafter:\n%s", string(modelChanged.metadataBytes), string(tableChanged.metadataBytes))
	}
}

func TestCLI_ProjectBasedExecution_RehearsalFailurePath(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	installControlledAlignedRuntime(t, "failure")

	outDir := filepath.Join(t.TempDir(), "out")
	_, err := executeRootCommand(t, []string{
		"--project", projectDir,
		"--out", outDir,
	})
	if err == nil {
		t.Fatal("expected project-mode root CLI execution to fail")
	}

	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun failed: %v", err)
	}
	runRoot := planner.BuildRunRoot(outDir, planned.PlanHash)

	reportBytes, err := os.ReadFile(filepath.Join(runRoot, report.FileName))
	if err != nil {
		t.Fatalf("expected deterministic %s for controlled project-mode failure: %v", report.FileName, err)
	}

	var runReport report.Report
	if err := json.Unmarshal(reportBytes, &runReport); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", report.FileName, err)
	}
	if runReport.Status != report.StatusFailed {
		t.Fatalf("expected failed report status, got %q", runReport.Status)
	}
	if runReport.Error == nil || strings.TrimSpace(runReport.Error.Message) == "" {
		t.Fatalf("expected non-empty error summary in report, got %#v", runReport.Error)
	}
}

func TestLoadPlannedRun_ProjectModeCaptureValidationRunsBeforePlanning(t *testing.T) {
	projectDir := writeCaptureBackedProjectFixture(t, captureBackedProjectFixtureOptions{
		dslParameters: []string{`    param width: number = 35`},
		captureParameters: []captureParameterFixture{
			{id: "par.root.length", name: "length", displayName: "Length"},
		},
	})

	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected capture-backed planning to fail")
	}
	if planned != nil {
		t.Fatalf("expected no planned run when capture validation fails, got %#v", planned)
	}
	if !strings.Contains(err.Error(), `product "Box" parameter "width" does not resolve to a semantic parameter by exact name`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCLI_ProjectModeCaptureValidationErrorSurfaceIsStableAcrossRuns(t *testing.T) {
	projectDir := writeCaptureBackedProjectFixture(t, captureBackedProjectFixtureOptions{
		dslParameters: []string{
			`    param length: number = 35`,
			`    param width: number = 12`,
		},
		captureParameters: []captureParameterFixture{
			{id: "par.root.length.b", name: "length", displayName: "Length B"},
			{id: "par.root.length.a", name: "length", displayName: "Length A"},
		},
	})

	args := []string{
		"--project", projectDir,
		"--print-plan",
	}

	_, firstVisibleError, firstErr := executeRootCommandWithVisibleError(t, args)
	if firstErr == nil {
		t.Fatal("expected first CLI validation run to fail")
	}

	_, secondVisibleError, secondErr := executeRootCommandWithVisibleError(t, args)
	if secondErr == nil {
		t.Fatal("expected second CLI validation run to fail")
	}

	if firstVisibleError != secondVisibleError {
		t.Fatalf("expected byte-stable CLI error output across repeated runs\nfirst:  %q\nsecond: %q", firstVisibleError, secondVisibleError)
	}
	if !strings.Contains(firstVisibleError, `product "Box" parameter "length" is ambiguous across semantic parameters ["par.root.length.a" "par.root.length.b"]`) {
		t.Fatalf("expected stable ambiguous reference text in CLI output, got %q", firstVisibleError)
	}
	if !strings.Contains(firstVisibleError, `product "Box" parameter "width" does not resolve to a semantic parameter by exact name`) {
		t.Fatalf("expected stable unknown reference text in CLI output, got %q", firstVisibleError)
	}
}

func TestLoadPlannedRun_ProjectModeCaptureValidationAcceptsExactMatch(t *testing.T) {
	projectDir := writeCaptureBackedProjectFixture(t, captureBackedProjectFixtureOptions{
		dslParameters: []string{`    param length: number = 35`},
		captureParameters: []captureParameterFixture{
			{id: "par.root.length", name: "length", displayName: "Length"},
		},
	})

	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun returned error: %v", err)
	}

	manifestPayload := firstManifestPayloadFromPlan(t, planned.Plan)
	if len(manifestPayload.ParameterAssignments) != 1 || manifestPayload.ParameterAssignments[0].Name != "length" {
		t.Fatalf("unexpected parameter assignments: %#v", manifestPayload.ParameterAssignments)
	}
}

func TestLoadPlannedRun_ProjectModeCaptureProjectionUsesSemanticOutputAndMutationBoundary(t *testing.T) {
	projectDir := writeCaptureBackedProjectFixture(t, captureBackedProjectFixtureOptions{
		dslParameters: []string{`    param length: number = 35`},
		captureParameters: []captureParameterFixture{
			{id: "par.root.length", name: "length", displayName: "Length"},
		},
	})

	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun returned error: %v", err)
	}

	manifestPayload := firstManifestPayloadFromPlan(t, planned.Plan)
	wantOutputs := []planner.ExportManifestOutput{
		{Type: "step", Filename: "outputs/Box.step", Object: "RootAssembly"},
	}
	if !reflect.DeepEqual(manifestPayload.Outputs, wantOutputs) {
		t.Fatalf("unexpected projected outputs\nwant: %#v\ngot:  %#v", wantOutputs, manifestPayload.Outputs)
	}
	if manifestPayload.AssemblyMutations == nil {
		t.Fatal("expected assembly mutations from semantic projection")
	}
	wantAssemblyParameters := []planner.ExportManifestParameterMutation{
		{Object: "RootAssembly", Property: "length", ValueParam: "length", Type: "number", Unit: "mm"},
	}
	if !reflect.DeepEqual(manifestPayload.AssemblyMutations.Parameters, wantAssemblyParameters) {
		t.Fatalf("unexpected assembly parameter mutations\nwant: %#v\ngot:  %#v", wantAssemblyParameters, manifestPayload.AssemblyMutations.Parameters)
	}
	if manifestPayload.PartMutations != nil {
		t.Fatalf("expected no part mutations for root assembly parameter, got %#v", manifestPayload.PartMutations)
	}
	if len(manifestPayload.ParameterAssignments) != 1 || manifestPayload.ParameterAssignments[0].Name != "length" {
		t.Fatalf("unexpected parameter assignments: %#v", manifestPayload.ParameterAssignments)
	}
	if manifestPayload.Outputs[0].Object == "Body" {
		t.Fatalf("expected semantic projection to replace legacy adapter default object, got %#v", manifestPayload.Outputs)
	}
}

func TestLoadPlannedRun_ProjectModeCaptureValidationFailsOnSourceModelSemanticMismatch(t *testing.T) {
	projectDir := writeCaptureBackedProjectFixture(t, captureBackedProjectFixtureOptions{
		dslSourceModelLogicalID: "other_model",
		captureParameters: []captureParameterFixture{
			{id: "par.root.length", name: "length", displayName: "Length"},
		},
		dslParameters: []string{`    param length: number = 35`},
	})

	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected capture-backed planning to fail on source_model mismatch")
	}
	if planned != nil {
		t.Fatalf("expected no planned run when semantic source_model validation fails, got %#v", planned)
	}
	if !strings.Contains(err.Error(), `product "Box" source_model "other_model" does not match semantic sourceDocumentLogicalId "box_model"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPlannedRun_ProjectModeCapturePlanningDeterministicAcrossRuns(t *testing.T) {
	projectDir := writeCaptureBackedProjectFixture(t, captureBackedProjectFixtureOptions{
		dslParameters: []string{`    param length: number = 35`},
		captureParameters: []captureParameterFixture{
			{id: "par.root.length", name: "length", displayName: "Length"},
		},
	})

	first, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("first loadPlannedRun returned error: %v", err)
	}
	second, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("second loadPlannedRun returned error: %v", err)
	}

	firstJSON, err := json.Marshal(firstManifestPayloadFromPlan(t, first.Plan))
	if err != nil {
		t.Fatalf("failed to marshal first manifest payload: %v", err)
	}
	secondJSON, err := json.Marshal(firstManifestPayloadFromPlan(t, second.Plan))
	if err != nil {
		t.Fatalf("failed to marshal second manifest payload: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("expected deterministic capture-backed manifest JSON bytes\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}

func TestLoadPlannedRun_ProjectModeCaptureProjectionNoLongerFallsBackToDSLValueAlone(t *testing.T) {
	projectDir := writeCaptureBackedProjectFixture(t, captureBackedProjectFixtureOptions{
		dslParameters: []string{`    param ratio: number = 35`},
		captureParameters: []captureParameterFixture{
			{id: "par.root.ratio", name: "ratio", displayName: "Ratio", nativeType: "Float", unit: "none"},
		},
	})

	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected capture-backed projection to fail for non-projectable semantic parameter")
	}
	if planned != nil {
		t.Fatalf("expected no planned run on non-projectable capture-backed projection, got %#v", planned)
	}
	if !strings.Contains(err.Error(), `semantic type "dimensionless" unit "none" is not projectable`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPlannedRun_ProjectModeCaptureProjectionFailsWithoutSemanticMap(t *testing.T) {
	writeSemanticMap := false
	projectDir := writeCaptureBackedProjectFixture(t, captureBackedProjectFixtureOptions{
		dslParameters: []string{`    param length: number = 35`},
		captureParameters: []captureParameterFixture{
			{id: "par.root.length", name: "length", displayName: "Length"},
		},
		writeSemanticMap: &writeSemanticMap,
	})

	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected capture-backed planning to fail without semantic map")
	}
	if planned != nil {
		t.Fatalf("expected no planned run without semantic map, got %#v", planned)
	}
	if !strings.Contains(err.Error(), "capture-backed manifest generation requires semantic map") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPlannedRun_ProjectModeCaptureProjectionFailsBeforeManifestGenerationWhenContractNotReady(t *testing.T) {
	breakMapPath := filepath.Join("..", "..", "internal", "engine", "semanticmap", "testdata", "break", "projection-review-missing-output", "parametron.semantic-map.json")
	breakMap, err := os.ReadFile(breakMapPath)
	if err != nil {
		t.Fatalf("failed to read break semantic map fixture: %v", err)
	}

	projectDir := writeCaptureBackedProjectFixture(t, captureBackedProjectFixtureOptions{
		dslParameters: []string{`    param length: number = 35`},
		captureParameters: []captureParameterFixture{
			{id: "par.root.length", name: "length", displayName: "Length"},
		},
		semanticMapJSON: string(breakMap),
	})

	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected capture-backed planning to fail when projection contract is not ready")
	}
	if planned != nil {
		t.Fatalf("expected no planned run when projection contract is not ready, got %#v", planned)
	}
	if !strings.Contains(err.Error(), `semanticToManifest.outputs must include manifestType "pdf"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPlannedRun_StandaloneDSLUnchangedWithoutCapture(t *testing.T) {
	dslPath := writeDSLFixture(t, `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step"]

    param label: string = "box"
}
`)

	planned, err := loadPlannedRun(dslPath, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun returned error: %v", err)
	}
	if planned == nil || planned.Plan == nil {
		t.Fatalf("expected standalone planning to remain unchanged, got %#v", planned)
	}
}

func TestLoadPlannedRun_ProjectModeUnchangedWithoutCaptureContract(t *testing.T) {
	projectDir := writeCaptureBackedProjectFixture(t, captureBackedProjectFixtureOptions{
		dslParameters: []string{`    param label: string = "box"`},
	})

	capturePath := filepath.Join(projectDir, "parametron.cad.json")
	if err := os.Remove(capturePath); err != nil {
		t.Fatalf("failed to remove capture contract: %v", err)
	}

	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("expected project mode without capture contract to remain unchanged, got %v", err)
	}
	if planned == nil || planned.Plan == nil {
		t.Fatalf("expected planned run without capture contract, got %#v", planned)
	}

	manifestPayload := firstManifestPayloadFromPlan(t, planned.Plan)
	if len(manifestPayload.ParameterAssignments) != 0 {
		t.Fatalf("expected no parameter assignments for string params without capture contract: %#v", manifestPayload.ParameterAssignments)
	}
}

type captureParameterFixture struct {
	id          string
	name        string
	displayName string
	nativeType  string
	unit        string
}

type captureBackedProjectFixtureOptions struct {
	sourceModelLogicalID    string
	dslSourceModelLogicalID string
	dslParameters           []string
	captureParameters       []captureParameterFixture
	semanticMapJSON         string
	writeSemanticMap        *bool
}

func writeCaptureBackedProjectFixture(t *testing.T, opts captureBackedProjectFixtureOptions) string {
	t.Helper()

	projectDir := t.TempDir()
	projectDSL := filepath.Join(projectDir, "project.dsl")
	modelDir := filepath.Join(projectDir, "input")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}

	modelPath := filepath.Join(modelDir, "box.FCStd")
	if err := os.WriteFile(modelPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("failed to write model fixture: %v", err)
	}

	sourceModelLogicalID := opts.sourceModelLogicalID
	if sourceModelLogicalID == "" {
		sourceModelLogicalID = "box_model"
	}
	dslSourceModelLogicalID := opts.dslSourceModelLogicalID
	if dslSourceModelLogicalID == "" {
		dslSourceModelLogicalID = "box_model"
	}

	dslBody := strings.Join(opts.dslParameters, "\n")
	if err := os.WriteFile(projectDSL, []byte(withVersionHeader(`
product Box {
    adapter = "freecad"
    source_model = "`+dslSourceModelLogicalID+`"
    outputs = ["step"]
`+"\n"+dslBody+`
}
`)), 0o644); err != nil {
		t.Fatalf("failed to write project DSL: %v", err)
	}

	projectMap := filepath.Join(projectDir, "parametron.project.json")
	if err := os.WriteFile(projectMap, []byte(`{
  "version": "1.0",
  "projectId": "capture-backed-project",
  "dsl": "project.dsl",
  "resources": {
    "models": {
      "box_model": "input/box.FCStd"
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("failed to write project map: %v", err)
	}

	writeSemanticMap := true
	if opts.writeSemanticMap != nil {
		writeSemanticMap = *opts.writeSemanticMap
	}
	if writeSemanticMap {
		semanticMapJSON := opts.semanticMapJSON
		if semanticMapJSON == "" {
			semanticMapJSON = defaultCaptureBackedSemanticMapJSON()
		}
		if err := os.WriteFile(filepath.Join(projectDir, "parametron.semantic-map.json"), []byte(semanticMapJSON), 0o644); err != nil {
			t.Fatalf("failed to write semantic map fixture: %v", err)
		}
	}

	parametersJSON := make([]string, 0, len(opts.captureParameters))
	for _, parameter := range opts.captureParameters {
		displayName := parameter.displayName
		if displayName == "" {
			displayName = parameter.name
		}
		nativeType := parameter.nativeType
		if nativeType == "" {
			nativeType = "Length"
		}
		unit := parameter.unit
		if unit == "" {
			unit = "mm"
		}
		parametersJSON = append(parametersJSON, `{
        "id": "`+parameter.id+`",
        "ownerKind": "component",
        "ownerId": "cmp.root",
        "componentId": "cmp.root",
        "name": "`+parameter.name+`",
        "displayName": "`+displayName+`",
        "cadType": "`+nativeType+`",
        "valueType": "number",
        "observable": true,
        "writable": true,
        "currentValue": 35,
        "unit": "`+unit+`",
        "stabilityClass": "stable",
        "annotations": {
          "description": "",
          "comment": "",
          "purpose": ""
        }
      }`)
	}

	capturePath := filepath.Join(projectDir, "parametron.cad.json")
	if err := os.WriteFile(capturePath, []byte(`{
  "schemaVersion": "1.0",
  "captureId": "cap.project",
  "adapter": {
    "name": "freecad",
    "version": ""
  },
  "cadSystem": {
    "name": "FreeCAD",
    "version": ""
  },
  "sourceDocument": {
    "logicalId": "`+sourceModelLogicalID+`",
    "path": "input/box.FCStd",
    "fingerprint": "sha256:fixture"
  },
  "rootProduct": {
    "id": "cmp.root"
  },
  "annotations": {
    "description": "",
    "comment": "",
    "purpose": ""
  },
  "entities": {
    "components": [
      {
        "id": "cmp.root",
        "kind": "assembly",
        "name": "RootAssembly",
        "displayName": "Root Assembly",
        "cadType": "App::Part",
        "quantity": 1,
        "material": "",
        "identitySource": {
          "kind": "parent_scoped_path",
          "path": "cmp.root"
        },
        "stabilityClass": "stable",
        "targetability": {
          "suppress": false,
          "unsuppress": false,
          "hide": true,
          "unhide": true,
          "delete": false
        },
        "annotations": {
          "description": "",
          "comment": "",
          "purpose": ""
        }
      }
    ],
    "features": [],
    "relationships": [],
    "parameterGroups": [],
    "parameters": [
`+strings.Join(parametersJSON, ",\n")+`
    ],
    "metadata": []
  },
  "structure": {
    "rootComponentId": "cmp.root",
    "nodes": [
      {
        "componentId": "cmp.root",
        "parentComponentId": "",
        "children": []
      }
    ]
  }
}`), 0o644); err != nil {
		t.Fatalf("failed to write capture contract: %v", err)
	}

	return projectDir
}

func defaultCaptureBackedSemanticMapJSON() string {
	return `{
  "schemaVersion": "1.0",
  "mappingId": "freecad-default-v1",
  "adapter": "freecad",
  "cadSystem": {
    "name": "FreeCAD",
    "minVersion": "1.1.0"
  },
  "semanticTypes": {
    "parameterTypes": {
      "length": {
        "valueKind": "number",
        "defaultUnit": "mm",
        "coercion": "number_to_length"
      },
      "count": {
        "valueKind": "integer",
        "defaultUnit": "count",
        "coercion": "number_to_integer_exact"
      },
      "dimensionless": {
        "valueKind": "number",
        "defaultUnit": "none",
        "coercion": "number_to_dimensionless"
      }
    },
    "resolutionPolicy": {
      "precedence": [
        "explicit_parameter_mapping",
        "capture_native_type_mapping"
      ],
      "allowEngineDefault": false,
      "allowEngineInference": false
    }
  },
  "captureToSemantic": {
    "parameterGroups": [
      {
        "captureKind": "parameter_group",
        "cadContainerType": "VarSet",
        "semanticKind": "parameter_group"
      }
    ],
    "parameters": [
      {
        "captureKind": "parameter",
        "semanticKind": "parameter",
        "identity": "capture.id",
        "name": "capture.name",
        "group": "capture.groupId",
        "semanticType": {
          "from": "capture.nativeType",
          "map": {
            "Length": "length",
            "Integer": "count",
            "Float": "dimensionless"
          }
        }
      }
    ],
    "components": [
      {
        "captureKind": "component",
        "semanticKinds": {
          "assembly": "assembly",
          "part": "part"
        },
        "identity": "capture.id",
        "targetability": "capture.targetability"
      }
    ],
    "metadata": [
      {
        "captureKind": "metadata",
        "semanticKind": "metadata",
        "identity": "capture.id",
        "key": "capture.key",
        "valueKind": "capture.nativeType"
      }
    ]
  },
  "semanticToManifest": {
    "parameters": {
      "target": "parameterAssignments",
      "name": "semantic.parameter.name",
      "value": "resolved.value",
      "type": "number",
      "unit": "resolved.unit"
    },
    "outputs": {
      "step": {
        "manifestType": "step",
        "targetField": "object",
        "targetSource": "semantic.output.target.manifestName"
      },
      "csv": {
        "manifestType": "csv",
        "targetField": "target",
        "targetSource": "semantic.output.target.manifestName",
        "scopeField": "rollup",
        "scopeMap": {
          "flat": "flat",
          "assembly": "assembly",
          "subtree": "assembly"
        }
      },
      "pdf": {
        "manifestType": "pdf"
      }
    },
    "mutations": {
      "set_parameter": {
        "manifestCollection": "partMutations.parameters",
        "targetField": "object",
        "propertyField": "property",
        "valueField": "valueParam"
      },
      "suppress": {
        "manifestCollection": "partMutations.suppression",
        "targetField": "object",
        "valueField": "suppressed",
        "value": true
      },
      "set_property": {
        "manifestCollection": "partMutations.properties",
        "targetField": "object",
        "propertyField": "property",
        "valueField": "value"
      }
    }
  },
  "overridePolicy": {
    "allowAdapterOverrides": false,
    "allowProjectOverrides": false,
    "allowUserOverrides": false
  },
  "operationCapabilities": {
    "write_parameter": {
      "requiresWritable": true,
      "allowedSemanticTargets": [
        "parameter"
      ]
    }
  },
  "determinism": {
    "allowImplicitFallback": false,
    "allowCaseInsensitiveMatch": false,
    "allowDisplayNameIdentity": false,
    "allowAdapterInference": false
  }
}`
}
