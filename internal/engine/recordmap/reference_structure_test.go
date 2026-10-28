package recordmap_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/resourcebinding"
)

// The inventory deliberately aliases source coordinates while preserving distinct
// logical identities and materialized coordinates. No native discovery is used.
func qualifiedTraversal(t *testing.T, prefix string) recordmap.ReferenceTraversalMappingInput {
	t.Helper()
	input := validReferenceTraversalInput()
	bindings := []resourcebinding.Binding{
		{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: "a", SourceCoordinate: "models/shared.FCStd", MaterializedCoordinate: prefix + "source/main.FCStd"},
		{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: "b", SourceCoordinate: "models/shared.FCStd", MaterializedCoordinate: prefix + "resources/b.FCStd"},
		{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: "c", SourceCoordinate: "models/c.FCStd", MaterializedCoordinate: prefix + "resources/c.FCStd"},
		{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: "unused", SourceCoordinate: "models/unused.FCStd", MaterializedCoordinate: prefix + "resources/unused.FCStd"},
	}
	var err error
	input.Resources, err = resourcebinding.New(bindings)
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedSourceDocument = bindings[0].MaterializedCoordinate
	input.Traversal.SourceDocument = input.ExpectedSourceDocument
	input.Traversal.Nodes = []recordmap.ReferenceTraversalRuntimeNode{
		referenceTraversalDocumentNode("doc", bindings[0].MaterializedCoordinate),
		referenceTraversalObjectNode("source", bindings[0].MaterializedCoordinate, "Part"),
		referenceTraversalObjectNode("b", bindings[1].MaterializedCoordinate, "Part"),
		referenceTraversalObjectNode("c", bindings[2].MaterializedCoordinate, "Part"),
	}
	input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
		referenceTraversalEdge("source", "c", "external_document_reference", "resolved"),
		referenceTraversalEdge("source", "b", "external_document_reference", "resolved"),
	}
	return input
}

func relationshipKeys(t *testing.T, record recordcontract.ReferenceRecord) []string {
	t.Helper()
	keys := []string{}
	for _, edge := range record.Reference.Edges {
		key, err := recordcontract.ReferenceRelationshipKey(edge)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	return keys
}

func TestReferenceStructureResourceJoinsAndRelocation(t *testing.T) {
	first := qualifiedTraversal(t, "")
	second := qualifiedTraversal(t, "relocated/")
	// Reverse inventory declaration order as well as native enumeration order.
	bindings := second.Resources.Bindings()
	slices.Reverse(bindings)
	var err error
	second.Resources, err = resourcebinding.New(bindings)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(second.Traversal.Nodes)
	slices.Reverse(second.Traversal.Edges)
	a, b := mapReferenceTraversalRecord(t, first), mapReferenceTraversalRecord(t, second)
	keys := relationshipKeys(t, a)
	if !reflect.DeepEqual(keys, relationshipKeys(t, b)) || len(keys) != 2 || keys[0] == keys[1] || !slices.IsSorted(keys) {
		t.Fatalf("unstable or collapsed relationships: %v", keys)
	}
	for _, edge := range a.Reference.Edges {
		if edge.Source.Resource == nil || edge.Target.Resource == nil {
			t.Fatal("lost resource join")
		}
		if *edge.Source.Resource != (recordcontract.ReferenceResource{ProjectID: "project-a", Kind: resourcebinding.ResourceKindModel, LogicalID: "a"}) {
			t.Fatalf("source: %+v", edge.Source)
		}
		if edge.Target.Resource.LogicalID != "b" && edge.Target.Resource.LogicalID != "c" {
			t.Fatal("invented target resource")
		}
		if edge.Source.Name != "Part" || edge.Target.Name != "Part" || edge.Source.NativeKind != "object" || edge.Source.Path != "" || edge.Target.Path != "" {
			t.Fatal("native identity or path qualification lost")
		}
	}
	if _, err := first.Resources.LookupCoordinate("models/shared.FCStd", false); err == nil {
		t.Fatal("aliased source coordinate arbitrarily selected")
	}
	if len(first.Resources.Bindings()) != 4 {
		t.Fatal("zero-reference resource disappeared")
	}
	if a.Reference.Source.Resource.LogicalID != "a" || a.Reference.Completeness != "complete" {
		t.Fatal("selected source or scope lost")
	}
}

func TestReferenceStructureDiscriminatorsIndependently(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*recordmap.ReferenceTraversalRuntimeEdge)
	}{
		{"property only", func(e *recordmap.ReferenceTraversalRuntimeEdge) { e.SourceProperty = strPtr("Link") }},
		{"mechanism only", func(e *recordmap.ReferenceTraversalRuntimeEdge) { e.ReferenceMechanism = strPtr("App::PropertyLink") }},
		{"null property", func(e *recordmap.ReferenceTraversalRuntimeEdge) { e.SourceProperty = nil }},
		{"null mechanism", func(e *recordmap.ReferenceTraversalRuntimeEdge) { e.ReferenceMechanism = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := validReferenceTraversalInput()
			original := input.Traversal.Edges[0]
			other := original
			tc.change(&other)
			input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{original, other, original}
			record := mapReferenceTraversalRecord(t, input)
			keys := relationshipKeys(t, record)
			if len(keys) != 2 || keys[0] == keys[1] {
				t.Fatal("distinct relationship discriminators collapsed or duplicates became quantity")
			}
			encoded, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			var roundtrip recordcontract.ReferenceRecord
			if err = json.Unmarshal(encoded, &roundtrip); err != nil {
				t.Fatal(err)
			}
			if err = recordcontract.ValidateReferenceRecord(roundtrip); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(record.Reference, roundtrip.Reference) {
				t.Fatal("discriminators lost in serialization")
			}
			for _, e := range record.Reference.Edges {
				if e.Role != "" {
					t.Fatal("fabricated role")
				}
			}
			// Check actual serialized keys recursively, so a future quantity/occurrence
			// field cannot slip through on a nested endpoint or summary.
			var tree any
			if err = json.Unmarshal(encoded, &tree); err != nil {
				t.Fatal(err)
			}
			var check func(any)
			check = func(v any) {
				switch x := v.(type) {
				case map[string]any:
					for k, v := range x {
						lower := strings.ToLower(k)
						if strings.Contains(lower, "occurrence") || strings.Contains(lower, "quantity") || strings.Contains(lower, "cardinality") || strings.Contains(lower, "bom") || lower == "parent" || lower == "children" {
							t.Fatalf("generic reference gained instance semantics: %s", k)
						}
						check(v)
					}
				case []any:
					for _, v := range x {
						check(v)
					}
				}
			}
			check(tree)
		})
	}
}

func TestReferenceStructureOptionAIdentity(t *testing.T) {
	input := qualifiedTraversal(t, "")
	// Add independent discriminators to ensure semantic ordering survives as well.
	other := input.Traversal.Edges[0]
	other.SourceProperty = strPtr("Link")
	input.Traversal.Edges = append(input.Traversal.Edges, other)
	rawA, err := json.Marshal(input.Traversal)
	if err != nil {
		t.Fatal(err)
	}
	input.EvidenceDigestSHA256 = fmt.Sprintf("%x", sha256.Sum256(rawA))
	a := mapReferenceTraversalRecord(t, input)
	slices.Reverse(input.Traversal.Edges)
	slices.Reverse(input.Traversal.Nodes)
	ids := map[string]string{}
	for n := range input.Traversal.Nodes {
		node := &input.Traversal.Nodes[n]
		ids[node.ID] = fmt.Sprintf("new-%d", n)
		node.ID = ids[node.ID]
		node.Sequence = 100 + n
	}
	for n := range input.Traversal.Edges {
		e := &input.Traversal.Edges[n]
		e.Source = ids[e.Source]
		e.Target = ids[e.Target]
		e.Sequence = 200 + n
	}
	rawB, err := json.MarshalIndent(input.Traversal, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	input.EvidenceDigestSHA256 = fmt.Sprintf("%x", sha256.Sum256(rawB))
	b := mapReferenceTraversalRecord(t, input)
	if !reflect.DeepEqual(relationshipKeys(t, a), relationshipKeys(t, b)) {
		t.Fatal("raw order, sequence or node IDs changed relationship keys/order")
	}
	if a.Identity == b.Identity {
		t.Fatal("record identity stopped reflecting exact evidence provenance")
	}
	for _, item := range []struct {
		record recordcontract.ReferenceRecord
		raw    []byte
	}{{a, rawA}, {b, rawB}} {
		digest := fmt.Sprintf("%x", sha256.Sum256(item.raw))
		if !hasReferenceTraversalEvidence(item.record.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef(), digest) {
			t.Fatal("raw digest provenance lost")
		}
		for _, e := range item.record.Reference.Edges {
			if e.Evidence.DigestSHA256 != digest || e.SourceProperty == nil || e.ReferenceMechanism == nil || e.Target.Resource == nil {
				t.Fatal("evidence or resource/discriminator loss")
			}
		}
	}
	if a.Provenance.Evidence[0].DigestSHA256 == b.Provenance.Evidence[0].DigestSHA256 {
		t.Fatal("fixture digests must differ")
	}
}

func TestReferenceStructureConflictsBeforeProjection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*recordmap.ReferenceTraversalMappingInput)
	}{
		{"edge state", func(i *recordmap.ReferenceTraversalMappingInput) { i.Traversal.Edges[1].State = "unresolved" }},
		{"edge diagnostic", func(i *recordmap.ReferenceTraversalMappingInput) {
			i.Traversal.Edges[1].Diagnostic = strPtr("contradictory diagnostic")
		}},
		{"native node state under different lookup id", func(i *recordmap.ReferenceTraversalMappingInput) {
			n := i.Traversal.Nodes[1]
			n.ID = "alias"
			n.State = "unresolved"
			i.Traversal.Nodes = append(i.Traversal.Nodes, n)
		}},
		{"native object type", func(i *recordmap.ReferenceTraversalMappingInput) {
			n := i.Traversal.Nodes[1]
			n.ID = "alias"
			n.ObjectType = strPtr("App::Link")
			i.Traversal.Nodes = append(i.Traversal.Nodes, n)
		}},
		{"same lookup id conflicting target binding", func(i *recordmap.ReferenceTraversalMappingInput) {
			n := i.Traversal.Nodes[1]
			n.ObjectName = strPtr("Other")
			i.Traversal.Nodes = append(i.Traversal.Nodes, n)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := validReferenceTraversalInput()
			i.Traversal.Edges = append(i.Traversal.Edges, i.Traversal.Edges[0])
			tc.mutate(&i)
			_, a := recordmap.MapReferenceTraversalToReferenceRecord(i)
			assertInvalidReferenceTraversalMapping(t, a)
			slices.Reverse(i.Traversal.Nodes)
			slices.Reverse(i.Traversal.Edges)
			_, b := recordmap.MapReferenceTraversalToReferenceRecord(i)
			assertInvalidReferenceTraversalMapping(t, b)
			if a.Error() != b.Error() {
				t.Fatalf("order-dependent conflict: %v / %v", a, b)
			}
			if !strings.Contains(a.Error(), "conflict") {
				t.Fatalf("fixture failed before conflict check: %v", a)
			}
		})
	}
	t.Run("exact duplicates with lookup aliases and sequences", func(t *testing.T) {
		i := validReferenceTraversalInput()
		expected := mapReferenceTraversalRecord(t, i)
		n := i.Traversal.Nodes[1]
		n.ID = "alias"
		n.Sequence = 99
		i.Traversal.Nodes = append(i.Traversal.Nodes, n)
		e := i.Traversal.Edges[0]
		e.Target = "alias"
		e.Sequence = 87
		i.Traversal.Edges = append(i.Traversal.Edges, e)
		actual := mapReferenceTraversalRecord(t, i)
		if !reflect.DeepEqual(expected, actual) {
			t.Fatal("equivalent duplicate changed normalized record")
		}
	})
}

func TestReferenceStructureInvalidEvidence(t *testing.T) {
	tests := map[string]func(*recordmap.ReferenceTraversalMappingInput){
		"empty property": func(i *recordmap.ReferenceTraversalMappingInput) { i.Traversal.Edges[0].SourceProperty = strPtr("") },
		"padded mechanism": func(i *recordmap.ReferenceTraversalMappingInput) {
			i.Traversal.Edges[0].ReferenceMechanism = strPtr(" App::Link")
		},
		"empty mechanism": func(i *recordmap.ReferenceTraversalMappingInput) {
			i.Traversal.Edges[0].ReferenceMechanism = strPtr("")
		},
		"nul property": func(i *recordmap.ReferenceTraversalMappingInput) {
			i.Traversal.Edges[0].SourceProperty = strPtr("Link\x00")
		},
		"wrong source correlation": func(i *recordmap.ReferenceTraversalMappingInput) { i.ExpectedSourceDocument = "other.FCStd" },
		"undeclared source": func(i *recordmap.ReferenceTraversalMappingInput) {
			i.Traversal.SourceDocument = "unknown.FCStd"
			i.ExpectedSourceDocument = ""
		},
		"absolute coordinate": func(i *recordmap.ReferenceTraversalMappingInput) {
			i.Traversal.Nodes[1].DocumentPath = "/tmp/Part.FCStd"
		},
		"escaping coordinate": func(i *recordmap.ReferenceTraversalMappingInput) { i.Traversal.Nodes[1].DocumentPath = "../Part.FCStd" },
		"noncanonical coordinate": func(i *recordmap.ReferenceTraversalMappingInput) {
			i.Traversal.Nodes[1].DocumentPath = "models/../Part.FCStd"
		},
		"uncompleted inventory": func(i *recordmap.ReferenceTraversalMappingInput) {
			bindings := i.Resources.Bindings()
			bindings[0].MaterializedCoordinate = ""
			i.Resources, _ = resourcebinding.New(bindings)
		},
		"resolved edge unresolved target": func(i *recordmap.ReferenceTraversalMappingInput) { i.Traversal.Nodes[1].State = "unresolved" },
		"missing edge resolved target":    func(i *recordmap.ReferenceTraversalMappingInput) { i.Traversal.Edges[0].State = "missing" },
		"failed edge no diagnostics": func(i *recordmap.ReferenceTraversalMappingInput) {
			i.Traversal.Edges[0].State = "failed"
			i.Traversal.Edges[0].Diagnostic = nil
			i.Traversal.Diagnostics = []recordmap.ReferenceTraversalRuntimeDiagnostic{}
		},
		"skipped node no diagnostics": func(i *recordmap.ReferenceTraversalMappingInput) {
			i.Traversal.Nodes[1].State = "skipped"
			i.Traversal.Diagnostics = []recordmap.ReferenceTraversalRuntimeDiagnostic{}
		},
		"partial without source observation": func(i *recordmap.ReferenceTraversalMappingInput) { i.Traversal.Status = "partial" },
		"partial without diagnostics": func(i *recordmap.ReferenceTraversalMappingInput) {
			i.Traversal.Status = "partial"
			i.Traversal.Nodes = append(i.Traversal.Nodes, referenceTraversalDocumentNode("doc", i.Traversal.SourceDocument))
			i.Traversal.Diagnostics = []recordmap.ReferenceTraversalRuntimeDiagnostic{}
		},
		"failed with usable edges": func(i *recordmap.ReferenceTraversalMappingInput) { i.Traversal.Status = "failed" },
		"unknown status":           func(i *recordmap.ReferenceTraversalMappingInput) { i.Traversal.Status = "unavailable" },
		"unsupported scope claims success": func(i *recordmap.ReferenceTraversalMappingInput) {
			i.Traversal.Diagnostics[0] = recordmap.ReferenceTraversalRuntimeDiagnostic{Severity: "warning", Code: "unsupported_reference_value_shape", Message: "unsupported", Stage: strPtr("reference_discovery")}
		},
		"obsolete boundary": func(i *recordmap.ReferenceTraversalMappingInput) { i.Traversal.Boundary = "engine_invocation" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			i := validReferenceTraversalInput()
			mutate(&i)
			_, err := recordmap.MapReferenceTraversalToReferenceRecord(i)
			assertInvalidReferenceTraversalMapping(t, err)
		})
	}
}

func TestReferenceStructureEmptyScopeAndUnsupportedTargets(t *testing.T) {
	for _, status := range []string{"succeeded", "partial", "failed"} {
		t.Run(status, func(t *testing.T) {
			i := qualifiedTraversal(t, "")
			i.Traversal.Status = status
			i.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{}
			if status == "failed" {
				i.Traversal.Nodes = []recordmap.ReferenceTraversalRuntimeNode{}
			}
			r := mapReferenceTraversalRecord(t, i)
			want := map[string]string{"succeeded": "complete", "partial": "incomplete", "failed": "unavailable"}[status]
			if r.Reference.Completeness != want || r.Reference.Source.Resource.LogicalID != "a" || len(r.Reference.Edges) != 0 {
				t.Fatalf("scope: %+v", r.Reference)
			}
		})
	}
	for _, kind := range []string{"external_document", "external_file"} {
		t.Run(kind, func(t *testing.T) {
			i := qualifiedTraversal(t, "")
			n := referenceTraversalExternalDocumentNode("unknown", "external/not-declared.FCStd")
			n.Kind = kind
			n.State = "unresolved"
			i.Traversal.Nodes = append(i.Traversal.Nodes, n)
			edgeKind := "external_document_reference"
			qualification := "undeclared"
			if kind == "external_file" {
				edgeKind = "external_file_reference"
				qualification = "unsupported"
			}
			i.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{referenceTraversalEdge("source", "unknown", edgeKind, "unresolved")}
			r := mapReferenceTraversalRecord(t, i)
			target := r.Reference.Edges[0].Target
			if target.Resource != nil || target.Qualification != qualification || target.Path != n.DocumentPath || r.Reference.Completeness != "incomplete" {
				t.Fatalf("invented join or completeness: %+v", r.Reference)
			}
			if r.Reference.Unsupported != (kind == "external_file") {
				t.Fatal("unsupported distinction lost")
			}
		})
	}
	t.Run("failed item may retain partial graph", func(t *testing.T) {
		i := qualifiedTraversal(t, "")
		i.Traversal.Status = "partial"
		i.Traversal.Edges[0].State = "failed"
		i.Traversal.Edges[0].Diagnostic = strPtr("native resolution failed")
		r := mapReferenceTraversalRecord(t, i)
		if r.Reference.Status != "partial" || r.Reference.Completeness != "incomplete" {
			t.Fatal("failed item incorrectly forced failed aggregate")
		}
	})
}

func TestReferenceStructureUnsupportedScopeRemainsExplicit(t *testing.T) {
	input := qualifiedTraversal(t, "")
	input.Traversal.Status = "partial"
	input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{}
	input.Traversal.Diagnostics = []recordmap.ReferenceTraversalRuntimeDiagnostic{{
		Severity: "warning", Code: "unsupported_reference_value_shape", Message: "reference value cannot be inspected", Stage: strPtr("reference_discovery"),
	}}
	record := mapReferenceTraversalRecord(t, input)
	if !record.Reference.Unsupported || record.Reference.Completeness != "incomplete" || len(record.Reference.Edges) != 0 {
		t.Fatalf("unsupported empty traversal appeared complete: %+v", record.Reference)
	}
}

func TestReferenceStructureRejectsAmbiguousSourceCoordinate(t *testing.T) {
	input := qualifiedTraversal(t, "")
	// Both a and b alias this project-source coordinate. Neither was materialized
	// here: the mapper must use completed coordinates rather than inverse selection.
	input.Traversal.SourceDocument = "models/shared.FCStd"
	input.ExpectedSourceDocument = ""
	_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
	assertInvalidReferenceTraversalMapping(t, err)
	if !strings.Contains(err.Error(), "completed declared resource") {
		t.Fatalf("wrong rejection boundary: %v", err)
	}
}
