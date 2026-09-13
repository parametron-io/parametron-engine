package cad

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSmokeFixturesParseAndValidate(t *testing.T) {
	fixtures := []string{
		"minimal-valid",
		"with-parameter-group",
		"with-hierarchy",
	}

	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			contract := loadFixture(t, "smoke", fixture)
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
				t.Fatalf("canonical JSON was not byte-stable\nfirst:  %s\nsecond: %s", first, second)
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
				t.Fatalf("canonical JSON changed across round trip\nfirst:  %s\nsecond: %s", first, roundTrip)
			}
		})
	}
}

func TestBreakFixturesFailDeterministically(t *testing.T) {
	cases := []struct {
		name string
		want string
		kind error
	}{
		{name: "malformed-json", want: "decode error", kind: ErrDecode},
		{name: "unsupported-schema-version", want: `schemaVersion must be "1.0"`, kind: ErrValidation},
		{name: "missing-schema-version", want: "schemaVersion is required", kind: ErrValidation},
		{name: "unknown-root-field", want: `root has unknown field "extra"`, kind: ErrValidation},
		{name: "unknown-nested-field", want: `adapter has unknown field "extra"`, kind: ErrValidation},
		{name: "missing-required-field", want: "adapter.name is required", kind: ErrValidation},
		{name: "null-required-array", want: "entities.components must be an array", kind: ErrValidation},
		{name: "duplicate-stable-id", want: `duplicates stable ID "cmp.root"`, kind: ErrValidation},
		{name: "duplicate-owner-scoped-identity-source", want: `duplicates owner-scoped identity source`, kind: ErrValidation},
		{name: "missing-owner-reference", want: `ownerId "fea.missing" must resolve to an existing feature`, kind: ErrValidation},
		{name: "missing-parameter-group-reference", want: `ownerId "grp.missing" must resolve to an existing parameter group`, kind: ErrValidation},
		{name: "missing-parent-reference", want: `parentComponentId "cmp.missing" must resolve to an existing component`, kind: ErrValidation},
		{name: "missing-annotation-field", want: "annotations.purpose is required", kind: ErrValidation},
		{name: "invalid-targetability-field-shape", want: "targetability.delete must be a boolean", kind: ErrValidation},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(fixturePath(t, "break", tc.name))
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

func TestErrorsAreClassified(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected missing file load to fail")
	}
	if !errors.Is(err, ErrIO) {
		t.Fatalf("expected ErrIO, got %v", err)
	}

	_, err = Parse([]byte(`{"schemaVersion":"1.0"`))
	if err == nil {
		t.Fatal("expected malformed Parse to fail")
	}
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("expected ErrDecode, got %v", err)
	}

	_, err = Load(fixturePath(t, "break", "missing-required-field"))
	if err == nil {
		t.Fatal("expected invalid fixture load to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestParseRejectsTrailingJSON(t *testing.T) {
	payload := []byte(`{"schemaVersion":"1.0","captureId":"cap.minimal","adapter":{"name":"freecad","version":""},"cadSystem":{"name":"FreeCAD","version":""},"sourceDocument":{"logicalId":"m","path":"input/M.FCStd","fingerprint":"sha256:x"},"rootProduct":{"id":"cmp.root"},"annotations":{"description":"","comment":"","purpose":""},"entities":{"components":[],"features":[],"relationships":[],"parameterGroups":[],"parameters":[],"metadata":[]},"structure":{"rootComponentId":"cmp.root","nodes":[]}} true`)
	_, err := Parse(payload)
	if err == nil {
		t.Fatal("expected Parse to fail")
	}
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("expected ErrDecode, got %v", err)
	}
	if !strings.Contains(err.Error(), "unexpected trailing JSON value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRejectsUnknownFieldsAtStrictBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "root",
			payload: `{"schemaVersion":"1.0","captureId":"cap","adapter":{"name":"freecad","version":""},"cadSystem":{"name":"FreeCAD","version":""},"sourceDocument":{"logicalId":"m","path":"input/M.FCStd","fingerprint":"sha256:x"},"rootProduct":{"id":"cmp.root"},"annotations":{"description":"","comment":"","purpose":""},"entities":{"components":[],"features":[],"relationships":[],"parameterGroups":[],"parameters":[],"metadata":[]},"structure":{"rootComponentId":"cmp.root","nodes":[]},"extra":true}`,
			want:    `root has unknown field "extra"`,
		},
		{
			name:    "annotations",
			payload: `{"schemaVersion":"1.0","captureId":"cap","adapter":{"name":"freecad","version":""},"cadSystem":{"name":"FreeCAD","version":""},"sourceDocument":{"logicalId":"m","path":"input/M.FCStd","fingerprint":"sha256:x"},"rootProduct":{"id":"cmp.root"},"annotations":{"description":"","comment":"","purpose":"","extra":true},"entities":{"components":[],"features":[],"relationships":[],"parameterGroups":[],"parameters":[],"metadata":[]},"structure":{"rootComponentId":"cmp.root","nodes":[]}}`,
			want:    `annotations has unknown field "extra"`,
		},
		{
			name:    "component",
			payload: `{"schemaVersion":"1.0","captureId":"cap","adapter":{"name":"freecad","version":""},"cadSystem":{"name":"FreeCAD","version":""},"sourceDocument":{"logicalId":"m","path":"input/M.FCStd","fingerprint":"sha256:x"},"rootProduct":{"id":"cmp.root"},"annotations":{"description":"","comment":"","purpose":""},"entities":{"components":[{"id":"cmp.root","kind":"assembly","name":"Root","displayName":"Root","cadType":"App::Part","quantity":1,"material":"","identitySource":{"kind":"parent_scoped_path","path":"cmp.root"},"targetability":{"suppress":false,"unsuppress":false,"hide":true,"unhide":true,"delete":false},"annotations":{"description":"","comment":"","purpose":""},"extra":true}],"features":[],"relationships":[],"parameterGroups":[],"parameters":[],"metadata":[]},"structure":{"rootComponentId":"cmp.root","nodes":[{"componentId":"cmp.root","parentComponentId":"","children":[]}]}}`,
			want:    `entities.components[0] has unknown field "extra"`,
		},
		{
			name:    "identitySource",
			payload: `{"schemaVersion":"1.0","captureId":"cap","adapter":{"name":"freecad","version":""},"cadSystem":{"name":"FreeCAD","version":""},"sourceDocument":{"logicalId":"m","path":"input/M.FCStd","fingerprint":"sha256:x"},"rootProduct":{"id":"cmp.root"},"annotations":{"description":"","comment":"","purpose":""},"entities":{"components":[{"id":"cmp.root","kind":"assembly","name":"Root","displayName":"Root","cadType":"App::Part","quantity":1,"material":"","identitySource":{"kind":"parent_scoped_path","path":"cmp.root","extra":true},"targetability":{"suppress":false,"unsuppress":false,"hide":true,"unhide":true,"delete":false},"annotations":{"description":"","comment":"","purpose":""}}],"features":[],"relationships":[],"parameterGroups":[],"parameters":[],"metadata":[]},"structure":{"rootComponentId":"cmp.root","nodes":[{"componentId":"cmp.root","parentComponentId":"","children":[]}]}}`,
			want:    `entities.components[0].identitySource has unknown field "extra"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.payload))
			if err == nil {
				t.Fatal("expected Parse to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestValidationProblemOrderingIsStable(t *testing.T) {
	contract := loadFixture(t, "smoke", "minimal-valid")
	contract.CaptureID = " cap "
	contract.RootProduct.ID = "cmp.missing"
	contract.Structure.RootComponentID = "cmp.other"

	err := Validate(contract)
	if err == nil {
		t.Fatal("expected Validate to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}

	want := []string{
		"captureId must not have leading or trailing whitespace",
		`rootProduct.id "cmp.missing" must resolve to an existing component`,
		`structure.rootComponentId "cmp.other" must resolve to an existing component`,
		"structure.rootComponentId must equal rootProduct.id",
		`structure.nodes[0].componentId "cmp.root" must equal structure.rootComponentId when parentComponentId is empty`,
	}
	if !reflect.DeepEqual(validationErr.Messages(), want) {
		t.Fatalf("unexpected problems\nwant: %#v\ngot:  %#v", want, validationErr.Messages())
	}
}

func TestCanonicalJSONSortsDeclaredDeterministicSections(t *testing.T) {
	contract := loadFixture(t, "smoke", "with-hierarchy")

	data, err := CanonicalJSON(contract)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}

	wantOrder := []string{
		`"id":"cmp.part.leaf","kind":"part"`,
		`"id":"cmp.root","kind":"assembly"`,
		`"id":"cmp.sub","kind":"assembly"`,
	}
	last := -1
	for _, fragment := range wantOrder {
		index := strings.Index(string(data)[last+1:], fragment)
		if index == -1 {
			t.Fatalf("expected canonical JSON to contain %s", fragment)
		}
		last += index + 1
	}
}

func TestReadHelper(t *testing.T) {
	data, err := os.ReadFile(fixturePath(t, "smoke", "minimal-valid"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	parsedFromParse, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	parsedFromRead, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	if !reflect.DeepEqual(parsedFromParse, parsedFromRead) {
		t.Fatalf("Read and Parse returned different values\nparse: %#v\nread:  %#v", parsedFromParse, parsedFromRead)
	}
}

func loadFixture(t *testing.T, category, name string) *CADContract {
	t.Helper()
	contract, err := Load(fixturePath(t, category, name))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	return contract
}

func fixturePath(t *testing.T, category, name string) string {
	t.Helper()
	return filepath.Join("testdata", category, name, "parametron.cad.json")
}
