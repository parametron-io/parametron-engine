package planner

import (
	"strings"
	"testing"

	"parametron/internal/engine/semantic"
)

// ===================== Part D: native-only (outputs=["none"]) planning =====================

// TestPlanner_NativeOnlyOutputsProducesZeroDerivedManifestOutputs proves the
// authoritative native-only planning shape for the non-capture-backed
// (file-based) planning path: WriteCSV -> WriteExportManifest(outputs=[])
// -> RunCADRuntime, with adapter="freecad", and manifest.Outputs being a
// non-nil empty slice (never null, never a synthetic "none" entry).
func TestPlanner_NativeOnlyOutputsProducesZeroDerivedManifestOutputs(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["none"]

    param label: string = "box"
}
`
	_, plan := parseValidateAndPlan(t, dslContent)

	if len(plan.Steps) != 3 {
		t.Fatalf("expected exactly 3 steps (WriteCSV, WriteExportManifest, RunCADRuntime), got %d: %+v", len(plan.Steps), plan.Steps)
	}
	wantOrder := []StepType{StepWriteCSV, StepWriteExportManifest, StepRunCADRuntime}
	for i, want := range wantOrder {
		if plan.Steps[i].Type != want {
			t.Fatalf("step %d: got %s want %s", i, plan.Steps[i].Type, want)
		}
	}
	manifest, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload at step 1, got %T", plan.Steps[1].Payload)
	}
	if manifest.Outputs == nil {
		t.Fatal("expected manifest.Outputs to be a non-nil empty slice, got nil")
	}
	if len(manifest.Outputs) != 0 {
		t.Fatalf("expected zero derived manifest outputs, got %+v", manifest.Outputs)
	}
	if manifest.Adapter != "freecad" {
		t.Fatalf("expected canonical adapter freecad, got %q", manifest.Adapter)
	}

	runtime, ok := plan.Steps[2].Payload.(RunCADRuntimePayload)
	if !ok {
		t.Fatalf("expected RunCADRuntimePayload at step 2, got %T", plan.Steps[2].Payload)
	}
	if runtime.Adapter != "freecad" {
		t.Fatalf("expected aligned RunCADRuntime adapter to remain freecad, got %q", runtime.Adapter)
	}
}

// TestPlanner_NativeOnlyCaptureBackedProducesZeroDerivedManifestOutputs
// proves the same native-only shape for the capture-backed semantic
// projection path -- the path exercised by real CAD-capture-driven products
// such as the cube rehearsal fixture -- without touching that fixture.
// Parameter assignments (mutation intent) must still survive even though
// zero derived outputs are requested.
func TestPlanner_NativeOnlyCaptureBackedProducesZeroDerivedManifestOutputs(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["none"]

    param width: number = 12
    param length: number = 35
}
`
	baseModel := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly", DisplayName: "Root Assembly"},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.width", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "width", NativeType: "Length"},
			{ID: "par.root.length", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "length", NativeType: "Length"},
		},
	}

	_, plan := parseValidateAndPlanCaptureBacked(t, dslContent, baseModel, testProjectionContract())

	hasRunCADRuntime := false
	var manifest WriteExportManifestPayload
	for _, step := range plan.Steps {
		switch step.Type {
		case StepRunCADRuntime:
			hasRunCADRuntime = true
		case StepWriteExportManifest:
			manifest = step.Payload.(WriteExportManifestPayload)
		}
	}
	if !hasRunCADRuntime {
		t.Fatal("expected aligned RunCADRuntime step to remain present for native-only capture-backed planning")
	}
	if len(manifest.Outputs) != 0 {
		t.Fatalf("expected zero derived manifest outputs for capture-backed native-only request, got %+v", manifest.Outputs)
	}
	if len(manifest.ParameterAssignments) == 0 {
		t.Fatal("expected parameter assignments (mutation intent) to survive a native-only request")
	}
}

// TestPlanner_AdapterNoneDoesNotProduceRunCADRuntime is the direct
// counterpart to native-only planning: adapter="none" is planning-only and
// must never enter CADRuntime, in contrast with adapter="freecad" +
// outputs=["none"], which must.
func TestPlanner_AdapterNoneDoesNotProduceRunCADRuntime(t *testing.T) {
	planningOnly := `
product PlanningOnly {
    adapter = "none"

    param width: number = 50
}
`
	_, planningOnlyPlan := parseValidateAndPlan(t, planningOnly)
	for _, step := range planningOnlyPlan.Steps {
		if step.Type == StepRunCADRuntime {
			t.Fatal(`adapter="none" must never produce a RunCADRuntime step`)
		}
	}

	nativeOnly := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["none"]

    param label: string = "box"
}
`
	_, nativeOnlyPlan := parseValidateAndPlan(t, nativeOnly)
	sawRunCADRuntime := false
	for _, step := range nativeOnlyPlan.Steps {
		if step.Type == StepRunCADRuntime {
			sawRunCADRuntime = true
		}
	}
	if !sawRunCADRuntime {
		t.Fatal(`adapter="freecad" outputs=["none"] must produce a RunCADRuntime step`)
	}
}

// ===================== Part E: accidental-empty protection =====================

// TestNormalizeFreeCADRuntimeOutputsForRequest_OnlyExplicitNoneAllowsEmpty
// is a white-box unit proof of the single invariant gate that decides
// whether a zero-length manifest output slice is legal: only a requested
// -formats slice that is exactly ["none"] (case/space-insensitively) may
// pair with zero outputs. Every other zero-output combination -- omitted
// formats, a real format with an accidentally empty derived slice, or
// "none" mixed with anything else -- must fail deterministically.
func TestNormalizeFreeCADRuntimeOutputsForRequest_OnlyExplicitNoneAllowsEmpty(t *testing.T) {
	tests := []struct {
		name             string
		outputs          []ExportManifestOutput
		requestedFormats []string
		wantErr          bool
	}{
		{"explicit none alone with empty outputs is valid", nil, []string{"none"}, false},
		{"case and whitespace insensitive none is valid", []ExportManifestOutput{}, []string{"  NoNe  "}, false},
		{"empty outputs with omitted formats is rejected", nil, nil, true},
		{"empty outputs with empty formats slice is rejected", nil, []string{}, true},
		{"empty outputs claiming step is rejected", nil, []string{"step"}, true},
		{"empty outputs with none plus step is rejected", nil, []string{"none", "step"}, true},
		{"empty outputs with two nones is rejected", nil, []string{"none", "none"}, true},
		{"non-empty outputs still validated normally", []ExportManifestOutput{{Type: "step", Filename: "outputs/box.step"}}, []string{"step"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := normalizeFreeCADRuntimeOutputsForRequest(tt.outputs, tt.requestedFormats)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

// TestBuildExportManifestOutputs_NativeOnlyRequestIsTheOnlyEmptyShortCircuit
// proves that buildExportManifestOutputs itself only special-cases the
// canonical ["none"] request; every other input either builds real outputs
// or fails. Note buildExportManifestOutputs alone does not reject a
// generically empty outputTypes slice (that responsibility belongs to
// normalizeFreeCADRuntimeOutputsForRequest, proven above) -- this test
// documents that division of responsibility precisely so it is not lost.
func TestBuildExportManifestOutputs_NativeOnlyRequestIsTheOnlyEmptyShortCircuit(t *testing.T) {
	noneOutputs, err := buildExportManifestOutputs("box", []string{"none"}, true)
	if err != nil {
		t.Fatalf("unexpected error for explicit none: %v", err)
	}
	if len(noneOutputs) != 0 {
		t.Fatalf("expected zero outputs for explicit none, got %+v", noneOutputs)
	}

	emptyOutputs, err := buildExportManifestOutputs("box", nil, true)
	if err != nil {
		t.Fatalf("unexpected error for nil outputTypes: %v", err)
	}
	if len(emptyOutputs) != 0 {
		t.Fatalf("expected zero outputs for nil outputTypes, got %+v", emptyOutputs)
	}

	stepOutputs, err := buildExportManifestOutputs("box", []string{"step"}, true)
	if err != nil {
		t.Fatalf("unexpected error for step: %v", err)
	}
	if len(stepOutputs) != 1 || stepOutputs[0].Type != "step" || stepOutputs[0].Object == "" {
		t.Fatalf("expected one object-bearing step output, got %+v", stepOutputs)
	}

	if _, err := buildExportManifestOutputs("box", []string{"stl"}, true); err == nil {
		t.Fatal("expected unsupported output format to fail")
	}
}

// TestPlanner_ExplicitEmptyOutputsListDiffersFromExplicitNone directly
// contrasts outputs=[] (explicit empty list, distinct from both omission
// and ["none"]) with outputs=["none"]: only the canonical ["none"] request
// may plan with zero derived outputs. This closes the accidental-empty gap
// explicitly at the CreatePlan boundary, alongside
// TestPlanner_RejectsInvalidAlignedOutputs/"no aligned output" which already
// covers the same rejection from the file-pattern-agnostic angle.
func TestPlanner_ExplicitEmptyOutputsListDiffersFromExplicitNone(t *testing.T) {
	emptyList := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = []

    param label: string = "box"
}
`
	_, _, err := parsePlanOnly(t, emptyList)
	if err == nil {
		t.Fatal("expected explicit empty outputs=[] to fail planning")
	}
	if !strings.Contains(err.Error(), "at least one item") {
		t.Fatalf("unexpected error for outputs=[]: %v", err)
	}

	explicitNone := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["none"]

    param label: string = "box"
}
`
	_, plan, err := parsePlanOnly(t, explicitNone)
	if err != nil {
		t.Fatalf("expected explicit none to plan successfully, got error: %v", err)
	}
	manifest := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if len(manifest.Outputs) != 0 {
		t.Fatalf("expected zero outputs for explicit none, got %+v", manifest.Outputs)
	}
}

// TestPlanner_OmittedOutputsRetainExistingDefaultBehaviorForFileBasedPlanning
// protects the non-capture-backed default-output-format behavior against a
// future "simplification" that collapses omission and explicit none:
// omitted outputs for a file-based freecad product must keep defaulting to
// a real derived STEP output, never to zero outputs.
func TestPlanner_OmittedOutputsRetainExistingDefaultBehaviorForFileBasedPlanning(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"

    param label: string = "box"
}
`
	_, plan := parseValidateAndPlan(t, dslContent)
	manifest := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if len(manifest.Outputs) == 0 {
		t.Fatal("expected omitted outputs to retain the existing non-empty default output behavior")
	}
	if manifest.Outputs[0].Type != "step" {
		t.Fatalf("expected omitted outputs to default to step, got %+v", manifest.Outputs)
	}
}

// TestPlanner_OmittedOutputsCaptureBackedRemainsRejectedLikePreExisting
// documents that the capture-backed semantic projection path treats omitted
// outputs as an error, identical to its pre-existing (pre-"none")
// behavior: capture-backed manifest intent is derived strictly from
// declared semantic Outputs, which are empty when product.outputs is
// omitted, and normalizeFreeCADRuntimeOutputsForRequest only tolerates zero
// outputs for the canonical ["none"] request -- omission does not qualify.
// This proves the "none" feature did not accidentally legalize omission on
// the capture-backed path.
func TestPlanner_OmittedOutputsCaptureBackedRemainsRejectedLikePreExisting(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"

    param width: number = 12
}
`
	baseModel := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly", DisplayName: "Root Assembly"},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.width", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "width", NativeType: "Length"},
		},
	}

	err := parseValidateAndPlanCaptureBackedError(t, dslContent, baseModel, testProjectionContract(), true)
	if err == nil {
		t.Fatal("expected omitted outputs on the capture-backed path to fail planning, matching pre-existing behavior")
	}
	if !strings.Contains(err.Error(), "at least one item") {
		t.Fatalf("unexpected error for omitted capture-backed outputs: %v", err)
	}
}

// ===================== Part F: determinism =====================

func TestPlanner_NativeOnlyPlanHashIsDeterministicAndDistinctFromRealOutputs(t *testing.T) {
	base := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["none"]

    param label: string = "box"
}
`
	_, planA := parseValidateAndPlan(t, base)
	_, planB := parseValidateAndPlan(t, base)

	hashA, err := ComputePlanHash(planA, nil)
	if err != nil {
		t.Fatalf("ComputePlanHash(A): %v", err)
	}
	hashB, err := ComputePlanHash(planB, nil)
	if err != nil {
		t.Fatalf("ComputePlanHash(B): %v", err)
	}
	if hashA != hashB {
		t.Fatalf("expected two equivalent native-only plans to hash identically, got %q vs %q", hashA, hashB)
	}

	variants := map[string]string{
		"step": `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step"]

    param label: string = "box"
}
`,
		"pdf": `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["pdf"]

    param label: string = "box"
}
`,
		"csv": `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["csv"]

    param label: string = "box"
}
`,
	}
	for name, content := range variants {
		t.Run("none_vs_"+name, func(t *testing.T) {
			_, variantPlan := parseValidateAndPlan(t, content)
			variantHash, err := ComputePlanHash(variantPlan, nil)
			if err != nil {
				t.Fatalf("ComputePlanHash(%s): %v", name, err)
			}
			if variantHash == hashA {
				t.Fatalf("expected changing none -> %s to change the plan hash", name)
			}
		})
	}
}

func TestPlanner_NativeOnlyCacheIdentityMatchesPlanHashAndChangesWithOutputs(t *testing.T) {
	noneAST, nonePlan := parseValidateAndPlan(t, `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["none"]

    param label: string = "box"
}
`)
	stepAST, stepPlan := parseValidateAndPlan(t, `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step"]

    param label: string = "box"
}
`)

	noneCtxA, err := ComputeCacheKeyContext(nonePlan, noneAST)
	if err != nil {
		t.Fatalf("ComputeCacheKeyContext(none, A): %v", err)
	}
	// Recompute from a freshly parsed/planned equivalent input.
	noneAST2, nonePlan2 := parseValidateAndPlan(t, `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["none"]

    param label: string = "box"
}
`)
	noneCtxB, err := ComputeCacheKeyContext(nonePlan2, noneAST2)
	if err != nil {
		t.Fatalf("ComputeCacheKeyContext(none, B): %v", err)
	}
	if noneCtxA.PlanHash != noneCtxB.PlanHash {
		t.Fatalf("expected identical cache/plan identity for equivalent native-only input, got %q vs %q", noneCtxA.PlanHash, noneCtxB.PlanHash)
	}

	stepCtx, err := ComputeCacheKeyContext(stepPlan, stepAST)
	if err != nil {
		t.Fatalf("ComputeCacheKeyContext(step): %v", err)
	}
	if stepCtx.PlanHash == noneCtxA.PlanHash {
		t.Fatal("expected cache/plan identity to change between none and step")
	}
}

func TestPlanner_NativeOnlyOutputOrderRemainsIrrelevantSinceOutputsIsEmpty(t *testing.T) {
	// For real multi-output plans, output declaration order is preserved
	// verbatim into the manifest (proven elsewhere); for native-only plans
	// there is nothing to order, so repeated construction must still be
	// byte-identical regardless of any incidental Go map iteration in the
	// surrounding pipeline.
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["none"]

    param label: string = "box"
}
`
	var hashes []string
	for i := 0; i < 5; i++ {
		_, plan := parseValidateAndPlan(t, dslContent)
		hash, err := ComputePlanHash(plan, nil)
		if err != nil {
			t.Fatalf("ComputePlanHash iteration %d: %v", i, err)
		}
		hashes = append(hashes, hash)
	}
	for i := 1; i < len(hashes); i++ {
		if hashes[i] != hashes[0] {
			t.Fatalf("expected all repeated native-only plan hashes to match, got %v", hashes)
		}
	}
}
