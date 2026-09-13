package semanticmap

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSmokeFixtureParsesValidatesAndCanonicalizes(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	if err := Validate(contract); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	first, err := CanonicalJSON(contract)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	second, err := CanonicalJSON(contract)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical JSON was not byte-stable\nfirst:  %q\nsecond: %q", first, second)
	}
	if len(first) == 0 || first[len(first)-1] != '\n' {
		t.Fatal("expected canonical JSON to be newline-terminated")
	}

	reparsed, err := Parse(first)
	if err != nil {
		t.Fatalf("Parse(canonical) returned error: %v", err)
	}
	roundTrip, err := CanonicalJSON(reparsed)
	if err != nil {
		t.Fatalf("CanonicalJSON(roundTrip) returned error: %v", err)
	}
	if !bytes.Equal(first, roundTrip) {
		t.Fatalf("canonical JSON changed across round trip\nfirst:  %q\nsecond: %q", first, roundTrip)
	}
}

func TestValidateParameterSemanticTypeReferencesAreDeclared(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	if err := Validate(contract); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestSupportedSchemaVersionIsExactV1(t *testing.T) {
	if got := SupportedSchemaVersion(); got != "1.0" {
		t.Fatalf("unexpected supported schema version: %q", got)
	}
}

func TestClassifySchemaVersionDistinguishesStatuses(t *testing.T) {
	cases := []struct {
		version string
		want    VersionStatus
	}{
		{version: "", want: VersionStatusMissing},
		{version: "1", want: VersionStatusMalformed},
		{version: " 1.0", want: VersionStatusMalformed},
		{version: "1.0", want: VersionStatusSupported},
		{version: "1.1", want: VersionStatusUnsupportedSameMajor},
		{version: "2.0", want: VersionStatusUnsupportedMajor},
		{version: "0.9", want: VersionStatusUnsupportedMajor},
	}

	for _, tc := range cases {
		if got := ClassifySchemaVersion(tc.version); got != tc.want {
			t.Fatalf("ClassifySchemaVersion(%q) = %q, want %q", tc.version, got, tc.want)
		}
	}
}

func TestValidateSchemaVersionAcceptsOnlyExactV1(t *testing.T) {
	if err := ValidateSchemaVersion("1.0"); err != nil {
		t.Fatalf("ValidateSchemaVersion returned error: %v", err)
	}

	cases := []struct {
		version string
		want    string
	}{
		{version: "", want: "schemaVersion is required"},
		{version: "1", want: `schemaVersion "1" is malformed`},
		{version: "1.1", want: `schemaVersion "1.1" is not supported; only "1.0" is supported`},
		{version: "2.0", want: `schemaVersion "2.0" is not supported; only "1.0" is supported`},
	}

	for _, tc := range cases {
		err := ValidateSchemaVersion(tc.version)
		assertFixtureError(t, err, ErrValidation, tc.want)
	}
}

func TestValidateRejectsUnknownCoercionDeterministically(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	entry := contract.SemanticTypes.ParameterTypes["length"]
	entry.Coercion = "number_to_magic"
	contract.SemanticTypes.ParameterTypes["length"] = entry

	first := Validate(contract)
	second := Validate(contract)
	if first == nil || second == nil {
		t.Fatal("expected repeated validation failures")
	}

	var firstErr *ValidationError
	if !errors.As(first, &firstErr) {
		t.Fatalf("expected first ValidationError, got %T", first)
	}
	var secondErr *ValidationError
	if !errors.As(second, &secondErr) {
		t.Fatalf("expected second ValidationError, got %T", second)
	}

	want := []string{
		`semanticTypes.parameterTypes["length"].coercion "number_to_magic" is not supported`,
	}
	if !reflect.DeepEqual(firstErr.Messages(), want) {
		t.Fatalf("unexpected first validation messages\nwant: %#v\ngot:  %#v", want, firstErr.Messages())
	}
	if !reflect.DeepEqual(secondErr.Messages(), want) {
		t.Fatalf("unexpected second validation messages\nwant: %#v\ngot:  %#v", want, secondErr.Messages())
	}
}

func TestValidateResolutionPolicyAcceptsV1Precedence(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	if err := Validate(contract); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	if got := contract.SemanticTypes.ResolutionPolicy.Precedence; !reflect.DeepEqual(got, []string{
		"explicit_parameter_mapping",
		"capture_native_type_mapping",
	}) {
		t.Fatalf("unexpected precedence order: %#v", got)
	}
}

func TestValidateOverridePolicyHelperReportsNoOverridesForValidV1(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	if err := ValidateOverridePolicy(contract.OverridePolicy); err != nil {
		t.Fatalf("ValidateOverridePolicy returned error: %v", err)
	}
	if contract.OverridePolicy.OverridesAllowed() {
		t.Fatal("expected OverridesAllowed to be false")
	}
	if contract.OverridePolicy.AllowsAdapterOverrides() {
		t.Fatal("expected AllowsAdapterOverrides to be false")
	}
	if contract.OverridePolicy.AllowsProjectOverrides() {
		t.Fatal("expected AllowsProjectOverrides to be false")
	}
	if contract.OverridePolicy.AllowsUserOverrides() {
		t.Fatal("expected AllowsUserOverrides to be false")
	}
}

func TestValidateResolutionPolicyRequiresPolicy(t *testing.T) {
	_, err := Load(fixturePath(t, "break/missing-resolution-policy"))
	assertFixtureError(t, err, ErrValidation, "semanticTypes.resolutionPolicy is required")
}

func TestValidateResolutionPolicyRequiresPrecedence(t *testing.T) {
	_, err := Load(fixturePath(t, "break/missing-resolution-policy-precedence"))
	assertFixtureError(t, err, ErrValidation, "semanticTypes.resolutionPolicy.precedence is required")
}

func TestValidateResolutionPolicyRejectsEmptyPrecedence(t *testing.T) {
	_, err := Load(fixturePath(t, "break/empty-resolution-policy-precedence"))
	assertFixtureError(t, err, ErrValidation, "semanticTypes.resolutionPolicy.precedence must not be empty")
}

func TestValidateResolutionPolicyRejectsUnknownPrecedence(t *testing.T) {
	_, err := Load(fixturePath(t, "break/unknown-resolution-policy-precedence"))
	assertFixtureError(t, err, ErrValidation, `semanticTypes.resolutionPolicy.precedence[1] "engine_guess" is not supported`)
}

func TestValidateResolutionPolicyRejectsDuplicatePrecedence(t *testing.T) {
	_, err := Load(fixturePath(t, "break/duplicate-resolution-policy-precedence"))
	assertFixtureError(t, err, ErrValidation, "semanticTypes.resolutionPolicy.precedence[1] duplicates semanticTypes.resolutionPolicy.precedence[0]")
}

func TestValidateResolutionPolicyRequiresEngineDefaultFlag(t *testing.T) {
	_, err := Load(fixturePath(t, "break/missing-resolution-policy-allow-engine-default"))
	assertFixtureError(t, err, ErrValidation, "semanticTypes.resolutionPolicy.allowEngineDefault is required")
}

func TestValidateResolutionPolicyRequiresEngineInferenceFlag(t *testing.T) {
	_, err := Load(fixturePath(t, "break/missing-resolution-policy-allow-engine-inference"))
	assertFixtureError(t, err, ErrValidation, "semanticTypes.resolutionPolicy.allowEngineInference is required")
}

func TestValidateResolutionPolicyRejectsEnabledEngineDefault(t *testing.T) {
	_, err := Load(fixturePath(t, "break/invalid-resolution-policy-allow-engine-default-true"))
	assertFixtureError(t, err, ErrValidation, "semanticTypes.resolutionPolicy.allowEngineDefault must be false")
}

func TestValidateResolutionPolicyRejectsEnabledEngineInference(t *testing.T) {
	_, err := Load(fixturePath(t, "break/invalid-resolution-policy-allow-engine-inference-true"))
	assertFixtureError(t, err, ErrValidation, "semanticTypes.resolutionPolicy.allowEngineInference must be false")
}

func TestValidateResolutionPolicyRejectsUnknownNestedField(t *testing.T) {
	_, err := Load(fixturePath(t, "break/unknown-resolution-policy-field"))
	assertFixtureError(t, err, ErrDecode, `unknown field "extra"`)
}

func TestValidateResolutionPolicyProblemsAreDeterministic(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	// Capture-native facts only become semantic meaning through semantic-map rules.
	// Engine defaults and engine inference remain explicitly modeled but disabled in v1.
	contract.SemanticTypes.ResolutionPolicy.Precedence = []string{
		"engine_guess",
		"explicit_parameter_mapping",
		"explicit_parameter_mapping",
	}
	contract.SemanticTypes.ResolutionPolicy.AllowEngineDefault = boolPtr(true)
	contract.SemanticTypes.ResolutionPolicy.AllowEngineInference = nil

	first := Validate(contract)
	second := Validate(contract)
	if first == nil || second == nil {
		t.Fatal("expected repeated validation failures")
	}

	var firstErr *ValidationError
	if !errors.As(first, &firstErr) {
		t.Fatalf("expected ValidationError, got %T", first)
	}
	var secondErr *ValidationError
	if !errors.As(second, &secondErr) {
		t.Fatalf("expected ValidationError, got %T", second)
	}

	want := []string{
		`semanticTypes.resolutionPolicy.precedence[0] "engine_guess" is not supported`,
		`semanticTypes.resolutionPolicy.precedence[2] duplicates semanticTypes.resolutionPolicy.precedence[1]`,
		"semanticTypes.resolutionPolicy.allowEngineDefault must be false",
		"semanticTypes.resolutionPolicy.allowEngineInference is required",
	}
	if !reflect.DeepEqual(firstErr.Messages(), want) {
		t.Fatalf("unexpected first validation messages\nwant: %#v\ngot:  %#v", want, firstErr.Messages())
	}
	if !reflect.DeepEqual(secondErr.Messages(), want) {
		t.Fatalf("unexpected second validation messages\nwant: %#v\ngot:  %#v", want, secondErr.Messages())
	}
}

func TestValidateParameterSemanticTypeReferenceProblemsAreDeterministic(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	contract.CaptureToSemantic.Parameters[0].SemanticType.Map["Float"] = "unknown-dimensionless"
	contract.CaptureToSemantic.Parameters[0].SemanticType.Map["Integer"] = "unknown-count"

	first := Validate(contract)
	second := Validate(contract)
	if first == nil || second == nil {
		t.Fatal("expected repeated validation failures")
	}
	if !errors.Is(first, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", first)
	}
	if !errors.Is(second, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", second)
	}

	var firstErr *ValidationError
	if !errors.As(first, &firstErr) {
		t.Fatalf("expected first ValidationError, got %T", first)
	}
	var secondErr *ValidationError
	if !errors.As(second, &secondErr) {
		t.Fatalf("expected second ValidationError, got %T", second)
	}

	want := []string{
		`captureToSemantic.parameters[0].semanticType.map["Float"] references unknown semanticTypes.parameterTypes key "unknown-dimensionless"`,
		`captureToSemantic.parameters[0].semanticType.map["Integer"] references unknown semanticTypes.parameterTypes key "unknown-count"`,
	}
	if !reflect.DeepEqual(firstErr.Messages(), want) {
		t.Fatalf("unexpected first validation messages\nwant: %#v\ngot:  %#v", want, firstErr.Messages())
	}
	if !reflect.DeepEqual(secondErr.Messages(), want) {
		t.Fatalf("unexpected second validation messages\nwant: %#v\ngot:  %#v", want, secondErr.Messages())
	}
}

func TestValidateOperationCapabilityAllowsTargetWithRequiredTargetability(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	err := validateOperationCapabilityForTarget(contract, "suppress", "feature", map[string]bool{
		"suppress": true,
	})
	if err != nil {
		t.Fatalf("validateOperationCapabilityForTarget returned error: %v", err)
	}
}

func TestSupportedOperationNamesAreAccepted(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	if err := Validate(contract); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	got := SupportedOperationNames()
	want := []string{"delete", "hide", "suppress", "unhide", "unsuppress", "write_metadata", "write_parameter"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected supported operation names\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestValidateOperationCapabilityRejectsMissingRequiredTargetability(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	err := validateOperationCapabilityForTarget(contract, "suppress", "feature", map[string]bool{
		"hide": true,
	})
	if err == nil {
		t.Fatal("expected validateOperationCapabilityForTarget to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), `operationCapabilities["suppress"] requires targetability "suppress" to be true`) {
		t.Fatalf("unexpected error: %v", err)
	}

	second := validateOperationCapabilityForTarget(contract, "suppress", "feature", map[string]bool{
		"hide": true,
	})
	if second == nil {
		t.Fatal("expected repeated validation failure")
	}
	if err.Error() != second.Error() {
		t.Fatalf("expected deterministic error text\nfirst:  %q\nsecond: %q", err.Error(), second.Error())
	}
}

func TestValidateOperationCapabilityRejectsDisallowedSemanticTarget(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	err := validateOperationCapabilityForTarget(contract, "delete", "part", map[string]bool{
		"delete": true,
	})
	if err == nil {
		t.Fatal("expected validateOperationCapabilityForTarget to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), `operationCapabilities["delete"] does not allow semantic target "part"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateOperationCapabilityRejectsUnknownOperation(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	err := validateOperationCapabilityForTarget(contract, "explode", "feature", map[string]bool{
		"suppress": true,
	})
	if err == nil {
		t.Fatal("expected validateOperationCapabilityForTarget to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), `operationCapabilities["explode"] is not declared`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCanonicalJSONRoundTripIsByteStable(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	if err := Validate(contract); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	first, err := CanonicalJSON(contract)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	if len(first) == 0 || first[len(first)-1] != '\n' {
		t.Fatal("expected first canonical JSON to be newline-terminated")
	}

	secondSameModel, err := CanonicalJSON(contract)
	if err != nil {
		t.Fatalf("CanonicalJSON(secondSameModel) returned error: %v", err)
	}
	if !bytes.Equal(first, secondSameModel) {
		t.Fatalf("expected repeated CanonicalJSON calls to match\nfirst:  %q\nsecond: %q", first, secondSameModel)
	}

	reparsed, err := Parse(first)
	if err != nil {
		t.Fatalf("Parse(canonical) returned error: %v", err)
	}
	if err := Validate(reparsed); err != nil {
		t.Fatalf("Validate(reparsed) returned error: %v", err)
	}

	second, err := CanonicalJSON(reparsed)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}
	if len(second) == 0 || second[len(second)-1] != '\n' {
		t.Fatal("expected second canonical JSON to be newline-terminated")
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("expected canonical JSON round trip to be byte-stable\nfirst:  %q\nsecond: %q", first, second)
	}
}

func TestCanonicalJSONPreservesRequiredEmptyCaptureArrays(t *testing.T) {
	cases := []struct {
		name  string
		field string
		empty func(*SemanticMap)
	}{
		{
			name:  "parameter_groups",
			field: "parameterGroups",
			empty: func(contract *SemanticMap) {
				contract.CaptureToSemantic.ParameterGroups = []ParameterGroupMapping{}
			},
		},
		{
			name:  "parameters",
			field: "parameters",
			empty: func(contract *SemanticMap) {
				contract.CaptureToSemantic.Parameters = []ParameterMapping{}
			},
		},
		{
			name:  "components",
			field: "components",
			empty: func(contract *SemanticMap) {
				contract.CaptureToSemantic.Components = []ComponentMapping{}
			},
		},
		{
			name:  "metadata",
			field: "metadata",
			empty: func(contract *SemanticMap) {
				contract.CaptureToSemantic.Metadata = []MetadataMapping{}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inputContract := loadFixture(t, "smoke/freecad-default")
			tc.empty(inputContract)
			input, err := json.Marshal(inputContract)
			if err != nil {
				t.Fatalf("Marshal(input) returned error: %v", err)
			}
			contract, err := Parse(input)
			if err != nil {
				t.Fatalf("Parse(input) returned error: %v", err)
			}
			if err := Validate(contract); err != nil {
				t.Fatalf("Validate(initial) returned error: %v", err)
			}

			first, err := CanonicalJSON(contract)
			if err != nil {
				t.Fatalf("CanonicalJSON(first) returned error: %v", err)
			}

			var canonical struct {
				CaptureToSemantic map[string]json.RawMessage `json:"captureToSemantic"`
			}
			if err := json.Unmarshal(first, &canonical); err != nil {
				t.Fatalf("Unmarshal(canonical) returned error: %v", err)
			}
			var values []json.RawMessage
			if err := json.Unmarshal(canonical.CaptureToSemantic[tc.field], &values); err != nil {
				t.Fatalf("captureToSemantic.%s is not an array: %v", tc.field, err)
			}
			if values == nil || len(values) != 0 {
				t.Fatalf("captureToSemantic.%s = %#v, want non-nil empty array", tc.field, values)
			}

			reparsed, err := Parse(first)
			if err != nil {
				t.Fatalf("Parse(canonical) returned error: %v", err)
			}
			if err := Validate(reparsed); err != nil {
				t.Fatalf("Validate(reparsed) returned error: %v", err)
			}
			second, err := CanonicalJSON(reparsed)
			if err != nil {
				t.Fatalf("CanonicalJSON(second) returned error: %v", err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("canonical JSON changed across round trip\nfirst:  %q\nsecond: %q", first, second)
			}
		})
	}
}

func TestBreakFixturesFailWithExpectedClassification(t *testing.T) {
	cases := []struct {
		name string
		want string
		kind error
	}{
		{name: "missing-schema-version", want: "schemaVersion is required", kind: ErrValidation},
		{name: "malformed-schema-version", want: `schemaVersion "1" is malformed`, kind: ErrValidation},
		{name: "unsupported-schema-version", want: `schemaVersion "2.0" is not supported; only "1.0" is supported`, kind: ErrValidation},
		{name: "unsupported-schema-version-major", want: `schemaVersion "2.0" is not supported; only "1.0" is supported`, kind: ErrValidation},
		{name: "unsupported-schema-version-same-major", want: `schemaVersion "1.1" is not supported; only "1.0" is supported`, kind: ErrValidation},
		{name: "unknown-root-field", want: `unknown field "extra"`, kind: ErrDecode},
		{name: "unknown-nested-field", want: `unknown field "extra"`, kind: ErrDecode},
		{name: "empty-mapping-id", want: "mappingId is required", kind: ErrValidation},
		{name: "whitespace-mapping-id", want: "mappingId must not have leading or trailing whitespace", kind: ErrValidation},
		{name: "empty-adapter", want: "adapter is required", kind: ErrValidation},
		{name: "missing-semantic-types", want: "semanticTypes is required", kind: ErrValidation},
		{name: "missing-resolution-policy", want: "semanticTypes.resolutionPolicy is required", kind: ErrValidation},
		{name: "missing-resolution-policy-precedence", want: "semanticTypes.resolutionPolicy.precedence is required", kind: ErrValidation},
		{name: "empty-resolution-policy-precedence", want: "semanticTypes.resolutionPolicy.precedence must not be empty", kind: ErrValidation},
		{name: "unknown-resolution-policy-precedence", want: `semanticTypes.resolutionPolicy.precedence[1] "engine_guess" is not supported`, kind: ErrValidation},
		{name: "duplicate-resolution-policy-precedence", want: "duplicates semanticTypes.resolutionPolicy.precedence[0]", kind: ErrValidation},
		{name: "missing-resolution-policy-allow-engine-default", want: "semanticTypes.resolutionPolicy.allowEngineDefault is required", kind: ErrValidation},
		{name: "missing-resolution-policy-allow-engine-inference", want: "semanticTypes.resolutionPolicy.allowEngineInference is required", kind: ErrValidation},
		{name: "invalid-resolution-policy-allow-engine-default-true", want: "semanticTypes.resolutionPolicy.allowEngineDefault must be false", kind: ErrValidation},
		{name: "invalid-resolution-policy-allow-engine-inference-true", want: "semanticTypes.resolutionPolicy.allowEngineInference must be false", kind: ErrValidation},
		{name: "unknown-resolution-policy-field", want: `unknown field "extra"`, kind: ErrDecode},
		{name: "unknown-parameter-semantic-type-reference", want: `references unknown semanticTypes.parameterTypes key "unknown"`, kind: ErrValidation},
		{name: "missing-override-policy", want: "overridePolicy is required", kind: ErrValidation},
		{name: "invalid-override-policy-flag-true", want: "overridePolicy.allowAdapterOverrides must be false", kind: ErrValidation},
		{name: "missing-determinism-allow-display-name-identity", want: "determinism.allowDisplayNameIdentity is required", kind: ErrValidation},
		{name: "invalid-determinism-flag-true", want: "determinism.allowAdapterInference must be false", kind: ErrValidation},
		{name: "invalid-projection-parameter-field", want: `semanticToManifest.parameters.type must equal "number"`, kind: ErrValidation},
		{name: "invalid-output-manifest-type", want: `manifestType "stl" is not supported`, kind: ErrValidation},
		{name: "invalid-mutation-manifest-collection", want: `manifestCollection "partMutations.unsupported" is not supported`, kind: ErrValidation},
		{name: "missing-semantic-to-manifest", want: "semanticToManifest is required", kind: ErrValidation},
		{name: "missing-operation-capabilities", want: "operationCapabilities is required", kind: ErrValidation},
		{name: "empty-operation-capability-target-list", want: "allowedSemanticTargets must not be empty", kind: ErrValidation},
		{name: "unknown-operation-capability", want: `operationCapabilities["explode"] is not supported`, kind: ErrValidation},
		{name: "duplicate-mapping-entry", want: "duplicates natural key", kind: ErrValidation},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(fixturePath(t, "break/"+tc.name))
			if err == nil {
				t.Fatal("expected Load to fail")
			}
			if !errors.Is(err, tc.kind) {
				t.Fatalf("expected %v, got %v", tc.kind, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLoadMissingFileIsClassifiedAsIO(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), FileName))
	if err == nil {
		t.Fatal("expected Load to fail")
	}
	if !errors.Is(err, ErrIO) {
		t.Fatalf("expected ErrIO, got %v", err)
	}
}

func TestReadMatchesParse(t *testing.T) {
	data, err := os.ReadFile(fixturePath(t, "smoke/freecad-default"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	read, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	if !reflect.DeepEqual(parsed, read) {
		t.Fatalf("Read and Parse returned different values\nparse: %#v\nread:  %#v", parsed, read)
	}
}

func TestValidationProblemOrderingIsStable(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	contract.MappingID = " map "
	contract.Adapter = ""
	contract.Determinism.AllowCaseInsensitiveMatch = boolPtr(true)
	contract.Determinism.AllowDisplayNameIdentity = boolPtr(true)

	first := Validate(contract)
	second := Validate(contract)
	if first == nil || second == nil {
		t.Fatal("expected repeated validation failures")
	}

	var firstErr *ValidationError
	if !errors.As(first, &firstErr) {
		t.Fatalf("expected ValidationError, got %T", first)
	}
	var secondErr *ValidationError
	if !errors.As(second, &secondErr) {
		t.Fatalf("expected ValidationError, got %T", second)
	}

	want := []string{
		"mappingId must not have leading or trailing whitespace",
		"adapter is required",
		"determinism.allowCaseInsensitiveMatch must be false",
		"determinism.allowDisplayNameIdentity must be false",
	}
	if !reflect.DeepEqual(firstErr.Messages(), want) {
		t.Fatalf("unexpected first validation messages\nwant: %#v\ngot:  %#v", want, firstErr.Messages())
	}
	if !reflect.DeepEqual(secondErr.Messages(), want) {
		t.Fatalf("unexpected second validation messages\nwant: %#v\ngot:  %#v", want, secondErr.Messages())
	}
}

func TestParseRejectsRootThatIsNotObject(t *testing.T) {
	_, err := Parse([]byte(`[]`))
	if err == nil {
		t.Fatal("expected Parse to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "root must be a JSON object") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateOverridePolicyProblemsAreDeterministic(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	contract.OverridePolicy.AllowAdapterOverrides = boolPtr(true)
	contract.OverridePolicy.AllowProjectOverrides = nil
	contract.OverridePolicy.AllowUserOverrides = boolPtr(true)

	first := Validate(contract)
	second := Validate(contract)
	if first == nil || second == nil {
		t.Fatal("expected repeated validation failures")
	}

	var firstErr *ValidationError
	if !errors.As(first, &firstErr) {
		t.Fatalf("expected first ValidationError, got %T", first)
	}
	var secondErr *ValidationError
	if !errors.As(second, &secondErr) {
		t.Fatalf("expected second ValidationError, got %T", second)
	}

	want := []string{
		"overridePolicy.allowAdapterOverrides must be false",
		"overridePolicy.allowProjectOverrides is required",
		"overridePolicy.allowUserOverrides must be false",
	}
	if !reflect.DeepEqual(firstErr.Messages(), want) {
		t.Fatalf("unexpected first validation messages\nwant: %#v\ngot:  %#v", want, firstErr.Messages())
	}
	if !reflect.DeepEqual(secondErr.Messages(), want) {
		t.Fatalf("unexpected second validation messages\nwant: %#v\ngot:  %#v", want, secondErr.Messages())
	}
}

func TestValidateOverridePolicyRejectsEachTrueFlagDeterministically(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	contract.OverridePolicy.AllowAdapterOverrides = boolPtr(true)
	contract.OverridePolicy.AllowProjectOverrides = boolPtr(true)
	contract.OverridePolicy.AllowUserOverrides = boolPtr(true)

	first := ValidateOverridePolicy(contract.OverridePolicy)
	second := ValidateOverridePolicy(contract.OverridePolicy)
	if first == nil || second == nil {
		t.Fatal("expected repeated validation failures")
	}

	var firstErr *ValidationError
	if !errors.As(first, &firstErr) {
		t.Fatalf("expected first ValidationError, got %T", first)
	}
	var secondErr *ValidationError
	if !errors.As(second, &secondErr) {
		t.Fatalf("expected second ValidationError, got %T", second)
	}

	want := []string{
		"overridePolicy.allowAdapterOverrides must be false",
		"overridePolicy.allowProjectOverrides must be false",
		"overridePolicy.allowUserOverrides must be false",
	}
	if !reflect.DeepEqual(firstErr.Messages(), want) {
		t.Fatalf("unexpected first validation messages\nwant: %#v\ngot:  %#v", want, firstErr.Messages())
	}
	if !reflect.DeepEqual(secondErr.Messages(), want) {
		t.Fatalf("unexpected second validation messages\nwant: %#v\ngot:  %#v", want, secondErr.Messages())
	}
}

func TestValidateSemanticToManifestProblemsAreDeterministic(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	contract.SemanticToManifest.Parameters.Type = "integer"
	contract.SemanticToManifest.Outputs["csv"] = OutputManifestMapping{
		ManifestType: "csv",
		TargetField:  "object",
		TargetSource: "semantic.output.target.manifestName",
		ScopeField:   "rollup",
		ScopeMap: map[string]string{
			"flat": "flat",
		},
	}
	contract.SemanticToManifest.Mutations["set_property"] = MutationMapping{
		ManifestCollection: "partMutations.properties",
		TargetField:        "object",
		PropertyField:      "property",
		ValueField:         "value",
		Value:              boolPtr(true),
	}

	first := Validate(contract)
	second := Validate(contract)
	if first == nil || second == nil {
		t.Fatal("expected repeated validation failures")
	}

	var firstErr *ValidationError
	if !errors.As(first, &firstErr) {
		t.Fatalf("expected first ValidationError, got %T", first)
	}
	var secondErr *ValidationError
	if !errors.As(second, &secondErr) {
		t.Fatalf("expected second ValidationError, got %T", second)
	}

	want := []string{
		`semanticToManifest.parameters.type must equal "number"`,
		`semanticToManifest.outputs["csv"].targetField must equal "target"`,
		`semanticToManifest.mutations["set_property"].value must not be set`,
	}
	if !reflect.DeepEqual(firstErr.Messages(), want) {
		t.Fatalf("unexpected first validation messages\nwant: %#v\ngot:  %#v", want, firstErr.Messages())
	}
	if !reflect.DeepEqual(secondErr.Messages(), want) {
		t.Fatalf("unexpected second validation messages\nwant: %#v\ngot:  %#v", want, secondErr.Messages())
	}
}

func TestReviewProjectionContractReportsSmokeFixtureReady(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	review := ReviewProjectionContract(contract)
	if !review.Ready {
		t.Fatalf("expected ready review, got problems: %#v", review.Problems)
	}
	if review.ParameterTarget != "parameterAssignments" {
		t.Fatalf("unexpected parameter target: %q", review.ParameterTarget)
	}
	if review.OverridesAllowed {
		t.Fatal("expected overrides to be disabled")
	}
	if review.AllowImplicitFallback || review.AllowAdapterInference || review.AllowDisplayNameIdentity || review.AllowCaseInsensitiveMatch {
		t.Fatalf("expected determinism flags to be disabled: %#v", review)
	}

	wantOutputs := []string{"csv", "pdf", "step"}
	if !reflect.DeepEqual(review.SupportedOutputTypes, wantOutputs) {
		t.Fatalf("unexpected supported output types\nwant: %#v\ngot:  %#v", wantOutputs, review.SupportedOutputTypes)
	}
	wantMutations := []string{
		"partMutations.parameters",
		"partMutations.properties",
		"partMutations.suppression",
	}
	if !reflect.DeepEqual(review.SupportedMutationCollections, wantMutations) {
		t.Fatalf("unexpected supported mutation collections\nwant: %#v\ngot:  %#v", wantMutations, review.SupportedMutationCollections)
	}

	if err := ValidateProjectionContract(contract); err != nil {
		t.Fatalf("ValidateProjectionContract returned error: %v", err)
	}
}

func TestReviewProjectionContractIsDeterministicAcrossCalls(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	first := ReviewProjectionContract(contract)
	second := ReviewProjectionContract(contract)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("projection review changed across calls\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestProjectionReview_DeterministicAcrossRuns(t *testing.T) {
	firstContract := loadFixture(t, "smoke/freecad-default")
	secondContract := loadFixture(t, "smoke/freecad-default")

	firstFromFirstLoad := ReviewProjectionContract(firstContract)
	secondFromFirstLoad := ReviewProjectionContract(firstContract)
	firstFromSecondLoad := ReviewProjectionContract(secondContract)
	secondFromSecondLoad := ReviewProjectionContract(secondContract)

	if !reflect.DeepEqual(firstFromFirstLoad, secondFromFirstLoad) {
		t.Fatalf("projection review changed across repeated calls on first load\nfirst:  %#v\nsecond: %#v", firstFromFirstLoad, secondFromFirstLoad)
	}
	if !reflect.DeepEqual(firstFromSecondLoad, secondFromSecondLoad) {
		t.Fatalf("projection review changed across repeated calls on second load\nfirst:  %#v\nsecond: %#v", firstFromSecondLoad, secondFromSecondLoad)
	}
	if !reflect.DeepEqual(firstFromFirstLoad, firstFromSecondLoad) {
		t.Fatalf("projection review changed across independent loads\nfirst load:  %#v\nsecond load: %#v", firstFromFirstLoad, firstFromSecondLoad)
	}

	wantOutputs := []string{"csv", "pdf", "step"}
	if !reflect.DeepEqual(firstFromFirstLoad.SupportedOutputTypes, wantOutputs) {
		t.Fatalf("unexpected supported output types\nwant: %#v\ngot:  %#v", wantOutputs, firstFromFirstLoad.SupportedOutputTypes)
	}
	wantMutations := []string{
		"partMutations.parameters",
		"partMutations.properties",
		"partMutations.suppression",
	}
	if !reflect.DeepEqual(firstFromFirstLoad.SupportedMutationCollections, wantMutations) {
		t.Fatalf("unexpected supported mutation collections\nwant: %#v\ngot:  %#v", wantMutations, firstFromFirstLoad.SupportedMutationCollections)
	}
}

func TestReviewProjectionContractReturnsDefensiveCopies(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	first := ReviewProjectionContract(contract)
	first.SupportedOutputTypes[0] = "mutated"
	first.SupportedMutationCollections[0] = "mutated"
	first.Problems = append(first.Problems, "mutated")

	second := ReviewProjectionContract(contract)
	if reflect.DeepEqual(first, second) {
		t.Fatal("expected second review to be independent from first")
	}

	wantOutputs := []string{"csv", "pdf", "step"}
	if !reflect.DeepEqual(second.SupportedOutputTypes, wantOutputs) {
		t.Fatalf("unexpected supported output types after mutation\nwant: %#v\ngot:  %#v", wantOutputs, second.SupportedOutputTypes)
	}
}

func TestReviewProjectionContractReportsMissingOutputNotReady(t *testing.T) {
	contract := loadFixture(t, "break/projection-review-missing-output")

	review := ReviewProjectionContract(contract)
	if review.Ready {
		t.Fatal("expected projection review to be not ready")
	}
	if !containsProblem(review.Problems, `semanticToManifest.outputs must include manifestType "pdf"`) {
		t.Fatalf("unexpected problems: %#v", review.Problems)
	}
}

func TestReviewProjectionContractReportsMissingMutationNotReady(t *testing.T) {
	contract := loadFixture(t, "break/projection-review-missing-mutation")

	review := ReviewProjectionContract(contract)
	if review.Ready {
		t.Fatal("expected projection review to be not ready")
	}
	if !containsProblem(review.Problems, `semanticToManifest.mutations must include manifestCollection "partMutations.parameters"`) {
		t.Fatalf("unexpected problems: %#v", review.Problems)
	}
}

func TestReviewProjectionContractReportsMalformedProjectionNotReady(t *testing.T) {
	contract := rawFixture(t, "break/invalid-projection-parameter-field")

	review := ReviewProjectionContract(contract)
	if review.Ready {
		t.Fatal("expected projection review to be not ready")
	}
	if !containsProblem(review.Problems, `semanticToManifest.parameters.type must equal "number"`) {
		t.Fatalf("unexpected problems: %#v", review.Problems)
	}
}

func TestReviewProjectionContractReportsEnabledFallbackNotReady(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	contract.Determinism.AllowImplicitFallback = boolPtr(true)

	review := ReviewProjectionContract(contract)
	if review.Ready {
		t.Fatal("expected projection review to be not ready")
	}
	if !containsProblem(review.Problems, "determinism.allowImplicitFallback must be false") {
		t.Fatalf("unexpected problems: %#v", review.Problems)
	}
}

func TestValidate_InvalidSemanticMap_DeterministicProblemOrdering(t *testing.T) {
	firstContract := rawFixture(t, "break/unknown-operation-capability")
	secondContract := rawFixture(t, "break/unknown-operation-capability")

	firstErr := Validate(firstContract)
	secondErr := Validate(secondContract)
	repeatedErr := Validate(firstContract)

	if firstErr == nil || secondErr == nil || repeatedErr == nil {
		t.Fatal("expected repeated validation failures")
	}
	if !errors.Is(firstErr, ErrValidation) {
		t.Fatalf("expected first ErrValidation, got %v", firstErr)
	}
	if !errors.Is(secondErr, ErrValidation) {
		t.Fatalf("expected second ErrValidation, got %v", secondErr)
	}
	if !errors.Is(repeatedErr, ErrValidation) {
		t.Fatalf("expected repeated ErrValidation, got %v", repeatedErr)
	}

	var firstValidationErr *ValidationError
	if !errors.As(firstErr, &firstValidationErr) {
		t.Fatalf("expected first ValidationError, got %T", firstErr)
	}
	var secondValidationErr *ValidationError
	if !errors.As(secondErr, &secondValidationErr) {
		t.Fatalf("expected second ValidationError, got %T", secondErr)
	}
	var repeatedValidationErr *ValidationError
	if !errors.As(repeatedErr, &repeatedValidationErr) {
		t.Fatalf("expected repeated ValidationError, got %T", repeatedErr)
	}

	if !reflect.DeepEqual(firstValidationErr.Messages(), secondValidationErr.Messages()) {
		t.Fatalf("validation messages changed across independent loads\nfirst:  %#v\nsecond: %#v", firstValidationErr.Messages(), secondValidationErr.Messages())
	}
	if !reflect.DeepEqual(firstValidationErr.Messages(), repeatedValidationErr.Messages()) {
		t.Fatalf("validation messages changed across repeated calls\nfirst:  %#v\nsecond: %#v", firstValidationErr.Messages(), repeatedValidationErr.Messages())
	}
	if firstErr.Error() != secondErr.Error() || firstErr.Error() != repeatedErr.Error() {
		t.Fatalf("validation error text changed across runs\nfirst:  %q\nsecond: %q\nthird:  %q", firstErr.Error(), secondErr.Error(), repeatedErr.Error())
	}
}

func loadFixture(t *testing.T, name string) *SemanticMap {
	t.Helper()
	contract, err := Load(fixturePath(t, name))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	return contract
}

func rawFixture(t *testing.T, name string) *SemanticMap {
	t.Helper()
	data, err := os.ReadFile(fixturePath(t, name))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	var contract SemanticMap
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	return &contract
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name, FileName)
}

func assertFixtureError(t *testing.T, err error, kind error, want string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected fixture load to fail")
	}
	if !errors.Is(err, kind) {
		t.Fatalf("expected %v, got %v", kind, err)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %v", want, err)
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func containsProblem(problems []string, want string) bool {
	for _, problem := range problems {
		if problem == want {
			return true
		}
	}
	return false
}
