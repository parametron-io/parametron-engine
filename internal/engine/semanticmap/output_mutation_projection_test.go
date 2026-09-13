package semanticmap

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/semantic"
)

func TestProjectOutputs_ProjectsPDFSTEPAndCSVDeterministically(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)
	intent := outputMutationProjectionIntent()

	outputs, err := ProjectOutputs(&OutputProjectionRequest{
		Model:           model,
		Intent:          intent,
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("ProjectOutputs returned error: %v", err)
	}

	want := []ManifestOutput{
		{Type: "csv", Target: "AssemblyA", Rollup: "assembly"},
		{Type: "pdf"},
		{Type: "step", Object: "AssemblyA"},
	}
	if !reflect.DeepEqual(outputs, want) {
		t.Fatalf("unexpected outputs\nwant: %#v\ngot:  %#v", want, outputs)
	}

	data, err := json.Marshal(outputs)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if strings.Contains(string(data), "Assembly A") || strings.Contains(string(data), "Body") {
		t.Fatalf("expected exact semantic names only, got %s", data)
	}
}

func TestProjectOutputs_MissingSemanticMapFailsDeterministically(t *testing.T) {
	_, err := ProjectOutputs(&OutputProjectionRequest{
		Model:  outputMutationProjectionModel(),
		Intent: outputMutationProjectionIntent(),
	})
	if err == nil {
		t.Fatal("expected missing semantic map failure")
	}
	if !errors.Is(err, ErrProjectionMissingSemanticMap) {
		t.Fatalf("expected missing semantic map sentinel, got %v", err)
	}
}

func TestProjectOutputs_InvalidSemanticMapFailsDeterministically(t *testing.T) {
	_, err := ProjectOutputs(&OutputProjectionRequest{
		Model:    outputMutationProjectionModel(),
		Intent:   outputMutationProjectionIntent(),
		Contract: rawFixture(t, "break/invalid-projection-parameter-field"),
	})
	if err == nil {
		t.Fatal("expected invalid semantic map failure")
	}
	if !errors.Is(err, ErrProjectionInvalidSemanticMap) {
		t.Fatalf("expected invalid semantic map sentinel, got %v", err)
	}
}

func TestProjectOutputs_InvalidSemanticModelFailsDeterministically(t *testing.T) {
	first, firstErr := ProjectOutputs(&OutputProjectionRequest{
		Model:    &semantic.Model{},
		Intent:   outputMutationProjectionIntent(),
		Contract: loadFixture(t, "smoke/freecad-default"),
	})
	second, secondErr := ProjectOutputs(&OutputProjectionRequest{
		Model:    &semantic.Model{},
		Intent:   outputMutationProjectionIntent(),
		Contract: loadFixture(t, "smoke/freecad-default"),
	})
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected invalid semantic model failure, got first=%v second=%v", firstErr, secondErr)
	}
	if first != nil || second != nil {
		t.Fatalf("expected no outputs on invalid model failure, got first=%#v second=%#v", first, second)
	}
	if !errors.Is(firstErr, ErrProjectionInvalidModel) {
		t.Fatalf("expected invalid model sentinel, got %v", firstErr)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic invalid model text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func TestProjectOutputs_MissingSemanticIntentFailsDeterministically(t *testing.T) {
	_, err := ProjectOutputs(&OutputProjectionRequest{
		Model:    outputMutationProjectionModel(),
		Contract: loadFixture(t, "smoke/freecad-default"),
	})
	if err == nil {
		t.Fatal("expected missing semantic intent failure")
	}
	if !errors.Is(err, ErrProjectionMissingSemanticIntent) {
		t.Fatalf("expected missing semantic intent sentinel, got %v", err)
	}
}

func TestProjectOutputs_MissingIdentityLinkageFailsDeterministically(t *testing.T) {
	_, err := ProjectOutputs(&OutputProjectionRequest{
		Model:    outputMutationProjectionModel(),
		Intent:   outputMutationProjectionIntentWithOutputs(semantic.OutputIntent{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.assembly", TargetNameSource: "identity_linkage"}),
		Contract: loadFixture(t, "smoke/freecad-default"),
	})
	if err == nil {
		t.Fatal("expected missing identity linkage failure")
	}
	if !errors.Is(err, ErrProjectionMissingIdentityLinkage) {
		t.Fatalf("expected missing identity linkage sentinel, got %v", err)
	}
}

func TestProjectOutputs_MissingTargetFailsDeterministically(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)

	first, firstErr := ProjectOutputs(&OutputProjectionRequest{
		Model: model,
		Intent: outputMutationProjectionIntentWithOutputs(
			semantic.OutputIntent{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.missing", TargetNameSource: "identity_linkage"},
		),
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	second, secondErr := ProjectOutputs(&OutputProjectionRequest{
		Model: model,
		Intent: outputMutationProjectionIntentWithOutputs(
			semantic.OutputIntent{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.missing", TargetNameSource: "identity_linkage"},
		),
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected missing target failure, got first=%v second=%v", firstErr, secondErr)
	}
	if first != nil || second != nil {
		t.Fatalf("expected no outputs on missing target failure, got first=%#v second=%#v", first, second)
	}
	if !errors.Is(firstErr, ErrProjectionNonProjectableOutput) {
		t.Fatalf("expected non-projectable output sentinel, got %v", firstErr)
	}
	var projectionErr *ProjectionError
	if !errors.As(firstErr, &projectionErr) {
		t.Fatalf("expected ProjectionError, got %T", firstErr)
	}
	wantProblems := []string{
		`semantic output "step" is not projectable: missing target for assembly "cmp.missing"`,
	}
	if !reflect.DeepEqual(projectionErr.ProblemList(), wantProblems) {
		t.Fatalf("unexpected problem list\nwant: %#v\ngot:  %#v", wantProblems, projectionErr.ProblemList())
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic failure text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func TestProjectOutputs_MissingIdentityLinkageEntryFailsDeterministically(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)
	filtered := make([]IdentityLink, 0, len(linkage.Entries))
	for _, entry := range linkage.Entries {
		if entry.EntityKind == "component" && entry.SemanticID == "cmp.assembly" {
			continue
		}
		filtered = append(filtered, entry)
	}
	linkage.Entries = filtered

	_, err := ProjectOutputs(&OutputProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntentWithOutputs(semantic.OutputIntent{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.assembly", TargetNameSource: "identity_linkage"}),
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err == nil {
		t.Fatal("expected missing identity linkage entry failure")
	}
	if !errors.Is(err, ErrProjectionMissingIdentityLinkage) {
		t.Fatalf("expected missing identity linkage sentinel, got %v", err)
	}
}

func TestProjectOutputs_NoCaseInsensitiveMatching(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)

	_, err := ProjectOutputs(&OutputProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntentWithOutputs(semantic.OutputIntent{OutputType: "STEP", TargetEntityKind: "assembly", TargetSemanticID: "cmp.assembly", TargetNameSource: "identity_linkage"}),
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err == nil {
		t.Fatal("expected exact-key output lookup failure")
	}
	if !errors.Is(err, ErrProjectionNonProjectableOutput) {
		t.Fatalf("expected non-projectable output sentinel, got %v", err)
	}
	if !strings.Contains(err.Error(), `semantic output format "STEP" is not projectable`) {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestProjectOutputs_NoDisplayNameOrCADFallback(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)

	outputs, err := ProjectOutputs(&OutputProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntentWithOutputs(semantic.OutputIntent{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.assembly", TargetNameSource: "identity_linkage"}),
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("ProjectOutputs returned error: %v", err)
	}
	if len(outputs) != 1 || outputs[0].Object != "AssemblyA" {
		t.Fatalf("expected exact semantic object name, got %#v", outputs)
	}
}

func TestProjectOutputs_ReductionRemovesMappingAndFallbackMetadata(t *testing.T) {
	contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	contract.MappingID = "mapping.output.capture-backed.v1"
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)

	outputs, err := ProjectOutputs(&OutputProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntent(),
		Contract:        contract,
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("ProjectOutputs returned error: %v", err)
	}

	data, err := MarshalReducedExecutionProjectionJSON(nil, outputs, nil)
	if err != nil {
		t.Fatalf("MarshalReducedExecutionProjectionJSON returned error: %v", err)
	}

	for _, forbidden := range []string{
		"mapping.output.capture-backed.v1",
		"cmp.assembly",
		"identity_linkage",
		"DisplayName",
		"Assembly A",
		"Part B",
		"fallback",
		"caseInsensitive",
		"adapterInference",
	} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("expected reduced output projection to exclude %q, got %s", forbidden, data)
		}
	}
}

func TestProjectOutputs_UnsupportedOutputScopeFailsDeterministically(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)

	_, err := ProjectOutputs(&OutputProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntentWithOutputs(semantic.OutputIntent{OutputType: "csv", TargetEntityKind: "assembly", TargetSemanticID: "cmp.assembly", TargetNameSource: "identity_linkage", Scope: "boom"}),
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err == nil {
		t.Fatal("expected unsupported output scope failure")
	}
	if !errors.Is(err, ErrProjectionNonProjectableOutput) {
		t.Fatalf("expected non-projectable output sentinel, got %v", err)
	}
}

func TestProjectOutputs_DeterministicOrderingIndependentOfSemanticInputOrder(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)

	first, err := ProjectOutputs(&OutputProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntent(),
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("first ProjectOutputs returned error: %v", err)
	}
	second, err := ProjectOutputs(&OutputProjectionRequest{
		Model: outputMutationProjectionModel(),
		Intent: outputMutationProjectionIntentWithOutputs(
			semantic.OutputIntent{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.assembly", TargetNameSource: "identity_linkage"},
			semantic.OutputIntent{OutputType: "pdf"},
			semantic.OutputIntent{OutputType: "csv", TargetEntityKind: "assembly", TargetSemanticID: "cmp.assembly", TargetNameSource: "identity_linkage", Scope: "assembly"},
		),
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("second ProjectOutputs returned error: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected deterministic outputs across input orders\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestProjectMutations_MissingSemanticMapFailsDeterministically(t *testing.T) {
	_, err := ProjectMutations(&MutationProjectionRequest{
		Model:  outputMutationProjectionModel(),
		Intent: outputMutationProjectionIntent(),
	})
	if err == nil {
		t.Fatal("expected missing semantic map failure")
	}
	if !errors.Is(err, ErrProjectionMissingSemanticMap) {
		t.Fatalf("expected missing semantic map sentinel, got %v", err)
	}
}

func TestProjectMutations_InvalidSemanticModelFailsDeterministically(t *testing.T) {
	_, err := ProjectMutations(&MutationProjectionRequest{
		Model:    &semantic.Model{},
		Intent:   outputMutationProjectionIntent(),
		Contract: loadFixture(t, "smoke/freecad-default"),
	})
	if err == nil {
		t.Fatal("expected invalid semantic model failure")
	}
	if !errors.Is(err, ErrProjectionInvalidModel) {
		t.Fatalf("expected invalid model sentinel, got %v", err)
	}
}

func TestProjectMutations_MissingSemanticIntentFailsDeterministically(t *testing.T) {
	_, err := ProjectMutations(&MutationProjectionRequest{
		Model:    outputMutationProjectionModel(),
		Contract: loadFixture(t, "smoke/freecad-default"),
	})
	if err == nil {
		t.Fatal("expected missing semantic intent failure")
	}
	if !errors.Is(err, ErrProjectionMissingSemanticIntent) {
		t.Fatalf("expected missing semantic intent sentinel, got %v", err)
	}
}

func TestProjectMutations_ProjectionReviewNotReadyFailsDeterministically(t *testing.T) {
	_, err := ProjectMutations(&MutationProjectionRequest{
		Model:    outputMutationProjectionModel(),
		Intent:   outputMutationProjectionIntent(),
		Contract: rawFixture(t, "break/projection-review-missing-mutation"),
	})
	if err == nil {
		t.Fatal("expected projection contract not ready failure")
	}
	if !errors.Is(err, ErrProjectionContractNotReady) {
		t.Fatalf("expected projection contract not ready sentinel, got %v", err)
	}
}

func TestProjectMutations_ProjectsStructuredMutationsWithoutMetadataLeakage(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)

	projection, err := ProjectMutations(&MutationProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntent(),
		ResolvedValues:  map[string]any{"part_number_a": "PN-1", "width": 35.0},
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("ProjectMutations returned error: %v", err)
	}

	want := &ManifestMutationProjection{
		Assembly: &ManifestMutationCollection{
			Properties:  []ManifestPropertyMutation{{Object: "AssemblyA", Property: "part_number_a", Value: "PN-1"}},
			Suppression: []ManifestSuppressionMutation{{Object: "Bracket-1", Suppressed: true}},
		},
		Part: &ManifestMutationCollection{
			Parameters:  []ManifestParameterMutation{{Object: "GroupB", Property: "width", ValueParam: "width", Type: "number", Unit: "mm"}},
			Suppression: []ManifestSuppressionMutation{{Object: "Pocket", Suppressed: false}},
		},
	}
	if !reflect.DeepEqual(projection, want) {
		t.Fatalf("unexpected mutation projection\nwant: %#v\ngot:  %#v", want, projection)
	}

	data, err := json.Marshal(projection)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	for _, forbidden := range []string{"cmp.", "par.", "meta.", "feat.", "capture", "mappingId", "semanticType"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("expected manifest JSON to exclude semantic/capture metadata %q, got %s", forbidden, data)
		}
	}
}

func TestProjectMutations_ReductionRemovesMappingAndDisplayMetadata(t *testing.T) {
	contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	contract.MappingID = "mapping.mutation.capture-backed.v1"
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)

	projection, err := ProjectMutations(&MutationProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntent(),
		ResolvedValues:  map[string]any{"part_number_a": "PN-1", "width": 35.0},
		Contract:        contract,
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("ProjectMutations returned error: %v", err)
	}

	data, err := MarshalReducedExecutionProjectionJSON(nil, nil, projection)
	if err != nil {
		t.Fatalf("MarshalReducedExecutionProjectionJSON returned error: %v", err)
	}

	for _, forbidden := range []string{
		"mapping.mutation.capture-backed.v1",
		"meta.a",
		"feat.assembly",
		"feat.part",
		"par.b",
		"Display",
		"Bracket 1",
		"Pocket Display",
		"fallback",
		"identity_linkage",
	} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("expected reduced mutation projection to exclude %q, got %s", forbidden, data)
		}
	}
}

func TestProjectMutations_AssemblyVsPartPlacementIsDeterministic(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)

	first, err := ProjectMutations(&MutationProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntent(),
		ResolvedValues:  map[string]any{"part_number_a": "PN-1", "width": 35.0},
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("first ProjectMutations returned error: %v", err)
	}
	second, err := ProjectMutations(&MutationProjectionRequest{
		Model: outputMutationProjectionModel(),
		Intent: outputMutationProjectionIntentWithMutations(
			semantic.MutationIntent{OperationKind: "unsuppress", TargetEntityKind: "feature", TargetSemanticID: "feat.part", Scope: "part", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter"}},
			semantic.MutationIntent{OperationKind: "suppress", TargetEntityKind: "feature", TargetSemanticID: "feat.assembly", Scope: "assembly", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter"}},
			semantic.MutationIntent{OperationKind: "set_property", TargetEntityKind: "metadata", TargetSemanticID: "meta.a", Scope: "assembly", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter", DSLParameterName: "part_number_a"}},
			semantic.MutationIntent{OperationKind: "set_parameter", TargetEntityKind: "parameter", TargetSemanticID: "par.b", TargetField: "width", Scope: "part", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter", DSLParameterName: "width", SemanticParameterID: "par.b"}},
		),
		ResolvedValues:  map[string]any{"part_number_a": "PN-1", "width": 35.0},
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("second ProjectMutations returned error: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected deterministic mutations across input orders\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestProjectMutations_UnsupportedMutationOperationFailsDeterministically(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)

	_, err := ProjectMutations(&MutationProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntentWithMutations(semantic.MutationIntent{OperationKind: "delete", TargetEntityKind: "feature", TargetSemanticID: "feat.part", Scope: "part", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter"}}),
		ResolvedValues:  map[string]any{},
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if err == nil {
		t.Fatal("expected unsupported mutation failure")
	}
	if !errors.Is(err, ErrProjectionNonProjectableMutation) {
		t.Fatalf("expected non-projectable mutation sentinel, got %v", err)
	}
}

func TestMarshalReducedExecutionProjectionJSON_OutputAndMutationDeterminismAcrossEquivalentInputs(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)
	contract := loadFixture(t, "smoke/freecad-default")

	firstOutputs, err := ProjectOutputs(&OutputProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntent(),
		Contract:        contract,
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("first ProjectOutputs returned error: %v", err)
	}
	firstMutations, err := ProjectMutations(&MutationProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntent(),
		ResolvedValues:  map[string]any{"part_number_a": "PN-1", "width": 35.0},
		Contract:        contract,
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("first ProjectMutations returned error: %v", err)
	}

	secondOutputs, err := ProjectOutputs(&OutputProjectionRequest{
		Model: outputMutationProjectionModel(),
		Intent: outputMutationProjectionIntentWithOutputs(
			semantic.OutputIntent{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.assembly", TargetNameSource: "identity_linkage"},
			semantic.OutputIntent{OutputType: "pdf"},
			semantic.OutputIntent{OutputType: "csv", TargetEntityKind: "assembly", TargetSemanticID: "cmp.assembly", TargetNameSource: "identity_linkage", Scope: "assembly"},
		),
		Contract:        contract,
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("second ProjectOutputs returned error: %v", err)
	}
	secondMutations, err := ProjectMutations(&MutationProjectionRequest{
		Model: outputMutationProjectionModel(),
		Intent: outputMutationProjectionIntentWithMutations(
			semantic.MutationIntent{OperationKind: "unsuppress", TargetEntityKind: "feature", TargetSemanticID: "feat.part", Scope: "part", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter"}},
			semantic.MutationIntent{OperationKind: "suppress", TargetEntityKind: "feature", TargetSemanticID: "feat.assembly", Scope: "assembly", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter"}},
			semantic.MutationIntent{OperationKind: "set_property", TargetEntityKind: "metadata", TargetSemanticID: "meta.a", Scope: "assembly", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter", DSLParameterName: "part_number_a"}},
			semantic.MutationIntent{OperationKind: "set_parameter", TargetEntityKind: "parameter", TargetSemanticID: "par.b", TargetField: "width", Scope: "part", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter", DSLParameterName: "width", SemanticParameterID: "par.b"}},
		),
		ResolvedValues: map[string]any{
			"width":         35.0,
			"part_number_a": "PN-1",
		},
		Contract:        contract,
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("second ProjectMutations returned error: %v", err)
	}

	firstJSON, err := MarshalReducedExecutionProjectionJSON(nil, firstOutputs, firstMutations)
	if err != nil {
		t.Fatalf("failed to marshal first reduced projection: %v", err)
	}
	secondJSON, err := MarshalReducedExecutionProjectionJSON(nil, secondOutputs, secondMutations)
	if err != nil {
		t.Fatalf("failed to marshal second reduced projection: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("reduced projection JSON changed across equivalent inputs\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}

func TestProjectMutations_IncompleteMutationValueSourceFailsDeterministically(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)

	first, firstErr := ProjectMutations(&MutationProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntentWithMutations(semantic.MutationIntent{OperationKind: "set_property", TargetEntityKind: "metadata", TargetSemanticID: "meta.a", Scope: "assembly", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter"}}),
		ResolvedValues:  map[string]any{},
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	second, secondErr := ProjectMutations(&MutationProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntentWithMutations(semantic.MutationIntent{OperationKind: "set_property", TargetEntityKind: "metadata", TargetSemanticID: "meta.a", Scope: "assembly", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter"}}),
		ResolvedValues:  map[string]any{},
		Contract:        loadFixture(t, "smoke/freecad-default"),
		IdentityLinkage: linkage,
	})
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected incomplete value source failure, got first=%v second=%v", firstErr, secondErr)
	}
	if first != nil || second != nil {
		t.Fatalf("expected no mutations on incomplete value source failure, got first=%#v second=%#v", first, second)
	}
	if !errors.Is(firstErr, ErrProjectionNonProjectableMutation) {
		t.Fatalf("expected non-projectable mutation sentinel, got %v", firstErr)
	}
	var projectionErr *ProjectionError
	if !errors.As(firstErr, &projectionErr) {
		t.Fatalf("expected ProjectionError, got %T", firstErr)
	}
	wantProblems := []string{
		`semantic mutation operation "set_property" requires semantic mutation value intent for manifest field "value"`,
	}
	if !reflect.DeepEqual(projectionErr.ProblemList(), wantProblems) {
		t.Fatalf("unexpected problem list\nwant: %#v\ngot:  %#v", wantProblems, projectionErr.ProblemList())
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic failure text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func mustLinkOutputMutationProjectionModel(t *testing.T, model *semantic.Model) *IdentityLinkage {
	t.Helper()

	linkage, err := LinkCaptureIdentities(loadFixture(t, "smoke/freecad-default"), model)
	if err != nil {
		t.Fatalf("LinkCaptureIdentities returned error: %v", err)
	}
	return linkage
}

func outputMutationProjectionIntent() *semantic.ProductIntent {
	return outputMutationProjectionIntentWithOutputsAndMutations(
		[]semantic.OutputIntent{
			{OutputType: "pdf"},
			{OutputType: "csv", TargetEntityKind: "assembly", TargetSemanticID: "cmp.assembly", TargetNameSource: "identity_linkage", Scope: "assembly"},
			{OutputType: "step", TargetEntityKind: "assembly", TargetSemanticID: "cmp.assembly", TargetNameSource: "identity_linkage"},
		},
		[]semantic.MutationIntent{
			{OperationKind: "set_parameter", TargetEntityKind: "parameter", TargetSemanticID: "par.b", TargetField: "width", Scope: "part", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter", DSLParameterName: "width", SemanticParameterID: "par.b"}},
			{OperationKind: "set_property", TargetEntityKind: "metadata", TargetSemanticID: "meta.a", Scope: "assembly", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter", DSLParameterName: "part_number_a"}},
			{OperationKind: "suppress", TargetEntityKind: "feature", TargetSemanticID: "feat.assembly", Scope: "assembly", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter"}},
			{OperationKind: "unsuppress", TargetEntityKind: "feature", TargetSemanticID: "feat.part", Scope: "part", ValueSource: semantic.MutationValueSource{Kind: "dsl_parameter"}},
		},
	)
}

func outputMutationProjectionIntentWithOutputs(outputs ...semantic.OutputIntent) *semantic.ProductIntent {
	return outputMutationProjectionIntentWithOutputsAndMutations(outputs, nil)
}

func outputMutationProjectionIntentWithMutations(mutations ...semantic.MutationIntent) *semantic.ProductIntent {
	return outputMutationProjectionIntentWithOutputsAndMutations(nil, mutations)
}

func outputMutationProjectionIntentWithOutputsAndMutations(outputs []semantic.OutputIntent, mutations []semantic.MutationIntent) *semantic.ProductIntent {
	return &semantic.ProductIntent{
		Name:                   "Widget",
		RequestedOutputFormats: []string{"pdf", "step", "csv"},
		Outputs:                append([]semantic.OutputIntent(nil), outputs...),
		Mutations:              append([]semantic.MutationIntent(nil), mutations...),
	}
}

func outputMutationProjectionModel() *semantic.Model {
	return &semantic.Model{
		SchemaVersion:   semantic.SchemaVersion,
		RootComponentID: "cmp.assembly",
		Components: []semantic.Component{
			{ID: "cmp.part", Kind: "part", Name: "PartB", DisplayName: "Part B", ParentID: "cmp.assembly", ChildrenIDs: []string{}, Quantity: 1},
			{ID: "cmp.assembly", Kind: "assembly", Name: "AssemblyA", DisplayName: "Assembly A", ChildrenIDs: []string{"cmp.part"}, Quantity: 1},
		},
		Features: []semantic.Feature{
			{ID: "feat.part", ComponentID: "cmp.part", Name: "Pocket", DisplayName: "Pocket Display", NativeType: "PartDesign::Pocket"},
			{ID: "feat.assembly", ComponentID: "cmp.assembly", Name: "Bracket-1", DisplayName: "Bracket 1", NativeType: "App::Part"},
		},
		ParameterGroups: []semantic.ParameterGroup{
			{ID: "grp.b", OwnerComponentID: "cmp.part", Name: "GroupB", DisplayName: "Group B", GroupKind: "parameter_group", NativeType: "Spreadsheet::Sheet", Observable: true, Writable: true},
			{ID: "grp.a", OwnerComponentID: "cmp.assembly", Name: "GroupA", DisplayName: "Group A", GroupKind: "parameter_group", NativeType: "Spreadsheet::Sheet", Observable: true, Writable: true},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.b", OwnerKind: semantic.OwnerKindGroup, OwnerID: "grp.b", ComponentID: "cmp.part", GroupID: "grp.b", Name: "width", DisplayName: "Width", ValueType: "number", NativeType: "Length", Unit: "mm", Observable: true, Writable: true},
			{ID: "par.a", OwnerKind: semantic.OwnerKindGroup, OwnerID: "grp.a", ComponentID: "cmp.assembly", GroupID: "grp.a", Name: "length", DisplayName: "Length", ValueType: "number", NativeType: "Length", Unit: "mm", Observable: true, Writable: true},
		},
		Metadata: []semantic.Metadata{
			{ID: "meta.b", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.part", ComponentID: "cmp.part", Key: "part_number_b", DisplayName: "Part Number B", ValueType: "string", NativeType: "App::PropertyString", Observable: true, Writable: true},
			{ID: "meta.a", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.assembly", ComponentID: "cmp.assembly", Key: "part_number_a", DisplayName: "Part Number A", ValueType: "string", NativeType: "App::PropertyString", Observable: true, Writable: true},
		},
	}
}
