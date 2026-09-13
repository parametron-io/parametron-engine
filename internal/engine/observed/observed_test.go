package observed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const validObservedJSON = `{
  "schemaVersion": "1.0",
  "workingCopy": {
    "path": "/tmp/model.FCStd",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "observation": {
    "parameters": [
      {
        "id": "varset:Dimensions.Length",
        "name": "Length",
        "groupId": "Dimensions",
        "value": 12.5,
        "valueKind": "number"
      },
      {
        "id": "varset:Flags.Enabled",
        "name": "Enabled",
        "groupId": "Flags",
        "value": true,
        "valueKind": "boolean"
      }
    ],
    "metadata": [
      {
        "id": "meta.document",
        "key": "document",
        "ownerId": null,
        "value": "widget",
        "valueKind": "string"
      }
    ],
    "references": [
      {
        "kind": "body",
        "name": "PartDesign::Body"
      }
    ],
    "components": [
      {
        "id": "assembly-root",
        "kind": "assembly",
        "name": "Widget",
        "parentId": null
      },
      {
        "id": "part-leg",
        "kind": "part",
        "name": "Leg",
        "parentId": "assembly-root"
      }
    ]
  }
}`

func TestParse_ValidObserved(t *testing.T) {
	parsed, err := Parse([]byte(validObservedJSON))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if parsed.SchemaVersion != SchemaVersion {
		t.Fatalf("unexpected schema version: %q", parsed.SchemaVersion)
	}
	if parsed.WorkingCopy.Path != "/tmp/model.FCStd" {
		t.Fatalf("unexpected working copy path: %q", parsed.WorkingCopy.Path)
	}
	if parsed.Observation.Parameters[0].ID != "varset:Dimensions.Length" {
		t.Fatalf("unexpected parameter id: %q", parsed.Observation.Parameters[0].ID)
	}
	if parsed.Observation.Parameters[0].GroupID != "Dimensions" {
		t.Fatalf("unexpected parameter group id: %q", parsed.Observation.Parameters[0].GroupID)
	}
	if got := string(parsed.Observation.Parameters[0].Value.Raw()); got != "12.5" {
		t.Fatalf("unexpected parameter value: %q", got)
	}
	if got := string(parsed.Observation.Parameters[1].Value.Raw()); got != "true" {
		t.Fatalf("unexpected boolean value: %q", got)
	}
	if parsed.Observation.Parameters[1].ValueKind != "boolean" {
		t.Fatalf("unexpected parameter value kind: %q", parsed.Observation.Parameters[1].ValueKind)
	}
	if len(parsed.Observation.Metadata) != 1 {
		t.Fatalf("unexpected metadata count: %d", len(parsed.Observation.Metadata))
	}
	if parsed.Observation.Metadata[0].ID != "meta.document" {
		t.Fatalf("unexpected metadata id: %q", parsed.Observation.Metadata[0].ID)
	}
	if parsed.Observation.Metadata[0].OwnerID != "" {
		t.Fatalf("expected metadata ownerId to normalize to empty string, got %q", parsed.Observation.Metadata[0].OwnerID)
	}
	if got := string(parsed.Observation.Metadata[0].Value.Raw()); got != `"widget"` {
		t.Fatalf("unexpected metadata value: %q", got)
	}
	if parsed.Observation.Metadata[0].ValueKind != "string" {
		t.Fatalf("unexpected metadata value kind: %q", parsed.Observation.Metadata[0].ValueKind)
	}
	if len(parsed.Observation.Components) != 2 {
		t.Fatalf("unexpected component count: %d", len(parsed.Observation.Components))
	}
	if parsed.Observation.Components[0].ParentID != "" {
		t.Fatalf("expected root parent to normalize to empty string, got %q", parsed.Observation.Components[0].ParentID)
	}
	if parsed.Observation.Components[1].ParentID != "assembly-root" {
		t.Fatalf("unexpected child parent id: %q", parsed.Observation.Components[1].ParentID)
	}
}

func TestParse_EmptyStringParentIDCanonicalizesAsRoot(t *testing.T) {
	input := []byte(`{
  "schemaVersion": "1.0",
  "workingCopy": {
    "path": "/tmp/model.FCStd",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "observation": {
    "parameters": [],
    "metadata": [],
    "references": [],
    "components": [
      {
        "id": "root",
        "kind": "assembly",
        "name": "Root",
        "parentId": ""
      }
    ]
  }
}`)

	parsed, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if err := Validate(parsed); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if len(parsed.Observation.Components) != 1 {
		t.Fatalf("unexpected component count: %d", len(parsed.Observation.Components))
	}
	if parsed.Observation.Components[0].ParentID != "" {
		t.Fatalf("expected empty-string parentId to normalize to root empty string, got %q", parsed.Observation.Components[0].ParentID)
	}

	first, err := CanonicalJSON(parsed)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	if !bytes.Contains(first, []byte(`"parentId":null`)) {
		t.Fatalf("expected canonical JSON to contain root parentId null, got %s", first)
	}
	if bytes.Contains(first, []byte(`"parentId":""`)) {
		t.Fatalf("expected canonical JSON to omit empty-string parentId, got %s", first)
	}

	reparsed, err := Parse(first)
	if err != nil {
		t.Fatalf("Parse(canonical) returned error: %v", err)
	}
	second, err := CanonicalJSON(reparsed)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("expected canonical JSON to remain stable across repeated canonicalization\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestValidate_IdempotentAndNonMutating(t *testing.T) {
	observed := validObserved()
	before := cloneObserved(observed)

	canonicalBefore, err := CanonicalJSON(observed)
	if err != nil {
		t.Fatalf("CanonicalJSON(before) returned error: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := Validate(observed); err != nil {
			t.Fatalf("Validate call %d returned error: %v", i+1, err)
		}
	}

	if !reflect.DeepEqual(observed, before) {
		t.Fatalf("Validate mutated observed\nbefore: %#v\nafter: %#v", before, observed)
	}

	canonicalAfter, err := CanonicalJSON(observed)
	if err != nil {
		t.Fatalf("CanonicalJSON(after) returned error: %v", err)
	}
	if !bytes.Equal(canonicalBefore, canonicalAfter) {
		t.Fatalf("canonical JSON changed across validation\nbefore: %s\nafter:  %s", canonicalBefore, canonicalAfter)
	}
}

func TestValidate_NilObservedFails(t *testing.T) {
	err := Validate(nil)
	if err == nil {
		t.Fatal("expected Validate to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "observed must not be nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_RequiresArrays(t *testing.T) {
	observed := validObserved()
	observed.Observation.Parameters = nil

	err := Validate(observed)
	if err == nil {
		t.Fatal("expected Validate to fail")
	}
	if !strings.Contains(err.Error(), "observation.parameters is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_RequiresComponentsArray(t *testing.T) {
	observed := validObserved()
	observed.Observation.Components = nil

	err := Validate(observed)
	if err == nil {
		t.Fatal("expected Validate to fail")
	}
	if !strings.Contains(err.Error(), "observation.components is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_RequiresEveryObservationArray(t *testing.T) {
	for _, category := range []string{"parameters", "metadata", "references", "components"} {
		t.Run(category, func(t *testing.T) {
			observed := validObserved()
			switch category {
			case "parameters":
				observed.Observation.Parameters = nil
			case "metadata":
				observed.Observation.Metadata = nil
			case "references":
				observed.Observation.References = nil
			case "components":
				observed.Observation.Components = nil
			}
			err := Validate(observed)
			if err == nil || !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "observation."+category+" is required") {
				t.Fatalf("expected strict nil-slice validation, got %v", err)
			}
		})
	}
}

func TestValidate_RejectsInvalidSHA256(t *testing.T) {
	observed := validObserved()
	observed.WorkingCopy.SHA256 = "ABC"

	err := Validate(observed)
	if err == nil {
		t.Fatal("expected Validate to fail")
	}
	if !strings.Contains(err.Error(), "workingCopy.sha256") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_AllowsEmptyComponents(t *testing.T) {
	observed := validObserved()
	observed.Observation.Components = []Component{}

	if err := Validate(observed); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestValidate_AcceptsSemanticParameterIDs(t *testing.T) {
	observed := validObserved()
	observed.Observation.Parameters = []Parameter{
		{ID: "par.root.length", Name: "Length", GroupID: "Dimensions", Value: mustValue(t, []byte(`12.5`)), ValueKind: "number"},
	}

	if err := Validate(observed); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestValidate_RejectsInvalidComponents(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Observed)
		want   string
	}{
		{
			name: "blank id",
			mutate: func(observed *Observed) {
				observed.Observation.Components[0].ID = ""
			},
			want: `observation.components[0].id is required`,
		},
		{
			name: "whitespace id",
			mutate: func(observed *Observed) {
				observed.Observation.Components[0].ID = "   "
			},
			want: `observation.components[0].id is required`,
		},
		{
			name: "duplicate ids",
			mutate: func(observed *Observed) {
				observed.Observation.Components = []Component{
					{ID: "dup", Kind: ComponentKindAssembly, Name: "Widget"},
					{ID: "dup", Kind: ComponentKindPart, Name: "Leg", ParentID: "dup"},
				}
			},
			want: `observation.components[1].id duplicates observation.components[0].id "dup"`,
		},
		{
			name: "invalid kind",
			mutate: func(observed *Observed) {
				observed.Observation.Components[0].Kind = "feature"
			},
			want: `observation.components[0].kind must be one of "assembly" or "part"`,
		},
		{
			name: "blank name",
			mutate: func(observed *Observed) {
				observed.Observation.Components[0].Name = ""
			},
			want: `observation.components[0].name is required`,
		},
		{
			name: "whitespace name",
			mutate: func(observed *Observed) {
				observed.Observation.Components[0].Name = "   "
			},
			want: `observation.components[0].name is required`,
		},
		{
			name: "missing parent reference",
			mutate: func(observed *Observed) {
				observed.Observation.Components[1].ParentID = "missing"
			},
			want: `observation.components[1].parentId references unknown component "missing"`,
		},
		{
			name: "blank parent id on non-root",
			mutate: func(observed *Observed) {
				observed.Observation.Components[1].ParentID = "   "
			},
			want: `observation.components[1].parentId must not be blank`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observed := validObserved()
			tc.mutate(observed)

			err1 := Validate(observed)
			if err1 == nil {
				t.Fatal("expected first Validate to fail")
			}
			if !errors.Is(err1, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err1)
			}
			if !strings.Contains(err1.Error(), tc.want) {
				t.Fatalf("expected first error containing %q, got %v", tc.want, err1)
			}

			err2 := Validate(observed)
			if err2 == nil {
				t.Fatal("expected second Validate to fail")
			}
			if !errors.Is(err2, ErrValidation) {
				t.Fatalf("expected ErrValidation on second call, got %v", err2)
			}
			if !strings.Contains(err2.Error(), tc.want) {
				t.Fatalf("expected second error containing %q, got %v", tc.want, err2)
			}
			if err1.Error() != err2.Error() {
				t.Fatalf("expected deterministic errors, got %q and %q", err1.Error(), err2.Error())
			}
		})
	}
}

func TestCanonicalJSON_StableOrdering(t *testing.T) {
	value, err := NewValue([]byte(`"alpha"`))
	if err != nil {
		t.Fatalf("NewValue returned error: %v", err)
	}

	observed := &Observed{
		SchemaVersion: SchemaVersion,
		WorkingCopy: WorkingCopy{
			Path:   "/tmp/example.FCStd",
			SHA256: strings.Repeat("1", 64),
		},
		Observation: Observation{
			Parameters: []Parameter{
				{ID: "varset:b.Length", Name: "Length", GroupID: "b", Value: value, ValueKind: "string"},
				{ID: "varset:a.Width", Name: "Width", Value: mustValue(t, []byte(`3`)), ValueKind: "integer"},
			},
			Metadata: []Metadata{{ID: "meta.b", Key: "b", Value: mustValue(t, []byte(`2`)), ValueKind: "integer"}},
			References: []Reference{{
				Kind: "sketch",
				Name: "Sketch001",
			}},
			Components: []Component{
				{ID: "part-leg", Kind: ComponentKindPart, Name: "Leg", ParentID: "assembly-root"},
				{ID: "assembly-root", Kind: ComponentKindAssembly, Name: "Widget"},
			},
		},
	}

	data, err := CanonicalJSON(observed)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}

	want := `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/example.FCStd","sha256":"1111111111111111111111111111111111111111111111111111111111111111"},"observation":{"parameters":[{"id":"varset:a.Width","name":"Width","groupId":null,"value":3,"valueKind":"integer"},{"id":"varset:b.Length","name":"Length","groupId":"b","value":"alpha","valueKind":"string"}],"metadata":[{"id":"meta.b","key":"b","ownerId":null,"value":2,"valueKind":"integer"}],"references":[{"kind":"sketch","name":"Sketch001"}],"components":[{"id":"assembly-root","kind":"assembly","name":"Widget","parentId":null},{"id":"part-leg","kind":"part","name":"Leg","parentId":"assembly-root"}]}}`
	if string(data) != want {
		t.Fatalf("unexpected canonical JSON\nwant: %s\ngot:  %s", want, data)
	}
}

func TestCanonicalJSON_EmitsExactExpectedBytesWithAllSectionsPopulated(t *testing.T) {
	value, err := NewValue([]byte(`12.5`))
	if err != nil {
		t.Fatalf("NewValue returned error: %v", err)
	}

	observed := &Observed{
		SchemaVersion: SchemaVersion,
		WorkingCopy: WorkingCopy{
			Path:   "/tmp/model.FCStd",
			SHA256: strings.Repeat("a", 64),
		},
		Observation: Observation{
			Parameters: []Parameter{
				{
					ID:        "varset:Dimensions.Length",
					Name:      "Length",
					GroupID:   "Dimensions",
					Value:     value,
					ValueKind: "number",
				},
				{
					ID:        "varset:Flags.Enabled",
					Name:      "Enabled",
					Value:     mustValue(t, []byte(`true`)),
					ValueKind: "boolean",
				},
			},
			Metadata: []Metadata{{ID: "meta.document", Key: "document", Value: mustValue(t, []byte(`"widget"`)), ValueKind: "string"}},
			References: []Reference{{
				Kind: "body",
				Name: "Body",
			}},
			Components: []Component{
				{ID: "part-b", Kind: ComponentKindPart, Name: "Bracket", ParentID: "assembly-root"},
				{ID: "assembly-root", Kind: ComponentKindAssembly, Name: "Widget"},
			},
		},
	}

	got, err := CanonicalJSON(observed)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}

	want := []byte(`{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[{"id":"varset:Dimensions.Length","name":"Length","groupId":"Dimensions","value":12.5,"valueKind":"number"},{"id":"varset:Flags.Enabled","name":"Enabled","groupId":null,"value":true,"valueKind":"boolean"}],"metadata":[{"id":"meta.document","key":"document","ownerId":null,"value":"widget","valueKind":"string"}],"references":[{"kind":"body","name":"Body"}],"components":[{"id":"assembly-root","kind":"assembly","name":"Widget","parentId":null},{"id":"part-b","kind":"part","name":"Bracket","parentId":"assembly-root"}]}}`)
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected canonical JSON\nwant: %s\ngot:  %s", want, got)
	}
}

func TestCanonicalJSON_InvalidProgrammaticObservedRejected(t *testing.T) {
	observed := validObserved()
	observed.Observation.Metadata[0].Key = " "

	_, err := CanonicalJSON(observed)
	if err == nil {
		t.Fatal("expected CanonicalJSON to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "observation.metadata[0].key is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParse_ObservedMetadataExpandedValidation(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "missing id",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[{"key":"document","ownerId":null,"value":"widget","valueKind":"string"}],"references":[],"components":[]}}`,
			want:    "observation.metadata[0].id is required",
		},
		{
			name:    "blank id",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[{"id":" ","key":"document","ownerId":null,"value":"widget","valueKind":"string"}],"references":[],"components":[]}}`,
			want:    "observation.metadata[0].id is required",
		},
		{
			name:    "missing key",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[{"id":"meta.document","ownerId":null,"value":"widget","valueKind":"string"}],"references":[],"components":[]}}`,
			want:    "observation.metadata[0].key is required",
		},
		{
			name:    "blank key",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[{"id":"meta.document","key":" ","ownerId":null,"value":"widget","valueKind":"string"}],"references":[],"components":[]}}`,
			want:    "observation.metadata[0].key is required",
		},
		{
			name:    "blank ownerId string",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[{"id":"meta.document","key":"document","ownerId":" ","value":"widget","valueKind":"string"}],"references":[],"components":[]}}`,
			want:    "observation.metadata[0].ownerId must not be blank",
		},
		{
			name:    "null value",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[{"id":"meta.document","key":"document","ownerId":null,"value":null,"valueKind":"string"}],"references":[],"components":[]}}`,
			want:    "observation.metadata[0].value must not be null",
		},
		{
			name:    "object value",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[{"id":"meta.document","key":"document","ownerId":null,"value":{"nested":true},"valueKind":"string"}],"references":[],"components":[]}}`,
			want:    "observation.metadata[0].value must be a JSON scalar",
		},
		{
			name:    "array value",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[{"id":"meta.document","key":"document","ownerId":null,"value":[1],"valueKind":"string"}],"references":[],"components":[]}}`,
			want:    "observation.metadata[0].value must be a JSON scalar",
		},
		{
			name:    "missing valueKind",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[{"id":"meta.document","key":"document","ownerId":null,"value":"widget"}],"references":[],"components":[]}}`,
			want:    "observation.metadata[0].valueKind is required",
		},
		{
			name:    "unsupported valueKind",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[{"id":"meta.document","key":"document","ownerId":null,"value":"widget","valueKind":"decimal"}],"references":[],"components":[]}}`,
			want:    `observation.metadata[0].valueKind must be one of "number", "integer", "string", or "boolean"`,
		},
		{
			name:    "unknown metadata field",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[{"id":"meta.document","key":"document","ownerId":null,"value":"widget","valueKind":"string","extra":true}],"references":[],"components":[]}}`,
			want:    `observation.metadata[0] has unknown field "extra"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.payload))
			if err == nil {
				t.Fatal("expected Parse to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestParse_AcceptsExpandedMetadataEntries(t *testing.T) {
	parsed, err := Parse([]byte(`{
  "schemaVersion": "1.0",
  "workingCopy": {
    "path": "/tmp/model.FCStd",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "observation": {
    "parameters": [],
    "metadata": [
      {
        "id": "meta.bool",
        "key": "enabled",
        "value": true,
        "valueKind": "boolean"
      },
      {
        "id": "meta.number",
        "key": "count",
        "ownerId": "cmp.root",
        "value": 12.5,
        "valueKind": "number"
      },
      {
        "id": "meta.string",
        "key": "label",
        "ownerId": null,
        "value": "widget",
        "valueKind": "string"
      }
    ],
    "references": [],
    "components": []
  }
}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got := string(parsed.Observation.Metadata[0].Value.Raw()); got != "true" {
		t.Fatalf("unexpected boolean metadata value: %q", got)
	}
	if parsed.Observation.Metadata[0].OwnerID != "" {
		t.Fatalf("expected missing ownerId to normalize to empty string, got %q", parsed.Observation.Metadata[0].OwnerID)
	}
	if got := string(parsed.Observation.Metadata[1].Value.Raw()); got != "12.5" {
		t.Fatalf("unexpected number metadata value: %q", got)
	}
	if parsed.Observation.Metadata[1].OwnerID != "cmp.root" {
		t.Fatalf("unexpected ownerId: %q", parsed.Observation.Metadata[1].OwnerID)
	}
	if got := string(parsed.Observation.Metadata[2].Value.Raw()); got != `"widget"` {
		t.Fatalf("unexpected string metadata value: %q", got)
	}
}

func TestCanonicalJSON_MetadataFieldOrderAndDeterministicOrdering(t *testing.T) {
	observed := validObserved()
	observed.Observation.Metadata = []Metadata{
		{ID: "z", Key: "alpha", OwnerID: "cmp.root", Value: mustValue(t, []byte(`true`)), ValueKind: "boolean"},
		{ID: "a", Key: "beta", Value: mustValue(t, []byte(`2`)), ValueKind: "integer"},
		{ID: "a", Key: "beta", OwnerID: "cmp.a", Value: mustValue(t, []byte(`3`)), ValueKind: "integer"},
	}

	data, err := CanonicalJSON(observed)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}

	want := `"metadata":[{"id":"a","key":"beta","ownerId":null,"value":2,"valueKind":"integer"},{"id":"a","key":"beta","ownerId":"cmp.a","value":3,"valueKind":"integer"},{"id":"z","key":"alpha","ownerId":"cmp.root","value":true,"valueKind":"boolean"}]`
	if !bytes.Contains(data, []byte(want)) {
		t.Fatalf("unexpected metadata canonical JSON: %s", data)
	}
}

func TestObservedMetadata_DeterministicOrdering_StableAcrossInputVariants(t *testing.T) {
	baseEntries := []Metadata{
		{ID: "b", Key: "k1", Value: mustValue(t, []byte(`"vb"`)), ValueKind: "string"},
		{ID: "a", Key: "k1", OwnerID: "grp1", Value: mustValue(t, []byte(`"va-grp1"`)), ValueKind: "string"},
		{ID: "c", Key: "k0", Value: mustValue(t, []byte(`"vc"`)), ValueKind: "string"},
		{ID: "a2", Key: "k1", Value: mustValue(t, []byte(`"va2"`)), ValueKind: "string"},
		{ID: "a", Key: "k1", Value: mustValue(t, []byte(`"va-null"`)), ValueKind: "string"},
	}

	variants := []struct {
		name    string
		entries []Metadata
	}{
		{
			name:    "original",
			entries: append([]Metadata(nil), baseEntries...),
		},
		{
			name: "reversed",
			entries: []Metadata{
				baseEntries[4],
				baseEntries[3],
				baseEntries[2],
				baseEntries[1],
				baseEntries[0],
			},
		},
		{
			name: "shuffled",
			entries: []Metadata{
				baseEntries[2],
				baseEntries[0],
				baseEntries[4],
				baseEntries[3],
				baseEntries[1],
			},
		},
	}

	var canonicalOutputs [][]byte
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			observed := validObserved()
			observed.Observation.Metadata = append([]Metadata(nil), variant.entries...)

			first, err := CanonicalJSON(observed)
			if err != nil {
				t.Fatalf("CanonicalJSON returned error: %v", err)
			}
			for i := 0; i < 10; i++ {
				next, err := CanonicalJSON(observed)
				if err != nil {
					t.Fatalf("CanonicalJSON repeat %d returned error: %v", i+1, err)
				}
				if !bytes.Equal(first, next) {
					t.Fatalf("expected repeated canonicalization to remain byte-stable\nfirst: %s\nnext:  %s", first, next)
				}
			}
			canonicalOutputs = append(canonicalOutputs, first)
		})
	}

	for i := 1; i < len(canonicalOutputs); i++ {
		if !bytes.Equal(canonicalOutputs[0], canonicalOutputs[i]) {
			t.Fatalf("expected canonical outputs to be identical across variants\nfirst:  %s\nother:  %s", canonicalOutputs[0], canonicalOutputs[i])
		}
	}

	parsed, err := Parse(canonicalOutputs[0])
	if err != nil {
		t.Fatalf("Parse(canonical) returned error: %v", err)
	}

	if len(parsed.Observation.Metadata) != 5 {
		t.Fatalf("unexpected metadata count: %d", len(parsed.Observation.Metadata))
	}

	expectedOrder := []struct {
		id      string
		key     string
		ownerID string
	}{
		{id: "a", key: "k1", ownerID: ""},
		{id: "a", key: "k1", ownerID: "grp1"},
		{id: "a2", key: "k1", ownerID: ""},
		{id: "b", key: "k1", ownerID: ""},
		{id: "c", key: "k0", ownerID: ""},
	}

	for i, want := range expectedOrder {
		got := parsed.Observation.Metadata[i]
		if got.ID != want.id || got.Key != want.key || got.OwnerID != want.ownerID {
			t.Fatalf("unexpected metadata order at index %d: got {id:%q key:%q ownerId:%q} want {id:%q key:%q ownerId:%q}",
				i, got.ID, got.Key, got.OwnerID, want.id, want.key, want.ownerID)
		}
	}
}

func TestParse_RejectsNonObjectVerificationStylePayloads(t *testing.T) {
	_, err := Parse([]byte(`[]`))
	if err == nil {
		t.Fatal("expected Parse to fail")
	}
	if !strings.Contains(err.Error(), "root must be a JSON object") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParse_RejectsUnknownField(t *testing.T) {
	_, err := Parse([]byte(`{
  "schemaVersion": "1.0",
  "workingCopy": {
    "path": "/tmp/model.FCStd",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "observation": {
    "parameters": [],
    "metadata": [],
    "references": [],
    "components": []
  },
  "extra": true
}`))
	if err == nil {
		t.Fatal("expected Parse to fail")
	}
	if !strings.Contains(err.Error(), `root has unknown field "extra"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParse_RejectsMissingRequiredFields(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "missing schemaVersion",
			payload: `{"workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "schemaVersion is required",
		},
		{
			name:    "missing workingCopy",
			payload: `{"schemaVersion":"1.0","observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "workingCopy is required",
		},
		{
			name:    "missing observation",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`,
			want:    "observation is required",
		},
		{
			name:    "missing workingCopy.path",
			payload: `{"schemaVersion":"1.0","workingCopy":{"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "workingCopy.path is required",
		},
		{
			name:    "missing workingCopy.sha256",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "workingCopy.sha256 is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.payload))
			if err == nil {
				t.Fatal("expected Parse to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestParse_RejectsWrongTypesForRequiredFields(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "non-string schemaVersion",
			payload: `{"schemaVersion":1,"workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "schemaVersion must be a string",
		},
		{
			name:    "non-object workingCopy",
			payload: `{"schemaVersion":"1.0","workingCopy":[],"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "workingCopy must be an object",
		},
		{
			name:    "non-object observation",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":[]}`,
			want:    "observation must be an object",
		},
		{
			name:    "non-string workingCopy.path",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":true,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "workingCopy.path must be a string",
		},
		{
			name:    "non-string workingCopy.sha256",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":true},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "workingCopy.sha256 must be a string",
		},
		{
			name:    "non-array observation.parameters",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":{},"metadata":[],"references":[],"components":[]}}`,
			want:    "observation.parameters must be an array",
		},
		{
			name:    "non-array observation.metadata",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":{},"references":[],"components":[]}}`,
			want:    "observation.metadata must be an array",
		},
		{
			name:    "non-array observation.references",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":{},"components":[]}}`,
			want:    "observation.references must be an array",
		},
		{
			name:    "non-array observation.components",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[],"components":{}}}`,
			want:    "observation.components must be an array",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.payload))
			if err == nil {
				t.Fatal("expected Parse to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestParse_AcceptsEmptyObservationObject(t *testing.T) {
	got, err := Parse([]byte(`{
  "schemaVersion": "1.0",
  "workingCopy": {
    "path": "/tmp/model.FCStd",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "observation": {}
}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	assertEmptyNonNilObservation(t, got.Observation)
	if err := Validate(got); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestParse_AcceptsParametersOnlySparseObservation(t *testing.T) {
	got, err := Parse(sparseObservedJSON(`{"parameters":[{"id":"p.count","name":"Count","groupId":"Spreadsheet","value":5,"valueKind":"integer"}]}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(got.Observation.Parameters) != 1 || got.Observation.Parameters[0].ID != "p.count" || got.Observation.Parameters[0].Name != "Count" || got.Observation.Parameters[0].GroupID != "Spreadsheet" || got.Observation.Parameters[0].ValueKind != "integer" || string(got.Observation.Parameters[0].Value.Raw()) != "5" {
		t.Fatalf("unexpected parameter: %#v", got.Observation.Parameters)
	}
	assertEmptyNonNilSlices(t, got.Observation.Metadata, got.Observation.References, got.Observation.Components)
}

func TestParse_AcceptsMetadataAndReferencesOnlySparseObservation(t *testing.T) {
	got, err := Parse(sparseObservedJSON(`{"metadata":[{"id":"m.author","key":"Author","ownerId":"Doc","value":"Jane","valueKind":"string"}],"references":[{"kind":"constraint","name":"Sketch001"}]}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got.Observation.Parameters == nil || len(got.Observation.Parameters) != 0 || got.Observation.Components == nil || len(got.Observation.Components) != 0 {
		t.Fatalf("omitted categories were not normalized: %#v", got.Observation)
	}
	if len(got.Observation.Metadata) != 1 || got.Observation.Metadata[0].ID != "m.author" || got.Observation.Metadata[0].OwnerID != "Doc" || string(got.Observation.Metadata[0].Value.Raw()) != `"Jane"` {
		t.Fatalf("unexpected metadata: %#v", got.Observation.Metadata)
	}
	if len(got.Observation.References) != 1 || got.Observation.References[0] != (Reference{Kind: "constraint", Name: "Sketch001"}) {
		t.Fatalf("unexpected references: %#v", got.Observation.References)
	}
}

func TestParse_NormalizesEachOmittedKnownCategory(t *testing.T) {
	categories := []string{"parameters", "metadata", "references", "components"}
	for _, omitted := range categories {
		t.Run(omitted, func(t *testing.T) {
			var root map[string]any
			if err := json.Unmarshal([]byte(validObservedJSON), &root); err != nil {
				t.Fatal(err)
			}
			delete(root["observation"].(map[string]any), omitted)
			payload, err := json.Marshal(root)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Parse(payload)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			lengths := map[string]int{"parameters": len(got.Observation.Parameters), "metadata": len(got.Observation.Metadata), "references": len(got.Observation.References), "components": len(got.Observation.Components)}
			nonNil := map[string]bool{"parameters": got.Observation.Parameters != nil, "metadata": got.Observation.Metadata != nil, "references": got.Observation.References != nil, "components": got.Observation.Components != nil}
			if !nonNil[omitted] || lengths[omitted] != 0 {
				t.Fatalf("omitted %s was not normalized", omitted)
			}
			wantLengths := map[string]int{"parameters": 2, "metadata": 1, "references": 1, "components": 2}
			for _, category := range categories {
				if category != omitted && lengths[category] != wantLengths[category] {
					t.Fatalf("provided %s entries were not retained: got %d want %d", category, lengths[category], wantLengths[category])
				}
			}
		})
	}
}

func TestCanonicalJSON_ExpandsSparseObservationToAllCategories(t *testing.T) {
	got, err := Parse(sparseObservedJSON(`{"parameters":[{"id":"p.count","name":"Count","groupId":"Spreadsheet","value":5,"valueKind":"integer"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := CanonicalJSON(got)
	if err != nil {
		t.Fatal(err)
	}
	wantObservation := `"observation":{"parameters":[{"id":"p.count","name":"Count","groupId":"Spreadsheet","value":5,"valueKind":"integer"}],"metadata":[],"references":[],"components":[]}`
	if !bytes.Contains(canonical, []byte(wantObservation)) {
		t.Fatalf("unexpected canonical category completion or ordering: %s", canonical)
	}
	if !json.Valid(canonical) {
		t.Fatalf("canonical output is invalid JSON: %s", canonical)
	}
	reparsed, err := Parse(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, reparsed) {
		t.Fatalf("normalized value changed across canonical round trip\nbefore: %#v\nafter: %#v", got, reparsed)
	}
}

func TestCanonicalJSON_SparseObservationIsStableAcrossRoundTrip(t *testing.T) {
	parsed, err := Parse(sparseObservedJSON(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	first, err := CanonicalJSON(parsed)
	if err != nil {
		t.Fatal(err)
	}
	reparsed, err := Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalJSON(reparsed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical output changed across round trip\nfirst: %s\nsecond: %s", first, second)
	}
}

func TestParse_RejectsExplicitNullObservationCategories(t *testing.T) {
	for _, category := range []string{"parameters", "metadata", "references", "components"} {
		t.Run(category, func(t *testing.T) {
			_, err := Parse(sparseObservedJSON(`{"` + category + `":null}`))
			if err == nil || !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "observation."+category+" must be an array") {
				t.Fatalf("expected field-specific ErrValidation, got %v", err)
			}
		})
	}
}

func TestParse_RejectsNonArrayObservationCategories(t *testing.T) {
	values := []string{`{}`, `"invalid"`, `1`, `true`}
	for _, category := range []string{"parameters", "metadata", "references", "components"} {
		for _, value := range values {
			t.Run(category+"_"+value, func(t *testing.T) {
				_, err := Parse(sparseObservedJSON(`{"` + category + `":` + value + `}`))
				if err == nil || !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "observation."+category+" must be an array") {
					t.Fatalf("expected field-specific ErrValidation, got %v", err)
				}
			})
		}
	}
}

func TestParse_AcceptsCurrentAlignedFreeCADSparseObservedPayload(t *testing.T) {
	got, err := Parse([]byte(`{"schemaVersion":"1.0","workingCopy":{"path":"/work/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[{"id":"p.count","name":"Count","groupId":"Spreadsheet","value":5,"valueKind":"integer"}]}}`))
	if err != nil {
		t.Fatalf("Parse returned error for current aligned FreeCAD payload: %v", err)
	}
	if len(got.Observation.Parameters) != 1 || got.Observation.Parameters[0].ID != "p.count" {
		t.Fatalf("unexpected aligned parameter: %#v", got.Observation.Parameters)
	}
	assertEmptyNonNilSlices(t, got.Observation.Metadata, got.Observation.References, got.Observation.Components)
}

func TestParse_RejectsMalformedArrayEntries(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "parameter entry not object",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[true],"metadata":[],"references":[],"components":[]}}`,
			want:    "observation.parameters[0] must be an object",
		},
		{
			name:    "metadata entry missing key",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[{"id":"meta.document","value":"widget","valueKind":"string"}],"references":[],"components":[]}}`,
			want:    "observation.metadata[0].key is required",
		},
		{
			name:    "reference entry wrong name type",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[{"kind":"body","name":false}],"components":[]}}`,
			want:    "observation.references[0].name must be a string",
		},
		{
			name:    "component entry wrong parent type",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[{"id":"root","kind":"assembly","name":"Widget","parentId":false}]}}`,
			want:    "observation.components[0].parentId must be a string or null",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.payload))
			if err == nil {
				t.Fatal("expected Parse to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestParse_RejectsUnknownFieldsAtStrictBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "root",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]},"extra":true}`,
			want:    `root has unknown field "extra"`,
		},
		{
			name:    "workingCopy",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","extra":true},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    `workingCopy has unknown field "extra"`,
		},
		{
			name:    "observation",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"geometry":[]}}`,
			want:    `observation has unknown field "geometry"`,
		},
		{
			name:    "parameter entry",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[{"id":"varset:Dimensions.Length","name":"Length","value":1,"valueKind":"number","extra":true}],"metadata":[],"references":[],"components":[]}}`,
			want:    `observation.parameters[0] has unknown field "extra"`,
		},
		{
			name:    "component entry",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[{"id":"root","kind":"assembly","name":"Widget","parentId":null,"extra":true}]}}`,
			want:    `observation.components[0] has unknown field "extra"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.payload))
			if err == nil {
				t.Fatal("expected Parse to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestParse_VersionModel(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "missing version",
			payload: `{"workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "schemaVersion is required",
		},
		{
			name:    "non-string version",
			payload: `{"schemaVersion":1,"workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "schemaVersion must be a string",
		},
		{
			name:    "unsupported version",
			payload: `{"schemaVersion":"1","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    `schemaVersion must be "1.0"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.payload))
			if err == nil {
				t.Fatal("expected Parse to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestParse_ObservedParametersValidation(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name: "missing id",
			payload: `{
  "schemaVersion": "1.0",
  "workingCopy": {
    "path": "/tmp/model.FCStd",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "observation": {
    "parameters": [
      {
        "name": "Length",
        "groupId": "Dimensions",
        "value": 12.5,
        "valueKind": "number"
      }
    ],
    "metadata": [],
    "references": [],
    "components": []
  }
}`,
			want: "observation.parameters[0].id is required",
		},
		{
			name:    "empty name",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[{"id":"varset:Dimensions.Length","name":"","groupId":"Dimensions","value":12.5,"valueKind":"number"}],"metadata":[],"references":[],"components":[]}}`,
			want:    "observation.parameters[0].name is required",
		},
		{
			name:    "unsupported valueKind",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[{"id":"varset:Dimensions.Length","name":"Length","groupId":"Dimensions","value":12.5,"valueKind":"decimal"}],"metadata":[],"references":[],"components":[]}}`,
			want:    `observation.parameters[0].valueKind must be one of "number", "integer", "string", or "boolean"`,
		},
		{
			name:    "null value",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[{"id":"varset:Dimensions.Length","name":"Length","groupId":"Dimensions","value":null,"valueKind":"number"}],"metadata":[],"references":[],"components":[]}}`,
			want:    "observation.parameters[0].value must not be null",
		},
		{
			name:    "object value",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[{"id":"varset:Dimensions.Length","name":"Length","groupId":"Dimensions","value":{"nested":true},"valueKind":"number"}],"metadata":[],"references":[],"components":[]}}`,
			want:    "observation.parameters[0].value must be a JSON scalar",
		},
		{
			name:    "array value",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[{"id":"varset:Dimensions.Length","name":"Length","groupId":"Dimensions","value":[1],"valueKind":"number"}],"metadata":[],"references":[],"components":[]}}`,
			want:    "observation.parameters[0].value must be a JSON scalar",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.payload))
			if err == nil {
				t.Fatal("expected Parse to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestParse_RejectsInvalidWorkingCopyFields(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "non absolute path",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"relative.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "workingCopy.path must be an absolute path",
		},
		{
			name:    "uppercase sha256",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "workingCopy.sha256 must be a lowercase 64-character hex string",
		},
		{
			name:    "short sha256",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"abc"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "workingCopy.sha256 must be a lowercase 64-character hex string",
		},
		{
			name:    "non hex sha256",
			payload: `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`,
			want:    "workingCopy.sha256 must be a lowercase 64-character hex string",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.payload))
			if err == nil {
				t.Fatal("expected Parse to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLoadFileAndWriteFile_RoundTrip(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "parametron.observed.json")
	observed := validObserved()

	if err := WriteFile(path, observed); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("expected trailing newline, got %q", data)
	}

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if !reflect.DeepEqual(loaded, observed) {
		t.Fatalf("loaded observed mismatch\nwant: %#v\ngot:  %#v", observed, loaded)
	}
}

func TestWriteFile_WritesCanonicalBytesWithSingleTrailingNewline(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "parametron.observed.json")
	observed := validObserved()

	canonical, err := CanonicalJSON(observed)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}

	if err := WriteFile(path, observed); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read observed file: %v", err)
	}

	want := append(append([]byte(nil), canonical...), '\n')
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected written bytes\nwant: %q\ngot:  %q", want, got)
	}
	if bytes.HasSuffix(got, []byte("\n\n")) {
		t.Fatalf("expected exactly one trailing newline, got %q", got)
	}
}

func TestWriteFile_RepeatedIdenticalWritesPreserveByteIdentity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "parametron.observed.json")
	observed := validObserved()

	if err := os.WriteFile(path, []byte("stale contents without newline"), 0o644); err != nil {
		t.Fatalf("failed to seed observed file: %v", err)
	}

	if err := WriteFile(path, observed); err != nil {
		t.Fatalf("first WriteFile returned error: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read first observed file: %v", err)
	}

	if err := WriteFile(path, observed); err != nil {
		t.Fatalf("second WriteFile returned error: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read second observed file: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("expected repeated writes to preserve byte identity\nfirst:  %q\nsecond: %q", first, second)
	}
	if bytes.HasSuffix(second, []byte("\n\n")) {
		t.Fatalf("expected exactly one trailing newline after overwrite, got %q", second)
	}
}

func TestCanonicalJSON_RoundTripStable(t *testing.T) {
	parsed, err := Parse([]byte(validObservedJSON))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	first, err := CanonicalJSON(parsed)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}

	reparsed, err := Parse(first)
	if err != nil {
		t.Fatalf("Parse(canonical) returned error: %v", err)
	}

	second, err := CanonicalJSON(reparsed)
	if err != nil {
		t.Fatalf("CanonicalJSON(reparsed) returned error: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("canonical JSON changed across round-trip\nfirst:  %s\nsecond: %s", first, second)
	}
	if !reflect.DeepEqual(parsed, reparsed) {
		t.Fatalf("parsed observed changed across round-trip\nbefore: %#v\nafter:  %#v", parsed, reparsed)
	}
}

func TestCanonicalJSON_NonMutating(t *testing.T) {
	observed := validObserved()
	before := cloneObserved(observed)

	first, err := CanonicalJSON(observed)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}
	second, err := CanonicalJSON(observed)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}

	if !reflect.DeepEqual(observed, before) {
		t.Fatalf("CanonicalJSON mutated observed\nbefore: %#v\nafter: %#v", before, observed)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical JSON changed across repeated calls\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestCanonicalJSON_RepeatedParseAndCanonicalizePreservesByteIdentity(t *testing.T) {
	original := validObserved()

	first, err := CanonicalJSON(original)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	reparsed, err := Parse(first)
	if err != nil {
		t.Fatalf("Parse(first) returned error: %v", err)
	}
	second, err := CanonicalJSON(reparsed)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}
	reparsedAgain, err := Parse(second)
	if err != nil {
		t.Fatalf("Parse(second) returned error: %v", err)
	}
	third, err := CanonicalJSON(reparsedAgain)
	if err != nil {
		t.Fatalf("CanonicalJSON(third) returned error: %v", err)
	}

	if !bytes.Equal(first, second) || !bytes.Equal(second, third) {
		t.Fatalf("expected canonical bytes to remain identical across repeated canonicalization\nfirst:  %s\nsecond: %s\nthird:  %s", first, second, third)
	}

	root := t.TempDir()
	path := filepath.Join(root, "parametron.observed.json")
	if err := WriteFile(path, reparsedAgain); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read observed file: %v", err)
	}
	if !bytes.Equal(written, append(append([]byte(nil), third...), '\n')) {
		t.Fatalf("expected WriteFile bytes to match canonical bytes plus single newline\ncanonical: %q\nwritten:   %q", third, written)
	}
	if bytes.HasSuffix(written, []byte("\n\n")) {
		t.Fatalf("expected exactly one trailing newline, got %q", written)
	}
}

func TestLoadFile_MissingFileClassified(t *testing.T) {
	_, err := LoadFile(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected LoadFile to fail")
	}
	if !errors.Is(err, ErrIO) {
		t.Fatalf("expected ErrIO, got %v", err)
	}
}

func TestValidate_AcceptsSelfReferentialParentID(t *testing.T) {
	// self-referential parentId is accepted — documents current behavior, not a bug
	observed := validObserved()
	observed.Observation.Components = []Component{
		{ID: "x", Kind: ComponentKindAssembly, Name: "Self", ParentID: "x"},
	}

	if err := Validate(observed); err != nil {
		t.Fatalf("Validate returned error for self-referential parentId: %v", err)
	}
}

func TestValidate_AcceptsSimpleCycle(t *testing.T) {
	// two-node cycle (A → B → A) is accepted — documents current behavior, not a bug
	observed := validObserved()
	observed.Observation.Components = []Component{
		{ID: "a", Kind: ComponentKindAssembly, Name: "A", ParentID: "b"},
		{ID: "b", Kind: ComponentKindAssembly, Name: "B", ParentID: "a"},
	}

	if err := Validate(observed); err != nil {
		t.Fatalf("Validate returned error for simple cycle: %v", err)
	}
}

func TestValidate_CyclicComponentsDeterministic(t *testing.T) {
	// repeated Validate calls on cyclic components must return no error consistently
	observed := validObserved()
	observed.Observation.Components = []Component{
		{ID: "a", Kind: ComponentKindAssembly, Name: "A", ParentID: "b"},
		{ID: "b", Kind: ComponentKindAssembly, Name: "B", ParentID: "a"},
	}

	for i := 0; i < 3; i++ {
		if err := Validate(observed); err != nil {
			t.Fatalf("Validate call %d returned error for cyclic components: %v", i+1, err)
		}
	}
}

func validObserved() *Observed {
	hash := sha256.Sum256([]byte("working copy bytes"))
	value, _ := NewValue([]byte(`42`))
	return &Observed{
		SchemaVersion: SchemaVersion,
		WorkingCopy: WorkingCopy{
			Path:   "/tmp/widget.FCStd",
			SHA256: hex.EncodeToString(hash[:]),
		},
		Observation: Observation{
			Parameters: []Parameter{{
				ID:        "varset:Dimensions.Length",
				Name:      "Length",
				GroupID:   "Dimensions",
				Value:     value,
				ValueKind: "integer",
			}},
			Metadata: []Metadata{{
				ID:        "meta.document",
				Key:       "document",
				Value:     valueFromRaw(`"widget"`),
				ValueKind: "string",
			}},
			References: []Reference{{
				Kind: "body",
				Name: "Body",
			}},
			Components: []Component{
				{ID: "assembly-root", Kind: ComponentKindAssembly, Name: "Widget"},
				{ID: "part-body", Kind: ComponentKindPart, Name: "Body", ParentID: "assembly-root"},
			},
		},
	}
}

func mustValue(t *testing.T, raw []byte) Value {
	t.Helper()
	value, err := NewValue(raw)
	if err != nil {
		t.Fatalf("NewValue returned error: %v", err)
	}
	return value
}

func cloneObserved(in *Observed) *Observed {
	if in == nil {
		return nil
	}
	out := *in
	out.Observation.Parameters = append([]Parameter(nil), in.Observation.Parameters...)
	for i := range out.Observation.Parameters {
		out.Observation.Parameters[i].Value = Value{
			raw: append([]byte(nil), in.Observation.Parameters[i].Value.raw...),
			set: in.Observation.Parameters[i].Value.set,
		}
	}
	out.Observation.Metadata = append([]Metadata(nil), in.Observation.Metadata...)
	for i := range out.Observation.Metadata {
		out.Observation.Metadata[i].Value = Value{
			raw: append([]byte(nil), in.Observation.Metadata[i].Value.raw...),
			set: in.Observation.Metadata[i].Value.set,
		}
	}
	out.Observation.References = append([]Reference(nil), in.Observation.References...)
	out.Observation.Components = append([]Component(nil), in.Observation.Components...)
	return &out
}

func sparseObservedJSON(observation string) []byte {
	return []byte(`{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":` + observation + `}`)
}

func assertEmptyNonNilObservation(t *testing.T, observation Observation) {
	t.Helper()
	assertEmptyNonNilSlices(t, observation.Parameters, observation.Metadata, observation.References, observation.Components)
}

func assertEmptyNonNilSlices(t *testing.T, slices ...any) {
	t.Helper()
	for i, slice := range slices {
		value := reflect.ValueOf(slice)
		if value.IsNil() || value.Len() != 0 {
			t.Fatalf("slice %d is not a non-nil empty slice: %#v", i, slice)
		}
	}
}

func valueFromRaw(raw string) Value {
	value, _ := NewValue([]byte(raw))
	return value
}
