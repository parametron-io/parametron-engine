package semanticmap

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/semantic"
)

func TestProjectParameterAssignments_LengthProjectsToManifestAssignment(t *testing.T) {
	assignments, err := ProjectParameterAssignments(&ParameterProjectionRequest{
		Model:          projectionModel("Length", "par.root.length", "length"),
		Intent:         projectionIntent("length", "par.root.length"),
		ResolvedValues: map[string]any{"length": 35.0},
		Contract:       loadFixture(t, "smoke/freecad-default"),
	})
	if err != nil {
		t.Fatalf("ProjectParameterAssignments returned error: %v", err)
	}

	want := []ManifestParameterAssignment{
		{Name: "length", Value: 35.0, Type: "number", Unit: "mm"},
	}
	if !reflect.DeepEqual(assignments, want) {
		t.Fatalf("unexpected assignments\nwant: %#v\ngot:  %#v", want, assignments)
	}

	data, err := json.Marshal(assignments)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if string(data) != `[{"name":"length","value":35,"type":"number","unit":"mm"}]` {
		t.Fatalf("unexpected assignment JSON: %s", data)
	}
}

func TestProjectParameterAssignments_MissingSemanticMapFailsDeterministically(t *testing.T) {
	request := &ParameterProjectionRequest{
		Model:          projectionModel("Length", "par.root.length", "length"),
		Intent:         projectionIntent("length", "par.root.length"),
		ResolvedValues: map[string]any{"length": 35.0},
	}

	_, err := ProjectParameterAssignments(request)
	if err == nil {
		t.Fatal("expected missing semantic map failure")
	}
	if !errors.Is(err, ErrProjectionMissingSemanticMap) {
		t.Fatalf("expected missing semantic map sentinel, got %v", err)
	}
}

func TestProjectParameterAssignments_ExactIntegerNormalizationDoesNotLeakDiagnostics(t *testing.T) {
	contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	contract.SemanticTypes.ParameterTypes["length"] = ParameterType{
		ValueKind:   "number",
		DefaultUnit: "mm",
		Coercion:    semantic.CoercionRuleNumberToIntegerExact,
	}

	assignments, err := ProjectParameterAssignments(&ParameterProjectionRequest{
		Model:          projectionModel("Length", "par.root.length", "length"),
		Intent:         projectionIntent("length", "par.root.length"),
		ResolvedValues: map[string]any{"length": 35.0},
		Contract:       contract,
	})
	if err != nil {
		t.Fatalf("ProjectParameterAssignments returned error: %v", err)
	}

	data, err := json.Marshal(assignments)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if string(data) != `[{"name":"length","value":35,"type":"number","unit":"mm"}]` {
		t.Fatalf("unexpected assignment JSON after normalization: %s", data)
	}
}

func TestProjectParameterAssignments_ReductionRemovesSemanticAndCaptureMetadata(t *testing.T) {
	contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	contract.MappingID = "mapping.capture-backed.v1"
	contract.SemanticTypes.ParameterTypes["distance_semantic_type"] = ParameterType{
		ValueKind:   "number",
		DefaultUnit: "mm",
		Coercion:    semantic.CoercionRuleNumberToLength,
	}
	contract.CaptureToSemantic.Parameters[0].SemanticType.Map["DistanceNative"] = "distance_semantic_type"

	assignments, err := ProjectParameterAssignments(&ParameterProjectionRequest{
		Model:          projectionModel("DistanceNative", "semantic.parameter.width", "width"),
		Intent:         projectionIntent("width", "semantic.parameter.width"),
		ResolvedValues: map[string]any{"width": 35.0},
		Contract:       contract,
	})
	if err != nil {
		t.Fatalf("ProjectParameterAssignments returned error: %v", err)
	}

	data, err := MarshalReducedExecutionProjectionJSON(assignments, nil, nil)
	if err != nil {
		t.Fatalf("MarshalReducedExecutionProjectionJSON returned error: %v", err)
	}

	for _, forbidden := range []string{
		"semantic.parameter.width",
		"capture.id",
		"distance_semantic_type",
		"coercion",
		"normalization",
		"mapping.capture-backed.v1",
	} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("expected reduced projection to exclude %q, got %s", forbidden, data)
		}
	}
}

func TestProjectParameterAssignments_FractionalCountCoercionFailsDeterministically(t *testing.T) {
	request := &ParameterProjectionRequest{
		Model:          projectionModel("Integer", "par.root.count", "count"),
		Intent:         projectionIntent("count", "par.root.count"),
		ResolvedValues: map[string]any{"count": 3.5},
		Contract:       loadFixture(t, "smoke/freecad-default"),
	}

	first, firstErr := ProjectParameterAssignments(request)
	second, secondErr := ProjectParameterAssignments(request)
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected deterministic coercion failure, got first=%v second=%v", firstErr, secondErr)
	}
	if first != nil || second != nil {
		t.Fatalf("expected no assignments on coercion failure, got first=%#v second=%#v", first, second)
	}
	if !errors.Is(firstErr, ErrProjectionCoercionFailure) || !errors.Is(firstErr, semantic.ErrCoercion) {
		t.Fatalf("expected coercion failure sentinel, got %v", firstErr)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic coercion failure text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func TestProjectParameterAssignments_NonProjectableSemanticTypeFailsBeforeManifestGeneration(t *testing.T) {
	contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	contract.SemanticTypes.ParameterTypes["opaque"] = ParameterType{
		ValueKind:   "string",
		DefaultUnit: "mm",
		Coercion:    semantic.CoercionRuleNumberToDimensionless,
	}
	contract.CaptureToSemantic.Parameters[0].SemanticType.Map["Opaque"] = "opaque"

	_, err := ProjectParameterAssignments(&ParameterProjectionRequest{
		Model:          projectionModel("Opaque", "par.root.opaque", "opaque"),
		Intent:         projectionIntent("opaque", "par.root.opaque"),
		ResolvedValues: map[string]any{"opaque": 12.5},
		Contract:       contract,
	})
	if err == nil {
		t.Fatal("expected non-projectable semantic type to fail")
	}
	if !errors.Is(err, ErrProjectionUnsupportedValueKind) {
		t.Fatalf("expected unsupported value kind sentinel, got %v", err)
	}
}

func TestProjectParameterAssignments_UnsupportedUnitFailsDeterministically(t *testing.T) {
	request := &ParameterProjectionRequest{
		Model:          projectionModel("Float", "par.root.ratio", "ratio"),
		Intent:         projectionIntent("ratio", "par.root.ratio"),
		ResolvedValues: map[string]any{"ratio": 2.5},
		Contract:       loadFixture(t, "smoke/freecad-default"),
	}

	first, err := ProjectParameterAssignments(request)
	if err == nil {
		t.Fatal("expected unsupported unit failure")
	}
	if first != nil {
		t.Fatalf("expected no assignments on unsupported unit failure, got %#v", first)
	}
	if !errors.Is(err, ErrProjectionUnsupportedUnit) {
		t.Fatalf("expected unsupported unit sentinel, got %v", err)
	}

	second, secondErr := ProjectParameterAssignments(request)
	if secondErr == nil {
		t.Fatal("expected repeated unsupported unit failure")
	}
	if second != nil {
		t.Fatalf("expected no assignments on repeated unsupported unit failure, got %#v", second)
	}
	if err.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic unsupported unit text\nfirst:  %q\nsecond: %q", err.Error(), secondErr.Error())
	}
}

func TestProjectParameterAssignments_ProjectionReviewNotReadyFailsDeterministically(t *testing.T) {
	request := &ParameterProjectionRequest{
		Model:          projectionModel("Length", "par.root.length", "length"),
		Intent:         projectionIntent("length", "par.root.length"),
		ResolvedValues: map[string]any{"length": 35.0},
		Contract:       rawFixture(t, "break/projection-review-missing-output"),
	}

	first, firstErr := ProjectParameterAssignments(request)
	second, secondErr := ProjectParameterAssignments(request)
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected not-ready projection contract failure, got first=%v second=%v", firstErr, secondErr)
	}
	if first != nil || second != nil {
		t.Fatalf("expected no assignments when projection contract is not ready, got first=%#v second=%#v", first, second)
	}
	if !errors.Is(firstErr, ErrProjectionContractNotReady) {
		t.Fatalf("expected projection contract not ready sentinel, got %v", firstErr)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic not-ready projection text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func TestProjectParameterAssignments_InvalidSemanticMapFailsDeterministically(t *testing.T) {
	request := &ParameterProjectionRequest{
		Model:          projectionModel("Length", "par.root.length", "length"),
		Intent:         projectionIntent("length", "par.root.length"),
		ResolvedValues: map[string]any{"length": 35.0},
		Contract:       rawFixture(t, "break/invalid-projection-parameter-field"),
	}

	_, err := ProjectParameterAssignments(request)
	if err == nil {
		t.Fatal("expected invalid semantic map failure")
	}
	if !errors.Is(err, ErrProjectionInvalidSemanticMap) {
		t.Fatalf("expected invalid semantic map sentinel, got %v", err)
	}
}

func TestProjectParameterAssignments_MissingResolvedFinalValueFailsDeterministically(t *testing.T) {
	request := &ParameterProjectionRequest{
		Model:          projectionModel("Length", "par.root.length", "length"),
		Intent:         projectionIntent("length", "par.root.length"),
		ResolvedValues: map[string]any{},
		Contract:       loadFixture(t, "smoke/freecad-default"),
	}

	_, err := ProjectParameterAssignments(request)
	if err == nil {
		t.Fatal("expected missing final value failure")
	}
	if !errors.Is(err, ErrProjectionMissingFinalValue) {
		t.Fatalf("expected missing final value sentinel, got %v", err)
	}
}

func TestProjectParameterAssignments_ReturnsDeterministicDefensiveCopies(t *testing.T) {
	request := &ParameterProjectionRequest{
		Model: projectionMultiModel(
			semantic.Parameter{ID: "par.root.width", Name: "width", NativeType: "Length"},
			semantic.Parameter{ID: "par.root.length", Name: "length", NativeType: "Length"},
		),
		Intent: &semantic.ProductIntent{
			Name: "Box",
			ExportedParameters: []semantic.ExportedParameterIntent{
				{DSLParameterName: "width", SemanticParameterID: "par.root.width"},
				{DSLParameterName: "length", SemanticParameterID: "par.root.length"},
			},
		},
		ResolvedValues: map[string]any{
			"width":  12.0,
			"length": 35.0,
		},
		Contract: loadFixture(t, "smoke/freecad-default"),
	}

	first, err := ProjectParameterAssignments(request)
	if err != nil {
		t.Fatalf("first ProjectParameterAssignments returned error: %v", err)
	}
	second, err := ProjectParameterAssignments(request)
	if err != nil {
		t.Fatalf("second ProjectParameterAssignments returned error: %v", err)
	}

	want := []ManifestParameterAssignment{
		{Name: "length", Value: 35.0, Type: "number", Unit: "mm"},
		{Name: "width", Value: 12.0, Type: "number", Unit: "mm"},
	}
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(second, want) {
		t.Fatalf("unexpected deterministic assignments\nfirst:  %#v\nsecond: %#v", first, second)
	}

	first[0].Name = "mutated"
	if reflect.DeepEqual(first, second) {
		t.Fatalf("expected independent assignment slices after mutation\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestProjectParameterAssignments_DeterministicOrdering(t *testing.T) {
	firstRequest := &ParameterProjectionRequest{
		Model: projectionMultiModel(
			semantic.Parameter{ID: "par.root.width", Name: "width", NativeType: "Length"},
			semantic.Parameter{ID: "par.root.length", Name: "length", NativeType: "Length"},
			semantic.Parameter{ID: "par.root.depth", Name: "depth", NativeType: "Length"},
		),
		Intent: &semantic.ProductIntent{
			Name: "Box",
			ExportedParameters: []semantic.ExportedParameterIntent{
				{DSLParameterName: "width", SemanticParameterID: "par.root.width"},
				{DSLParameterName: "length", SemanticParameterID: "par.root.length"},
				{DSLParameterName: "depth", SemanticParameterID: "par.root.depth"},
			},
		},
		ResolvedValues: map[string]any{
			"width":  12.0,
			"length": 35.0,
			"depth":  7.0,
		},
		Contract: loadFixture(t, "smoke/freecad-default"),
	}
	secondRequest := &ParameterProjectionRequest{
		Model: projectionMultiModel(
			semantic.Parameter{ID: "par.root.depth", Name: "depth", NativeType: "Length"},
			semantic.Parameter{ID: "par.root.width", Name: "width", NativeType: "Length"},
			semantic.Parameter{ID: "par.root.length", Name: "length", NativeType: "Length"},
		),
		Intent: &semantic.ProductIntent{
			Name: "Box",
			ExportedParameters: []semantic.ExportedParameterIntent{
				{DSLParameterName: "depth", SemanticParameterID: "par.root.depth"},
				{DSLParameterName: "width", SemanticParameterID: "par.root.width"},
				{DSLParameterName: "length", SemanticParameterID: "par.root.length"},
			},
		},
		ResolvedValues: map[string]any{
			"length": 35.0,
			"depth":  7.0,
			"width":  12.0,
		},
		Contract: loadFixture(t, "smoke/freecad-default"),
	}

	firstAssignments, err := ProjectParameterAssignments(firstRequest)
	if err != nil {
		t.Fatalf("first ProjectParameterAssignments returned error: %v", err)
	}
	secondAssignments, err := ProjectParameterAssignments(secondRequest)
	if err != nil {
		t.Fatalf("second ProjectParameterAssignments returned error: %v", err)
	}

	firstJSON, err := json.Marshal(firstAssignments)
	if err != nil {
		t.Fatalf("failed to marshal first assignments: %v", err)
	}
	secondJSON, err := json.Marshal(secondAssignments)
	if err != nil {
		t.Fatalf("failed to marshal second assignments: %v", err)
	}

	want := []ManifestParameterAssignment{
		{Name: "depth", Value: 7.0, Type: "number", Unit: "mm"},
		{Name: "length", Value: 35.0, Type: "number", Unit: "mm"},
		{Name: "width", Value: 12.0, Type: "number", Unit: "mm"},
	}
	if !reflect.DeepEqual(firstAssignments, want) {
		t.Fatalf("unexpected first assignment ordering/content: %#v", firstAssignments)
	}
	if !reflect.DeepEqual(firstAssignments, secondAssignments) {
		t.Fatalf("assignment ordering changed across equivalent input orders\nfirst:  %#v\nsecond: %#v", firstAssignments, secondAssignments)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("assignment JSON changed across equivalent input orders\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}

func TestMarshalReducedExecutionProjectionJSON_ParameterAssignmentsDeterministicAcrossMapAndInputOrder(t *testing.T) {
	firstAssignments, err := ProjectParameterAssignments(&ParameterProjectionRequest{
		Model: projectionMultiModel(
			semantic.Parameter{ID: "par.root.width", Name: "width", NativeType: "Length"},
			semantic.Parameter{ID: "par.root.length", Name: "length", NativeType: "Length"},
			semantic.Parameter{ID: "par.root.depth", Name: "depth", NativeType: "Length"},
		),
		Intent: &semantic.ProductIntent{
			Name: "Box",
			ExportedParameters: []semantic.ExportedParameterIntent{
				{DSLParameterName: "width", SemanticParameterID: "par.root.width"},
				{DSLParameterName: "length", SemanticParameterID: "par.root.length"},
				{DSLParameterName: "depth", SemanticParameterID: "par.root.depth"},
			},
		},
		ResolvedValues: map[string]any{
			"width":  12.0,
			"length": 35.0,
			"depth":  7.0,
		},
		Contract: loadFixture(t, "smoke/freecad-default"),
	})
	if err != nil {
		t.Fatalf("first ProjectParameterAssignments returned error: %v", err)
	}
	secondAssignments, err := ProjectParameterAssignments(&ParameterProjectionRequest{
		Model: projectionMultiModel(
			semantic.Parameter{ID: "par.root.depth", Name: "depth", NativeType: "Length"},
			semantic.Parameter{ID: "par.root.width", Name: "width", NativeType: "Length"},
			semantic.Parameter{ID: "par.root.length", Name: "length", NativeType: "Length"},
		),
		Intent: &semantic.ProductIntent{
			Name: "Box",
			ExportedParameters: []semantic.ExportedParameterIntent{
				{DSLParameterName: "depth", SemanticParameterID: "par.root.depth"},
				{DSLParameterName: "width", SemanticParameterID: "par.root.width"},
				{DSLParameterName: "length", SemanticParameterID: "par.root.length"},
			},
		},
		ResolvedValues: map[string]any{
			"depth":  7.0,
			"width":  12.0,
			"length": 35.0,
		},
		Contract: loadFixture(t, "smoke/freecad-default"),
	})
	if err != nil {
		t.Fatalf("second ProjectParameterAssignments returned error: %v", err)
	}

	firstJSON, err := MarshalReducedExecutionProjectionJSON(firstAssignments, nil, nil)
	if err != nil {
		t.Fatalf("failed to marshal first reduced projection: %v", err)
	}
	secondJSON, err := MarshalReducedExecutionProjectionJSON(secondAssignments, nil, nil)
	if err != nil {
		t.Fatalf("failed to marshal second reduced projection: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("reduced projection JSON changed across equivalent input orders\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}

func projectionModel(nativeType, id, name string) *semantic.Model {
	return projectionMultiModel(semantic.Parameter{
		ID:         id,
		Name:       name,
		NativeType: nativeType,
	})
}

func projectionMultiModel(parameters ...semantic.Parameter) *semantic.Model {
	return &semantic.Model{
		SchemaVersion: "1.0",
		Parameters:    append([]semantic.Parameter(nil), parameters...),
	}
}

func projectionIntent(name, semanticID string) *semantic.ProductIntent {
	return &semantic.ProductIntent{
		Name: "Box",
		ExportedParameters: []semantic.ExportedParameterIntent{
			{DSLParameterName: name, SemanticParameterID: semanticID},
		},
	}
}
