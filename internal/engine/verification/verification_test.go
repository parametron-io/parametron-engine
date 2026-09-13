package verification

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/observed"
)

func TestParseValidateCanonicalRoundTrip(t *testing.T) {
	parsed, err := Parse([]byte(validVerificationJSON()))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if err := Validate(parsed); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	got, err := CanonicalJSON(parsed)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}
	want := []byte(`{"schemaVersion":"1.0","observe":{"components":false,"parameters":false,"metadata":true,"references":true},"observationContext":{"parameters":[]},"expected":{"components":[],"parameters":[{"id":"par.root.height","name":"height","type":"number","unit":"mm","value":5},{"id":"par.root.length","name":"length","type":"number","unit":"mm","value":35}],"metadata":[{"key":"working_copy_sha256","value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],"references":[{"kind":"working_copy_path","name":"/tmp/out/_working/widget.FCStd"}]},"checks":{"components":{"enabled":false},"parameters":{"enabled":false},"metadata":{"enabled":true},"references":{"enabled":true}}}`)
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected canonical JSON\nwant: %s\ngot:  %s", want, got)
	}

	reparsed, err := Parse(got)
	if err != nil {
		t.Fatalf("Parse(canonical) returned error: %v", err)
	}
	gotAgain, err := CanonicalJSON(reparsed)
	if err != nil {
		t.Fatalf("CanonicalJSON(reparsed) returned error: %v", err)
	}
	if !bytes.Equal(got, gotAgain) {
		t.Fatalf("canonical JSON changed across round-trip\nfirst:  %s\nsecond: %s", got, gotAgain)
	}
}

func TestParseRejectsUnknownAndMissingFields(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name: "unknown top-level field",
			payload: `{
  "schemaVersion": "1.0",
  "observe": {"components": false, "parameters": false, "metadata": true, "references": true},
  "expected": {"components": [], "parameters": [], "metadata": [], "references": []},
  "checks": {"components": {"enabled": false}, "parameters": {"enabled": false}, "metadata": {"enabled": true}, "references": {"enabled": true}},
  "extra": true
}`,
			want: `unknown field "extra"`,
		},
		{
			name: "unknown nested field",
			payload: `{
  "schemaVersion": "1.0",
  "observe": {"components": false, "parameters": false, "metadata": true, "references": true},
  "expected": {"components": [], "parameters": [], "metadata": [], "references": [{"kind": "working_copy_path", "name": "/tmp/x", "extra": true}]},
  "checks": {"components": {"enabled": false}, "parameters": {"enabled": false}, "metadata": {"enabled": true}, "references": {"enabled": true}}
}`,
			want: `expected.references[0] contains unknown field "extra"`,
		},
		{
			name: "missing required field",
			payload: `{
  "schemaVersion": "1.0",
  "observe": {"components": false, "parameters": false, "metadata": true, "references": true},
  "expected": {"components": [], "parameters": [], "references": []},
  "checks": {"components": {"enabled": false}, "parameters": {"enabled": false}, "metadata": {"enabled": true}, "references": {"enabled": true}}
}`,
			want: `expected.metadata is required`,
		},
		{
			name: "wrong version",
			payload: `{
  "schemaVersion": "2.0",
  "observe": {"components": false, "parameters": false, "metadata": true, "references": true},
  "expected": {"components": [], "parameters": [], "metadata": [], "references": []},
  "checks": {"components": {"enabled": false}, "parameters": {"enabled": false}, "metadata": {"enabled": true}, "references": {"enabled": true}}
}`,
			want: `schemaVersion must be "1.0"`,
		},
		{
			name: "invalid parameter entry",
			payload: `{
  "schemaVersion": "1.0",
  "observe": {"components": false, "parameters": false, "metadata": true, "references": true},
  "expected": {"components": [], "parameters": [{"id": "par.root.length", "name": "length", "type": "number", "value": 35}], "metadata": [], "references": []},
  "checks": {"components": {"enabled": false}, "parameters": {"enabled": false}, "metadata": {"enabled": true}, "references": {"enabled": true}}
}`,
			want: `expected.parameters[0].unit is required`,
		},
		{
			name: "invalid metadata entry",
			payload: `{
  "schemaVersion": "1.0",
  "observe": {"components": false, "parameters": false, "metadata": true, "references": true},
  "expected": {"components": [], "parameters": [], "metadata": [{"key": "working_copy_sha256", "value": ""}], "references": []},
  "checks": {"components": {"enabled": false}, "parameters": {"enabled": false}, "metadata": {"enabled": true}, "references": {"enabled": true}}
}`,
			want: `expected.metadata[0].value is required`,
		},
		{
			name: "invalid reference entry",
			payload: `{
  "schemaVersion": "1.0",
  "observe": {"components": false, "parameters": false, "metadata": true, "references": true},
  "expected": {"components": [], "parameters": [], "metadata": [], "references": [{"kind": "working_copy_path", "name": "relative.FCStd"}]},
  "checks": {"components": {"enabled": false}, "parameters": {"enabled": false}, "metadata": {"enabled": true}, "references": {"enabled": true}}
}`,
			want: `expected.references[0].name must be an absolute path`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.payload))
			if err == nil {
				t.Fatal("expected Parse to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("unexpected error\nwant substring: %s\ngot:            %v", tt.want, err)
			}
		})
	}
}

func TestValidate_ExpectedParameterIdentityRules(t *testing.T) {
	t.Run("missing id rejected", func(t *testing.T) {
		contract := validContract()
		contract.Expected.Parameters[0].ID = ""
		err := Validate(contract)
		if err == nil || !strings.Contains(err.Error(), "expected.parameters[0].id is required") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("duplicate ids rejected", func(t *testing.T) {
		contract := validContract()
		contract.Expected.Parameters[1].ID = contract.Expected.Parameters[0].ID
		err := Validate(contract)
		if err == nil || !strings.Contains(err.Error(), "duplicates expected.parameters[0].id") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("duplicate names allowed when ids differ", func(t *testing.T) {
		contract := validContract()
		contract.Expected.Parameters[1].Name = contract.Expected.Parameters[0].Name
		if err := Validate(contract); err != nil {
			t.Fatalf("expected duplicate names with distinct ids to validate, got %v", err)
		}
	})
}

func TestValidate_ParameterChecksRequireParameterObservation(t *testing.T) {
	t.Run("enabled accepted when observe.parameters is true", func(t *testing.T) {
		contract := validContract()
		contract.Observe.Parameters = true
		contract.Checks.Parameters.Enabled = true
		if err := Validate(contract); err != nil {
			t.Fatalf("expected parameter checks to validate, got %v", err)
		}
	})

	t.Run("enabled rejected when observe.parameters is false", func(t *testing.T) {
		contract := validContract()
		contract.Checks.Parameters.Enabled = true
		err := Validate(contract)
		if err == nil || !strings.Contains(err.Error(), "checks.parameters.enabled requires observe.parameters to be true") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestCanonicalJSONAndWriteFileAreByteStable(t *testing.T) {
	contract := validContract()
	first, err := CanonicalJSON(contract)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	second, err := CanonicalJSON(contract)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical JSON changed across repeated calls\nfirst:  %s\nsecond: %s", first, second)
	}

	path := filepath.Join(t.TempDir(), "execution", "parametron.verification.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatalf("failed to seed stale file: %v", err)
	}
	if err := WriteFile(path, contract); err != nil {
		t.Fatalf("WriteFile(first) returned error: %v", err)
	}
	writtenFirst, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read first written file: %v", err)
	}
	if !bytes.HasSuffix(writtenFirst, []byte("\n")) {
		t.Fatalf("expected trailing newline, got %q", writtenFirst)
	}
	if bytes.HasSuffix(writtenFirst, []byte("\n\n")) {
		t.Fatalf("expected exactly one trailing newline, got %q", writtenFirst)
	}

	if err := WriteFile(path, contract); err != nil {
		t.Fatalf("WriteFile(second) returned error: %v", err)
	}
	writtenSecond, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read second written file: %v", err)
	}
	if !bytes.Equal(writtenFirst, writtenSecond) {
		t.Fatalf("written file changed across repeated writes\nfirst:  %s\nsecond: %s", writtenFirst, writtenSecond)
	}
}

func TestCanonicalJSON_StableWithComponentsAndMetadataIdentity(t *testing.T) {
	contract := metadataIdentityContract()
	contract.Observe.Components = true
	contract.Checks.Components.Enabled = true
	contract.Expected.Components = []ExpectedComponent{
		{ID: "cmp.part.leg", Kind: observed.ComponentKindPart, Name: "Leg", ParentID: "cmp.root"},
		{ID: "cmp.root", Kind: observed.ComponentKindAssembly, Name: "Widget", ParentID: ""},
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
		t.Fatalf("canonical JSON changed across repeated richer-structure calls\nfirst:  %s\nsecond: %s", first, second)
	}

	want := []byte(`{"schemaVersion":"1.0","observe":{"components":true,"parameters":false,"metadata":true,"references":true},"observationContext":{"parameters":[]},"expected":{"components":[{"id":"cmp.part.leg","kind":"part","name":"Leg","parentId":"cmp.root"},{"id":"cmp.root","kind":"assembly","name":"Widget","parentId":null}],"parameters":[{"id":"par.root.height","name":"height","type":"number","unit":"mm","value":5},{"id":"par.root.length","name":"length","type":"number","unit":"mm","value":35}],"metadata":[{"id":"meta.mass","key":"mass","ownerId":"cmp.part.leg","value":"12.5","valueKind":"number"}],"references":[{"kind":"working_copy_path","name":"/tmp/out/_working/widget.FCStd"}]},"checks":{"components":{"enabled":true},"parameters":{"enabled":false},"metadata":{"enabled":true},"references":{"enabled":true}}}`)
	if !bytes.Equal(first, want) {
		t.Fatalf("unexpected richer canonical JSON\nwant: %s\ngot:  %s", want, first)
	}
}

func TestVerifyDeterministicPassAndFailOutcomes(t *testing.T) {
	contract := validContract()
	obs := matchingObserved(contract)

	result, err := Verify(contract, obs)
	if err != nil {
		t.Fatalf("Verify(pass) returned error: %v", err)
	}
	if result.Status != StatusPass {
		t.Fatalf("expected pass status, got %+v", result)
	}
	if result.Categories.Parameters.Status != CategoryStatusSkipped {
		t.Fatalf("expected disabled parameter category to stay skipped, got %+v", result.Categories.Parameters)
	}

	failures := []struct {
		name           string
		mutate         func(*observed.Observed)
		wantClass      FailureClass
		wantMessageSub string
		wantErr        error
	}{
		{
			name: "missing metadata",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Metadata = []observed.Metadata{}
			},
			wantClass:      FailureClassRequiredObservationMissing,
			wantMessageSub: `required observed metadata "working_copy_sha256" is missing`,
			wantErr:        ErrObservationMissing,
		},
		{
			name: "metadata mismatch",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Metadata[0].Value = mustObservedMetadataValue(strings.Repeat("b", 64))
			},
			wantClass:      FailureClassMetadataMismatch,
			wantMessageSub: `observed metadata "working_copy_sha256" mismatch`,
			wantErr:        ErrMetadataMismatch,
		},
		{
			name: "missing reference",
			mutate: func(obs *observed.Observed) {
				obs.Observation.References = []observed.Reference{}
			},
			wantClass:      FailureClassRequiredObservationMissing,
			wantMessageSub: `required observed reference "working_copy_path" is missing`,
			wantErr:        ErrObservationMissing,
		},
		{
			name: "reference mismatch",
			mutate: func(obs *observed.Observed) {
				obs.Observation.References[0].Name = "/tmp/other.FCStd"
			},
			wantClass:      FailureClassReferenceMismatch,
			wantMessageSub: `observed reference "working_copy_path" mismatch`,
			wantErr:        ErrReferenceMismatch,
		},
	}

	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			firstObserved := cloneObserved(obs)
			tt.mutate(firstObserved)
			firstResult, firstErr := Verify(contract, firstObserved)
			if firstErr == nil {
				t.Fatal("expected Verify to fail")
			}
			if !errors.Is(firstErr, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, firstErr)
			}
			if firstResult.Failure != tt.wantClass {
				t.Fatalf("unexpected failure class: %+v", firstResult)
			}
			if !strings.Contains(firstResult.Message, tt.wantMessageSub) {
				t.Fatalf("unexpected failure message: %s", firstResult.Message)
			}

			secondObserved := cloneObserved(obs)
			tt.mutate(secondObserved)
			secondResult, secondErr := Verify(contract, secondObserved)
			if secondErr == nil {
				t.Fatal("expected repeated Verify to fail")
			}
			if firstResult.Failure != secondResult.Failure {
				t.Fatalf("failure classification changed\nfirst:  %+v\nsecond: %+v", firstResult, secondResult)
			}
			if firstResult.Message != secondResult.Message {
				t.Fatalf("failure message changed\nfirst:  %s\nsecond: %s", firstResult.Message, secondResult.Message)
			}
			if firstErr.Error() != secondErr.Error() {
				t.Fatalf("error surface changed\nfirst:  %s\nsecond: %s", firstErr.Error(), secondErr.Error())
			}
		})
	}
}

func TestVerify_ComponentComparisonRules(t *testing.T) {
	tests := []struct {
		name           string
		mutate         func(*observed.Observed)
		wantStatus     Status
		wantClass      FailureClass
		wantMessageSub string
		wantErr        error
	}{
		{
			name:       "components pass on exact id kind name parentId match",
			mutate:     func(_ *observed.Observed) {},
			wantStatus: StatusPass,
		},
		{
			name: "missing component fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Components = obs.Observation.Components[:1]
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassRequiredObservationMissing,
			wantMessageSub: `required observed component "cmp.part.leg" is missing`,
			wantErr:        ErrObservationMissing,
		},
		{
			name: "duplicate observed component id fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Components = append(obs.Observation.Components, observed.Component{
					ID:       "cmp.part.leg",
					Kind:     observed.ComponentKindPart,
					Name:     "Leg duplicate",
					ParentID: "cmp.root",
				})
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassComponentMismatch,
			wantMessageSub: `observed component "cmp.part.leg" is not unique`,
			wantErr:        ErrComponentMismatch,
		},
		{
			name: "component kind mismatch fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Components[1].Kind = observed.ComponentKindAssembly
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassComponentMismatch,
			wantMessageSub: `observed component "cmp.part.leg" kind mismatch`,
			wantErr:        ErrComponentMismatch,
		},
		{
			name: "component name mismatch fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Components[1].Name = "Leg renamed"
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassComponentMismatch,
			wantMessageSub: `observed component "cmp.part.leg" name mismatch`,
			wantErr:        ErrComponentMismatch,
		},
		{
			name: "component parentId mismatch fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Components[1].ParentID = ""
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassComponentMismatch,
			wantMessageSub: `observed component "cmp.part.leg" parentId mismatch`,
			wantErr:        ErrComponentMismatch,
		},
		{
			name: "extra observed components are ignored",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Components = append(obs.Observation.Components, observed.Component{
					ID:       "cmp.part.extra",
					Kind:     observed.ComponentKindPart,
					Name:     "Brace",
					ParentID: "cmp.root",
				})
			},
			wantStatus: StatusPass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := componentEnabledContract()
			obs := matchingObserved(contract)
			tt.mutate(obs)

			result, err := Verify(contract, obs)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected pass, got error %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected Verify to fail")
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
			}
			if result.Status != tt.wantStatus {
				t.Fatalf("unexpected status: %+v", result)
			}
			if tt.wantStatus == StatusPass {
				if result.Categories.Components.Status != CategoryStatusPass {
					t.Fatalf("expected component pass, got %+v", result.Categories.Components)
				}
				return
			}
			if result.Categories.Components.Status != CategoryStatusFail {
				t.Fatalf("expected component fail, got %+v", result.Categories.Components)
			}
			if result.Failure != tt.wantClass {
				t.Fatalf("unexpected failure class: %+v", result)
			}
			if !strings.Contains(result.Message, tt.wantMessageSub) {
				t.Fatalf("unexpected result message: %q", result.Message)
			}
			if !strings.Contains(result.Categories.Components.Message, tt.wantMessageSub) {
				t.Fatalf("unexpected category message: %q", result.Categories.Components.Message)
			}
		})
	}
}

func TestVerify_DisabledComponentCheckSkipsDeterministically(t *testing.T) {
	contract := validContract()
	obs := matchingObserved(contract)
	obs.Observation.Components = []observed.Component{
		{ID: "cmp.root", Kind: observed.ComponentKindAssembly, Name: "Widget"},
		{ID: "cmp.root", Kind: observed.ComponentKindPart, Name: "Wrong duplicate"},
	}

	result, err := Verify(contract, obs)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if result.Status != StatusPass {
		t.Fatalf("expected pass status with disabled component verification, got %+v", result)
	}
	if result.Categories.Components.Status != CategoryStatusSkipped {
		t.Fatalf("expected skipped component category, got %+v", result.Categories.Components)
	}
	if result.Categories.Components.Message != componentVerificationDisabledMessage {
		t.Fatalf("unexpected component skip message: %q", result.Categories.Components.Message)
	}
}

func TestVerify_DoesNotInvokeObservedIdentityComparabilityImplicitly(t *testing.T) {
	contract := validContract()
	obs := matchingObserved(contract)
	obs.Observation.Components = []observed.Component{
		{ID: "cmp.unknown", Kind: observed.ComponentKindAssembly, Name: "Widget"},
	}
	obs.Observation.Parameters = []observed.Parameter{{
		ID:        "par.unknown",
		Name:      "length",
		GroupID:   "grp.unknown",
		Value:     mustObservedValue(t, []byte(`999`)),
		ValueKind: "integer",
	}}
	obs.Observation.Metadata = []observed.Metadata{
		{
			ID:        "meta.unknown",
			Key:       contract.Expected.Metadata[0].Key,
			OwnerID:   "owner.unknown",
			Value:     mustObservedMetadataValue(contract.Expected.Metadata[0].Value),
			ValueKind: "string",
		},
	}

	result, err := Verify(contract, obs)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if result.Status != StatusPass {
		t.Fatalf("expected pass status while identity comparability remains out of Verify, got %+v", result)
	}
	if result.Categories.Parameters.Status != CategoryStatusSkipped {
		t.Fatalf("expected parameters to remain skipped, got %+v", result.Categories.Parameters)
	}
	if result.Categories.Metadata.Status != CategoryStatusPass || result.Categories.References.Status != CategoryStatusPass {
		t.Fatalf("expected enabled categories to continue using existing verifier behavior, got %+v", result.Categories)
	}
}

func TestVerify_ParameterComparisonRules(t *testing.T) {
	tests := []struct {
		name           string
		mutate         func(*observed.Observed)
		wantStatus     Status
		wantClass      FailureClass
		wantMessageSub string
		wantErr        error
	}{
		{
			name:       "matching expected and observed parameter passes",
			mutate:     func(_ *observed.Observed) {},
			wantStatus: StatusPass,
		},
		{
			name: "missing observed parameter fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Parameters = obs.Observation.Parameters[:1]
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassRequiredObservationMissing,
			wantMessageSub: `required observed parameter "par.root.length" is missing`,
			wantErr:        ErrObservationMissing,
		},
		{
			name: "duplicate observed parameter id fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Parameters = append(obs.Observation.Parameters, observed.Parameter{
					ID:        "par.root.length",
					Name:      "Length duplicate",
					GroupID:   "Other",
					Value:     mustObservedValue(t, []byte(`35`)),
					ValueKind: "number",
				})
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassParameterMismatch,
			wantMessageSub: `observed parameter "par.root.length" is not unique`,
			wantErr:        ErrParameterMismatch,
		},
		{
			name: "valueKind mismatch fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Parameters[1].ValueKind = "integer"
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassParameterMismatch,
			wantMessageSub: `observed parameter "par.root.length" valueKind mismatch: want "number", got "integer"`,
			wantErr:        ErrParameterMismatch,
		},
		{
			name: "value mismatch fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Parameters[1].Value = mustObservedValue(t, []byte(`35.0001`))
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassParameterMismatch,
			wantMessageSub: `observed parameter "par.root.length" mismatch: want 35, got 35.0001`,
			wantErr:        ErrParameterMismatch,
		},
		{
			name: "extra observed parameters are ignored",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Parameters = append(obs.Observation.Parameters, observed.Parameter{
					ID:        "par.extra",
					Name:      "Extra",
					GroupID:   "Dimensions",
					Value:     mustObservedValue(t, []byte(`99`)),
					ValueKind: "number",
				})
			},
			wantStatus: StatusPass,
		},
		{
			name: "name mismatch does not matter when id and value match",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Parameters[1].Name = "Length renamed"
			},
			wantStatus: StatusPass,
		},
		{
			name: "same name but different id does not match",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Parameters[1].ID = "par.other.length"
				obs.Observation.Parameters[1].Name = "length"
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassRequiredObservationMissing,
			wantMessageSub: `required observed parameter "par.root.length" is missing`,
			wantErr:        ErrObservationMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := parameterEnabledContract()
			obs := matchingObserved(contract)
			tt.mutate(obs)

			result, err := Verify(contract, obs)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected pass, got error %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected Verify to fail")
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
			}
			if result.Status != tt.wantStatus {
				t.Fatalf("unexpected status: %+v", result)
			}
			if tt.wantStatus == StatusPass {
				if result.Categories.Parameters.Status != CategoryStatusPass {
					t.Fatalf("expected parameter pass, got %+v", result.Categories.Parameters)
				}
				return
			}
			if result.Categories.Parameters.Status != CategoryStatusFail {
				t.Fatalf("expected parameter fail, got %+v", result.Categories.Parameters)
			}
			if result.Failure != tt.wantClass {
				t.Fatalf("unexpected failure class: %+v", result)
			}
			if !strings.Contains(result.Message, tt.wantMessageSub) {
				t.Fatalf("unexpected result message: %q", result.Message)
			}
			if !strings.Contains(result.Categories.Parameters.Message, tt.wantMessageSub) {
				t.Fatalf("unexpected category message: %q", result.Categories.Parameters.Message)
			}
		})
	}
}

func TestVerify_ParameterStrictValueKindWinsOverSameScalarValue(t *testing.T) {
	contract := &Contract{
		SchemaVersion: SchemaVersion,
		Observe: Observe{
			Components: false,
			Parameters: true,
			Metadata:   false,
			References: false,
		},
		ObservationContext: ObservationContext{
			Parameters: []ObservedParameterBinding{
				{ID: "param.width", Name: "Width", GroupName: "Dimensions"},
			},
		},
		Expected: Expected{
			Components: []ExpectedComponent{},
			Parameters: []ExpectedParameter{
				{ID: "param.width", Name: "width", Type: "number", Unit: "mm", Value: 10},
			},
			Metadata:   []ExpectedMetadata{},
			References: []ExpectedReference{},
		},
		Checks: Checks{
			Components: Check{Enabled: false},
			Parameters: Check{Enabled: true},
			Metadata:   Check{Enabled: false},
			References: Check{Enabled: false},
		},
	}
	obs := &observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		WorkingCopy: observed.WorkingCopy{
			Path:   "/tmp/out/_working/widget.FCStd",
			SHA256: strings.Repeat("a", 64),
		},
		Observation: observed.Observation{
			Parameters: []observed.Parameter{
				{
					ID:        "param.width",
					Name:      "Width",
					GroupID:   "Dimensions",
					Value:     mustObservedValue(t, []byte(`10`)),
					ValueKind: "integer",
				},
			},
			Metadata:   []observed.Metadata{},
			References: []observed.Reference{},
			Components: []observed.Component{},
		},
	}

	result, err := Verify(contract, obs)
	if err == nil {
		t.Fatal("expected Verify to fail")
	}
	if result.Failure != FailureClassParameterMismatch {
		t.Fatalf("unexpected failure class: %+v", result)
	}
	if !errors.Is(err, ErrParameterMismatch) {
		t.Fatalf("expected ErrParameterMismatch, got %v", err)
	}
	wantMessage := `observed parameter "param.width" valueKind mismatch: want "number", got "integer"`
	if result.Message != wantMessage {
		t.Fatalf("unexpected result message\nwant: %q\ngot:  %q", wantMessage, result.Message)
	}
}

func TestVerify_DuplicateObservedParameterBeatsMissingLaterExpectedParameter(t *testing.T) {
	contract := &Contract{
		SchemaVersion: SchemaVersion,
		Observe: Observe{
			Components: false,
			Parameters: true,
			Metadata:   false,
			References: false,
		},
		ObservationContext: ObservationContext{
			Parameters: []ObservedParameterBinding{
				{ID: "param.width", Name: "Width", GroupName: "Dimensions"},
				{ID: "param.height", Name: "Height", GroupName: "Dimensions"},
			},
		},
		Expected: Expected{
			Components: []ExpectedComponent{},
			Parameters: []ExpectedParameter{
				{ID: "param.width", Name: "width", Type: "number", Unit: "mm", Value: 10},
				{ID: "param.height", Name: "height", Type: "number", Unit: "mm", Value: 20},
			},
			Metadata:   []ExpectedMetadata{},
			References: []ExpectedReference{},
		},
		Checks: Checks{
			Components: Check{Enabled: false},
			Parameters: Check{Enabled: true},
			Metadata:   Check{Enabled: false},
			References: Check{Enabled: false},
		},
	}
	obs := &observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		WorkingCopy: observed.WorkingCopy{
			Path:   "/tmp/out/_working/widget.FCStd",
			SHA256: strings.Repeat("a", 64),
		},
		Observation: observed.Observation{
			Parameters: []observed.Parameter{
				{
					ID:        "param.width",
					Name:      "Width",
					GroupID:   "Dimensions",
					Value:     mustObservedValue(t, []byte(`10`)),
					ValueKind: "number",
				},
				{
					ID:        "param.width",
					Name:      "Width duplicate",
					GroupID:   "Dimensions",
					Value:     mustObservedValue(t, []byte(`10`)),
					ValueKind: "number",
				},
			},
			Metadata:   []observed.Metadata{},
			References: []observed.Reference{},
			Components: []observed.Component{},
		},
	}

	result, err := Verify(contract, obs)
	if err == nil {
		t.Fatal("expected Verify to fail")
	}
	if result.Failure != FailureClassParameterMismatch {
		t.Fatalf("expected parameter mismatch to be authoritative, got %+v", result)
	}
	if errors.Is(err, ErrObservationMissing) {
		t.Fatalf("did not expect missing-observation classification, got %v", err)
	}
	if !errors.Is(err, ErrParameterMismatch) {
		t.Fatalf("expected ErrParameterMismatch, got %v", err)
	}
	wantMessage := `observed parameter "param.width" is not unique`
	if result.Message != wantMessage {
		t.Fatalf("unexpected result message\nwant: %q\ngot:  %q", wantMessage, result.Message)
	}
}

func TestVerify_DisabledVsEnabledParameterChecksChangeOutcomeDeterministically(t *testing.T) {
	base := parameterEnabledContract()
	mismatchingObserved := matchingObserved(base)
	mismatchingObserved.Observation.Parameters[1].Value = mustObservedValue(t, []byte(`999`))

	disabled := parameterEnabledContract()
	disabled.Checks.Parameters.Enabled = false
	disabled.Observe.Parameters = false

	disabledResult, disabledErr := Verify(disabled, cloneObserved(mismatchingObserved))
	if disabledErr != nil {
		t.Fatalf("expected disabled parameter checks to ignore mismatch, got %v", disabledErr)
	}
	if disabledResult.Status != StatusPass {
		t.Fatalf("expected pass with disabled parameters, got %+v", disabledResult)
	}
	if disabledResult.Categories.Parameters.Status != CategoryStatusSkipped {
		t.Fatalf("expected skipped parameter category, got %+v", disabledResult.Categories.Parameters)
	}

	enabledResult, enabledErr := Verify(base, cloneObserved(mismatchingObserved))
	if enabledErr == nil {
		t.Fatal("expected enabled parameter checks to fail")
	}
	if enabledResult.Failure != FailureClassParameterMismatch {
		t.Fatalf("unexpected enabled failure class: %+v", enabledResult)
	}
	if !errors.Is(enabledErr, ErrParameterMismatch) {
		t.Fatalf("expected ErrParameterMismatch, got %v", enabledErr)
	}
}

func TestVerify_ParameterFailureIsAuthoritativeBeforeMetadataAndReference(t *testing.T) {
	contract := parameterEnabledContract()
	obs := matchingObserved(contract)
	obs.Observation.Parameters[1].Value = mustObservedValue(t, []byte(`999`))
	obs.Observation.Metadata[0].Value = mustObservedMetadataValue(strings.Repeat("0", 64))
	obs.Observation.References[0].Name = "/tmp/other.FCStd"

	result, err := Verify(contract, obs)
	if err == nil {
		t.Fatal("expected Verify to fail")
	}
	if !errors.Is(err, ErrParameterMismatch) {
		t.Fatalf("expected parameter mismatch to be authoritative, got %v", err)
	}
	if result.Failure != FailureClassParameterMismatch {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Message != result.Categories.Parameters.Message {
		t.Fatalf("expected overall message to come from parameters, got %+v", result)
	}
	if result.Categories.Metadata.Status != CategoryStatusFail || result.Categories.References.Status != CategoryStatusFail {
		t.Fatalf("expected later categories to still evaluate and fail, got %+v", result.Categories)
	}
}

func TestVerify_RepeatedParameterFailureIsStable(t *testing.T) {
	contract := parameterEnabledContract()
	firstObserved := matchingObserved(contract)
	firstObserved.Observation.Parameters[1].Value = mustObservedValue(t, []byte(`999`))

	firstResult, firstErr := Verify(contract, firstObserved)
	if firstErr == nil {
		t.Fatal("expected first Verify to fail")
	}
	if !errors.Is(firstErr, ErrParameterMismatch) {
		t.Fatalf("expected ErrParameterMismatch, got %v", firstErr)
	}

	secondObserved := cloneObserved(firstObserved)
	secondResult, secondErr := Verify(contract, secondObserved)
	if secondErr == nil {
		t.Fatal("expected second Verify to fail")
	}
	if !errors.Is(secondErr, ErrParameterMismatch) {
		t.Fatalf("expected ErrParameterMismatch on repeat, got %v", secondErr)
	}
	if firstResult.Failure != secondResult.Failure || firstResult.Message != secondResult.Message {
		t.Fatalf("expected stable parameter failure\nfirst:  %+v\nsecond: %+v", firstResult, secondResult)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected stable parameter error text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func TestVerify_ErrorsIsSupportsErrParameterMismatch(t *testing.T) {
	contract := parameterEnabledContract()
	obs := matchingObserved(contract)
	obs.Observation.Parameters[1].Value = mustObservedValue(t, []byte(`999`))

	_, err := Verify(contract, obs)
	if err == nil {
		t.Fatal("expected Verify to fail")
	}
	if !errors.Is(err, ErrParameterMismatch) {
		t.Fatalf("expected errors.Is(err, ErrParameterMismatch) to succeed, got %v", err)
	}
}

func TestVerify_ExplicitPassRequiresAllEnabledCategoriesAndIsStable(t *testing.T) {
	contract := validContract()
	firstObserved := matchingObserved(contract)
	firstObserved.Observation.Parameters = []observed.Parameter{{
		ID:        "varset:Dimensions.length",
		Name:      "length",
		GroupID:   "Dimensions",
		Value:     mustObservedValue(t, []byte(`999`)),
		ValueKind: "integer",
	}}

	firstResult, firstErr := Verify(contract, firstObserved)
	if firstErr != nil {
		t.Fatalf("expected explicit verifier pass, got %v", firstErr)
	}
	if firstResult.Status != StatusPass {
		t.Fatalf("expected overall pass when every enabled category passes, got %+v", firstResult)
	}
	if firstResult.Message != "verification passed" {
		t.Fatalf("expected stable pass message, got %q", firstResult.Message)
	}
	if firstResult.Failure != FailureClassNone {
		t.Fatalf("expected no failure classification on pass, got %+v", firstResult)
	}
	if firstResult.Categories.Parameters.Enabled {
		t.Fatalf("expected disabled parameter category to remain non-authoritative, got %+v", firstResult.Categories.Parameters)
	}
	if firstResult.Categories.Parameters.Status != CategoryStatusSkipped {
		t.Fatalf("expected disabled parameter category to be skipped, got %+v", firstResult.Categories.Parameters)
	}
	if firstResult.Categories.Metadata.Status != CategoryStatusPass || firstResult.Categories.References.Status != CategoryStatusPass {
		t.Fatalf("expected every enabled category to pass, got %+v", firstResult.Categories)
	}

	secondObserved := cloneObserved(firstObserved)
	secondResult, secondErr := Verify(contract, secondObserved)
	if secondErr != nil {
		t.Fatalf("expected repeated explicit verifier pass, got %v", secondErr)
	}
	if !reflect.DeepEqual(firstResult, secondResult) {
		t.Fatalf("expected identical pass results across repeated runs\nfirst:  %+v\nsecond: %+v", firstResult, secondResult)
	}
}

func TestVerifyRejectsInvalidContractAndObserved(t *testing.T) {
	invalidContract := validContract()
	invalidContract.Checks.Parameters.Enabled = true

	result, err := Verify(invalidContract, matchingObserved(validContract()))
	if err == nil {
		t.Fatal("expected invalid contract failure")
	}
	if !errors.Is(err, ErrContractInvalid) {
		t.Fatalf("expected ErrContractInvalid, got %v", err)
	}
	if result.Failure != FailureClassContractInvalid {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !strings.Contains(result.Message, "checks.parameters.enabled requires observe.parameters to be true") {
		t.Fatalf("unexpected contract invalid message: %q", result.Message)
	}

	invalidObserved := matchingObserved(validContract())
	invalidObserved.WorkingCopy.Path = "relative.FCStd"
	result, err = Verify(validContract(), invalidObserved)
	if err == nil {
		t.Fatal("expected invalid observed failure")
	}
	if !errors.Is(err, ErrObservedInvalid) {
		t.Fatalf("expected ErrObservedInvalid, got %v", err)
	}
	if result.Failure != FailureClassObservedInvalid {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestVerify_ParameterCategoryRemainsSkippedInV1Baseline(t *testing.T) {
	contract := validContract()
	obs := matchingObserved(contract)
	obs.Observation.Parameters = []observed.Parameter{{
		ID:        "varset:Dimensions.length",
		Name:      "length",
		GroupID:   "Dimensions",
		Value:     mustObservedValue(t, []byte(`999`)),
		ValueKind: "integer",
	}}

	result, err := Verify(contract, obs)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if result.Status != StatusPass {
		t.Fatalf("expected overall pass when enabled categories pass, got %+v", result)
	}
	if result.Categories.Parameters.Enabled {
		t.Fatalf("expected parameters to remain disabled in v1 baseline, got %+v", result.Categories.Parameters)
	}
	if result.Categories.Parameters.Status != CategoryStatusSkipped {
		t.Fatalf("expected skipped parameter status, got %+v", result.Categories.Parameters)
	}
	if result.Categories.Parameters.Message != parameterVerificationDisabledMessage {
		t.Fatalf("unexpected parameter skip message: %q", result.Categories.Parameters.Message)
	}
}

func TestVerify_MetadataComparisonRules(t *testing.T) {
	tests := []struct {
		name           string
		mutate         func(*observed.Observed)
		wantStatus     Status
		wantClass      FailureClass
		wantMessageSub string
		wantErr        error
	}{
		{
			name:       "exact match passes",
			mutate:     func(_ *observed.Observed) {},
			wantStatus: StatusPass,
		},
		{
			name: "missing required metadata fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Metadata = []observed.Metadata{}
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassRequiredObservationMissing,
			wantMessageSub: `required observed metadata "working_copy_sha256" is missing`,
			wantErr:        ErrObservationMissing,
		},
		{
			name: "duplicate observed metadata key fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Metadata = append(obs.Observation.Metadata, observed.Metadata{
					ID:        "working-copy:sha256-duplicate",
					Key:       obs.Observation.Metadata[0].Key,
					Value:     cloneObservedValue(obs.Observation.Metadata[0].Value),
					ValueKind: obs.Observation.Metadata[0].ValueKind,
				})
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassMetadataMismatch,
			wantMessageSub: `observed metadata "working_copy_sha256" is not unique`,
			wantErr:        ErrMetadataMismatch,
		},
		{
			name: "wrong metadata value fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Metadata[0].Value = mustObservedMetadataValue(strings.Repeat("b", 64))
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassMetadataMismatch,
			wantMessageSub: `observed metadata "working_copy_sha256" mismatch`,
			wantErr:        ErrMetadataMismatch,
		},
		{
			name: "extra unrelated observed metadata is ignored",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Metadata = append(obs.Observation.Metadata, observed.Metadata{
					ID:        "meta.unrequested",
					Key:       "unrequested_metadata",
					Value:     mustObservedMetadataValue("kept-but-ignored"),
					ValueKind: "string",
				})
			},
			wantStatus: StatusPass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := validContract()
			obs := matchingObserved(contract)
			tt.mutate(obs)

			result, err := Verify(contract, obs)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected pass, got error %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected Verify to fail")
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
			}
			if result.Status != tt.wantStatus {
				t.Fatalf("unexpected status: %+v", result)
			}
			if result.Categories.Metadata.Enabled != contract.Checks.Metadata.Enabled {
				t.Fatalf("unexpected metadata enabled flag: %+v", result.Categories.Metadata)
			}
			if tt.wantStatus == StatusPass {
				if result.Categories.Metadata.Status != CategoryStatusPass {
					t.Fatalf("expected metadata pass, got %+v", result.Categories.Metadata)
				}
				if result.Failure != FailureClassNone {
					t.Fatalf("expected no failure class, got %+v", result)
				}
				return
			}
			if result.Categories.Metadata.Status != CategoryStatusFail {
				t.Fatalf("expected metadata fail, got %+v", result.Categories.Metadata)
			}
			if result.Failure != tt.wantClass {
				t.Fatalf("unexpected failure class: %+v", result)
			}
			if !strings.Contains(result.Message, tt.wantMessageSub) {
				t.Fatalf("unexpected result message: %q", result.Message)
			}
			if !strings.Contains(result.Categories.Metadata.Message, tt.wantMessageSub) {
				t.Fatalf("unexpected category message: %q", result.Categories.Metadata.Message)
			}
		})
	}
}

func TestVerify_MetadataIdentityComparisonRules(t *testing.T) {
	tests := []struct {
		name           string
		mutate         func(*observed.Observed)
		wantStatus     Status
		wantClass      FailureClass
		wantMessageSub string
		wantErr        error
	}{
		{
			name:       "id-based metadata exact match passes",
			mutate:     func(_ *observed.Observed) {},
			wantStatus: StatusPass,
		},
		{
			name: "missing metadata id fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Metadata = []observed.Metadata{}
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassRequiredObservationMissing,
			wantMessageSub: `required observed metadata "meta.mass" is missing`,
			wantErr:        ErrObservationMissing,
		},
		{
			name: "duplicate observed metadata id fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Metadata = append(obs.Observation.Metadata, observed.Metadata{
					ID:        "meta.mass",
					Key:       "mass",
					OwnerID:   "cmp.part.leg",
					Value:     mustObservedScalarValue("12.5"),
					ValueKind: "number",
				})
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassMetadataMismatch,
			wantMessageSub: `observed metadata "meta.mass" is not unique`,
			wantErr:        ErrMetadataMismatch,
		},
		{
			name: "metadata key mismatch fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Metadata[0].Key = "weight"
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassMetadataMismatch,
			wantMessageSub: `observed metadata "meta.mass" key mismatch`,
			wantErr:        ErrMetadataMismatch,
		},
		{
			name: "metadata ownerId mismatch fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Metadata[0].OwnerID = ""
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassMetadataMismatch,
			wantMessageSub: `observed metadata "meta.mass" ownerId mismatch`,
			wantErr:        ErrMetadataMismatch,
		},
		{
			name: "metadata valueKind mismatch fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Metadata[0].ValueKind = "integer"
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassMetadataMismatch,
			wantMessageSub: `observed metadata "meta.mass" valueKind mismatch`,
			wantErr:        ErrMetadataMismatch,
		},
		{
			name: "metadata scalar value mismatch fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.Metadata[0].Value = mustObservedScalarValue("99")
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassMetadataMismatch,
			wantMessageSub: `observed metadata "meta.mass" mismatch`,
			wantErr:        ErrMetadataMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := metadataIdentityContract()
			obs := matchingObserved(contract)
			tt.mutate(obs)

			result, err := Verify(contract, obs)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected pass, got error %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected Verify to fail")
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
			}
			if result.Status != tt.wantStatus {
				t.Fatalf("unexpected status: %+v", result)
			}
			if tt.wantStatus == StatusPass {
				if result.Categories.Metadata.Status != CategoryStatusPass {
					t.Fatalf("expected metadata pass, got %+v", result.Categories.Metadata)
				}
				return
			}
			if result.Categories.Metadata.Status != CategoryStatusFail {
				t.Fatalf("expected metadata fail, got %+v", result.Categories.Metadata)
			}
			if result.Failure != tt.wantClass {
				t.Fatalf("unexpected failure class: %+v", result)
			}
			if !strings.Contains(result.Message, tt.wantMessageSub) {
				t.Fatalf("unexpected result message: %q", result.Message)
			}
		})
	}
}

func TestVerify_ReferenceComparisonRules(t *testing.T) {
	tests := []struct {
		name           string
		mutate         func(*observed.Observed)
		wantStatus     Status
		wantClass      FailureClass
		wantMessageSub string
		wantErr        error
	}{
		{
			name:       "exact match passes",
			mutate:     func(_ *observed.Observed) {},
			wantStatus: StatusPass,
		},
		{
			name: "missing required reference fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.References = []observed.Reference{}
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassRequiredObservationMissing,
			wantMessageSub: `required observed reference "working_copy_path" is missing`,
			wantErr:        ErrObservationMissing,
		},
		{
			name: "duplicate observed reference kind fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.References = append(obs.Observation.References, observed.Reference{
					Kind: obs.Observation.References[0].Kind,
					Name: obs.Observation.References[0].Name,
				})
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassReferenceMismatch,
			wantMessageSub: `observed reference "working_copy_path" is not unique`,
			wantErr:        ErrReferenceMismatch,
		},
		{
			name: "wrong reference name fails",
			mutate: func(obs *observed.Observed) {
				obs.Observation.References[0].Name = "/tmp/unexpected.FCStd"
			},
			wantStatus:     StatusFail,
			wantClass:      FailureClassReferenceMismatch,
			wantMessageSub: `observed reference "working_copy_path" mismatch`,
			wantErr:        ErrReferenceMismatch,
		},
		{
			name: "extra unrelated observed references are ignored",
			mutate: func(obs *observed.Observed) {
				obs.Observation.References = append(obs.Observation.References, observed.Reference{
					Kind: "other_reference",
					Name: "/tmp/ignored.FCStd",
				})
			},
			wantStatus: StatusPass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := validContract()
			obs := matchingObserved(contract)
			tt.mutate(obs)

			result, err := Verify(contract, obs)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected pass, got error %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected Verify to fail")
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
			}
			if result.Status != tt.wantStatus {
				t.Fatalf("unexpected status: %+v", result)
			}
			if tt.wantStatus == StatusPass {
				if result.Categories.References.Status != CategoryStatusPass {
					t.Fatalf("expected reference pass, got %+v", result.Categories.References)
				}
				return
			}
			if result.Categories.References.Status != CategoryStatusFail {
				t.Fatalf("expected reference fail, got %+v", result.Categories.References)
			}
			if result.Failure != tt.wantClass {
				t.Fatalf("unexpected failure class: %+v", result)
			}
			if !strings.Contains(result.Message, tt.wantMessageSub) {
				t.Fatalf("unexpected result message: %q", result.Message)
			}
			if !strings.Contains(result.Categories.References.Message, tt.wantMessageSub) {
				t.Fatalf("unexpected category message: %q", result.Categories.References.Message)
			}
		})
	}
}

func TestVerify_EvaluationOrderPrefersComponentsThenParameters(t *testing.T) {
	t.Run("component failure beats later parameter metadata and reference failures", func(t *testing.T) {
		contract := componentEnabledContract()
		contract.Observe.Parameters = true
		contract.Checks.Parameters.Enabled = true
		contract.ObservationContext.Parameters = []ObservedParameterBinding{
			{ID: "par.root.height", Name: "Height", GroupName: "Dimensions"},
			{ID: "par.root.length", Name: "Length", GroupName: "Dimensions"},
		}

		obs := matchingObserved(contract)
		obs.Observation.Components[1].Name = "Leg mismatch"
		obs.Observation.Parameters[1].Value = mustObservedValue(t, []byte(`999`))
		obs.Observation.Metadata[0].Value = mustObservedMetadataValue(strings.Repeat("e", 64))
		obs.Observation.References[0].Name = "/tmp/wrong.FCStd"

		result, err := Verify(contract, obs)
		if err == nil {
			t.Fatal("expected Verify to fail")
		}
		if result.Failure != FailureClassComponentMismatch {
			t.Fatalf("expected component mismatch to be authoritative, got %+v", result)
		}
		if !errors.Is(err, ErrComponentMismatch) {
			t.Fatalf("expected component mismatch classification, got %v", err)
		}
		if result.Categories.Components.Status != CategoryStatusFail || result.Categories.Parameters.Status != CategoryStatusFail || result.Categories.Metadata.Status != CategoryStatusFail || result.Categories.References.Status != CategoryStatusFail {
			t.Fatalf("expected all enabled failing categories to remain marked failed, got %+v", result.Categories)
		}
	})

	t.Run("parameter failure still beats metadata and references when components are disabled", func(t *testing.T) {
		contract := parameterEnabledContract()
		obs := matchingObserved(contract)
		obs.Observation.Parameters[1].Value = mustObservedValue(t, []byte(`999`))
		obs.Observation.Metadata[0].Value = mustObservedMetadataValue(strings.Repeat("f", 64))
		obs.Observation.References[0].Name = "/tmp/still-wrong.FCStd"

		result, err := Verify(contract, obs)
		if err == nil {
			t.Fatal("expected Verify to fail")
		}
		if result.Failure != FailureClassParameterMismatch {
			t.Fatalf("expected parameter mismatch to remain authoritative, got %+v", result)
		}
		if !errors.Is(err, ErrParameterMismatch) {
			t.Fatalf("expected parameter mismatch classification, got %v", err)
		}
		if result.Categories.Components.Status != CategoryStatusSkipped {
			t.Fatalf("expected disabled components to remain skipped, got %+v", result.Categories.Components)
		}
	})
}

func TestVerify_RicherStructureFailuresRemainDeterministic(t *testing.T) {
	contract := componentEnabledContract()
	contract.Expected.Metadata = []ExpectedMetadata{{
		ID:        "meta.mass",
		Key:       "mass",
		OwnerID:   "cmp.part.leg",
		Value:     "12.5",
		ValueKind: "number",
	}}
	obs := matchingObserved(contract)
	obs.Observation.Components[1].ParentID = ""

	firstResult, firstErr := Verify(contract, cloneObserved(obs))
	if firstErr == nil {
		t.Fatal("expected first Verify to fail")
	}
	secondResult, secondErr := Verify(contract, cloneObserved(obs))
	if secondErr == nil {
		t.Fatal("expected second Verify to fail")
	}
	if firstResult.Failure != secondResult.Failure {
		t.Fatalf("failure class changed\nfirst:  %+v\nsecond: %+v", firstResult, secondResult)
	}
	if firstResult.Message != secondResult.Message {
		t.Fatalf("failure message changed\nfirst:  %q\nsecond: %q", firstResult.Message, secondResult.Message)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("error text changed\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func TestVerify_OverallAcceptanceRulesAndDeterminism(t *testing.T) {
	passContract := validContract()
	passObserved := matchingObserved(passContract)

	passResult, err := Verify(passContract, passObserved)
	if err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
	if passResult.Status != StatusPass {
		t.Fatalf("unexpected pass result: %+v", passResult)
	}
	if passResult.Categories.Parameters.Status != CategoryStatusSkipped {
		t.Fatalf("expected skipped parameters, got %+v", passResult.Categories.Parameters)
	}
	if passResult.Categories.Metadata.Status != CategoryStatusPass || passResult.Categories.References.Status != CategoryStatusPass {
		t.Fatalf("expected enabled categories to pass, got %+v", passResult.Categories)
	}

	failObserved := matchingObserved(passContract)
	failObserved.Observation.Metadata[0].Value = mustObservedMetadataValue(strings.Repeat("c", 64))
	failObserved.Observation.References[0].Name = "/tmp/other.FCStd"

	firstResult, firstErr := Verify(passContract, failObserved)
	if firstErr == nil {
		t.Fatal("expected failure when one enabled category fails")
	}
	if !errors.Is(firstErr, ErrMetadataMismatch) {
		t.Fatalf("expected first failing class to stay deterministic, got %v", firstErr)
	}
	if firstResult.Status != StatusFail {
		t.Fatalf("expected overall fail, got %+v", firstResult)
	}
	if firstResult.Categories.Metadata.Status != CategoryStatusFail || firstResult.Categories.References.Status != CategoryStatusFail {
		t.Fatalf("expected both enabled failing categories to be marked failed, got %+v", firstResult.Categories)
	}
	if firstResult.Categories.Parameters.Status != CategoryStatusSkipped {
		t.Fatalf("expected disabled category to remain skipped, got %+v", firstResult.Categories.Parameters)
	}

	secondObserved := cloneObserved(failObserved)
	secondResult, secondErr := Verify(passContract, secondObserved)
	if secondErr == nil {
		t.Fatal("expected repeated failure")
	}
	if !reflect.DeepEqual(firstResult, secondResult) {
		t.Fatalf("expected identical result objects across repeated runs\nfirst:  %+v\nsecond: %+v", firstResult, secondResult)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected identical error messages across repeated runs\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func TestVerify_FirstEnabledFailingCategoryRemainsAuthoritativeAcrossRepeatedRuns(t *testing.T) {
	contract := validContract()
	observedWithMultipleFailures := matchingObserved(contract)
	observedWithMultipleFailures.Observation.Parameters = []observed.Parameter{{
		ID:        "varset:Dimensions.length",
		Name:      "length",
		GroupID:   "Dimensions",
		Value:     mustObservedValue(t, []byte(`999`)),
		ValueKind: "integer",
	}}
	observedWithMultipleFailures.Observation.Metadata[0].Value = mustObservedMetadataValue(strings.Repeat("d", 64))
	observedWithMultipleFailures.Observation.References[0].Name = "/tmp/wrong.FCStd"

	const runs = 5

	var firstStatus Status
	var firstClass FailureClass
	var firstMessage string
	var firstErrorText string

	for run := 1; run <= runs; run++ {
		currentObserved := cloneObserved(observedWithMultipleFailures)
		result, err := Verify(contract, currentObserved)
		if err == nil {
			t.Fatalf("run %d: expected Verify to fail when multiple enabled categories fail", run)
		}

		if result.Status != StatusFail {
			t.Fatalf("run %d: expected overall fail, got %+v", run, result)
		}
		if result.Categories.Parameters.Status != CategoryStatusSkipped {
			t.Fatalf("run %d: expected disabled parameters to remain skipped, got %+v", run, result.Categories.Parameters)
		}
		if result.Categories.Metadata.Status != CategoryStatusFail {
			t.Fatalf("run %d: expected metadata category to fail, got %+v", run, result.Categories.Metadata)
		}
		if result.Categories.References.Status != CategoryStatusFail {
			t.Fatalf("run %d: expected references category to fail, got %+v", run, result.Categories.References)
		}

		wantMessage := result.Categories.Metadata.Message
		if result.Failure != FailureClassMetadataMismatch {
			t.Fatalf("run %d: expected first enabled failing category to win with %q, got %+v", run, FailureClassMetadataMismatch, result)
		}
		if result.Message != wantMessage {
			t.Fatalf("run %d: expected overall message to come from authoritative metadata failure\nwant: %q\ngot:  %q", run, wantMessage, result.Message)
		}
		if result.Message == result.Categories.References.Message {
			t.Fatalf("run %d: expected later failing reference category not to overtake metadata authority", run)
		}
		if !errors.Is(err, ErrMetadataMismatch) {
			t.Fatalf("run %d: expected metadata mismatch classification, got %v", run, err)
		}

		if run == 1 {
			firstStatus = result.Status
			firstClass = result.Failure
			firstMessage = result.Message
			firstErrorText = err.Error()
			continue
		}
		if result.Status != firstStatus {
			t.Fatalf("run %d: expected stable status across repeated runs\nfirst:  %q\nsecond: %q", run, firstStatus, result.Status)
		}
		if result.Failure != firstClass {
			t.Fatalf("run %d: expected stable authoritative failure class across repeated runs\nfirst:  %q\nsecond: %q", run, firstClass, result.Failure)
		}
		if result.Message != firstMessage {
			t.Fatalf("run %d: expected stable authoritative failure message across repeated runs\nfirst:  %q\nsecond: %q", run, firstMessage, result.Message)
		}
		if err.Error() != firstErrorText {
			t.Fatalf("run %d: expected stable error text across repeated runs\nfirst:  %q\nsecond: %q", run, firstErrorText, err.Error())
		}
	}
}

func TestAggregateResult_ClassifiesMissingFailureClassAsInternalError(t *testing.T) {
	categories := CategoryResults{
		Parameters: skippedCategoryResult(false, parameterVerificationDisabledMessage),
		Metadata: CategoryResult{
			Enabled: true,
			Status:  CategoryStatusFail,
			Message: "metadata comparison failed unexpectedly",
		},
		References: skippedCategoryResult(true, referencesNotEvaluatedMessage),
	}

	result := aggregateResult(categories)
	if result.Status != StatusFail {
		t.Fatalf("expected fail status, got %+v", result)
	}
	if result.Failure != FailureClassInternalError {
		t.Fatalf("expected internal verification failure class, got %+v", result)
	}
	if result.Message != "internal verification error: failed verification category did not declare a failure class" {
		t.Fatalf("unexpected internal verification message: %q", result.Message)
	}
	if result.Categories.Metadata.Message != "metadata comparison failed unexpectedly" {
		t.Fatalf("expected original category message to remain intact, got %+v", result.Categories.Metadata)
	}
	if verifyErr := (&VerifyError{Class: result.Failure, Message: result.Message}); !errors.Is(verifyErr, ErrInternal) {
		t.Fatalf("expected internal verification sentinel mapping, got %v", verifyErr)
	}
}

func TestCategoryResultFailureClassAccessor(t *testing.T) {
	pass := passedCategoryResult(true, "metadata passed")
	if got := pass.FailureClass(); got != FailureClassNone {
		t.Fatalf("pass FailureClass() = %q, want empty", got)
	}

	skipped := skippedCategoryResult(false, "metadata skipped")
	if got := skipped.FailureClass(); got != FailureClassNone {
		t.Fatalf("skipped FailureClass() = %q, want empty", got)
	}

	failed := failedCategoryResult(true, FailureClassMetadataMismatch, "metadata failed")
	if got := failed.FailureClass(); got != FailureClassMetadataMismatch {
		t.Fatalf("failed FailureClass() = %q, want %q", got, FailureClassMetadataMismatch)
	}

	copied := failed
	copied.class = FailureClassComponentMismatch
	if failed.FailureClass() != FailureClassMetadataMismatch {
		t.Fatalf("mutating accessor return changed category state: %q", failed.FailureClass())
	}
}

func TestDeriveVerifyMetadataPassAndFail(t *testing.T) {
	root := t.TempDir()
	outputDir := filepath.Join(root, "products", "widget")
	workingCopyPath := filepath.Join(root, "attempt", "source", "widget.FCStd")
	workingCopyBytes := []byte("fully verified working copy")
	writeFile(t, workingCopyPath, string(workingCopyBytes))

	manifest := planner.WriteExportManifestPayload{
		SchemaVersion: planner.ExportManifestSchemaVersion,
		ParameterAssignments: []planner.ExportManifestParameterAssignment{
			{Name: "length", Value: 35, Type: "number", Unit: "mm"},
		},
	}

	contract, err := DeriveFromManifestAndWorkingCopy(manifest, workingCopyPath)
	if err != nil {
		t.Fatalf("DeriveFromManifestAndWorkingCopy returned error: %v", err)
	}
	verificationPath := filepath.Join(outputDir, "parametron.verification.json")
	if err := WriteFile(verificationPath, contract); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	parsedObserved := matchingObserved(contract)
	result, err := Verify(contract, parsedObserved)
	if err != nil {
		t.Fatalf("Verify(pass) returned error: %v", err)
	}
	if result.Status != StatusPass {
		t.Fatalf("expected pass result, got %+v", result)
	}

	tampered := cloneObserved(parsedObserved)
	tampered.Observation.Metadata[0].Value = mustObservedMetadataValue(strings.Repeat("0", 64))

	failResult, failErr := Verify(contract, tampered)
	if failErr == nil {
		t.Fatal("expected tampered verification to fail")
	}
	if !errors.Is(failErr, ErrMetadataMismatch) {
		t.Fatalf("expected ErrMetadataMismatch, got %v", failErr)
	}
	if failResult.Status != StatusFail || failResult.Failure != FailureClassMetadataMismatch {
		t.Fatalf("unexpected fail result: %+v", failResult)
	}
	if !strings.Contains(failResult.Message, `observed metadata "working_copy_sha256" mismatch`) {
		t.Fatalf("unexpected fail message: %s", failResult.Message)
	}
}

func TestEndToEndDeriveVerifyParameterPassAndFailWithoutFabricatedIdentity(t *testing.T) {
	root := t.TempDir()
	workingCopyPath := filepath.Join(root, "attempt", "source", "widget.FCStd")
	writeFile(t, workingCopyPath, "fully verified working copy")

	contract, err := DeriveFromManifestAndWorkingCopy(planner.WriteExportManifestPayload{
		SchemaVersion: planner.ExportManifestSchemaVersion,
		Verification: planner.VerificationManifestIntent{
			ExpectedParameters: []planner.VerificationExpectedParameter{
				{ID: "par.root.length", Name: "length", Value: 35, Type: "number", Unit: "mm"},
			},
			ObservationParameterLinks: []planner.VerificationObservationParameterLink{
				{ID: "par.root.length", Name: "Length", GroupName: "Dimensions"},
			},
		},
	}, workingCopyPath)
	if err != nil {
		t.Fatalf("DeriveFromManifestAndWorkingCopy returned error: %v", err)
	}
	if !contract.Observe.Parameters || !contract.Checks.Parameters.Enabled {
		t.Fatalf("expected parameter verification to be enabled, got %+v", contract)
	}

	passObserved := matchingObserved(contract)
	passResult, passErr := Verify(contract, passObserved)
	if passErr != nil {
		t.Fatalf("Verify(pass) returned error: %v", passErr)
	}
	if passResult.Categories.Parameters.Status != CategoryStatusPass {
		t.Fatalf("expected parameter category pass, got %+v", passResult.Categories.Parameters)
	}

	failObserved := cloneObserved(passObserved)
	failObserved.Observation.Parameters[0].Value = mustObservedValue(t, []byte(`36`))
	failResult, failErr := Verify(contract, failObserved)
	if failErr == nil {
		t.Fatal("expected parameter verification failure")
	}
	if !errors.Is(failErr, ErrParameterMismatch) {
		t.Fatalf("expected ErrParameterMismatch, got %v", failErr)
	}
	if failResult.Failure != FailureClassParameterMismatch {
		t.Fatalf("unexpected fail result: %+v", failResult)
	}
}

func validVerificationJSON() string {
	return `{
  "schemaVersion": "1.0",
  "observe": {
    "components": false,
    "parameters": false,
    "metadata": true,
    "references": true
  },
  "observationContext": {
    "parameters": []
  },
  "expected": {
    "components": [],
    "parameters": [
      {
        "id": "par.root.height",
        "name": "height",
        "type": "number",
        "unit": "mm",
        "value": 5
      },
      {
        "id": "par.root.length",
        "name": "length",
        "type": "number",
        "unit": "mm",
        "value": 35
      }
    ],
    "metadata": [
      {
        "key": "working_copy_sha256",
        "value": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      }
    ],
    "references": [
      {
        "kind": "working_copy_path",
        "name": "/tmp/out/_working/widget.FCStd"
      }
    ]
  },
  "checks": {
    "components": {
      "enabled": false
    },
    "parameters": {
      "enabled": false
    },
    "metadata": {
      "enabled": true
    },
    "references": {
      "enabled": true
    }
  }
}`
}

func validContract() *Contract {
	parsed, err := Parse([]byte(validVerificationJSON()))
	if err != nil {
		panic(err)
	}
	return parsed
}

func parameterEnabledContract() *Contract {
	contract := validContract()
	contract.Observe.Parameters = true
	contract.Checks.Parameters.Enabled = true
	contract.ObservationContext.Parameters = []ObservedParameterBinding{
		{ID: "par.root.height", Name: "Height", GroupName: "Dimensions"},
		{ID: "par.root.length", Name: "Length", GroupName: "Dimensions"},
	}
	return contract
}

func componentEnabledContract() *Contract {
	contract := validContract()
	contract.Observe.Components = true
	contract.Checks.Components.Enabled = true
	contract.Expected.Components = []ExpectedComponent{
		{ID: "cmp.root", Kind: observed.ComponentKindAssembly, Name: "Widget", ParentID: ""},
		{ID: "cmp.part.leg", Kind: observed.ComponentKindPart, Name: "Leg", ParentID: "cmp.root"},
	}
	return contract
}

func metadataIdentityContract() *Contract {
	contract := validContract()
	contract.Expected.Metadata = []ExpectedMetadata{{
		ID:        "meta.mass",
		Key:       "mass",
		OwnerID:   "cmp.part.leg",
		Value:     "12.5",
		ValueKind: "number",
	}}
	return contract
}

func matchingObserved(contract *Contract) *observed.Observed {
	obs := &observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		WorkingCopy: observed.WorkingCopy{
			Path:   contract.Expected.References[0].Name,
			SHA256: contract.Expected.Metadata[0].Value,
		},
		Observation: observed.Observation{
			Parameters: []observed.Parameter{},
			Metadata: []observed.Metadata{{
				ID:        "working-copy:sha256",
				Key:       contract.Expected.Metadata[0].Key,
				Value:     mustObservedMetadataValue(contract.Expected.Metadata[0].Value),
				ValueKind: "string",
			}},
			References: []observed.Reference{{
				Kind: contract.Expected.References[0].Kind,
				Name: contract.Expected.References[0].Name,
			}},
			Components: []observed.Component{},
		},
	}
	if contract.Checks.Parameters.Enabled {
		obs.Observation.Parameters = matchingObservedParameters(contract)
	}
	if len(contract.Expected.Components) > 0 {
		obs.Observation.Components = matchingObservedComponents(contract)
	}
	if len(contract.Expected.Metadata) > 0 && contract.Expected.Metadata[0].ID != "" {
		obs.Observation.Metadata = matchingObservedMetadata(contract)
		obs.WorkingCopy.SHA256 = strings.Repeat("a", 64)
	}
	return obs
}

func matchingObservedParameters(contract *Contract) []observed.Parameter {
	byID := make(map[string]ObservedParameterBinding, len(contract.ObservationContext.Parameters))
	for _, binding := range contract.ObservationContext.Parameters {
		byID[binding.ID] = binding
	}

	parameters := make([]observed.Parameter, 0, len(contract.Expected.Parameters))
	for _, parameter := range contract.Expected.Parameters {
		name := parameter.Name
		groupID := ""
		if binding, ok := byID[parameter.ID]; ok {
			if binding.Name != "" {
				name = binding.Name
			}
			groupID = binding.GroupName
		}
		parameters = append(parameters, observed.Parameter{
			ID:        parameter.ID,
			Name:      name,
			GroupID:   groupID,
			Value:     mustObservedScalarValue(strconv.FormatFloat(parameter.Value, 'f', -1, 64)),
			ValueKind: "number",
		})
	}
	return parameters
}

func matchingObservedComponents(contract *Contract) []observed.Component {
	components := make([]observed.Component, 0, len(contract.Expected.Components))
	for _, component := range contract.Expected.Components {
		components = append(components, observed.Component{
			ID:       component.ID,
			Kind:     component.Kind,
			Name:     component.Name,
			ParentID: component.ParentID,
		})
	}
	return components
}

func matchingObservedMetadata(contract *Contract) []observed.Metadata {
	entries := make([]observed.Metadata, 0, len(contract.Expected.Metadata))
	for _, entry := range contract.Expected.Metadata {
		value := mustObservedMetadataValue(entry.Value)
		if entry.ValueKind != "" && entry.ValueKind != "string" {
			value = mustObservedScalarValue(entry.Value)
		}
		entries = append(entries, observed.Metadata{
			ID:        entry.ID,
			Key:       entry.Key,
			OwnerID:   entry.OwnerID,
			Value:     value,
			ValueKind: entry.ValueKind,
		})
	}
	return entries
}

func cloneObserved(in *observed.Observed) *observed.Observed {
	out := *in
	out.Observation.Parameters = slicesClone(in.Observation.Parameters)
	out.Observation.Metadata = slicesClone(in.Observation.Metadata)
	for i := range out.Observation.Metadata {
		out.Observation.Metadata[i].Value = cloneObservedValue(in.Observation.Metadata[i].Value)
	}
	out.Observation.References = slicesClone(in.Observation.References)
	out.Observation.Components = slicesClone(in.Observation.Components)
	return &out
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create parent dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func slicesClone[T any](in []T) []T {
	if in == nil {
		return nil
	}
	out := make([]T, len(in))
	copy(out, in)
	return out
}

func mustObservedValue(t *testing.T, raw []byte) observed.Value {
	t.Helper()
	value, err := observed.NewValue(raw)
	if err != nil {
		t.Fatalf("observed.NewValue returned error: %v", err)
	}
	return value
}

func mustObservedMetadataValue(value string) observed.Value {
	raw, err := observed.NewValue([]byte(`"` + value + `"`))
	if err != nil {
		panic(err)
	}
	return raw
}

func mustObservedScalarValue(value string) observed.Value {
	raw, err := observed.NewValue([]byte(value))
	if err != nil {
		panic(err)
	}
	return raw
}

func cloneObservedValue(value observed.Value) observed.Value {
	cloned, err := observed.NewValue(value.Raw())
	if err != nil {
		panic(err)
	}
	return cloned
}

func metadataStringValue(t *testing.T, value observed.Value) string {
	t.Helper()
	raw := value.Raw()
	var out string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("failed to decode metadata string value %q: %v", raw, err)
	}
	return out
}
