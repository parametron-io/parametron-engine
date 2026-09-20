package job

import (
	"os"
	"reflect"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
	"parametron/internal/engine/cad"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
)

// Phase 5 Task 12 permanent identity contract at the job boundary: Job
// identity (job.New / job.FromPlan) is a pure function of the canonical
// planner-produced WriteExportManifestPayload -- it performs no independent
// reordering of its own. Consequently keep is identical to declaration
// omission, mutation-producing actions distinctly affect Job.ID, and
// equivalent declaration-order permutations (which the planner already
// canonicalizes per Task 12) converge to the same Job.ID.
//
// mutationIdentityModel / mutationIdentityProduct / mutationIdentityContract
// mirror the Task 11/12 planner fixture (runtimeMutationManifestModel /
// runtimeMutationManifestProduct / testProjectionContract in
// internal/authoring/planner) so the real capture-backed planner pipeline --
// not a hand-built payload -- produces the plans under test here. Part-scope
// features: Pad, Slot (owned by cmp.leg). Assembly-scope features: Rail,
// Chamfer, Latch (owned by cmp.root).

func mutationIdentityModel() *semantic.Model {
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
			{ID: "feat.rail", ComponentID: "cmp.root", Name: "Rail", NativeType: "PartDesign::Pad", Targetability: allBits},
			{ID: "feat.chamfer", ComponentID: "cmp.root", Name: "Chamfer", NativeType: "PartDesign::Chamfer", Targetability: allBits},
			{ID: "feat.latch", ComponentID: "cmp.root", Name: "Latch", NativeType: "PartDesign::Pad", Targetability: allBits},
		},
	}
}

func mutationIdentityProduct(body string) string {
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

func mutationIdentityContract() *semanticmap.SemanticMap {
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

func mutationIdentityPlan(t *testing.T, body string) *planner.ExecutionPlan {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "job_mutation_identity_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp DSL: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(mutationIdentityProduct(body))); err != nil {
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

	intentModel, err := semantic.InjectDSLIntent(semantic.Clone(mutationIdentityModel()), ast)
	if err != nil {
		t.Fatalf("failed to inject DSL intent: %v", err)
	}

	plan, err := planner.CreatePlanWithTablesAndSemanticModel(ast, map[string]string{}, nil, intentModel, mutationIdentityContract())
	if err != nil {
		t.Fatalf("failed to create capture-backed plan: %v", err)
	}
	return plan
}

func mutationIdentityJobID(t *testing.T, body string) string {
	t.Helper()
	j, err := FromPlan(mutationIdentityPlan(t, body))
	if err != nil {
		t.Fatalf("FromPlan failed: %v", err)
	}
	return j.ID
}

// ---------------------------------------------------------------------------
// keep == declaration omission
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_JobID_KeepEqualsOmission(t *testing.T) {
	omittedID := mutationIdentityJobID(t, "")
	keptID := mutationIdentityJobID(t, "    target Pad: action = keep")
	if omittedID != keptID {
		t.Fatalf("keep and omission produced different Job.ID: omitted=%q kept=%q", omittedID, keptID)
	}
}

func TestTargetMutationIdentity_JobID_KeepEqualsOmissionDeterministic10x(t *testing.T) {
	first := mutationIdentityJobID(t, "")
	for i := 0; i < 10; i++ {
		omittedID := mutationIdentityJobID(t, "")
		keptID := mutationIdentityJobID(t, "    target Pad: action = keep")
		if omittedID != first || keptID != first {
			t.Fatalf("iteration %d: Job.ID drifted: omitted=%q kept=%q want %q", i, omittedID, keptID, first)
		}
	}
}

// ---------------------------------------------------------------------------
// mutation-producing actions affect Job.ID
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_JobID_ActionsDifferFromOmissionAndEachOther(t *testing.T) {
	omittedID := mutationIdentityJobID(t, "")
	ids := map[string]string{"omission": omittedID}
	for _, action := range []string{"suppress", "unsuppress", "hide", "unhide", "delete"} {
		ids[action] = mutationIdentityJobID(t, "    target Pad: action = "+action)
		if ids[action] == omittedID {
			t.Fatalf("%s Job.ID equals omission Job.ID: %q", action, ids[action])
		}
	}
	seen := make(map[string]string, len(ids))
	for name, id := range ids {
		if other, exists := seen[id]; exists {
			t.Fatalf("%s and %s produced colliding Job.ID %q", name, other, id)
		}
		seen[id] = name
	}
}

func TestTargetMutationIdentity_JobID_ChangedMutationChangesID(t *testing.T) {
	suppressID := mutationIdentityJobID(t, "    target Pad: action = suppress")
	hideID := mutationIdentityJobID(t, "    target Pad: action = hide")
	if suppressID == hideID {
		t.Fatal("changing the mutation family did not change Job.ID")
	}
}

// ---------------------------------------------------------------------------
// declaration-order permutation equivalence (planner canonicalization
// propagates through job.FromPlan without job performing its own sort)
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_JobID_PartPermutationEquivalence(t *testing.T) {
	orderA := mutationIdentityJobID(t, "    target Slot: action = suppress\n    target Pad: action = suppress")
	orderB := mutationIdentityJobID(t, "    target Pad: action = suppress\n    target Slot: action = suppress")
	if orderA != orderB {
		t.Fatalf("equivalent Part declaration permutations produced different Job.ID: A=%q B=%q", orderA, orderB)
	}
}

func TestTargetMutationIdentity_JobID_AssemblyPermutationEquivalence(t *testing.T) {
	orderA := mutationIdentityJobID(t, "    target Rail: action = hide\n    target Chamfer: action = hide\n    target Latch: action = hide")
	orderB := mutationIdentityJobID(t, "    target Latch: action = hide\n    target Rail: action = hide\n    target Chamfer: action = hide")
	if orderA != orderB {
		t.Fatalf("equivalent Assembly declaration permutations produced different Job.ID: A=%q B=%q", orderA, orderB)
	}
}

func TestTargetMutationIdentity_JobID_MixedFamilyPermutationEquivalence(t *testing.T) {
	orderA := "    target Pad: action = suppress\n" +
		"    target Slot: action = hide\n" +
		"    target Chamfer: action = delete"
	orderB := "    target Chamfer: action = delete\n" +
		"    target Slot: action = hide\n" +
		"    target Pad: action = suppress"
	idA := mutationIdentityJobID(t, orderA)
	idB := mutationIdentityJobID(t, orderB)
	if idA != idB {
		t.Fatalf("equivalent mixed-family declaration permutations produced different Job.ID: A=%q B=%q", idA, idB)
	}
}

func TestTargetMutationIdentity_JobID_PermutationEquivalenceDeterministic10x(t *testing.T) {
	orderA := "    target Slot: action = suppress\n    target Pad: action = suppress"
	orderB := "    target Pad: action = suppress\n    target Slot: action = suppress"
	first := mutationIdentityJobID(t, orderA)
	for i := 0; i < 10; i++ {
		idA := mutationIdentityJobID(t, orderA)
		idB := mutationIdentityJobID(t, orderB)
		if idA != first || idB != first {
			t.Fatalf("iteration %d: Job.ID drifted: A=%q B=%q want %q", i, idA, idB, first)
		}
	}
}

// ---------------------------------------------------------------------------
// job performs no independent sorting: a directly constructed job step with
// a non-canonical (reverse-lexical) mutation family order produces a
// different Job.ID than the canonical order, because job.New serializes
// step payloads verbatim rather than normalizing them itself.
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_JobDoesNotIndependentlySort(t *testing.T) {
	canonicalOrder := planner.WriteExportManifestPayload{
		ProductKey: "widget", ManifestFilename: "widget.json", SchemaVersion: planner.ExportManifestSchemaVersion,
		Product: planner.ExportManifestProduct{ID: "widget"},
		PartMutations: &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Alpha", Suppressed: true}, {Object: "Zulu", Suppressed: true}},
		},
	}
	reverseOrder := planner.WriteExportManifestPayload{
		ProductKey: "widget", ManifestFilename: "widget.json", SchemaVersion: planner.ExportManifestSchemaVersion,
		Product: planner.ExportManifestProduct{ID: "widget"},
		PartMutations: &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Zulu", Suppressed: true}, {Object: "Alpha", Suppressed: true}},
		},
	}

	canonicalJob, err := New("widget", []planner.Step{{Type: planner.StepWriteExportManifest, Payload: canonicalOrder}})
	if err != nil {
		t.Fatalf("canonical order: %v", err)
	}
	reverseJob, err := New("widget", []planner.Step{{Type: planner.StepWriteExportManifest, Payload: reverseOrder}})
	if err != nil {
		t.Fatalf("reverse order: %v", err)
	}
	if canonicalJob.ID == reverseJob.ID {
		t.Fatal("job.New produced the same ID for differently-ordered mutation payloads: it must not perform its own sort (canonicalization is the planner's exclusive responsibility)")
	}
}

// ---------------------------------------------------------------------------
// runtime family shape preserved verbatim through job.Plan()
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_JobPlanPreservesCanonicalOrder(t *testing.T) {
	plan := mutationIdentityPlan(t, "    target Slot: action = hide\n    target Pad: action = hide")
	j, err := FromPlan(plan)
	if err != nil {
		t.Fatalf("FromPlan: %v", err)
	}
	roundTripped := j.Plan()
	if !reflect.DeepEqual(plan.Steps, roundTripped.Steps) {
		t.Fatalf("job.Plan() did not preserve steps verbatim:\noriginal=%#v\nroundTripped=%#v", plan.Steps, roundTripped.Steps)
	}
}
