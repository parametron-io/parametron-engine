package semanticmap

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/semantic"
)

func metadataResolutionModel() *semantic.Model {
	return &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "metadata_resolution_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{
				ID:          "cmp.root",
				Kind:        "assembly",
				Name:        "RootAssembly",
				DisplayName: "Root Assembly",
				NativeRef:   "Assembly01",
				ChildrenIDs: []string{"cmp.bracket"},
				Quantity:    1,
			},
			{
				ID:          "cmp.bracket",
				Kind:        "part",
				Name:        "bracket",
				DisplayName: "Bracket",
				NativeRef:   "Body01",
				ChildrenIDs: []string{},
				Quantity:    1,
			},
		},
		Features: []semantic.Feature{
			{
				ID:          "fea.mountingBracket",
				ComponentID: "cmp.bracket",
				Name:        "mounting_bracket",
				DisplayName: "Mounting Bracket",
				NativeRef:   "MountingBracket01",
			},
		},
		Metadata: []semantic.Metadata{
			{
				ID:          "meta.bracket.material",
				OwnerKind:   semantic.OwnerKindComponent,
				OwnerID:     "cmp.bracket",
				ComponentID: "cmp.bracket",
				Key:         "material",
				DisplayName: "Material",
				ValueType:   "string",
				NativeType:  "App::PropertyString",
				Observable:  true,
				Writable:    true,
			},
			{
				ID:          "meta.feature.partNumber",
				OwnerKind:   semantic.OwnerKindFeature,
				OwnerID:     "fea.mountingBracket",
				ComponentID: "cmp.bracket",
				Key:         "part_number",
				DisplayName: "Part Number",
				ValueType:   "string",
				NativeType:  "App::PropertyString",
				Observable:  true,
				Writable:    true,
			},
			{
				ID:          "meta.feature.revision",
				OwnerKind:   semantic.OwnerKindFeature,
				OwnerID:     "fea.mountingBracket",
				ComponentID: "cmp.bracket",
				Key:         "revision",
				DisplayName: "Revision",
				ValueType:   "number",
				NativeType:  "App::PropertyInteger",
				Observable:  true,
				Writable:    true,
			},
			{
				ID:          "meta.root.assemblyCount",
				OwnerKind:   semantic.OwnerKindComponent,
				OwnerID:     "cmp.root",
				ComponentID: "cmp.root",
				Key:         "assembly_count",
				DisplayName: "Assembly Count",
				ValueType:   "number",
				NativeType:  "App::PropertyLength",
				Observable:  true,
				Writable:    true,
			},
		},
	}
}

func mustMetadataResolutionLinkage(t *testing.T, contract *SemanticMap, model *semantic.Model) *IdentityLinkage {
	t.Helper()
	linkage, err := LinkCaptureIdentities(contract, model)
	if err != nil {
		t.Fatalf("LinkCaptureIdentities returned error: %v", err)
	}
	return linkage
}

func metadataResolutionRequest(contract *SemanticMap, model *semantic.Model, linkage *IdentityLinkage, target, key string) *TargetMetadataResolutionRequest {
	return &TargetMetadataResolutionRequest{
		Model:           model,
		IdentityLinkage: linkage,
		Contract:        contract,
		TargetName:      target,
		MetadataKey:     key,
	}
}

func mustResolveTargetMetadata(t *testing.T, request *TargetMetadataResolutionRequest) ResolvedTargetMetadata {
	t.Helper()
	resolved, err := ResolveTargetMetadata(request)
	if err != nil {
		t.Fatalf("ResolveTargetMetadata returned error: %v", err)
	}
	return resolved
}

func resolveTargetMetadataError(t *testing.T, request *TargetMetadataResolutionRequest) *MetadataResolutionError {
	t.Helper()
	resolved, err := ResolveTargetMetadata(request)
	if err == nil {
		t.Fatalf("expected ResolveTargetMetadata error, got %#v", resolved)
	}
	var typed *MetadataResolutionError
	if !errors.As(err, &typed) {
		t.Fatalf("expected *MetadataResolutionError, got %T: %v", err, err)
	}
	return typed
}

func TestResolveTargetMetadata_ComponentTargetSuccess(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	model := metadataResolutionModel()
	linkage := mustMetadataResolutionLinkage(t, contract, model)

	resolved := mustResolveTargetMetadata(t, metadataResolutionRequest(contract, model, linkage, "bracket", "material"))
	if resolved.Metadata.ID != "meta.bracket.material" {
		t.Fatalf("unexpected metadata ID: %#v", resolved)
	}
	if resolved.NativeObject != "Body01" {
		t.Fatalf("expected native owner 'Body01', got %q", resolved.NativeObject)
	}
	if resolved.NativeProperty != "material" {
		t.Fatalf("expected native property 'material', got %q", resolved.NativeProperty)
	}
	if resolved.ValueKind != MetadataValueKindString {
		t.Fatalf("expected value kind %q, got %q", MetadataValueKindString, resolved.ValueKind)
	}
	if resolved.Target.SemanticID != "cmp.bracket" {
		t.Fatalf("expected semantic target cmp.bracket, got %#v", resolved.Target)
	}
}

func TestResolveTargetMetadata_FeatureTargetSuccess(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	model := metadataResolutionModel()
	linkage := mustMetadataResolutionLinkage(t, contract, model)

	resolved := mustResolveTargetMetadata(t, metadataResolutionRequest(contract, model, linkage, "mounting_bracket", "part_number"))
	if resolved.Metadata.ID != "meta.feature.partNumber" {
		t.Fatalf("unexpected metadata ID: %#v", resolved)
	}
	if resolved.NativeObject != "MountingBracket01" {
		t.Fatalf("expected native owner 'MountingBracket01', got %q", resolved.NativeObject)
	}
	if resolved.NativeProperty != "part_number" {
		t.Fatalf("expected native property 'part_number', got %q", resolved.NativeProperty)
	}
	if resolved.ValueKind != MetadataValueKindString {
		t.Fatalf("expected value kind %q, got %q", MetadataValueKindString, resolved.ValueKind)
	}
}

func TestResolveTargetMetadata_IntegerAndNumberValueKinds(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	model := metadataResolutionModel()
	linkage := mustMetadataResolutionLinkage(t, contract, model)

	integer := mustResolveTargetMetadata(t, metadataResolutionRequest(contract, model, linkage, "mounting_bracket", "revision"))
	if integer.ValueKind != MetadataValueKindInteger {
		t.Fatalf("expected integer value kind, got %q", integer.ValueKind)
	}

	number := mustResolveTargetMetadata(t, metadataResolutionRequest(contract, model, linkage, "RootAssembly", "assembly_count"))
	if number.ValueKind != MetadataValueKindNumber {
		t.Fatalf("expected number value kind, got %q", number.ValueKind)
	}
	if number.NativeObject != "Assembly01" {
		t.Fatalf("expected native owner 'Assembly01', got %q", number.NativeObject)
	}
}

func TestResolveTargetMetadata_TargetResolutionErrors(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	model := metadataResolutionModel()
	linkage := mustMetadataResolutionLinkage(t, contract, model)

	t.Run("not found", func(t *testing.T) {
		typed := resolveTargetMetadataError(t, metadataResolutionRequest(contract, model, linkage, "missing_target", "material"))
		if typed.Code != metadataResolutionCodeTargetNotFound {
			t.Fatalf("expected code %q, got %#v", metadataResolutionCodeTargetNotFound, typed)
		}
		if !errors.Is(typed, semantic.ErrSemanticTargetNotFound) {
			t.Fatalf("expected wrapped semantic target not found, got %v", typed)
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		ambiguousModel := metadataResolutionModel()
		ambiguousModel.Features = append(ambiguousModel.Features, semantic.Feature{
			ID:          "fea.mountingBracketOther",
			ComponentID: "cmp.bracket",
			Name:        "mounting_bracket",
			DisplayName: "Mounting Bracket Other",
			NativeRef:   "MountingBracket02",
		})
		typed := resolveTargetMetadataError(t, metadataResolutionRequest(contract, ambiguousModel, linkage, "mounting_bracket", "part_number"))
		if typed.Code != metadataResolutionCodeTargetAmbiguous {
			t.Fatalf("expected code %q, got %#v", metadataResolutionCodeTargetAmbiguous, typed)
		}
		if !errors.Is(typed, semantic.ErrSemanticTargetAmbiguous) {
			t.Fatalf("expected wrapped semantic target ambiguous, got %v", typed)
		}
	})
}

func TestResolveTargetMetadata_ReferenceResolutionErrors(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	model := metadataResolutionModel()
	linkage := mustMetadataResolutionLinkage(t, contract, model)

	t.Run("not found", func(t *testing.T) {
		typed := resolveTargetMetadataError(t, metadataResolutionRequest(contract, model, linkage, "bracket", "supplier"))
		if typed.Code != metadataResolutionCodeReferenceNotFound {
			t.Fatalf("expected code %q, got %#v", metadataResolutionCodeReferenceNotFound, typed)
		}
	})

	t.Run("no cross-target fallback", func(t *testing.T) {
		typed := resolveTargetMetadataError(t, metadataResolutionRequest(contract, model, linkage, "mounting_bracket", "material"))
		if typed.Code != metadataResolutionCodeReferenceNotFound {
			t.Fatalf("expected code %q, got %#v", metadataResolutionCodeReferenceNotFound, typed)
		}
	})

	t.Run("display label and native name are not fallback keys", func(t *testing.T) {
		for _, key := range []string{"Material", "Body01", "META.BRACKET.MATERIAL"} {
			typed := resolveTargetMetadataError(t, metadataResolutionRequest(contract, model, linkage, "bracket", key))
			if typed.Code != metadataResolutionCodeReferenceNotFound {
				t.Fatalf("key %q: expected code %q, got %#v", key, metadataResolutionCodeReferenceNotFound, typed)
			}
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		ambiguousModel := metadataResolutionModel()
		ambiguousModel.Metadata = append(ambiguousModel.Metadata, semantic.Metadata{
			ID:          "meta.bracket.materialOther",
			OwnerKind:   semantic.OwnerKindComponent,
			OwnerID:     "cmp.bracket",
			ComponentID: "cmp.bracket",
			Key:         "material",
			DisplayName: "Material Other",
			ValueType:   "string",
			NativeType:  "App::PropertyString",
			Observable:  true,
			Writable:    true,
		})
		typed := resolveTargetMetadataError(t, metadataResolutionRequest(contract, ambiguousModel, linkage, "bracket", "material"))
		if typed.Code != metadataResolutionCodeReferenceAmbiguous {
			t.Fatalf("expected code %q, got %#v", metadataResolutionCodeReferenceAmbiguous, typed)
		}
	})
}

func TestResolveTargetMetadata_CapabilityErrors(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	model := metadataResolutionModel()
	linkage := mustMetadataResolutionLinkage(t, contract, model)

	model.Metadata[0].Writable = false
	typed := resolveTargetMetadataError(t, metadataResolutionRequest(contract, model, linkage, "bracket", "material"))
	if typed.Code != metadataResolutionCodeNotWritable {
		t.Fatalf("expected code %q, got %#v", metadataResolutionCodeNotWritable, typed)
	}

	model.Metadata[0].Writable = true
	model.Metadata[0].Observable = false
	typed = resolveTargetMetadataError(t, metadataResolutionRequest(contract, model, linkage, "bracket", "material"))
	if typed.Code != metadataResolutionCodeNotObservable {
		t.Fatalf("expected code %q, got %#v", metadataResolutionCodeNotObservable, typed)
	}
}

func TestResolveTargetMetadata_TypeErrors(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	model := metadataResolutionModel()
	linkage := mustMetadataResolutionLinkage(t, contract, model)

	t.Run("unsupported captured valueType", func(t *testing.T) {
		model.Metadata[0].ValueType = "date"
		typed := resolveTargetMetadataError(t, metadataResolutionRequest(contract, model, linkage, "bracket", "material"))
		if typed.Code != metadataResolutionCodeUnsupportedValueType {
			t.Fatalf("expected code %q, got %#v", metadataResolutionCodeUnsupportedValueType, typed)
		}
		model.Metadata[0].ValueType = "string"
	})

	t.Run("captured valueType and native kind mismatch", func(t *testing.T) {
		model.Metadata[0].NativeType = "App::PropertyInteger"
		typed := resolveTargetMetadataError(t, metadataResolutionRequest(contract, model, linkage, "bracket", "material"))
		if typed.Code != metadataResolutionCodeValueTypeMismatch {
			t.Fatalf("expected code %q, got %#v", metadataResolutionCodeValueTypeMismatch, typed)
		}
		model.Metadata[0].NativeType = "App::PropertyString"
	})

	t.Run("missing native value kind mapping", func(t *testing.T) {
		missing := cloneSemanticMap(contract)
		missing.CaptureToSemantic.Metadata[0].NativeValueKinds = nil
		typed := resolveTargetMetadataError(t, metadataResolutionRequest(missing, model, linkage, "bracket", "material"))
		if typed.Code != metadataResolutionCodeValueKindMissing {
			t.Fatalf("expected code %q, got %#v", metadataResolutionCodeValueKindMissing, typed)
		}
	})

	t.Run("unmapped native type", func(t *testing.T) {
		model.Metadata[0].NativeType = "App::PropertyPath"
		typed := resolveTargetMetadataError(t, metadataResolutionRequest(contract, model, linkage, "bracket", "material"))
		if typed.Code != metadataResolutionCodeValueKindMissing {
			t.Fatalf("expected code %q, got %#v", metadataResolutionCodeValueKindMissing, typed)
		}
		model.Metadata[0].NativeType = "App::PropertyString"
	})

	t.Run("unsupported mapped value kind", func(t *testing.T) {
		invalid := cloneSemanticMap(contract)
		invalid.CaptureToSemantic.Metadata[0].NativeValueKinds["App::PropertyString"] = "date"
		typed := resolveTargetMetadataError(t, metadataResolutionRequest(invalid, model, linkage, "bracket", "material"))
		if typed.Code != metadataResolutionCodeInvalidSemanticMap {
			t.Fatalf("expected code %q, got %#v", metadataResolutionCodeInvalidSemanticMap, typed)
		}
		if !strings.Contains(typed.Error(), `nativeValueKinds["App::PropertyString"] "date" is not supported`) {
			t.Fatalf("unexpected error: %v", typed)
		}
	})
}

func TestResolveTargetMetadata_NativeIdentityErrors(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	model := metadataResolutionModel()
	linkage := mustMetadataResolutionLinkage(t, contract, model)

	t.Run("missing native owner", func(t *testing.T) {
		model.Components[1].NativeRef = ""
		typed := resolveTargetMetadataError(t, metadataResolutionRequest(contract, model, linkage, "bracket", "material"))
		if typed.Code != metadataResolutionCodeNativeOwnerInvalid {
			t.Fatalf("expected code %q, got %#v", metadataResolutionCodeNativeOwnerInvalid, typed)
		}
		model.Components[1].NativeRef = "Body01"
	})

	t.Run("invalid native owner", func(t *testing.T) {
		model.Components[1].NativeRef = " Body01 "
		typed := resolveTargetMetadataError(t, metadataResolutionRequest(contract, model, linkage, "bracket", "material"))
		if typed.Code != metadataResolutionCodeNativeOwnerInvalid {
			t.Fatalf("expected code %q, got %#v", metadataResolutionCodeNativeOwnerInvalid, typed)
		}
		model.Components[1].NativeRef = "Body01"
	})

	t.Run("invalid native property", func(t *testing.T) {
		for _, key := range []string{" material", "material ", "mate\x00rial"} {
			model.Metadata[0].Key = key
			typed := resolveTargetMetadataError(t, metadataResolutionRequest(contract, model, linkage, "bracket", key))
			if typed.Code != metadataResolutionCodeNativePropertyInvalid {
				t.Fatalf("key %q: expected code %q, got %#v", key, metadataResolutionCodeNativePropertyInvalid, typed)
			}
		}
		model.Metadata[0].Key = "material"
	})
}

func TestResolveTargetMetadata_Determinism(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	model := metadataResolutionModel()
	linkage := mustMetadataResolutionLinkage(t, contract, model)
	request := metadataResolutionRequest(contract, model, linkage, "bracket", "material")

	first := mustResolveTargetMetadata(t, request)
	second := mustResolveTargetMetadata(t, request)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected deterministic resolution\nfirst:  %#v\nsecond: %#v", first, second)
	}

	reordered := metadataResolutionModel()
	reordered.Components[0], reordered.Components[1] = reordered.Components[1], reordered.Components[0]
	reordered.Metadata[0], reordered.Metadata[3] = reordered.Metadata[3], reordered.Metadata[0]
	reorderedLinkage := mustMetadataResolutionLinkage(t, contract, reordered)
	third := mustResolveTargetMetadata(t, metadataResolutionRequest(contract, reordered, reorderedLinkage, "bracket", "material"))
	if !reflect.DeepEqual(first, third) {
		t.Fatalf("expected input-order-independent resolution\nfirst:  %#v\nthird: %#v", first, third)
	}
}

func TestValidateResolvedMetadataValue(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		value    any
		wantErr  bool
		errMatch string
	}{
		{name: "string ok", kind: MetadataValueKindString, value: "S235JR"},
		{name: "string wrong", kind: MetadataValueKindString, value: true, wantErr: true, errMatch: "requires a string"},
		{name: "boolean ok", kind: MetadataValueKindBoolean, value: true},
		{name: "boolean wrong", kind: MetadataValueKindBoolean, value: "true", wantErr: true, errMatch: "requires a boolean"},
		{name: "number ok", kind: MetadataValueKindNumber, value: 7.85},
		{name: "number integral ok", kind: MetadataValueKindNumber, value: 5.0},
		{name: "number wrong", kind: MetadataValueKindNumber, value: "5", wantErr: true, errMatch: "requires a number"},
		{name: "number NaN", kind: MetadataValueKindNumber, value: math.NaN(), wantErr: true, errMatch: "finite"},
		{name: "integer ok", kind: MetadataValueKindInteger, value: 5.0},
		{name: "integer fractional", kind: MetadataValueKindInteger, value: 5.5, wantErr: true, errMatch: "exact integral"},
		{name: "integer wrong type", kind: MetadataValueKindInteger, value: 5, wantErr: true, errMatch: "requires a number"},
		{name: "integer infinity", kind: MetadataValueKindInteger, value: math.Inf(1), wantErr: true, errMatch: "finite"},
		{name: "unsupported kind", kind: "date", value: "2026-01-01", wantErr: true, errMatch: "unsupported metadata value kind"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResolvedMetadataValue(tt.kind, tt.value)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected validation error")
			}
			if tt.errMatch != "" && !strings.Contains(err.Error(), tt.errMatch) {
				t.Fatalf("expected error containing %q, got %v", tt.errMatch, err)
			}
		})
	}
}

func TestValidateSemanticMap_NativeValueKindsRules(t *testing.T) {
	base := loadFixture(t, "smoke/freecad-default")

	t.Run("nil map is accepted", func(t *testing.T) {
		contract := cloneSemanticMap(base)
		contract.CaptureToSemantic.Metadata[0].NativeValueKinds = nil
		if err := Validate(contract); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})

	tests := []struct {
		name    string
		mutate  func(*SemanticMap)
		wantErr string
	}{
		{
			name: "empty map",
			mutate: func(contract *SemanticMap) {
				contract.CaptureToSemantic.Metadata[0].NativeValueKinds = map[string]string{}
			},
			wantErr: "captureToSemantic.metadata[0].nativeValueKinds must not be empty when present",
		},
		{
			name: "empty key",
			mutate: func(contract *SemanticMap) {
				contract.CaptureToSemantic.Metadata[0].NativeValueKinds = map[string]string{"": "string"}
			},
			wantErr: "captureToSemantic.metadata[0].nativeValueKinds key must not be empty",
		},
		{
			name: "whitespace key",
			mutate: func(contract *SemanticMap) {
				contract.CaptureToSemantic.Metadata[0].NativeValueKinds = map[string]string{" App::PropertyString": "string"}
			},
			wantErr: `captureToSemantic.metadata[0].nativeValueKinds key " App::PropertyString" must not have leading or trailing whitespace`,
		},
		{
			name: "empty value kind",
			mutate: func(contract *SemanticMap) {
				contract.CaptureToSemantic.Metadata[0].NativeValueKinds = map[string]string{"App::PropertyString": ""}
			},
			wantErr: `captureToSemantic.metadata[0].nativeValueKinds["App::PropertyString"] must reference a non-empty metadata value kind`,
		},
		{
			name: "whitespace value kind",
			mutate: func(contract *SemanticMap) {
				contract.CaptureToSemantic.Metadata[0].NativeValueKinds = map[string]string{"App::PropertyString": " string"}
			},
			wantErr: `captureToSemantic.metadata[0].nativeValueKinds["App::PropertyString"] must not have leading or trailing whitespace`,
		},
		{
			name: "unsupported value kind",
			mutate: func(contract *SemanticMap) {
				contract.CaptureToSemantic.Metadata[0].NativeValueKinds = map[string]string{"App::PropertyString": "date"}
			},
			wantErr: `captureToSemantic.metadata[0].nativeValueKinds["App::PropertyString"] "date" is not supported`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := cloneSemanticMap(base)
			tt.mutate(contract)
			err := Validate(contract)
			if err == nil {
				t.Fatalf("expected validation error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestCanonicalJSON_NativeValueKindsDeterministic(t *testing.T) {
	left := loadFixture(t, "smoke/freecad-default")
	right := cloneSemanticMap(left)
	right.CaptureToSemantic.Metadata[0].NativeValueKinds = map[string]string{
		"App::PropertyQuantity":          "number",
		"App::PropertyAngle":             "number",
		"App::PropertyDistance":          "number",
		"App::PropertyLength":            "number",
		"App::PropertyFloat":             "number",
		"App::PropertyFloatConstraint":   "number",
		"App::PropertyIntegerConstraint": "integer",
		"App::PropertyInteger":           "integer",
		"App::PropertyBool":              "boolean",
		"App::PropertyString":            "string",
	}

	leftJSON, err := CanonicalJSON(left)
	if err != nil {
		t.Fatalf("CanonicalJSON(left) returned error: %v", err)
	}
	rightJSON, err := CanonicalJSON(right)
	if err != nil {
		t.Fatalf("CanonicalJSON(right) returned error: %v", err)
	}
	if !bytes.Equal(leftJSON, rightJSON) {
		t.Fatalf("expected insertion-order-independent canonical JSON\nleft:  %s\nright: %s", leftJSON, rightJSON)
	}
}
