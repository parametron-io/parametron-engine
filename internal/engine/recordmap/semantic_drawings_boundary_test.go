package recordmap_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/semantic"
)

// #86 boundary coverage: semantic capture drawings are not reference edges and
// are never derived from traversal evidence. ReferenceKindDrawing remains
// generic record vocabulary that native traversal mapping does not produce.

func TestReferenceTraversalMappingNeverEmitsDrawingKind(t *testing.T) {
	for _, rawKind := range []string{"document_internal_reference", "external_document_reference", "external_file_reference"} {
		t.Run(rawKind, func(t *testing.T) {
			input := validReferenceTraversalInput()
			if rawKind != "document_internal_reference" {
				input.Traversal.Nodes[1].DocumentPath = "External/Spec.FCStd"
			}
			if rawKind == "external_file_reference" {
				input.Traversal.Nodes[1].Kind = "external_file"
				input.Traversal.Nodes[1].ObjectName = nil
			}
			input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
				referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", rawKind, "resolved"),
			}

			record := mapReferenceTraversalRecord(t, input)
			if len(record.Reference.Edges) != 1 {
				t.Fatalf("len(Edges) = %d, want 1", len(record.Reference.Edges))
			}
			for _, edge := range record.Reference.Edges {
				if edge.Kind == recordcontract.ReferenceKindDrawing {
					t.Fatalf("traversal edge kind %q must not map to %q", rawKind, recordcontract.ReferenceKindDrawing)
				}
			}
		})
	}
}

func TestReferenceTraversalMappingRejectsDrawingEdgeKinds(t *testing.T) {
	for _, rawKind := range []string{"drawing", "drawing_reference", "techdraw_page_reference"} {
		t.Run(rawKind, func(t *testing.T) {
			input := validReferenceTraversalInput()
			input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
				referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", rawKind, "resolved"),
			}

			_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
			assertInvalidReferenceTraversalMapping(t, err)
			if !strings.Contains(err.Error(), "unsupported edge kind") {
				t.Fatalf("expected unsupported edge kind rejection, got %v", err)
			}
		})
	}
}

func TestSemanticDrawingModelDoesNotEnterReferenceRecords(t *testing.T) {
	input := validReferenceTraversalInput()
	before := mapReferenceTraversalRecord(t, input)

	model := &semantic.Model{
		SchemaVersion:   semantic.SchemaVersion,
		RootComponentID: "cmp.root",
		Components: []semantic.Component{{
			ID:          "cmp.root",
			Kind:        "assembly",
			Name:        "Root",
			ChildrenIDs: []string{},
			Quantity:    1,
			NativeRef:   "Assembly01",
			DisplayName: "Root Assembly",
			Material:    "",
			ParentID:    "",
		}},
		Drawings: []semantic.Drawing{{
			NativeRef:    "Drawing",
			ID:           "drw.reference.boundary",
			Name:         "reference-boundary",
			DisplayName:  "Reference Boundary",
			ComponentIDs: []string{"cmp.root"},
			Resource:     semantic.DrawingResource{Topology: "embedded"},
			Sheets: []semantic.DrawingSheet{{
				NativeRef:   "Page",
				ID:          "sht.reference.boundary",
				Name:        "sheet",
				DisplayName: "",
				Order:       1,
				NativeType:  "TechDraw::DrawPage",
			}},
		}},
	}
	if err := semantic.Validate(model); err != nil {
		t.Fatalf("semantic model with drawings is invalid: %v", err)
	}

	after := mapReferenceTraversalRecord(t, input)
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		t.Fatalf("Marshal(before) returned error: %v", err)
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		t.Fatalf("Marshal(after) returned error: %v", err)
	}
	if !bytes.Equal(beforeJSON, afterJSON) {
		t.Fatalf("semantic drawings changed reference records\nbefore: %s\nafter:  %s", beforeJSON, afterJSON)
	}
	for _, forbidden := range []string{"drw.reference.boundary", "sht.reference.boundary", "Reference Boundary", "TechDraw::DrawPage", "Assembly01", `"drawing"`} {
		if bytes.Contains(afterJSON, []byte(forbidden)) {
			t.Fatalf("semantic drawing/native identity leaked into reference record: %s", afterJSON)
		}
	}
	for _, edge := range after.Reference.Edges {
		if edge.Kind == recordcontract.ReferenceKindDrawing {
			t.Fatalf("reference record unexpectedly contains a drawing edge: %#v", edge)
		}
	}
}
