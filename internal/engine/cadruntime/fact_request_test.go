package cadruntime

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"parametron/internal/engine/recordcontract"
)

// Issue #51: the fact request sidecar is a strict versioned Engine-owned
// contract. Intent is explicit; no binding is inferred from native evidence.

func TestDecodeFactRequestAcceptsVersionedContract(t *testing.T) {
	withContext := factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm")
	withContext.Context = []recordcontract.OccurrenceContextFrame{{
		Parent: recordcontract.OccurrenceEntity{Resource: recordcontract.ReferenceResource{ProjectID: "project-a", Kind: "model", LogicalID: "widget"}, NativeKind: "object", Name: "Assembly"},
		Member: "Link1",
	}}
	request := factTestRequest(factTestBinding("widget", "Body", "Material", recordcontract.FactValueKindString, ""), withContext)
	content, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeFactRequest(content)
	if err != nil {
		t.Fatalf("DecodeFactRequest returned error: %v", err)
	}
	if !reflect.DeepEqual(decoded, *request) {
		t.Fatalf("decoded request = %#v, want %#v", decoded, *request)
	}
}

func TestDecodeFactRequestRejectsInvalidContent(t *testing.T) {
	tests := map[string]string{
		"unknown field":        `{"schemaVersion":"1.0","bindings":[],"unexpected":true}`,
		"trailing JSON":        `{"schemaVersion":"1.0","bindings":[]} {}`,
		"duplicate JSON field": `{"schemaVersion":"1.0","schemaVersion":"1.0","bindings":[]}`,
		"unsupported schema":   `{"schemaVersion":"2.0","bindings":[]}`,
		"no bindings":          `{"schemaVersion":"1.0","bindings":[]}`,
		"null bindings":        `{"schemaVersion":"1.0","bindings":null}`,
		"unsupported kind":     `{"schemaVersion":"1.0","bindings":[{"kind":"physical_mass","subject":{"resource":{"projectId":"project-a","kind":"model","logicalId":"widget"},"nativeKind":"object","name":"Body"},"context":[],"property":"Mass","valueKind":"number","requirement":"optional"}]}`,
		"unit on string value": `{"schemaVersion":"1.0","bindings":[{"kind":"bound_property","subject":{"resource":{"projectId":"project-a","kind":"model","logicalId":"widget"},"nativeKind":"object","name":"Body"},"context":[],"property":"Material","valueKind":"string","unit":"mm","requirement":"optional"}]}`,
		"duplicate binding":    `{"schemaVersion":"1.0","bindings":[{"kind":"bound_property","subject":{"resource":{"projectId":"project-a","kind":"model","logicalId":"widget"},"nativeKind":"object","name":"Body"},"context":[],"property":"Length","valueKind":"number","unit":"mm","requirement":"optional"},{"kind":"bound_property","subject":{"resource":{"projectId":"project-a","kind":"model","logicalId":"widget"},"nativeKind":"object","name":"Body"},"context":[],"property":"Length","valueKind":"number","unit":"cm","requirement":"optional"}]}`,
		"invalid requirement":  `{"schemaVersion":"1.0","bindings":[{"kind":"bound_property","subject":{"resource":{"projectId":"project-a","kind":"model","logicalId":"widget"},"nativeKind":"object","name":"Body"},"context":[],"property":"Length","valueKind":"number","requirement":"mandatory"}]}`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeFactRequest([]byte(content)); !errors.Is(err, recordcontract.ErrInvalidFact) {
				t.Fatalf("DecodeFactRequest error = %v, want ErrInvalidFact", err)
			}
		})
	}
}

func TestCloneFactRequestIsIndependent(t *testing.T) {
	if recordcontract.CloneFactRequest(nil) != nil {
		t.Fatal("nil intent must stay nil")
	}
	original := factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"))
	original.Bindings[0].Context = []recordcontract.OccurrenceContextFrame{}
	clone := recordcontract.CloneFactRequest(original)
	clone.Bindings[0].Subject.Name = "Other"
	clone.Bindings[0].Context = append(clone.Bindings[0].Context, recordcontract.OccurrenceContextFrame{Member: "Link1"})
	if original.Bindings[0].Subject.Name != "Body" || len(original.Bindings[0].Context) != 0 {
		t.Fatalf("clone aliases caller-owned intent: %#v", original.Bindings[0])
	}
}

func TestWriteFactRequestMaterializesExactVersionedIntent(t *testing.T) {
	root := t.TempDir()
	request := factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"))
	if err := writeFactRequest(root, request); err != nil {
		t.Fatalf("writeFactRequest returned error: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, FactRequestFilename))
	if err != nil {
		t.Fatalf("read materialized request: %v", err)
	}
	decoded, err := DecodeFactRequest(content)
	if err != nil {
		t.Fatalf("DecodeFactRequest(materialized) returned error: %v", err)
	}
	if !reflect.DeepEqual(decoded, *request) {
		t.Fatalf("materialized request = %#v, want %#v", decoded, *request)
	}

	// Nil intent removes stale request material instead of leaving stale scope.
	if err := writeFactRequest(root, nil); err != nil {
		t.Fatalf("writeFactRequest(nil) returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, FactRequestFilename)); !os.IsNotExist(err) {
		t.Fatalf("stale request was not removed, stat err = %v", err)
	}

	invalid := factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"))
	invalid.Bindings[0].Kind = "physical_mass"
	if err := writeFactRequest(root, invalid); !errors.Is(err, recordcontract.ErrInvalidFact) {
		t.Fatalf("writeFactRequest(invalid) error = %v, want ErrInvalidFact", err)
	}
	if _, err := os.Stat(filepath.Join(root, FactRequestFilename)); !os.IsNotExist(err) {
		t.Fatalf("invalid request was materialized, stat err = %v", err)
	}
}
