package handoff

import (
	"os"
	"reflect"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
	"parametron/internal/engine/cad"
	"parametron/internal/engine/job"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
)

// Phase 5 Task 12 permanent identity contract at the handoff boundary:
// Package.JobID and the cloned WriteExportManifestPayload carried by the
// handoff package are pure functions of the canonical planner-produced job --
// handoff performs no independent reordering of mutation families (only
// CloneTableInputs sorts, and that is unrelated table metadata, not target
// mutations). Consequently keep is identical to declaration omission, and
// equivalent declaration-order permutations converge to the same handoff
// identity.
//
// The fixture (mutationIdentityModel / mutationIdentityProduct /
// mutationIdentityContract / mutationIdentityPlan) mirrors the Task 11/12
// planner fixture (runtimeMutationManifestModel /
// runtimeMutationManifestProduct / testProjectionContract in
// internal/authoring/planner), duplicated here because handoff cannot import
// planner's unexported test helpers. Part-scope features: Pad, Slot (owned by
// cmp.leg). Assembly-scope features: Rail, Chamfer, Latch (owned by cmp.root).

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

	tmpFile, err := os.CreateTemp("", "handoff_mutation_identity_*.dsl")
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

func mutationIdentityPackage(t *testing.T, body string) *Package {
	t.Helper()
	j, err := job.FromPlan(mutationIdentityPlan(t, body))
	if err != nil {
		t.Fatalf("job.FromPlan failed: %v", err)
	}
	pkg, err := FromJob(j)
	if err != nil {
		t.Fatalf("FromJob failed: %v", err)
	}
	return pkg
}

// ---------------------------------------------------------------------------
// keep == declaration omission
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_Handoff_KeepEqualsOmission(t *testing.T) {
	omitted := mutationIdentityPackage(t, "")
	kept := mutationIdentityPackage(t, "    target Pad: action = keep")

	if omitted.JobID != kept.JobID {
		t.Fatalf("keep and omission produced different Package.JobID: omitted=%q kept=%q", omitted.JobID, kept.JobID)
	}
	if !reflect.DeepEqual(omitted.Manifest, kept.Manifest) {
		t.Fatalf("keep and omission produced different manifest payload:\nomitted=%#v\nkept=%#v", omitted.Manifest, kept.Manifest)
	}
}

func TestTargetMutationIdentity_Handoff_KeepEqualsOmissionDeterministic10x(t *testing.T) {
	first := mutationIdentityPackage(t, "").JobID
	for i := 0; i < 10; i++ {
		omitted := mutationIdentityPackage(t, "")
		kept := mutationIdentityPackage(t, "    target Pad: action = keep")
		if omitted.JobID != first || kept.JobID != first {
			t.Fatalf("iteration %d: Package.JobID drifted: omitted=%q kept=%q want %q", i, omitted.JobID, kept.JobID, first)
		}
	}
}

// ---------------------------------------------------------------------------
// declaration-order permutation equivalence
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_Handoff_PermutationEquivalence(t *testing.T) {
	orderA := mutationIdentityPackage(t, "    target Slot: action = suppress\n    target Pad: action = suppress")
	orderB := mutationIdentityPackage(t, "    target Pad: action = suppress\n    target Slot: action = suppress")

	if orderA.JobID != orderB.JobID {
		t.Fatalf("equivalent declaration permutations produced different Package.JobID: A=%q B=%q", orderA.JobID, orderB.JobID)
	}
	if !reflect.DeepEqual(orderA.Manifest, orderB.Manifest) {
		t.Fatalf("equivalent declaration permutations produced different manifest payload:\nA=%#v\nB=%#v", orderA.Manifest, orderB.Manifest)
	}
	want := []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}, {Object: "Slot", Suppressed: true}}
	if !reflect.DeepEqual(orderA.Manifest.PartMutations.Suppression, want) {
		t.Fatalf("handoff manifest did not carry the planner-canonicalized order: %#v", orderA.Manifest.PartMutations.Suppression)
	}
}

func TestTargetMutationIdentity_Handoff_PermutationEquivalenceDeterministic10x(t *testing.T) {
	orderA := "    target Slot: action = suppress\n    target Pad: action = suppress"
	orderB := "    target Pad: action = suppress\n    target Slot: action = suppress"
	first := mutationIdentityPackage(t, orderA).JobID
	for i := 0; i < 10; i++ {
		idA := mutationIdentityPackage(t, orderA).JobID
		idB := mutationIdentityPackage(t, orderB).JobID
		if idA != first || idB != first {
			t.Fatalf("iteration %d: Package.JobID drifted: A=%q B=%q want %q", i, idA, idB, first)
		}
	}
}

// ---------------------------------------------------------------------------
// mutation-producing actions affect handoff identity
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_Handoff_ActionsDifferFromOmission(t *testing.T) {
	omitted := mutationIdentityPackage(t, "")
	for _, action := range []string{"suppress", "unsuppress", "hide", "unhide", "delete"} {
		t.Run(action, func(t *testing.T) {
			pkg := mutationIdentityPackage(t, "    target Pad: action = "+action)
			if pkg.JobID == omitted.JobID {
				t.Fatalf("%s Package.JobID equals omission Package.JobID: %q", action, pkg.JobID)
			}
			if reflect.DeepEqual(pkg.Manifest, omitted.Manifest) {
				t.Fatalf("%s manifest payload equals omission manifest payload", action)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handoff performs no independent sorting of mutation families: cloning
// preserves whatever order the job (and therefore the planner) supplied,
// verbatim.
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_HandoffCloneDoesNotSort(t *testing.T) {
	reverseOrder := &planner.ExportManifestMutationCollection{
		Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Zulu", Suppressed: true}, {Object: "Alpha", Suppressed: true}},
	}
	cloned := cloneMutationCollection(reverseOrder)
	if !reflect.DeepEqual(cloned.Suppression, reverseOrder.Suppression) {
		t.Fatalf("handoff clone reordered mutation entries: got %#v want %#v", cloned.Suppression, reverseOrder.Suppression)
	}
}

// TestTargetMutationIdentity_Handoff_PlanRoundTripPreservesCanonicalOrder
// scopes its round-trip assertion to the Task 12 mutation-family surface
// only (PartMutations / AssemblyMutations via pkg.Manifest and via
// pkg.Plan()'s reconstructed WriteExportManifest step): other manifest
// fields such as Verification are a pre-existing, unrelated handoff
// round-trip gap that is out of Task 12 scope.
func TestTargetMutationIdentity_Handoff_PlanRoundTripPreservesCanonicalOrder(t *testing.T) {
	plan := mutationIdentityPlan(t, "    target Slot: action = hide\n    target Pad: action = hide")
	originalPayload := manifestPayloadFromPlanSteps(t, plan.Steps)

	j, err := job.FromPlan(plan)
	if err != nil {
		t.Fatalf("job.FromPlan: %v", err)
	}
	pkg, err := FromJob(j)
	if err != nil {
		t.Fatalf("FromJob: %v", err)
	}
	if !reflect.DeepEqual(pkg.Manifest.PartMutations, originalPayload.PartMutations) {
		t.Fatalf("pkg.Manifest.PartMutations diverged from the original plan: got %#v want %#v", pkg.Manifest.PartMutations, originalPayload.PartMutations)
	}
	if !reflect.DeepEqual(pkg.Manifest.AssemblyMutations, originalPayload.AssemblyMutations) {
		t.Fatalf("pkg.Manifest.AssemblyMutations diverged from the original plan: got %#v want %#v", pkg.Manifest.AssemblyMutations, originalPayload.AssemblyMutations)
	}

	roundTripped := pkg.Plan()
	roundTrippedPayload := manifestPayloadFromPlanSteps(t, roundTripped.Steps)
	if !reflect.DeepEqual(roundTrippedPayload.PartMutations, originalPayload.PartMutations) {
		t.Fatalf("pkg.Plan() PartMutations diverged from the original plan: got %#v want %#v", roundTrippedPayload.PartMutations, originalPayload.PartMutations)
	}
	if !reflect.DeepEqual(roundTrippedPayload.AssemblyMutations, originalPayload.AssemblyMutations) {
		t.Fatalf("pkg.Plan() AssemblyMutations diverged from the original plan: got %#v want %#v", roundTrippedPayload.AssemblyMutations, originalPayload.AssemblyMutations)
	}
}

func manifestPayloadFromPlanSteps(t *testing.T, steps []planner.Step) planner.WriteExportManifestPayload {
	t.Helper()
	for _, step := range steps {
		if payload, ok := step.Payload.(planner.WriteExportManifestPayload); ok {
			return payload
		}
	}
	t.Fatal("no WriteExportManifestPayload step found")
	return planner.WriteExportManifestPayload{}
}
