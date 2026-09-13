package recordemit

import (
	"os"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
	"parametron/internal/engine/cad"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
)

// Phase 5 Task 12 permanent identity contract at the record/package boundary:
// recordemit's package key remains exactly "engine-run:<planHash>" -- a pure
// string-formatting function of whatever plan hash the caller supplies.
// recordemit owns no mutation-family logic itself; the Task 12 consequence at
// this layer is entirely a function of plan hash equality/inequality, which
// is proven directly against real capture-backed plan hashes here rather than
// by inventing arbitrary hash strings. This file is an internal
// (package recordemit) test file specifically to reach the unexported
// packageKeyForPlanHash formatter without introducing any new exported
// package-identity API.
//
// The fixture mirrors the Task 11/12 planner fixture
// (runtimeMutationManifestModel / runtimeMutationManifestProduct /
// testProjectionContract in internal/authoring/planner), duplicated here
// because recordemit cannot import planner's unexported test helpers.

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

func mutationIdentityPlanHash(t *testing.T, body string) string {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "recordemit_mutation_identity_*.dsl")
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
	for _, step := range plan.Steps {
		if payload, ok := step.Payload.(planner.WriteExportManifestPayload); ok {
			if payload.PlanHash == "" {
				t.Fatal("expected non-empty plan hash")
			}
			return payload.PlanHash
		}
	}
	t.Fatal("no WriteExportManifestPayload step found")
	return ""
}

func TestTargetMutationIdentity_PackageKeyFormatContract(t *testing.T) {
	hash := mutationIdentityPlanHash(t, "    target Pad: action = suppress")
	got := packageKeyForPlanHash(hash)
	want := "engine-run:" + hash
	if got != want {
		t.Fatalf("packageKeyForPlanHash: got %q want %q", got, want)
	}
}

func TestTargetMutationIdentity_PackageKey_KeepEqualsOmission(t *testing.T) {
	omittedHash := mutationIdentityPlanHash(t, "")
	keptHash := mutationIdentityPlanHash(t, "    target Pad: action = keep")
	if omittedHash != keptHash {
		t.Fatalf("keep and omission produced different plan hashes: omitted=%q kept=%q", omittedHash, keptHash)
	}
	if packageKeyForPlanHash(omittedHash) != packageKeyForPlanHash(keptHash) {
		t.Fatalf("keep and omission produced different package keys: omitted=%q kept=%q", packageKeyForPlanHash(omittedHash), packageKeyForPlanHash(keptHash))
	}
}

func TestTargetMutationIdentity_PackageKey_MutationChangeChangesPackageKey(t *testing.T) {
	omittedHash := mutationIdentityPlanHash(t, "")
	suppressHash := mutationIdentityPlanHash(t, "    target Pad: action = suppress")
	if omittedHash == suppressHash {
		t.Fatal("expected a mutation-bearing declaration to change the plan hash")
	}
	if packageKeyForPlanHash(omittedHash) == packageKeyForPlanHash(suppressHash) {
		t.Fatal("expected the plan-hash change to propagate to a different package key")
	}
}
