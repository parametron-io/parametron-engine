package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/runtimecap"
)

// Phase 5 Task 13 Stage 2 permanent handoff-contract matrix: the root CLI
// `--json-plan` path carries a normal planner.ExecutionPlan (never Task 8's
// private semantic.MutationIntent slice), and the mutation-bearing
// planner.WriteExportManifestPayload it exposes projects byte-identically
// (modulo the legitimate attempt-local sourceDocument rewrite) into both the
// product-level and attempt-local aligned FreeCAD runtime manifests.
//
// Every test in this file drives the real root CLI (newRootCmd / cmd.Execute,
// the same harness as cli_test.go's TestCLI) against a real project+capture
// fixture. No second semantic/planner pipeline is built to produce an
// "expected" model: the CLI's own --json-plan payload is the source of truth
// for the parity assertions.

// ---------------------------------------------------------------------------
// fixture: a capture-backed project whose DSL declares Part and Assembly
// target actions across all three canonical mutation families (suppression,
// visibility, deletion), plus one same-family pair (LegSuppress/LegSuppress2,
// SubSuppress/SubSuppress2) declared out of lexical order in the DSL, so
// canonical ordering is actually exercised rather than accidentally already
// sorted.
// ---------------------------------------------------------------------------

func task13BoolJSON(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func task13Component(id, kind, name string, suppress, hide, del bool) string {
	return `{
        "id": "` + id + `",
        "kind": "` + kind + `",
        "name": "` + name + `",
        "displayName": "` + name + `",
        "cadType": "App::Part",
        "quantity": 1,
        "material": "",
        "stabilityClass": "stable",
        "targetability": {
          "suppress": ` + task13BoolJSON(suppress) + `,
          "unsuppress": false,
          "hide": ` + task13BoolJSON(hide) + `,
          "unhide": false,
          "delete": ` + task13BoolJSON(del) + `
        },
        "annotations": {"description": "", "comment": "", "purpose": ""}
      }`
}

func task13StructureNode(id, parent string, children []string) string {
	childrenJSON := "[]"
	if len(children) > 0 {
		quoted := make([]string, len(children))
		for i, c := range children {
			quoted[i] = `"` + c + `"`
		}
		childrenJSON = "[" + strings.Join(quoted, ", ") + "]"
	}
	return `{"componentId": "` + id + `", "parentComponentId": "` + parent + `", "children": ` + childrenJSON + `}`
}

// writeTask13TargetMutationHandoffFixture writes a temp project directory
// (DSL + parametron.project.json + parametron.cad.json capture contract +
// parametron.semantic-map.json, reusing defaultCaptureBackedSemanticMapJSON
// from cli_test.go) whose DSL exercises Part suppression/visibility/deletion
// and Assembly suppression/visibility/deletion target actions in a single
// product, plus a top-level parameter assignment.
func writeTask13TargetMutationHandoffFixture(t *testing.T) string {
	t.Helper()

	projectDir := t.TempDir()
	modelDir := filepath.Join(projectDir, "input")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "box.FCStd"), []byte("fixture"), 0o644); err != nil {
		t.Fatalf("failed to write model fixture: %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "project.dsl"), []byte(withVersionHeader(`
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param width: number = 42

    target LegSuppress2: action = suppress
    target LegSuppress: action = suppress
    target LegHide: action = hide
    target LegDelete: action = delete
    target SubSuppress2: action = suppress
    target SubSuppress: action = suppress
    target SubHide: action = hide
    target SubDelete: action = delete
}
`)), 0o644); err != nil {
		t.Fatalf("failed to write project DSL: %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "parametron.project.json"), []byte(`{
  "version": "1.0",
  "projectId": "task13-handoff-fixture",
  "dsl": "project.dsl",
  "resources": {
    "models": {
      "box_model": "input/box.FCStd"
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("failed to write project map: %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "parametron.semantic-map.json"), []byte(defaultCaptureBackedSemanticMapJSON()), 0o644); err != nil {
		t.Fatalf("failed to write semantic map fixture: %v", err)
	}

	leafIDs := []string{
		"cmp.legSuppress", "cmp.legSuppress2", "cmp.legHide", "cmp.legDelete",
		"cmp.subSuppress", "cmp.subSuppress2", "cmp.subHide", "cmp.subDelete",
	}
	components := strings.Join([]string{
		task13Component("cmp.root", "assembly", "MainAssembly", false, false, false),
		task13Component("cmp.legSuppress", "part", "LegSuppress", true, false, false),
		task13Component("cmp.legSuppress2", "part", "LegSuppress2", true, false, false),
		task13Component("cmp.legHide", "part", "LegHide", false, true, false),
		task13Component("cmp.legDelete", "part", "LegDelete", false, false, true),
		task13Component("cmp.subSuppress", "assembly", "SubSuppress", true, false, false),
		task13Component("cmp.subSuppress2", "assembly", "SubSuppress2", true, false, false),
		task13Component("cmp.subHide", "assembly", "SubHide", false, true, false),
		task13Component("cmp.subDelete", "assembly", "SubDelete", false, false, true),
	}, ",\n")

	structureNodes := []string{task13StructureNode("cmp.root", "", leafIDs)}
	for _, id := range leafIDs {
		structureNodes = append(structureNodes, task13StructureNode(id, "cmp.root", nil))
	}

	captureJSON := `{
  "schemaVersion": "1.0",
  "captureId": "cap.task13.handoff",
  "adapter": {"name": "freecad", "version": ""},
  "cadSystem": {"name": "FreeCAD", "version": ""},
  "sourceDocument": {"logicalId": "box_model", "path": "input/box.FCStd", "fingerprint": "sha256:fixture"},
  "rootProduct": {"id": "cmp.root"},
  "annotations": {"description": "", "comment": "", "purpose": ""},
  "entities": {
    "components": [
` + components + `
    ],
    "features": [],
    "relationships": [],
    "parameterGroups": [],
    "parameters": [
      {
        "id": "par.root.width",
        "ownerKind": "component",
        "ownerId": "cmp.root",
        "componentId": "cmp.root",
        "name": "width",
        "displayName": "Width",
        "cadType": "Length",
        "valueType": "number",
        "observable": true,
        "writable": true,
        "currentValue": 42,
        "unit": "mm",
        "stabilityClass": "stable",
        "annotations": {"description": "", "comment": "", "purpose": ""}
      }
    ],
    "metadata": []
  },
  "structure": {
    "rootComponentId": "cmp.root",
    "nodes": [
` + strings.Join(structureNodes, ",\n") + `
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(projectDir, "parametron.cad.json"), []byte(captureJSON), 0o644); err != nil {
		t.Fatalf("failed to write capture contract: %v", err)
	}

	return projectDir
}

// ---------------------------------------------------------------------------
// --json-plan decode helpers
// ---------------------------------------------------------------------------

type task13JSONPlanStep struct {
	Type    string          `json:"Type"`
	Payload json.RawMessage `json:"Payload"`
}

type task13JSONPlanEnvelope struct {
	Plan struct {
		Steps []task13JSONPlanStep `json:"Steps"`
	} `json:"plan"`
	Hash string `json:"hash"`
}

// task13DecodeJSONPlanManifestPayload decodes the real root CLI --json-plan
// stdout and extracts the WriteExportManifest step's typed payload. It
// decodes into the real planner.ExecutionPlan step shape via a minimal
// mirror struct (Step.Payload is an interface, so a generic json.RawMessage
// pass then a typed re-decode is required), never via string scraping.
func task13DecodeJSONPlanManifestPayload(t *testing.T, stdout string) (planner.WriteExportManifestPayload, string) {
	t.Helper()

	var envelope task13JSONPlanEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("failed to decode --json-plan output: %v\n%s", err, stdout)
	}
	if envelope.Hash == "" {
		t.Fatalf("expected non-empty plan hash in --json-plan output:\n%s", stdout)
	}
	for _, step := range envelope.Plan.Steps {
		if step.Type != string(planner.StepWriteExportManifest) {
			continue
		}
		var payload planner.WriteExportManifestPayload
		if err := json.Unmarshal(step.Payload, &payload); err != nil {
			t.Fatalf("failed to decode WriteExportManifestPayload: %v\n%s", err, step.Payload)
		}
		return payload, envelope.Hash
	}
	t.Fatalf("no %s step found in --json-plan output:\n%s", planner.StepWriteExportManifest, stdout)
	return planner.WriteExportManifestPayload{}, ""
}

func task13RunJSONPlan(t *testing.T, projectDir string) (planner.WriteExportManifestPayload, string) {
	t.Helper()
	stdout, err := executeRootCommand(t, []string{"--project", projectDir, "--json-plan"})
	if err != nil {
		t.Fatalf("unexpected --json-plan error: %v\n%s", err, stdout)
	}
	return task13DecodeJSONPlanManifestPayload(t, stdout)
}

// task13WantPartMutations / task13WantAssemblyMutations are the literal
// expected canonical mutation collections for the fixture above: the DSL
// deliberately declares LegSuppress2 before LegSuppress (and SubSuppress2
// before SubSuppress) so that a passing assertion here proves genuine
// canonical (lexical-by-Object) ordering, not mere declaration-order
// preservation.
func task13WantPartMutations() *planner.ExportManifestMutationCollection {
	return &planner.ExportManifestMutationCollection{
		Suppression: []planner.ExportManifestSuppressionMutation{
			{Object: "LegSuppress", Suppressed: true},
			{Object: "LegSuppress2", Suppressed: true},
		},
		Visibility: []planner.ExportManifestVisibilityMutation{{Object: "LegHide", Visible: false}},
		Deletion:   []planner.ExportManifestDeletionMutation{{Object: "LegDelete"}},
	}
}

// The fixture's "width" parameter is owned by cmp.root ("MainAssembly", an
// assembly-kind component), so the planner's implicit parameter-target
// fallback records its resolved Object.Property in AssemblyMutations.
// Parameters at the plan level (this internal bookkeeping is what the Task
// 11 runtime projector filters out -- see
// TestTask13CLI_JSONPlanMatchesProductRuntimeManifest, which asserts it is
// absent from the written runtime manifest).
func task13WantAssemblyMutations() *planner.ExportManifestMutationCollection {
	return &planner.ExportManifestMutationCollection{
		Parameters: []planner.ExportManifestParameterMutation{
			{Object: "MainAssembly", Property: "width", ValueParam: "width", Type: "number", Unit: "mm"},
		},
		Suppression: []planner.ExportManifestSuppressionMutation{
			{Object: "SubSuppress", Suppressed: true},
			{Object: "SubSuppress2", Suppressed: true},
		},
		Visibility: []planner.ExportManifestVisibilityMutation{{Object: "SubHide", Visible: false}},
		Deletion:   []planner.ExportManifestDeletionMutation{{Object: "SubDelete"}},
	}
}

// ---------------------------------------------------------------------------
// Gap 6 / parity cells JSONPlanUsesNormalPlanner + JSONPlanCarriesMutationPayload
// ---------------------------------------------------------------------------

// TestTask13CLI_JSONPlanCarriesCanonicalTargetMutationPayload locks that the
// root CLI --json-plan output (a) is the normal planner.ExecutionPlan / a
// WriteExportManifest step (never a private MutationIntent slice), and (b)
// its WriteExportManifestPayload carries the exact canonical Part+Assembly
// mutation-bearing manifest content, with the top-level parameterAssignments
// and outputs the real planner produced.
func TestTask13CLI_JSONPlanCarriesCanonicalTargetMutationPayload(t *testing.T) {
	resetGlobals()
	projectDir := writeTask13TargetMutationHandoffFixture(t)

	payload, hash := task13RunJSONPlan(t, projectDir)
	if hash == "" {
		t.Fatal("expected non-empty plan hash")
	}
	// The normal planner path emits the single canonical schema for a
	// mutation-bearing plan; it never selects a separate mutation schema.
	if payload.SchemaVersion != "1.0" {
		t.Fatalf("expected canonical schemaVersion \"1.0\", got %q", payload.SchemaVersion)
	}

	// The raw --json-plan bytes carry the canonical runtime manifest filename
	// on the RunCADRuntime step and never a schema 2.0 manifest.
	stdout, err := executeRootCommand(t, []string{"--project", projectDir, "--json-plan"})
	if err != nil {
		t.Fatalf("unexpected --json-plan error: %v\n%s", err, stdout)
	}
	if !regexp.MustCompile(`"manifestFilename":\s*"prm\.export-manifest\.json"`).MatchString(stdout) {
		t.Fatalf("expected canonical manifestFilename in --json-plan output:\n%s", stdout)
	}
	if regexp.MustCompile(`"schemaVersion":\s*"2\.0"`).MatchString(stdout) {
		t.Fatalf("--json-plan emitted a schema 2.0 manifest:\n%s", stdout)
	}
	if !regexp.MustCompile(`"schemaVersion":\s*"1\.0"`).MatchString(stdout) {
		t.Fatalf("--json-plan lacks canonical schema 1.0 manifest:\n%s", stdout)
	}

	if payload.PartMutations == nil {
		t.Fatal("expected partMutations to be present")
	}
	if payload.AssemblyMutations == nil {
		t.Fatal("expected assemblyMutations to be present")
	}
	if !reflect.DeepEqual(payload.PartMutations, task13WantPartMutations()) {
		t.Fatalf("partMutations mismatch\nwant: %#v\ngot:  %#v", task13WantPartMutations(), payload.PartMutations)
	}
	if !reflect.DeepEqual(payload.AssemblyMutations, task13WantAssemblyMutations()) {
		t.Fatalf("assemblyMutations mismatch\nwant: %#v\ngot:  %#v", task13WantAssemblyMutations(), payload.AssemblyMutations)
	}

	if len(payload.ParameterAssignments) != 1 {
		t.Fatalf("expected exactly one parameter assignment, got %#v", payload.ParameterAssignments)
	}
	if payload.ParameterAssignments[0].Name != "width" || payload.ParameterAssignments[0].Value != 42 {
		t.Fatalf("unexpected parameterAssignments: %#v", payload.ParameterAssignments)
	}
	if len(payload.Outputs) != 1 || payload.Outputs[0].Type != "step" {
		t.Fatalf("unexpected outputs: %#v", payload.Outputs)
	}
}

// TestTask13CLI_JSONPlanMutationPayloadIsRepeatable is the optional but
// inexpensive CLI-level determinism proof: 10 real --json-plan invocations
// against the same fixture produce byte-identical stdout, so the plan hash
// and the mutation-bearing payload it carries are stable across repeats.
func TestTask13CLI_JSONPlanMutationPayloadIsRepeatable(t *testing.T) {
	resetGlobals()
	projectDir := writeTask13TargetMutationHandoffFixture(t)

	resetGlobals()
	first, err := executeRootCommand(t, []string{"--project", projectDir, "--json-plan"})
	if err != nil {
		t.Fatalf("unexpected --json-plan error: %v", err)
	}
	for i := 0; i < 10; i++ {
		resetGlobals()
		got, err := executeRootCommand(t, []string{"--project", projectDir, "--json-plan"})
		if err != nil {
			t.Fatalf("iteration %d: unexpected --json-plan error: %v", i, err)
		}
		if got != first {
			t.Fatalf("iteration %d: --json-plan stdout drifted", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Gap 7 -- CLI plan -> product runtime manifest parity
// ---------------------------------------------------------------------------

// task13WithProductionProjectionMode reconstructs the non-serialized
// execution-routing fields that planner.WriteExportManifestPayload
// deliberately excludes from --json-plan (ProductKey, ManifestFilename,
// ManifestProjectionMode all carry `json:"-"` tags: they are internal
// plumbing, not plan content). Within a single real execution these fields
// are never round-tripped through JSON at all -- they stay on the original
// in-memory struct. Supplying their known constant values here (the fixture
// always uses adapter="freecad") is that same plumbing, not a second
// semantic derivation: every mutation-bearing field asserted below comes
// straight from the decoded --json-plan payload.
func task13WithProductionProjectionMode(payload planner.WriteExportManifestPayload) planner.WriteExportManifestPayload {
	payload.ManifestProjectionMode = planner.ExportManifestProjectionModeFreeCADRuntimeNative
	return payload
}

// TestTask13CLI_JSONPlanMatchesProductRuntimeManifest projects the exact
// WriteExportManifestPayload decoded from a real --json-plan run through the
// production product-level projector/serializer and proves the written
// aligned runtime manifest matches the plan's mutation-bearing content
// exactly, with the Task 11 runtime filtering contract intact.
func TestTask13CLI_JSONPlanMatchesProductRuntimeManifest(t *testing.T) {
	resetGlobals()
	projectDir := writeTask13TargetMutationHandoffFixture(t)
	payload, _ := task13RunJSONPlan(t, projectDir)

	manifest, err := freecad.ProjectFreeCADRuntimeExportManifest(task13WithProductionProjectionMode(payload))
	if err != nil {
		t.Fatalf("product-level projection failed: %v", err)
	}
	data, err := freecad.MarshalFreeCADRuntimeExportManifestJSON(manifest)
	if err != nil {
		t.Fatalf("product-level marshal failed: %v", err)
	}

	if manifest.SchemaVersion != "1.0" {
		t.Fatalf("schemaVersion mismatch: got %q want 1.0", manifest.SchemaVersion)
	}
	if !strings.Contains(string(data), `"schemaVersion": "1.0"`) || strings.Contains(string(data), `"2.0"`) {
		t.Fatalf("serialized product manifest is not canonical schema 1.0: %s", data)
	}
	if manifest.PartMutations == nil || manifest.AssemblyMutations == nil {
		t.Fatalf("expected both runtime mutation sections present: %#v", manifest)
	}
	wantPart := &freecad.FreeCADRuntimeMutationCollection{
		Suppression: []freecad.FreeCADRuntimeSuppressionMutation{
			{Object: "LegSuppress", Suppressed: true},
			{Object: "LegSuppress2", Suppressed: true},
		},
		Visibility: []freecad.FreeCADRuntimeVisibilityMutation{{Object: "LegHide", Visible: false}},
		Deletion:   []freecad.FreeCADRuntimeDeletionMutation{{Object: "LegDelete"}},
	}
	if !reflect.DeepEqual(manifest.PartMutations, wantPart) {
		t.Fatalf("product runtime partMutations mismatch\nwant: %#v\ngot:  %#v", wantPart, manifest.PartMutations)
	}
	wantAssembly := &freecad.FreeCADRuntimeMutationCollection{
		Suppression: []freecad.FreeCADRuntimeSuppressionMutation{
			{Object: "SubSuppress", Suppressed: true},
			{Object: "SubSuppress2", Suppressed: true},
		},
		Visibility: []freecad.FreeCADRuntimeVisibilityMutation{{Object: "SubHide", Visible: false}},
		Deletion:   []freecad.FreeCADRuntimeDeletionMutation{{Object: "SubDelete"}},
	}
	if !reflect.DeepEqual(manifest.AssemblyMutations, wantAssembly) {
		t.Fatalf("product runtime assemblyMutations mismatch\nwant: %#v\ngot:  %#v", wantAssembly, manifest.AssemblyMutations)
	}
	if len(manifest.ParameterAssignments) != len(payload.ParameterAssignments) {
		t.Fatalf("parameterAssignments count mismatch: plan=%d runtime=%d", len(payload.ParameterAssignments), len(manifest.ParameterAssignments))
	}
	if len(manifest.Outputs) != len(payload.Outputs) {
		t.Fatalf("outputs count mismatch: plan=%d runtime=%d", len(payload.Outputs), len(manifest.Outputs))
	}

	if strings.Contains(string(data), `"parameters"`) || strings.Contains(string(data), `"properties"`) {
		t.Fatalf("product runtime manifest leaked nested Parameters/Properties: %s", data)
	}
	if strings.Contains(string(data), "cmp.") || strings.Contains(string(data), "feat.") {
		t.Fatalf("product runtime manifest leaked a semantic ID: %s", data)
	}
}

// ---------------------------------------------------------------------------
// Gap 8 -- CLI plan -> attempt-local runtime manifest parity
// ---------------------------------------------------------------------------

// task13AttemptRequestFromCLIPayload builds the CADRuntimeOrchestrationRequest
// scaffolding needed to materialize the attempt-local manifest. The scaffold
// fields (JobID/ProductDir/StepID/Attempt/CSV/CADRuntime/Executable) are pure
// execution plumbing with no DSL/semantic content of their own -- mirroring
// the same construction used by the attempt-local test harness in
// internal/engine/adapter (validFreeCADRuntimeAttemptRequest). The Manifest
// field is the exact CLI-decoded payload, with only the non-serialized
// routing fields restored (see task13WithProductionProjectionMode).
func task13AttemptRequestFromCLIPayload(t *testing.T, payload planner.WriteExportManifestPayload) adapter.CADRuntimeOrchestrationRequest {
	t.Helper()

	manifest := task13WithProductionProjectionMode(payload)
	manifest.ProductKey = "box"
	manifest.ManifestFilename = planner.ExportManifestFilename

	root := t.TempDir()
	return adapter.CADRuntimeOrchestrationRequest{
		JobID:      "job-task13-handoff",
		ProductKey: "box",
		ProductDir: filepath.Join(root, "product"),
		StepID:     "2",
		Attempt:    1,
		CSV: planner.WriteCSVPayload{
			ProductKey: "box",
			Filename:   "box.csv",
			Headers:    []string{"width"},
			Values:     []any{42.0},
		},
		Manifest: manifest,
		CADRuntime: planner.RunCADRuntimePayload{
			ProductKey:       "box",
			Adapter:          runtimecap.FreeCADAdapterID,
			ManifestFilename: manifest.ManifestFilename,
			ResultFilename:   "cad_result.json",
		},
		Executable: runtimecap.ExecutableSelection{
			Adapter: runtimecap.FreeCADAdapterID,
			Command: "/opt/parametron/bin/parametron-freecad",
			Path:    "/opt/parametron/bin/parametron-freecad",
			Source:  runtimecap.ExecutableSelectionSourceConfigured,
		},
	}
}

// TestTask13CLI_JSONPlanMatchesAttemptLocalRuntimeManifest materializes the
// same real --json-plan payload through the attempt-local path
// (ComposeFreeCADRuntimeManifest) and proves it matches the product-level
// projection exactly, except for the one legitimate attempt-local rewrite:
// sourceDocument, which the attempt path canonicalizes to the working-copy
// path.
func TestTask13CLI_JSONPlanMatchesAttemptLocalRuntimeManifest(t *testing.T) {
	resetGlobals()
	projectDir := writeTask13TargetMutationHandoffFixture(t)
	payload, _ := task13RunJSONPlan(t, projectDir)

	productManifest, err := freecad.ProjectFreeCADRuntimeExportManifest(task13WithProductionProjectionMode(payload))
	if err != nil {
		t.Fatalf("product-level projection failed: %v", err)
	}

	req := task13AttemptRequestFromCLIPayload(t, payload)
	materialization, err := freecad.ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatalf("attempt-local compose failed: %v", err)
	}

	if materialization.Manifest.SchemaVersion != productManifest.SchemaVersion {
		t.Fatalf("schemaVersion parity break: attempt=%q product=%q", materialization.Manifest.SchemaVersion, productManifest.SchemaVersion)
	}
	if !reflect.DeepEqual(materialization.Manifest.PartMutations, productManifest.PartMutations) {
		t.Fatalf("partMutations parity break\nattempt: %#v\nproduct: %#v", materialization.Manifest.PartMutations, productManifest.PartMutations)
	}
	if !reflect.DeepEqual(materialization.Manifest.AssemblyMutations, productManifest.AssemblyMutations) {
		t.Fatalf("assemblyMutations parity break\nattempt: %#v\nproduct: %#v", materialization.Manifest.AssemblyMutations, productManifest.AssemblyMutations)
	}
	if !reflect.DeepEqual(materialization.Manifest.ParameterAssignments, productManifest.ParameterAssignments) {
		t.Fatalf("parameterAssignments parity break\nattempt: %#v\nproduct: %#v", materialization.Manifest.ParameterAssignments, productManifest.ParameterAssignments)
	}
	if !reflect.DeepEqual(materialization.Manifest.Outputs, productManifest.Outputs) {
		t.Fatalf("outputs parity break\nattempt: %#v\nproduct: %#v", materialization.Manifest.Outputs, productManifest.Outputs)
	}

	// sourceDocument is the one field the attempt-local path is allowed to
	// canonicalize away from the plan's raw value (to the working-copy-facing
	// reference); for this fixture both the plan-level and attempt-local
	// values already resolve to the same canonical "source/<basename>"
	// reference, so no rewrite is actually observed here -- applying it is
	// still exercised below and is a no-op in that case.

	// Applying the SAME rewrite to the product-level payload's SourceDocument
	// (the only legitimate difference) makes the two manifests byte-identical.
	normalizedPayload := task13WithProductionProjectionMode(payload)
	normalizedPayload.SourceDocument = materialization.Manifest.SourceDocument
	normalizedProductManifest, err := freecad.ProjectFreeCADRuntimeExportManifest(normalizedPayload)
	if err != nil {
		t.Fatalf("normalized product-level projection failed: %v", err)
	}
	normalizedProductJSON, err := freecad.MarshalFreeCADRuntimeExportManifestJSON(normalizedProductManifest)
	if err != nil {
		t.Fatalf("normalized product-level marshal failed: %v", err)
	}
	if string(normalizedProductJSON) != string(materialization.JSON) {
		t.Fatalf("attempt-local and SourceDocument-normalized product bytes diverge:\nattempt:\n%s\nproduct:\n%s", materialization.JSON, normalizedProductJSON)
	}
}

// ---------------------------------------------------------------------------
// Gap 9 -- three-way parity matrix
// ---------------------------------------------------------------------------

// TestTask13HandoffMatrix_JSONPlanProductAttemptParity is the single explicit
// three-way lock: CLI --json-plan payload <-> product runtime manifest <->
// attempt-local runtime manifest. One real CLI invocation feeds one product
// projection and one attempt-local materialization; every subtest below
// attributes one roadmap parity cell to an explicit, independently-failing
// assertion.
func TestTask13HandoffMatrix_JSONPlanProductAttemptParity(t *testing.T) {
	resetGlobals()
	projectDir := writeTask13TargetMutationHandoffFixture(t)
	payload, hash := task13RunJSONPlan(t, projectDir)

	productManifest, err := freecad.ProjectFreeCADRuntimeExportManifest(task13WithProductionProjectionMode(payload))
	if err != nil {
		t.Fatalf("product-level projection failed: %v", err)
	}
	req := task13AttemptRequestFromCLIPayload(t, payload)
	materialization, err := freecad.ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatalf("attempt-local compose failed: %v", err)
	}
	attemptManifest := materialization.Manifest

	t.Run("SchemaParity", func(t *testing.T) {
		if payload.SchemaVersion != "1.0" {
			t.Fatalf("plan payload is not canonical schema 1.0: %q", payload.SchemaVersion)
		}
		if payload.SchemaVersion != productManifest.SchemaVersion || productManifest.SchemaVersion != attemptManifest.SchemaVersion {
			t.Fatalf("schema parity break: plan=%q product=%q attempt=%q", payload.SchemaVersion, productManifest.SchemaVersion, attemptManifest.SchemaVersion)
		}
	})

	t.Run("PartAssemblyParity", func(t *testing.T) {
		if (payload.PartMutations == nil) != (productManifest.PartMutations == nil) || (productManifest.PartMutations == nil) != (attemptManifest.PartMutations == nil) {
			t.Fatal("Part-section presence parity break across plan/product/attempt")
		}
		if (payload.AssemblyMutations == nil) != (productManifest.AssemblyMutations == nil) || (productManifest.AssemblyMutations == nil) != (attemptManifest.AssemblyMutations == nil) {
			t.Fatal("Assembly-section presence parity break across plan/product/attempt")
		}
	})

	familyCases := []struct {
		name    string
		product any
		attempt any
	}{
		{"Part/Suppression", productManifest.PartMutations.Suppression, attemptManifest.PartMutations.Suppression},
		{"Part/Visibility", productManifest.PartMutations.Visibility, attemptManifest.PartMutations.Visibility},
		{"Part/Deletion", productManifest.PartMutations.Deletion, attemptManifest.PartMutations.Deletion},
		{"Assembly/Suppression", productManifest.AssemblyMutations.Suppression, attemptManifest.AssemblyMutations.Suppression},
		{"Assembly/Visibility", productManifest.AssemblyMutations.Visibility, attemptManifest.AssemblyMutations.Visibility},
		{"Assembly/Deletion", productManifest.AssemblyMutations.Deletion, attemptManifest.AssemblyMutations.Deletion},
	}
	t.Run("FamilyParity", func(t *testing.T) {
		for _, fc := range familyCases {
			t.Run(fc.name, func(t *testing.T) {
				if !reflect.DeepEqual(fc.product, fc.attempt) {
					t.Fatalf("product/attempt family parity break for %s\nproduct: %#v\nattempt: %#v", fc.name, fc.product, fc.attempt)
				}
			})
		}
	})

	t.Run("ObjectParity", func(t *testing.T) {
		if productManifest.PartMutations.Suppression[0].Object != "LegSuppress" || productManifest.PartMutations.Suppression[1].Object != "LegSuppress2" {
			t.Fatalf("canonical Object ordering not preserved: %#v", productManifest.PartMutations.Suppression)
		}
	})

	t.Run("BooleanParity", func(t *testing.T) {
		if !productManifest.PartMutations.Suppression[0].Suppressed || !attemptManifest.PartMutations.Suppression[0].Suppressed {
			t.Fatal("suppressed=true parity break")
		}
		if productManifest.PartMutations.Visibility[0].Visible || attemptManifest.PartMutations.Visibility[0].Visible {
			t.Fatal("visible=false (hide) parity break")
		}
	})

	t.Run("ParameterAssignmentsParity", func(t *testing.T) {
		if len(payload.ParameterAssignments) != 1 || len(productManifest.ParameterAssignments) != 1 || len(attemptManifest.ParameterAssignments) != 1 {
			t.Fatalf("parameterAssignments count parity break: plan=%d product=%d attempt=%d",
				len(payload.ParameterAssignments), len(productManifest.ParameterAssignments), len(attemptManifest.ParameterAssignments))
		}
		if !reflect.DeepEqual(productManifest.ParameterAssignments, attemptManifest.ParameterAssignments) {
			t.Fatalf("product/attempt parameterAssignments parity break\nproduct: %#v\nattempt: %#v", productManifest.ParameterAssignments, attemptManifest.ParameterAssignments)
		}
	})

	t.Run("FilteringParity", func(t *testing.T) {
		productJSON, err := freecad.MarshalFreeCADRuntimeExportManifestJSON(productManifest)
		if err != nil {
			t.Fatalf("product marshal: %v", err)
		}
		for _, data := range [][]byte{productJSON, materialization.JSON} {
			if strings.Contains(string(data), `"parameters"`) || strings.Contains(string(data), `"properties"`) {
				t.Fatalf("nested Parameters/Properties leaked into runtime manifest: %s", data)
			}
		}
	})

	if hash == "" {
		t.Fatal("expected non-empty plan hash")
	}
}
