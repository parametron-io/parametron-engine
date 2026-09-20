package freecad

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
	"parametron/internal/engine/cad"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
)

// Phase 5 Task 12 permanent identity contract at the adapter boundary: the
// shared FreeCAD runtime projector (ProjectFreeCADRuntimeExportManifest) and
// serializer (MarshalFreeCADRuntimeExportManifestJSON) -- used by both the
// product-level path and the attempt-local path (ComposeFreeCADRuntimeManifest)
// -- preserve whatever Part/Assembly mutation family order the planner
// payload carries verbatim. The adapter performs no independent sort, so
// equivalent declaration-order permutations that the planner already
// canonicalizes converge to byte-identical product-level and attempt-local
// manifest bytes, and keep is identical to declaration omission at the byte
// level too.

// ---------------------------------------------------------------------------
// adapter does not independently sort: feeding a deliberately non-canonical
// (reverse-lexical) mutation order through the projector/serializer preserves
// that exact order in the output bytes.
// ---------------------------------------------------------------------------

func TestTargetMutationDeterminism_AdapterDoesNotIndependentlySort(t *testing.T) {
	reverseOrder := planner.WriteExportManifestPayload{
		SchemaVersion:          planner.ExportManifestSchemaVersion,
		ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
		SourceDocument:         "source/widget.FCStd",
		Outputs:                []planner.ExportManifestOutput{},
		PartMutations: &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Zulu", Suppressed: true}, {Object: "Alpha", Suppressed: true}},
		},
	}
	manifest, err := ProjectFreeCADRuntimeExportManifest(reverseOrder)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	want := []FreeCADRuntimeSuppressionMutation{{Object: "Zulu", Suppressed: true}, {Object: "Alpha", Suppressed: true}}
	if !reflect.DeepEqual(manifest.PartMutations.Suppression, want) {
		t.Fatalf("adapter reordered a non-canonical mutation family: got %#v want %#v", manifest.PartMutations.Suppression, want)
	}
}

// ---------------------------------------------------------------------------
// planner canonical order is preserved through the adapter, product-level and
// attempt-local, byte-for-byte.
// ---------------------------------------------------------------------------

func TestTargetMutationDeterminism_PlannerCanonicalOrderPreservedByAdapter(t *testing.T) {
	canonical := planner.WriteExportManifestPayload{
		SchemaVersion:          planner.ExportManifestSchemaVersion,
		ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
		SourceDocument:         "source/widget.FCStd",
		Outputs:                []planner.ExportManifestOutput{},
		PartMutations: &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Alpha", Suppressed: true}, {Object: "Zulu", Suppressed: true}},
		},
	}
	manifest, err := ProjectFreeCADRuntimeExportManifest(canonical)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	want := []FreeCADRuntimeSuppressionMutation{{Object: "Alpha", Suppressed: true}, {Object: "Zulu", Suppressed: true}}
	if !reflect.DeepEqual(manifest.PartMutations.Suppression, want) {
		t.Fatalf("adapter did not preserve planner canonical order: got %#v want %#v", manifest.PartMutations.Suppression, want)
	}
}

func TestTargetMutationDeterminism_ProductAndAttemptLocalBytesEqualForCanonicalOrder(t *testing.T) {
	req := alignedMutationManifestRequest(t, func(m *planner.WriteExportManifestPayload) {
		m.PartMutations = &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Alpha", Suppressed: true}, {Object: "Zulu", Suppressed: true}},
		}
		m.AssemblyMutations = &planner.ExportManifestMutationCollection{
			Visibility: []planner.ExportManifestVisibilityMutation{{Object: "Chamfer", Visible: false}, {Object: "Rail", Visible: false}},
		}
	})

	attemptLocal, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatalf("attempt-local compose: %v", err)
	}

	productPayload := req.Manifest
	productPayload.SourceDocument = attemptLocal.Manifest.SourceDocument
	productManifest, err := ProjectFreeCADRuntimeExportManifest(productPayload)
	if err != nil {
		t.Fatalf("product-level project: %v", err)
	}
	productJSON, err := MarshalFreeCADRuntimeExportManifestJSON(productManifest)
	if err != nil {
		t.Fatalf("product-level marshal: %v", err)
	}

	if !bytes.Equal(productJSON, attemptLocal.JSON) {
		t.Fatalf("product-level and attempt-local bytes diverge:\nproduct:\n%s\nattempt:\n%s", productJSON, attemptLocal.JSON)
	}
}

func TestTargetMutationDeterminism_ProductLevelBytesRepeated10x(t *testing.T) {
	req := alignedMutationManifestRequest(t, func(m *planner.WriteExportManifestPayload) {
		m.PartMutations = &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Alpha", Suppressed: true}, {Object: "Zulu", Suppressed: true}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "Pad", Visible: false}},
		}
	})
	manifest, err := ProjectFreeCADRuntimeExportManifest(req.Manifest)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	first, err := MarshalFreeCADRuntimeExportManifestJSON(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for i := 0; i < 10; i++ {
		m, err := ProjectFreeCADRuntimeExportManifest(req.Manifest)
		if err != nil {
			t.Fatalf("iteration %d: project: %v", i, err)
		}
		got, err := MarshalFreeCADRuntimeExportManifestJSON(m)
		if err != nil {
			t.Fatalf("iteration %d: marshal: %v", i, err)
		}
		if !bytes.Equal(got, first) {
			t.Fatalf("iteration %d: product-level bytes drifted:\n%s\nvs\n%s", i, got, first)
		}
	}
}

func TestTargetMutationDeterminism_AttemptLocalBytesRepeated10x(t *testing.T) {
	req := alignedMutationManifestRequest(t, func(m *planner.WriteExportManifestPayload) {
		m.AssemblyMutations = &planner.ExportManifestMutationCollection{
			Deletion: []planner.ExportManifestDeletionMutation{{Object: "Chamfer"}, {Object: "Rail"}},
		}
	})
	first, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		got, err := ComposeFreeCADRuntimeManifest(cloneFreeCADRuntimeAttemptRequest(req))
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if !bytes.Equal(got.JSON, first.JSON) {
			t.Fatalf("iteration %d: attempt-local bytes drifted:\n%s\nvs\n%s", i, got.JSON, first.JSON)
		}
	}
}

// ---------------------------------------------------------------------------
// equivalent real capture-backed declaration-order permutations converge to
// the same product-level bytes. This exercises the real planner pipeline
// (not a hand-built payload) end to end through the adapter projector.
//
// The fixture mirrors the Task 11/12 planner fixture
// (runtimeMutationManifestModel / runtimeMutationManifestProduct /
// testProjectionContract in internal/authoring/planner), duplicated here
// because the adapter package cannot import planner's unexported test
// helpers.
// ---------------------------------------------------------------------------

func mutationDeterminismModel() *semantic.Model {
	allBits := cad.Targetability{Suppress: true, Unsuppress: true, Hide: true, Unhide: true, Delete: true}
	return &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly"},
			{ID: "cmp.leg", Kind: "part", Name: "Leg", ParentID: "cmp.root", Targetability: allBits},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.width", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "width", NativeType: "Length"},
		},
		Features: []semantic.Feature{
			{ID: "feat.pad", ComponentID: "cmp.leg", Name: "Pad", NativeType: "PartDesign::Pad", Targetability: allBits},
			{ID: "feat.slot", ComponentID: "cmp.leg", Name: "Slot", NativeType: "PartDesign::Pocket", Targetability: allBits},
		},
	}
}

func mutationDeterminismProduct(body string) string {
	return "dsl v1.0\n" + `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param width: number = 12
` + body + `
}
`
}

func mutationDeterminismContract() *semanticmap.SemanticMap {
	boolPtr := func(v bool) *bool { return &v }
	return &semanticmap.SemanticMap{
		SchemaVersion: "1.0",
		MappingID:     "test-freecad-default-v1",
		Adapter:       "freecad",
		CADSystem:     &semanticmap.CADSystem{Name: "FreeCAD"},
		SemanticTypes: &semanticmap.SemanticTypes{
			ParameterTypes: map[string]semanticmap.ParameterType{
				"length": {ValueKind: "number", DefaultUnit: "mm", Coercion: semantic.CoercionRuleNumberToLength},
			},
			ResolutionPolicy: &semanticmap.ResolutionPolicy{
				Precedence:           []string{"explicit_parameter_mapping", "capture_native_type_mapping"},
				AllowEngineDefault:   boolPtr(false),
				AllowEngineInference: boolPtr(false),
			},
		},
		CaptureToSemantic: &semanticmap.CaptureToSemantic{
			ParameterGroups: []semanticmap.ParameterGroupMapping{
				{CaptureKind: "parameter_group", CADContainerType: "VarSet", SemanticKind: "parameter_group"},
			},
			Parameters: []semanticmap.ParameterMapping{
				{
					CaptureKind: "parameter", SemanticKind: "parameter",
					Identity: "capture.id", Name: "capture.name", Group: "capture.groupId",
					SemanticType: semanticmap.ParameterSemanticTypeRef{From: "capture.nativeType", Map: map[string]string{"Length": "length"}},
				},
			},
			Components: []semanticmap.ComponentMapping{
				{
					CaptureKind:   "component",
					SemanticKinds: map[string]string{"assembly": "assembly", "part": "part"},
					Identity:      "capture.id",
					Targetability: "capture.targetability",
				},
			},
			Metadata: []semanticmap.MetadataMapping{
				{CaptureKind: "metadata", SemanticKind: "metadata", Identity: "capture.id", Key: "capture.key", ValueKind: "capture.nativeType"},
			},
		},
		SemanticToManifest: &semanticmap.SemanticToManifest{
			Parameters: &semanticmap.ParameterManifestMapping{
				Target: "parameterAssignments", Name: "semantic.parameter.name", Value: "resolved.value", Type: "number", Unit: "resolved.unit",
			},
			Outputs: map[string]semanticmap.OutputManifestMapping{
				"csv":  {ManifestType: "csv", TargetField: "target", TargetSource: "semantic.output.target.manifestName", ScopeField: "rollup", ScopeMap: map[string]string{"flat": "flat", "assembly": "assembly", "subtree": "assembly"}},
				"pdf":  {ManifestType: "pdf"},
				"step": {ManifestType: "step", TargetField: "object", TargetSource: "semantic.output.target.manifestName"},
			},
			Mutations: map[string]semanticmap.MutationMapping{
				"set_parameter": {ManifestCollection: "partMutations.parameters", TargetField: "object", PropertyField: "property", ValueField: "valueParam"},
				"set_property":  {ManifestCollection: "partMutations.properties", TargetField: "object", PropertyField: "property", ValueField: "value"},
				"suppress":      {ManifestCollection: "partMutations.suppression", TargetField: "object", ValueField: "suppressed", Value: boolPtr(true)},
			},
		},
		OverridePolicy: &semanticmap.OverridePolicy{
			AllowAdapterOverrides: boolPtr(false), AllowProjectOverrides: boolPtr(false), AllowUserOverrides: boolPtr(false),
		},
		OperationCapabilities: map[string]semanticmap.Capability{
			"write_parameter": {RequiresWritable: true, AllowedSemanticTargets: []string{"parameter"}},
		},
		Determinism: &semanticmap.Determinism{
			AllowImplicitFallback: boolPtr(false), AllowCaseInsensitiveMatch: boolPtr(false), AllowDisplayNameIdentity: boolPtr(false), AllowAdapterInference: boolPtr(false),
		},
	}
}

func mutationDeterminismManifestPayload(t *testing.T, body string) planner.WriteExportManifestPayload {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "adapter_mutation_determinism_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp DSL: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(mutationDeterminismProduct(body))); err != nil {
		t.Fatalf("failed to write temp DSL: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp DSL: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("failed to validate DSL: %v", err)
	}

	intentModel, err := semantic.InjectDSLIntent(semantic.Clone(mutationDeterminismModel()), ast)
	if err != nil {
		t.Fatalf("failed to inject DSL intent: %v", err)
	}

	plan, err := planner.CreatePlanWithTablesAndSemanticModel(ast, map[string]string{}, nil, intentModel, mutationDeterminismContract())
	if err != nil {
		t.Fatalf("failed to create capture-backed plan: %v", err)
	}
	for _, step := range plan.Steps {
		if payload, ok := step.Payload.(planner.WriteExportManifestPayload); ok {
			return payload
		}
	}
	t.Fatal("no WriteExportManifestPayload step found")
	return planner.WriteExportManifestPayload{}
}

func TestTargetMutationDeterminism_RealPlannerPermutationsProduceEqualProductBytes(t *testing.T) {
	orderA := mutationDeterminismManifestPayload(t, "    target Slot: action = suppress\n    target Pad: action = suppress")
	orderB := mutationDeterminismManifestPayload(t, "    target Pad: action = suppress\n    target Slot: action = suppress")

	manifestA, err := ProjectFreeCADRuntimeExportManifest(orderA)
	if err != nil {
		t.Fatalf("project A: %v", err)
	}
	manifestB, err := ProjectFreeCADRuntimeExportManifest(orderB)
	if err != nil {
		t.Fatalf("project B: %v", err)
	}
	jsonA, err := MarshalFreeCADRuntimeExportManifestJSON(manifestA)
	if err != nil {
		t.Fatalf("marshal A: %v", err)
	}
	jsonB, err := MarshalFreeCADRuntimeExportManifestJSON(manifestB)
	if err != nil {
		t.Fatalf("marshal B: %v", err)
	}
	if !bytes.Equal(jsonA, jsonB) {
		t.Fatalf("equivalent real capture-backed declaration permutations produced different adapter bytes:\nA:\n%s\nB:\n%s", jsonA, jsonB)
	}
}

func TestTargetMutationDeterminism_RealPlannerKeepEqualsOmissionProductBytes(t *testing.T) {
	omitted := mutationDeterminismManifestPayload(t, "")
	kept := mutationDeterminismManifestPayload(t, "    target Pad: action = keep")

	manifestOmitted, err := ProjectFreeCADRuntimeExportManifest(omitted)
	if err != nil {
		t.Fatalf("project omitted: %v", err)
	}
	manifestKept, err := ProjectFreeCADRuntimeExportManifest(kept)
	if err != nil {
		t.Fatalf("project kept: %v", err)
	}
	jsonOmitted, err := MarshalFreeCADRuntimeExportManifestJSON(manifestOmitted)
	if err != nil {
		t.Fatalf("marshal omitted: %v", err)
	}
	jsonKept, err := MarshalFreeCADRuntimeExportManifestJSON(manifestKept)
	if err != nil {
		t.Fatalf("marshal kept: %v", err)
	}
	if !bytes.Equal(jsonOmitted, jsonKept) {
		t.Fatalf("keep and omission produced different adapter bytes:\nomitted:\n%s\nkept:\n%s", jsonOmitted, jsonKept)
	}
}
