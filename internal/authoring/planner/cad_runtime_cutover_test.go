package planner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
)

// parsePlanOnly parses DSL without running dsl.Validate, then plans directly.
// This exercises the planner's own eligibility/contract logic (shouldRunCADAdapter,
// resolveProductExportManifestContract, normalizeFreeCADRuntimeOutputs) independent
// of the stricter DSL-level adapter allowlist (which only permits "freecad"/"none").
// Test content must not declare top-level constants.
func parsePlanOnly(t *testing.T, dslContent string) (*dsl.AST, *ExecutionPlan, error) {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "cad_runtime_cutover_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("failed to write temp DSL: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp DSL: %v", err)
	}
	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	plan, err := CreatePlan(ast, nil)
	return ast, plan, err
}

// ===================== Part A: planner eligibility =====================

func TestPlanner_EligibleFreeCADProductUsesAlignedRuntime(t *testing.T) {
	dslContent := `
product Box {
    adapter = "  FreeCAD  "
    source_model = "input/box.FCStd"
    outputs = ["step", "csv", "pdf"]

    param label: string = "box"
}
`
	_, plan := parseValidateAndPlan(t, dslContent)

	if len(plan.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d: %+v", len(plan.Steps), plan.Steps)
	}
	wantOrder := []StepType{StepWriteCSV, StepWriteExportManifest, StepRunCADRuntime}
	for i, want := range wantOrder {
		if plan.Steps[i].Type != want {
			t.Fatalf("step %d: got %s want %s", i, plan.Steps[i].Type, want)
		}
	}

	runtimeSteps := 0
	for _, step := range plan.Steps {
		if step.Type == StepRunCADRuntime {
			runtimeSteps++
		}
	}
	if runtimeSteps != 1 {
		t.Fatalf("expected exactly one RunCADRuntime step, got %d", runtimeSteps)
	}
	if plan.Steps[len(plan.Steps)-1].Type != StepRunCADRuntime {
		t.Fatal("expected RunCADRuntime to be the final step")
	}

	manifest, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload at step 1, got %T", plan.Steps[1].Payload)
	}
	runtime, ok := plan.Steps[2].Payload.(RunCADRuntimePayload)
	if !ok {
		t.Fatalf("expected RunCADRuntimePayload at step 2, got %T", plan.Steps[2].Payload)
	}

	if manifest.Adapter != "freecad" {
		t.Fatalf("expected canonical adapter freecad in manifest, got %q", manifest.Adapter)
	}
	if runtime.Adapter != "freecad" {
		t.Fatalf("expected canonical adapter freecad in runtime payload, got %q", runtime.Adapter)
	}
	if runtime.ManifestFilename != manifest.ManifestFilename {
		t.Fatalf("manifest filename mismatch: runtime=%q manifest=%q", runtime.ManifestFilename, manifest.ManifestFilename)
	}
	if runtime.ManifestFilename != ExportManifestFilename {
		t.Fatalf("expected canonical manifest filename %q, got %q", ExportManifestFilename, runtime.ManifestFilename)
	}
	if runtime.ResultFilename != FreeCADRuntimeResultFilename {
		t.Fatalf("expected canonical result filename %q, got %q", FreeCADRuntimeResultFilename, runtime.ResultFilename)
	}
	if manifest.ManifestProjectionMode != ExportManifestProjectionModeFreeCADRuntimeNative {
		t.Fatalf("expected native runtime projection mode, got %q", manifest.ManifestProjectionMode)
	}
	if runtime.ProductKey != "Box" || manifest.ProductKey != "Box" {
		t.Fatalf("expected stable product key Box, got runtime=%q manifest=%q", runtime.ProductKey, manifest.ProductKey)
	}
}

func TestPlanner_FreeCADAdapterEligibilityIsCaseInsensitive(t *testing.T) {
	cases := []string{"freecad", "FreeCAD", "FREECAD", "  freecad"}
	for _, adapter := range cases {
		t.Run(strings.TrimSpace(adapter)+"/"+adapter, func(t *testing.T) {
			dslContent := `
product Box {
    adapter = "` + adapter + `"
    source_model = "input/box.FCStd"

    param label: string = "box"
}
`
			_, plan := parseValidateAndPlan(t, dslContent)
			if len(plan.Steps) != 3 {
				t.Fatalf("adapter %q: expected 3 steps, got %d", adapter, len(plan.Steps))
			}
			manifest, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
			if !ok {
				t.Fatalf("adapter %q: expected WriteExportManifestPayload, got %T", adapter, plan.Steps[1].Payload)
			}
			runtime, ok := plan.Steps[2].Payload.(RunCADRuntimePayload)
			if !ok {
				t.Fatalf("adapter %q: expected RunCADRuntimePayload, got %T", adapter, plan.Steps[2].Payload)
			}
			if manifest.Adapter != "freecad" {
				t.Fatalf("adapter %q: expected canonical manifest adapter freecad, got %q", adapter, manifest.Adapter)
			}
			if runtime.Adapter != "freecad" {
				t.Fatalf("adapter %q: expected canonical runtime adapter freecad, got %q", adapter, runtime.Adapter)
			}
		})
	}
}

func TestPlanner_NonEligibleProductsDoNotUseAlignedRuntime(t *testing.T) {
	cases := []struct {
		name       string
		dslContent string
		useRawPlan bool
	}{
		{
			name: "adapter absent",
			dslContent: `
product Widget {
    param width: number = 120
}
`,
		},
		{
			name: "adapter none",
			dslContent: `
product Widget {
    adapter = "none"
    param width: number = 120
}
`,
		},
		{
			name: "product does not require CAD execution",
			dslContent: `
product Widget {
    adapter = "none"
    outputs = []
    param width: number = 120
}
`,
		},
		{
			// "" and non-freecad adapter values are rejected by the DSL-level
			// adapter allowlist before reaching the planner at all; CreatePlan
			// itself does not call dsl.Validate, so this exercises the
			// planner's own shouldRunCADAdapter boundary directly.
			name: "adapter empty (planner boundary, bypasses DSL allowlist)",
			dslContent: `
product Widget {
    adapter = ""
    param width: number = 120
}
`,
			useRawPlan: true,
		},
		{
			name: "non-freecad adapter (planner boundary, bypasses DSL allowlist)",
			dslContent: `
product Widget {
    adapter = "onshape"
    param width: number = 120
}
`,
			useRawPlan: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var plan *ExecutionPlan
			if tt.useRawPlan {
				var err error
				_, plan, err = parsePlanOnly(t, tt.dslContent)
				if err != nil {
					t.Fatalf("unexpected planning error: %v", err)
				}
			} else {
				_, plan = parseValidateAndPlan(t, tt.dslContent)
			}
			if len(plan.Steps) != 2 {
				t.Fatalf("expected 2 steps (no CAD runtime), got %d: %+v", len(plan.Steps), plan.Steps)
			}
			for _, step := range plan.Steps {
				if step.Type == StepRunCADRuntime {
					t.Fatal("did not expect a RunCADRuntime step")
				}
			}
		})
	}
}

func TestPlanner_OnlyEligibleProductsUseAlignedRuntime(t *testing.T) {
	dslContent := `
product CADBox {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step", "pdf"]

    param label: string = "box"
}

product PlanningOnly {
    adapter = "none"
    outputs = []

    param width: number = 50
}
`
	_, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) != 5 {
		t.Fatalf("expected 5 steps, got %d: %+v", len(plan.Steps), plan.Steps)
	}

	runtimeCount := 0
	for _, step := range plan.Steps {
		if step.Type != StepRunCADRuntime {
			continue
		}
		runtimeCount++
		payload, ok := step.Payload.(RunCADRuntimePayload)
		if !ok {
			t.Fatalf("expected RunCADRuntimePayload, got %T", step.Payload)
		}
		if payload.ProductKey != "CADBox" {
			t.Fatalf("expected runtime step to belong to CADBox, got %q", payload.ProductKey)
		}
	}
	if runtimeCount != 1 {
		t.Fatalf("expected exactly one RunCADRuntime step across products, got %d", runtimeCount)
	}

	// No cross-product leakage into the non-eligible product's manifest.
	secondManifest, ok := plan.Steps[4].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload at step 4, got %T", plan.Steps[4].Payload)
	}
	if secondManifest.ProductKey != "PlanningOnly" {
		t.Fatalf("expected second manifest to belong to PlanningOnly, got %q", secondManifest.ProductKey)
	}
	if len(secondManifest.Outputs) != 0 {
		t.Fatalf("expected no outputs leaked into PlanningOnly manifest, got %+v", secondManifest.Outputs)
	}
	if secondManifest.SourceDocument != "" {
		t.Fatalf("expected no aligned source document leaked into PlanningOnly manifest, got %q", secondManifest.SourceDocument)
	}
}

// ===================== Part B: planner output contract =====================

func TestPlanner_AlignedOutputsUseCanonicalOutputDirectory(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step", "csv", "pdf"]

    param label: string = "box"
}
`
	_, plan := parseValidateAndPlan(t, dslContent)
	manifest := plan.Steps[1].Payload.(WriteExportManifestPayload)

	wantOrder := []struct {
		typ      string
		filename string
	}{
		{"step", "outputs/Box.step"},
		{"csv", "outputs/Box.csv"},
		{"pdf", "outputs/Box.pdf"},
	}
	if len(manifest.Outputs) != len(wantOrder) {
		t.Fatalf("expected %d outputs, got %d: %+v", len(wantOrder), len(manifest.Outputs), manifest.Outputs)
	}
	for i, want := range wantOrder {
		got := manifest.Outputs[i]
		if got.Type != want.typ || got.Filename != want.filename {
			t.Fatalf("output %d: got %+v, want type=%q filename=%q", i, got, want.typ, want.filename)
		}
		if !strings.HasPrefix(got.Filename, "outputs/") {
			t.Fatalf("output %d filename %q is not under outputs/", i, got.Filename)
		}
		if strings.Contains(got.Filename, "outputs/outputs/") {
			t.Fatalf("output %d filename %q duplicates the outputs/ prefix", i, got.Filename)
		}
		if strings.Contains(got.Filename, "\\") {
			t.Fatalf("output %d filename %q is not slash-separated", i, got.Filename)
		}
	}
}

func TestPlanner_AlignedOutputNormalizationCoversNestedAndInvalidPaths(t *testing.T) {
	// normalizeFreeCADRuntimeOutputs is package-private and is the sole
	// enforcement point for canonical output-directory placement; exercise
	// it directly to cover nested paths and invalid-path rejection that are
	// not otherwise reachable through the DSL-driven baseName (which is
	// always a flat, separator-free segment).
	t.Run("nested relative path is canonicalized under outputs/", func(t *testing.T) {
		outputs := []ExportManifestOutput{{Type: "step", Filename: "assembly/Part.step"}}
		if err := normalizeFreeCADRuntimeOutputs(outputs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outputs[0].Filename != "outputs/assembly/Part.step" {
			t.Fatalf("unexpected filename: %q", outputs[0].Filename)
		}
	})
	t.Run("already-prefixed path is not doubled", func(t *testing.T) {
		outputs := []ExportManifestOutput{{Type: "step", Filename: "outputs/Part.step"}}
		if err := normalizeFreeCADRuntimeOutputs(outputs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outputs[0].Filename != "outputs/Part.step" {
			t.Fatalf("unexpected filename: %q", outputs[0].Filename)
		}
	})
	t.Run("empty outputs rejected", func(t *testing.T) {
		if err := normalizeFreeCADRuntimeOutputs(nil); err == nil {
			t.Fatal("expected error for empty outputs")
		}
	})
	t.Run("empty filename rejected", func(t *testing.T) {
		outputs := []ExportManifestOutput{{Type: "step", Filename: ""}}
		if err := normalizeFreeCADRuntimeOutputs(outputs); err == nil {
			t.Fatal("expected error for empty filename")
		}
	})
	t.Run("absolute path rejected", func(t *testing.T) {
		outputs := []ExportManifestOutput{{Type: "step", Filename: "/etc/passwd"}}
		if err := normalizeFreeCADRuntimeOutputs(outputs); err == nil {
			t.Fatal("expected error for absolute path")
		}
	})
	t.Run("traversal path rejected", func(t *testing.T) {
		outputs := []ExportManifestOutput{{Type: "step", Filename: "../escape.step"}}
		if err := normalizeFreeCADRuntimeOutputs(outputs); err == nil {
			t.Fatal("expected error for traversal path")
		}
	})
	t.Run("root-level dot path rejected", func(t *testing.T) {
		outputs := []ExportManifestOutput{{Type: "step", Filename: "outputs/.."}}
		if err := normalizeFreeCADRuntimeOutputs(outputs); err == nil {
			t.Fatal("expected error for root-level traversal")
		}
	})
	t.Run("duplicate output filenames rejected", func(t *testing.T) {
		outputs := []ExportManifestOutput{
			{Type: "step", Filename: "Part.step"},
			{Type: "step", Filename: "outputs/Part.step"},
		}
		err := normalizeFreeCADRuntimeOutputs(outputs)
		if err == nil {
			t.Fatal("expected error for conflicting/duplicate output filenames")
		}
		if !strings.Contains(err.Error(), "collides") {
			t.Fatalf("expected a collision error, got: %v", err)
		}
	})
}

func TestPlanner_AlignedManifestUsesDeterministicSourceDocument(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"

    param label: string = "box"
}
`
	_, plan := parseValidateAndPlan(t, dslContent)
	manifest := plan.Steps[1].Payload.(WriteExportManifestPayload)

	if manifest.SourceDocument != "source/box.FCStd" {
		t.Fatalf("expected deterministic source document %q, got %q", "source/box.FCStd", manifest.SourceDocument)
	}
	forbidden := []string{string(filepath.Separator) + "tmp", "attempt", os.TempDir(), "PARAMETRON", "/home", "/usr", "/nix"}
	for _, f := range forbidden {
		if f == "" {
			continue
		}
		if strings.Contains(manifest.SourceDocument, f) {
			t.Fatalf("source document %q leaks environment/host data (contains %q)", manifest.SourceDocument, f)
		}
	}
	if filepath.IsAbs(manifest.SourceDocument) {
		t.Fatalf("expected relative source document, got absolute %q", manifest.SourceDocument)
	}
}

func TestPlanner_RejectsAlignedResultFilenameCollisions(t *testing.T) {
	// The aligned runtime output contract structurally namespaces CAD-native
	// outputs under outputs/ while the manifest (prm.export-manifest.json) and
	// runtime result (prm.result.json) live at the working-copy root, and the
	// per-product CSV always carries a .csv extension distinct from those
	// fixed names. The only filename collision reachable within the planner's
	// own declared-output contract is between two declared outputs of the
	// same output type sharing one product (the same base filename). This is
	// covered directly via normalizeFreeCADRuntimeOutputs above
	// ("duplicate output filenames rejected"). Collisions against the CSV
	// filename, export-manifest filename, or observed filename are not
	// reachable through the current planner contract because those live in a
	// disjoint namespace from outputs/.
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step", "step"]

    param label: string = "box"
}
`
	tmpFile, err := os.CreateTemp("", "cad_runtime_output_collision_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("failed to write temp DSL: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp DSL: %v", err)
	}
	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	_, err = CreatePlan(ast, nil)
	if err == nil {
		t.Fatal("expected a deterministic planning error for colliding declared outputs")
	}
	if !strings.Contains(err.Error(), "collides") {
		t.Fatalf("expected a collision error, got: %v", err)
	}
}

func TestPlanner_RejectsAlignedProductWithoutSourceModel(t *testing.T) {
	cases := []struct {
		name       string
		dslContent string
	}{
		{
			name: "missing source_model",
			dslContent: `
product Box {
    adapter = "freecad"

    param label: string = "box"
}
`,
		},
		{
			name: "empty source_model (planner boundary, bypasses DSL allowlist)",
			dslContent: `
product Box {
    adapter = "freecad"
    source_model = ""

    param label: string = "box"
}
`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parsePlanOnly(t, tt.dslContent)
			if err == nil {
				t.Fatal("expected planning error for missing/empty source_model")
			}
			if !strings.Contains(err.Error(), "source_model") {
				t.Fatalf("expected source_model error, got: %v", err)
			}
		})
	}
}

func TestPlanner_RejectsInvalidAlignedOutputs(t *testing.T) {
	t.Run("no aligned output", func(t *testing.T) {
		dslContent := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = []

    param label: string = "box"
}
`
		_, _, err := parsePlanOnly(t, dslContent)
		if err == nil {
			t.Fatal("expected error for empty aligned outputs")
		}
		if !strings.Contains(err.Error(), "at least one item") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("unsupported output format", func(t *testing.T) {
		dslContent := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["stl"]

    param label: string = "box"
}
`
		_, _, err := parsePlanOnly(t, dslContent)
		if err == nil {
			t.Fatal("expected error for unsupported output format")
		}
		if !strings.Contains(err.Error(), "unsupported format") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	// Absolute/traversal/non-canonical/duplicate output-path rejection is
	// covered directly against normalizeFreeCADRuntimeOutputs above, since
	// the DSL `outputs` field only ever selects an output *type* (step/csv/
	// pdf); it cannot itself carry an absolute or traversal filename.
}

func TestPlanner_AlignedProductContainsRunCADRuntime(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"

    param label: string = "box"
}
`
	_, plan := parseValidateAndPlan(t, dslContent)
	hasRunCADRuntime := false
	for _, step := range plan.Steps {
		if step.Type == StepRunCADRuntime {
			hasRunCADRuntime = true
		}
	}
	if !hasRunCADRuntime {
		t.Fatal("expected the eligible plan to contain a RunCADRuntime step")
	}
}

func TestPlanner_AlignedFilePatternSeedMatchesFinalStepShape(t *testing.T) {
	withPattern := `
profile Dev {
    file_pattern = "{profile}_{product}_{param:label}"
}
use profile Dev

product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step"]

    param label: string = "prod"
}
`
	withoutPattern := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step"]

    param label: string = "prod"
}
`
	_, patternPlan := parseValidateAndPlan(t, withPattern)
	_, plainPlan := parseValidateAndPlan(t, withoutPattern)

	if len(patternPlan.Steps) != len(plainPlan.Steps) {
		t.Fatalf("expected identical step counts, got %d vs %d", len(patternPlan.Steps), len(plainPlan.Steps))
	}
	for i := range patternPlan.Steps {
		if patternPlan.Steps[i].Type != plainPlan.Steps[i].Type {
			t.Fatalf("step %d type mismatch: %s vs %s", i, patternPlan.Steps[i].Type, plainPlan.Steps[i].Type)
		}
	}

	patternManifest := patternPlan.Steps[1].Payload.(WriteExportManifestPayload)
	plainManifest := plainPlan.Steps[1].Payload.(WriteExportManifestPayload)
	patternRuntime := patternPlan.Steps[2].Payload.(RunCADRuntimePayload)
	plainRuntime := plainPlan.Steps[2].Payload.(RunCADRuntimePayload)

	if patternManifest.Adapter != plainManifest.Adapter {
		t.Fatalf("adapter mismatch: %q vs %q", patternManifest.Adapter, plainManifest.Adapter)
	}
	if patternManifest.ManifestProjectionMode != plainManifest.ManifestProjectionMode {
		t.Fatalf("manifest projection mode mismatch: %q vs %q", patternManifest.ManifestProjectionMode, plainManifest.ManifestProjectionMode)
	}
	if patternManifest.ManifestFilename != plainManifest.ManifestFilename {
		t.Fatalf("manifest filename mismatch: %q vs %q", patternManifest.ManifestFilename, plainManifest.ManifestFilename)
	}
	if patternRuntime.ResultFilename != plainRuntime.ResultFilename {
		t.Fatalf("result filename mismatch: %q vs %q", patternRuntime.ResultFilename, plainRuntime.ResultFilename)
	}
	if patternRuntime.ManifestFilename != plainRuntime.ManifestFilename {
		t.Fatalf("runtime manifest filename mismatch: %q vs %q", patternRuntime.ManifestFilename, plainRuntime.ManifestFilename)
	}
	if !strings.HasPrefix(patternManifest.Outputs[0].Filename, "outputs/") || !strings.HasPrefix(plainManifest.Outputs[0].Filename, "outputs/") {
		t.Fatalf("expected both seed and final outputs under outputs/, got %q vs %q", patternManifest.Outputs[0].Filename, plainManifest.Outputs[0].Filename)
	}
	// File-pattern naming legitimately changes the base filename, but not the
	// logical prefix/extension shape.
	if !strings.HasSuffix(patternManifest.Outputs[0].Filename, ".step") || !strings.HasSuffix(plainManifest.Outputs[0].Filename, ".step") {
		t.Fatalf("expected both outputs to retain .step extension, got %q vs %q", patternManifest.Outputs[0].Filename, plainManifest.Outputs[0].Filename)
	}
}

func TestPlanner_AlignedPlanIdentityIsDeterministic(t *testing.T) {
	base := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step"]

    param label: string = "box"
}
`
	_, planA := parseValidateAndPlan(t, base)
	_, planB := parseValidateAndPlan(t, base)

	hashA, err := ComputePlanHash(planA, nil)
	if err != nil {
		t.Fatalf("ComputePlanHash: %v", err)
	}
	hashB, err := ComputePlanHash(planB, nil)
	if err != nil {
		t.Fatalf("ComputePlanHash: %v", err)
	}
	if hashA != hashB {
		t.Fatalf("expected deterministic hash for equivalent planning, got %q vs %q", hashA, hashB)
	}

	manifestA := planA.Steps[1].Payload.(WriteExportManifestPayload)
	manifestB := planB.Steps[1].Payload.(WriteExportManifestPayload)
	runtimeA := planA.Steps[2].Payload.(RunCADRuntimePayload)
	runtimeB := planB.Steps[2].Payload.(RunCADRuntimePayload)
	if manifestA.Adapter != manifestB.Adapter || manifestA.SourceDocument != manifestB.SourceDocument ||
		manifestA.ManifestProjectionMode != manifestB.ManifestProjectionMode {
		t.Fatalf("manifest payload not stable across runs: %+v vs %+v", manifestA, manifestB)
	}
	if runtimeA != runtimeB {
		t.Fatalf("runtime payload not stable across runs: %+v vs %+v", runtimeA, runtimeB)
	}

	variants := []struct {
		name    string
		content string
	}{
		{"different source model", `
product Box {
    adapter = "freecad"
    source_model = "input/other.FCStd"
    outputs = ["step"]

    param label: string = "box"
}
`},
		{"different output type", `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["pdf"]

    param label: string = "box"
}
`},
		{"different parameter value", `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step"]

    param label: string = "other"
}
`},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			_, planV := parseValidateAndPlan(t, v.content)
			hashV, err := ComputePlanHash(planV, nil)
			if err != nil {
				t.Fatalf("ComputePlanHash: %v", err)
			}
			if hashV == hashA {
				t.Fatalf("expected changing %s to change plan hash", v.name)
			}
		})
	}
}

func TestPlanner_AlignedOutputPathChangesPlanIdentity(t *testing.T) {
	patternA := `
profile Dev {
    file_pattern = "{param:label}_a"
}
use profile Dev

product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step"]

    param label: string = "box"
}
`
	patternB := `
profile Dev {
    file_pattern = "{param:label}_b"
}
use profile Dev

product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step"]

    param label: string = "box"
}
`
	_, planA := parseValidateAndPlan(t, patternA)
	_, planB := parseValidateAndPlan(t, patternB)
	hashA, err := ComputePlanHash(planA, nil)
	if err != nil {
		t.Fatalf("ComputePlanHash: %v", err)
	}
	hashB, err := ComputePlanHash(planB, nil)
	if err != nil {
		t.Fatalf("ComputePlanHash: %v", err)
	}
	if hashA == hashB {
		t.Fatal("expected changing the resolved output path to change plan hash")
	}
}
