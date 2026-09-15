package recordmap_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/recordpackage"
)

const referenceTraversalRecordKey = "reference-traversal:job-a:step-a"

// --- fixtures -----------------------------------------------------------

func strPtr(value string) *string { return &value }

func referenceTraversalDigestA() string { return strings.Repeat("a", 64) }
func referenceTraversalDigestB() string { return strings.Repeat("b", 64) }
func referenceTraversalDigestC() string { return strings.Repeat("c", 64) }

func referenceTraversalCanonicalRef() string {
	return recordpackage.RawRuntimeReferenceTraversalContractPath()
}

func referenceTraversalObjectNode(id, documentPath, objectName string) recordmap.ReferenceTraversalRuntimeNode {
	return recordmap.ReferenceTraversalRuntimeNode{
		Sequence:     0,
		ID:           id,
		Kind:         "object",
		State:        "resolved",
		DocumentPath: documentPath,
		ObjectName:   strPtr(objectName),
		ObjectType:   strPtr("Part::FeaturePython"),
		Label:        strPtr(objectName + " (renamed label)"),
	}
}

func referenceTraversalDocumentNode(id, documentPath string) recordmap.ReferenceTraversalRuntimeNode {
	return recordmap.ReferenceTraversalRuntimeNode{
		ID:           id,
		Kind:         "document",
		State:        "resolved",
		DocumentPath: documentPath,
		Label:        strPtr("document label"),
	}
}

func referenceTraversalExternalDocumentNode(id, documentPath string) recordmap.ReferenceTraversalRuntimeNode {
	return recordmap.ReferenceTraversalRuntimeNode{
		ID:           id,
		Kind:         "external_document",
		State:        "resolved",
		DocumentPath: documentPath,
		Label:        strPtr("external document label"),
	}
}

func referenceTraversalExternalFileNode(id, documentPath string) recordmap.ReferenceTraversalRuntimeNode {
	return recordmap.ReferenceTraversalRuntimeNode{
		ID:           id,
		Kind:         "external_file",
		State:        "resolved",
		DocumentPath: documentPath,
		Label:        strPtr("external file label"),
	}
}

func referenceTraversalEdge(source, target, kind, state string) recordmap.ReferenceTraversalRuntimeEdge {
	return recordmap.ReferenceTraversalRuntimeEdge{
		Source:             source,
		Target:             target,
		Kind:               kind,
		State:              state,
		SourceProperty:     strPtr("Support"),
		ReferenceMechanism: strPtr("App::PropertyXLink"),
		Diagnostic:         strPtr("edge diagnostic note"),
	}
}

// validReferenceTraversalInput returns a base valid schema-2 mapping input with
// one document_internal_reference edge between two object nodes.
func validReferenceTraversalInput() recordmap.ReferenceTraversalMappingInput {
	return recordmap.ReferenceTraversalMappingInput{
		RecordKey: referenceTraversalRecordKey,
		Traversal: recordmap.ReferenceTraversalRuntimeEvidence{
			SchemaVersion:  "2.0",
			Kind:           "raw_reference_traversal",
			Boundary:       "engine_invocation",
			Operation:      "reference_traversal",
			Status:         "succeeded",
			SourceDocument: "Assemblies/Main.FCStd",
			Nodes: []recordmap.ReferenceTraversalRuntimeNode{
				referenceTraversalObjectNode("object:raw-runtime-node-source", "Assemblies/Main.FCStd", "Bracket"),
				referenceTraversalObjectNode("object:raw-runtime-node-target", "Assemblies/Main.FCStd", "Fastener"),
			},
			Edges: []recordmap.ReferenceTraversalRuntimeEdge{
				referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", "document_internal_reference", "resolved"),
			},
			Diagnostics: []recordmap.ReferenceTraversalRuntimeDiagnostic{
				{Severity: "warning", Code: "unsupported_reference_value_shape", Message: "note", Stage: strPtr("reference_discovery")},
			},
		},
	}
}

func mapReferenceTraversalRecord(t *testing.T, input recordmap.ReferenceTraversalMappingInput) recordcontract.ReferenceRecord {
	t.Helper()
	record, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
	if err != nil {
		t.Fatalf("MapReferenceTraversalToReferenceRecord returned error: %v", err)
	}
	if record == nil {
		t.Fatal("MapReferenceTraversalToReferenceRecord returned nil record, want reference record")
	}
	if err := recordcontract.ValidateReferenceRecord(*record); err != nil {
		t.Fatalf("ValidateReferenceRecord(mapped) returned error: %v", err)
	}
	return *record
}

func assertInvalidReferenceTraversalMapping(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want invalid reference traversal mapping error")
	}
	if !errors.Is(err, recordmap.ErrInvalidReferenceTraversalMapping) {
		t.Fatalf("error = %v, want errors.Is(..., ErrInvalidReferenceTraversalMapping)", err)
	}
}

func hasReferenceTraversalEvidence(evidence []recordcontract.EvidenceReference, kind, ref, digest string) bool {
	for _, item := range evidence {
		if item.Kind == kind && item.Ref == ref && item.DigestSHA256 == digest {
			return true
		}
	}
	return false
}

func countReferenceTraversalEvidence(evidence []recordcontract.EvidenceReference, kind, ref string) int {
	count := 0
	for _, item := range evidence {
		if item.Kind == kind && item.Ref == ref {
			count++
		}
	}
	return count
}

// --- 1: valid schema-2 internal reference mapping ------------------------

func TestMapReferenceTraversalInternalReferenceMapsToComponent(t *testing.T) {
	input := validReferenceTraversalInput()
	record := mapReferenceTraversalRecord(t, input)

	if len(record.Reference.Edges) != 1 {
		t.Fatalf("len(Edges) = %d, want 1: %#v", len(record.Reference.Edges), record.Reference.Edges)
	}
	edge := record.Reference.Edges[0]

	if edge.Kind != recordcontract.ReferenceKindComponent {
		t.Fatalf("Kind = %q, want %q", edge.Kind, recordcontract.ReferenceKindComponent)
	}
	if edge.Resolution != recordcontract.ReferenceResolutionResolved {
		t.Fatalf("Resolution = %q, want %q", edge.Resolution, recordcontract.ReferenceResolutionResolved)
	}
	if edge.Role != "" {
		t.Fatalf("Role = %q, want empty", edge.Role)
	}

	if edge.Source.Path != "Assemblies/Main.FCStd" || edge.Source.Name != "Bracket" {
		t.Fatalf("Source = %#v, want Path=Assemblies/Main.FCStd Name=Bracket", edge.Source)
	}
	if edge.Target.Path != "Assemblies/Main.FCStd" || edge.Target.Name != "Fastener" {
		t.Fatalf("Target = %#v, want Path=Assemblies/Main.FCStd Name=Fastener", edge.Target)
	}

	// Raw label/objectType/node-id/sourceProperty/referenceMechanism/diagnostic/status
	// material must not leak into normalized identity or role fields.
	forbidden := []string{
		"Bracket (renamed label)", "Fastener (renamed label)",
		"Part::FeaturePython", "object:raw-runtime-node-source", "object:raw-runtime-node-target",
		"Support", "App::PropertyXLink", "edge diagnostic note", "succeeded",
	}
	for _, value := range forbidden {
		if edge.Source.Name == value || edge.Source.Path == value || edge.Source.ID == value ||
			edge.Target.Name == value || edge.Target.Path == value || edge.Target.ID == value ||
			edge.Role == value {
			t.Fatalf("raw evidence value %q leaked into normalized edge: %#v", value, edge)
		}
	}
	if edge.Source.ID != "" || edge.Target.ID != "" {
		t.Fatalf("Source/Target.ID = %q/%q, want empty (raw node id must not become PDM identity)", edge.Source.ID, edge.Target.ID)
	}
	if edge.Source.AssetID != "" || edge.Source.RevisionID != "" || edge.Source.DigestSHA256 != "" {
		t.Fatalf("Source asset/revision/digest = %#v, want empty", edge.Source)
	}
	if edge.Target.AssetID != "" || edge.Target.RevisionID != "" || edge.Target.DigestSHA256 != "" {
		t.Fatalf("Target asset/revision/digest = %#v, want empty", edge.Target)
	}

	if edge.Evidence.SourceKind != "reference-traversal" {
		t.Fatalf("Evidence.SourceKind = %q, want reference-traversal", edge.Evidence.SourceKind)
	}
	if edge.Evidence.SourceRef != referenceTraversalCanonicalRef() {
		t.Fatalf("Evidence.SourceRef = %q, want %q", edge.Evidence.SourceRef, referenceTraversalCanonicalRef())
	}
}

// --- 2: external document mapping ----------------------------------------

func TestMapReferenceTraversalExternalDocumentMapsToExternal(t *testing.T) {
	input := validReferenceTraversalInput()
	input.Traversal.Nodes = []recordmap.ReferenceTraversalRuntimeNode{
		referenceTraversalObjectNode("object:raw-runtime-node-source", "Assemblies/Main.FCStd", "Bracket"),
		referenceTraversalExternalDocumentNode("external-document:raw-runtime-node-abc", "External/Spec.FCStd"),
	}
	input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
		referenceTraversalEdge("object:raw-runtime-node-source", "external-document:raw-runtime-node-abc", "external_document_reference", "resolved"),
	}

	record := mapReferenceTraversalRecord(t, input)
	if len(record.Reference.Edges) != 1 {
		t.Fatalf("len(Edges) = %d, want 1", len(record.Reference.Edges))
	}
	edge := record.Reference.Edges[0]
	if edge.Kind != recordcontract.ReferenceKindExternal {
		t.Fatalf("Kind = %q, want %q", edge.Kind, recordcontract.ReferenceKindExternal)
	}
	if edge.Target.Path != "External/Spec.FCStd" {
		t.Fatalf("Target.Path = %q, want External/Spec.FCStd", edge.Target.Path)
	}
	if edge.Target.Name != "" {
		t.Fatalf("Target.Name = %q, want empty for external_document node", edge.Target.Name)
	}
	if edge.Target.ID != "" {
		t.Fatalf("Target.ID = %q, want empty (raw node id not identity)", edge.Target.ID)
	}
}

// --- 3: external file mapping ---------------------------------------------

func TestMapReferenceTraversalExternalFileMapsToExternal(t *testing.T) {
	input := validReferenceTraversalInput()
	input.Traversal.Nodes = []recordmap.ReferenceTraversalRuntimeNode{
		referenceTraversalObjectNode("object:raw-runtime-node-source", "Assemblies/Main.FCStd", "Bracket"),
		referenceTraversalExternalFileNode("external-file:raw-runtime-node-def", "External/drawing.pdf"),
	}
	input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
		referenceTraversalEdge("object:raw-runtime-node-source", "external-file:raw-runtime-node-def", "external_file_reference", "resolved"),
	}

	record := mapReferenceTraversalRecord(t, input)
	if len(record.Reference.Edges) != 1 {
		t.Fatalf("len(Edges) = %d, want 1", len(record.Reference.Edges))
	}
	edge := record.Reference.Edges[0]
	if edge.Kind != recordcontract.ReferenceKindExternal {
		t.Fatalf("Kind = %q, want %q", edge.Kind, recordcontract.ReferenceKindExternal)
	}
	if edge.Target.Path != "External/drawing.pdf" {
		t.Fatalf("Target.Path = %q, want External/drawing.pdf", edge.Target.Path)
	}
	if edge.Target.Name != "" {
		t.Fatalf("Target.Name = %q, want empty for external_file node (no drawing-kind/type inference)", edge.Target.Name)
	}
	if edge.Target.AssetID != "" || edge.Target.RevisionID != "" || edge.Target.DigestSHA256 != "" {
		t.Fatalf("Target asset/revision/digest = %#v, want empty (no inference from filename)", edge.Target)
	}
}

// --- 4/5: raw state compression matrix + unknown state rejection ---------

func TestMapReferenceTraversalStateCompressionMatrix(t *testing.T) {
	tests := []struct {
		rawState   string
		wantResult recordcontract.ReferenceResolutionState
	}{
		{"resolved", recordcontract.ReferenceResolutionResolved},
		{"missing", recordcontract.ReferenceResolutionUnresolved},
		{"unresolved", recordcontract.ReferenceResolutionUnresolved},
		{"skipped", recordcontract.ReferenceResolutionUnresolved},
		{"failed", recordcontract.ReferenceResolutionUnresolved},
	}

	for _, tt := range tests {
		t.Run(tt.rawState, func(t *testing.T) {
			input := validReferenceTraversalInput()
			input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
				referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", "document_internal_reference", tt.rawState),
			}
			record := mapReferenceTraversalRecord(t, input)
			if got := record.Reference.Edges[0].Resolution; got != tt.wantResult {
				t.Fatalf("Resolution = %q, want %q", got, tt.wantResult)
			}
		})
	}
}

func TestMapReferenceTraversalUnknownStateRejected(t *testing.T) {
	t.Run("edge state", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
			referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", "document_internal_reference", "pending"),
		}
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("node state", func(t *testing.T) {
		input := validReferenceTraversalInput()
		node := input.Traversal.Nodes[0]
		node.State = "pending"
		input.Traversal.Nodes[0] = node
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})
}

// --- 6/7: edge-kind matrix + unknown edge-kind rejection ------------------

func TestMapReferenceTraversalEdgeKindMatrix(t *testing.T) {
	tests := []struct {
		rawKind    string
		wantResult recordcontract.ReferenceKind
	}{
		{"document_internal_reference", recordcontract.ReferenceKindComponent},
		{"external_document_reference", recordcontract.ReferenceKindExternal},
		{"external_file_reference", recordcontract.ReferenceKindExternal},
	}

	for _, tt := range tests {
		t.Run(tt.rawKind, func(t *testing.T) {
			input := validReferenceTraversalInput()
			input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
				referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", tt.rawKind, "resolved"),
			}
			record := mapReferenceTraversalRecord(t, input)
			if got := record.Reference.Edges[0].Kind; got != tt.wantResult {
				t.Fatalf("Kind = %q, want %q", got, tt.wantResult)
			}
		})
	}
}

func TestMapReferenceTraversalUnknownEdgeKindRejected(t *testing.T) {
	tests := []string{
		"internal_reference",
		"external_reference",
		"document_reference",
		"Document_Internal_Reference",
		"document_internal_reference ",
		"",
	}
	for _, kind := range tests {
		t.Run("kind:"+kind, func(t *testing.T) {
			input := validReferenceTraversalInput()
			input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
				referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", kind, "resolved"),
			}
			_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
			assertInvalidReferenceTraversalMapping(t, err)
		})
	}
}

// --- node-kind validation --------------------------------------------------

func TestMapReferenceTraversalNodeKindsAccepted(t *testing.T) {
	tests := []struct {
		name string
		node func() recordmap.ReferenceTraversalRuntimeNode
	}{
		{"document", func() recordmap.ReferenceTraversalRuntimeNode {
			return referenceTraversalDocumentNode("document:raw-runtime-node-doc", "Assemblies/Sub.FCStd")
		}},
		{"object", func() recordmap.ReferenceTraversalRuntimeNode {
			return referenceTraversalObjectNode("object:raw-runtime-node-obj", "Assemblies/Sub.FCStd", "Widget")
		}},
		{"external_document", func() recordmap.ReferenceTraversalRuntimeNode {
			return referenceTraversalExternalDocumentNode("external-document:raw-runtime-node-ed", "External/Spec.FCStd")
		}},
		{"external_file", func() recordmap.ReferenceTraversalRuntimeNode {
			return referenceTraversalExternalFileNode("external-file:raw-runtime-node-ef", "External/drawing.pdf")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := tt.node()
			input := validReferenceTraversalInput()
			input.Traversal.Nodes = []recordmap.ReferenceTraversalRuntimeNode{
				referenceTraversalObjectNode("object:raw-runtime-node-source", "Assemblies/Main.FCStd", "Bracket"),
				target,
			}
			input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
				referenceTraversalEdge("object:raw-runtime-node-source", target.ID, "document_internal_reference", "resolved"),
			}
			record := mapReferenceTraversalRecord(t, input)
			if len(record.Reference.Edges) != 1 {
				t.Fatalf("len(Edges) = %d, want 1", len(record.Reference.Edges))
			}
		})
	}
}

func TestMapReferenceTraversalUnknownNodeKindRejected(t *testing.T) {
	input := validReferenceTraversalInput()
	node := input.Traversal.Nodes[1]
	node.Kind = "component"
	input.Traversal.Nodes[1] = node
	_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
	assertInvalidReferenceTraversalMapping(t, err)
}

// --- 8: schema-version validation ------------------------------------------

func TestMapReferenceTraversalSchemaVersionBehavior(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{name: "two zero accepted", version: "2.0"},
		{name: "empty rejected", version: "", wantErr: true},
		{name: "one zero rejected", version: "1.0", wantErr: true},
		{name: "three zero rejected", version: "3.0", wantErr: true},
		{name: "leading whitespace rejected", version: " 2.0", wantErr: true},
		{name: "trailing whitespace rejected", version: "2.0 ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validReferenceTraversalInput()
			input.Traversal.SchemaVersion = tt.version
			_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
			if tt.wantErr {
				assertInvalidReferenceTraversalMapping(t, err)
				return
			}
			if err != nil {
				t.Fatalf("MapReferenceTraversalToReferenceRecord returned error: %v", err)
			}
		})
	}
}

// --- 9: node ID resolution --------------------------------------------------

func TestMapReferenceTraversalNodeIDResolution(t *testing.T) {
	t.Run("duplicate node id", func(t *testing.T) {
		input := validReferenceTraversalInput()
		dup := input.Traversal.Nodes[0]
		dup.ID = input.Traversal.Nodes[1].ID
		input.Traversal.Nodes[0] = dup
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("empty node id", func(t *testing.T) {
		input := validReferenceTraversalInput()
		node := input.Traversal.Nodes[0]
		node.ID = ""
		input.Traversal.Nodes[0] = node
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("empty edge source", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
			referenceTraversalEdge("", "object:raw-runtime-node-target", "document_internal_reference", "resolved"),
		}
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("empty edge target", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
			referenceTraversalEdge("object:raw-runtime-node-source", "", "document_internal_reference", "resolved"),
		}
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("unknown source id", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
			referenceTraversalEdge("object:does-not-exist", "object:raw-runtime-node-target", "document_internal_reference", "resolved"),
		}
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("unknown target id", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
			referenceTraversalEdge("object:raw-runtime-node-source", "object:does-not-exist", "document_internal_reference", "resolved"),
		}
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})
}

// --- 10: endpoint semantic identity validation -----------------------------

func TestMapReferenceTraversalRequiredEndpointMaterial(t *testing.T) {
	t.Run("object node missing objectName", func(t *testing.T) {
		input := validReferenceTraversalInput()
		node := input.Traversal.Nodes[1]
		node.ObjectName = nil
		input.Traversal.Nodes[1] = node
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("object node missing documentPath", func(t *testing.T) {
		input := validReferenceTraversalInput()
		node := input.Traversal.Nodes[1]
		node.DocumentPath = ""
		input.Traversal.Nodes[1] = node
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("label does not substitute for missing objectName", func(t *testing.T) {
		input := validReferenceTraversalInput()
		node := input.Traversal.Nodes[1]
		node.ObjectName = nil
		node.Label = strPtr("Fastener")
		input.Traversal.Nodes[1] = node
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("document node missing documentPath", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Traversal.Nodes = []recordmap.ReferenceTraversalRuntimeNode{
			input.Traversal.Nodes[0],
			referenceTraversalDocumentNode("document:raw-runtime-node-doc", ""),
		}
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
			referenceTraversalEdge("object:raw-runtime-node-source", "document:raw-runtime-node-doc", "document_internal_reference", "resolved"),
		}
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("document node with objectName present is rejected", func(t *testing.T) {
		input := validReferenceTraversalInput()
		node := referenceTraversalDocumentNode("document:raw-runtime-node-doc", "Assemblies/Sub.FCStd")
		node.ObjectName = strPtr("should not be here")
		input.Traversal.Nodes = []recordmap.ReferenceTraversalRuntimeNode{
			input.Traversal.Nodes[0],
			node,
		}
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
			referenceTraversalEdge("object:raw-runtime-node-source", "document:raw-runtime-node-doc", "document_internal_reference", "resolved"),
		}
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("external_document node missing documentPath", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Traversal.Nodes = []recordmap.ReferenceTraversalRuntimeNode{
			input.Traversal.Nodes[0],
			referenceTraversalExternalDocumentNode("external-document:raw-runtime-node-ed", ""),
		}
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
			referenceTraversalEdge("object:raw-runtime-node-source", "external-document:raw-runtime-node-ed", "external_document_reference", "resolved"),
		}
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("external_file node missing documentPath", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Traversal.Nodes = []recordmap.ReferenceTraversalRuntimeNode{
			input.Traversal.Nodes[0],
			referenceTraversalExternalFileNode("external-file:raw-runtime-node-ef", ""),
		}
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
			referenceTraversalEdge("object:raw-runtime-node-source", "external-file:raw-runtime-node-ef", "external_file_reference", "resolved"),
		}
		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})
}

// --- canonical evidence linkage --------------------------------------------

func TestMapReferenceTraversalCanonicalEvidenceLinkage(t *testing.T) {
	record := mapReferenceTraversalRecord(t, validReferenceTraversalInput())
	edge := record.Reference.Edges[0]

	if edge.Evidence.SourceKind != "reference-traversal" {
		t.Fatalf("Evidence.SourceKind = %q, want reference-traversal", edge.Evidence.SourceKind)
	}
	if edge.Evidence.SourceRef != "raw/runtime/prm.reference-traversal.json" {
		t.Fatalf("Evidence.SourceRef = %q, want raw/runtime/prm.reference-traversal.json", edge.Evidence.SourceRef)
	}
	if edge.Evidence.SourceRef != recordpackage.RawRuntimeReferenceTraversalContractPath() {
		t.Fatalf("Evidence.SourceRef = %q, want %q", edge.Evidence.SourceRef, recordpackage.RawRuntimeReferenceTraversalContractPath())
	}
	if edge.Evidence.SourceRef == recordpackage.RawObservedContractPath() {
		t.Fatalf("Evidence.SourceRef used observed evidence path: %q", edge.Evidence.SourceRef)
	}
	if edge.Evidence.SourceRef == recordpackage.RawRuntimeResultContractPath() {
		t.Fatalf("Evidence.SourceRef used runtime result evidence path: %q", edge.Evidence.SourceRef)
	}
}

// --- 14: optional evidence digest ------------------------------------------

func TestMapReferenceTraversalEvidenceDigestValidation(t *testing.T) {
	t.Run("valid digest propagates to edge and provenance", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.EvidenceDigestSHA256 = referenceTraversalDigestA()

		record := mapReferenceTraversalRecord(t, input)
		if record.Reference.Edges[0].Evidence.DigestSHA256 != referenceTraversalDigestA() {
			t.Fatalf("Edge.Evidence.DigestSHA256 = %q, want %q", record.Reference.Edges[0].Evidence.DigestSHA256, referenceTraversalDigestA())
		}
		if !hasReferenceTraversalEvidence(record.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef(), referenceTraversalDigestA()) {
			t.Fatalf("Provenance.Evidence = %#v, want digest %q", record.Provenance.Evidence, referenceTraversalDigestA())
		}
	})

	t.Run("whitespace-padded valid digest is trimmed and accepted", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.EvidenceDigestSHA256 = " " + referenceTraversalDigestA() + " "

		record := mapReferenceTraversalRecord(t, input)
		if record.Reference.Edges[0].Evidence.DigestSHA256 != referenceTraversalDigestA() {
			t.Fatalf("Edge.Evidence.DigestSHA256 = %q, want trimmed %q", record.Reference.Edges[0].Evidence.DigestSHA256, referenceTraversalDigestA())
		}
	})

	tests := []struct {
		name   string
		digest string
	}{
		{name: "uppercase", digest: "A" + referenceTraversalDigestA()[1:]},
		{name: "short", digest: referenceTraversalDigestA()[:63]},
		{name: "long", digest: referenceTraversalDigestA() + "a"},
		{name: "non hex", digest: "g" + referenceTraversalDigestA()[1:]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validReferenceTraversalInput()
			input.EvidenceDigestSHA256 = tt.digest
			_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
			assertInvalidReferenceTraversalMapping(t, err)
		})
	}
}

// --- 15: caller linkage preservation ----------------------------------------

func TestMapReferenceTraversalLinkagePreservation(t *testing.T) {
	t.Run("linkage trimmed and preserved", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Linkage = recordmap.ReferenceTraversalMappingLinkage{
			JobID:      " job-a ",
			ProductKey: " product-a ",
			StepRef:    " step-a ",
		}
		record := mapReferenceTraversalRecord(t, input)
		edge := record.Reference.Edges[0]
		if edge.Linkage.JobID != "job-a" || edge.Linkage.ProductKey != "product-a" || edge.Linkage.StepRef != "step-a" {
			t.Fatalf("Linkage = %#v, want trimmed caller linkage", edge.Linkage)
		}
	})

	t.Run("empty linkage allowed", func(t *testing.T) {
		record := mapReferenceTraversalRecord(t, validReferenceTraversalInput())
		if record.Reference.Edges[0].Linkage != (recordcontract.ReferenceLinkage{}) {
			t.Fatalf("Linkage = %#v, want empty", record.Reference.Edges[0].Linkage)
		}
	})
}

// --- 16/26: caller provenance preservation + non-mutation ------------------

func referenceTraversalCallerProvenance() recordcontract.Provenance {
	return recordcontract.Provenance{
		SourceRevision: recordcontract.SourceRevisionProvenance{RevisionID: " rev-1 "},
		Inputs: []recordcontract.ProvenanceInput{
			{Kind: " dsl ", Identity: " model.pmtn ", DigestSHA256: referenceTraversalDigestB()},
		},
		Plan: recordcontract.PlanProvenance{PlanID: " plan-1 "},
		Linkage: recordcontract.LinkageProvenance{
			ProductKey: " product-a ", JobID: " job-a ", StepRef: " step-a ",
		},
		Evidence: []recordcontract.EvidenceReference{
			{Kind: " report ", Ref: " raw/report.json "},
		},
		Runtime: recordcontract.RuntimeProvenance{ToolID: " freecad ", RuntimeID: " runtime-1 ", Adapter: " freecad-runner "},
	}
}

func TestMapReferenceTraversalProvenancePreservationAndCopySafety(t *testing.T) {
	input := validReferenceTraversalInput()
	input.EvidenceDigestSHA256 = referenceTraversalDigestA()
	input.Provenance = referenceTraversalCallerProvenance()
	original := input.Provenance

	first := mapReferenceTraversalRecord(t, input)

	if !reflect.DeepEqual(input.Provenance, original) {
		t.Fatalf("input provenance mutated:\n got: %#v\nwant: %#v", input.Provenance, original)
	}

	if first.Provenance.SourceRevision.RevisionID != "rev-1" ||
		first.Provenance.Plan.PlanID != "plan-1" ||
		first.Provenance.Runtime.ToolID != "freecad" ||
		len(first.Provenance.Inputs) != 1 || first.Provenance.Inputs[0].Kind != "dsl" {
		t.Fatalf("caller provenance not preserved after normalization: %#v", first.Provenance)
	}
	if !hasReferenceTraversalEvidence(first.Provenance.Evidence, "report", "raw/report.json", "") {
		t.Fatalf("caller evidence not preserved: %#v", first.Provenance.Evidence)
	}
	if !hasReferenceTraversalEvidence(first.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef(), referenceTraversalDigestA()) {
		t.Fatalf("canonical traversal evidence not added: %#v", first.Provenance.Evidence)
	}

	// Returned record must not alias caller-owned mutable evidence slice.
	first.Provenance.Evidence[0].DigestSHA256 = referenceTraversalDigestC()
	if input.Provenance.Evidence[0].DigestSHA256 != "" {
		t.Fatalf("mutating returned provenance changed input provenance: %#v", input.Provenance.Evidence)
	}

	second := mapReferenceTraversalRecord(t, input)
	if second.Provenance.Evidence[0].DigestSHA256 == referenceTraversalDigestC() {
		t.Fatalf("fresh mapping call reused mutated returned state")
	}
}

func TestMapReferenceTraversalInputNonMutation(t *testing.T) {
	input := validReferenceTraversalInput()
	input.EvidenceDigestSHA256 = referenceTraversalDigestA()
	input.Provenance = referenceTraversalCallerProvenance()

	originalNodes := append([]recordmap.ReferenceTraversalRuntimeNode(nil), input.Traversal.Nodes...)
	originalEdges := append([]recordmap.ReferenceTraversalRuntimeEdge(nil), input.Traversal.Edges...)
	originalDiagnostics := append([]recordmap.ReferenceTraversalRuntimeDiagnostic(nil), input.Traversal.Diagnostics...)
	originalProvenance := input.Provenance

	record := mapReferenceTraversalRecord(t, input)
	recordBeforeMutation := record

	if !reflect.DeepEqual(input.Traversal.Nodes, originalNodes) {
		t.Fatalf("Traversal.Nodes mutated:\nbefore: %#v\nafter:  %#v", originalNodes, input.Traversal.Nodes)
	}
	if !reflect.DeepEqual(input.Traversal.Edges, originalEdges) {
		t.Fatalf("Traversal.Edges mutated:\nbefore: %#v\nafter:  %#v", originalEdges, input.Traversal.Edges)
	}
	if !reflect.DeepEqual(input.Traversal.Diagnostics, originalDiagnostics) {
		t.Fatalf("Traversal.Diagnostics mutated:\nbefore: %#v\nafter:  %#v", originalDiagnostics, input.Traversal.Diagnostics)
	}
	if !reflect.DeepEqual(input.Provenance, originalProvenance) {
		t.Fatalf("Provenance mutated:\nbefore: %#v\nafter:  %#v", originalProvenance, input.Provenance)
	}

	// Mutating caller input after mapping must not change the previously returned record.
	input.Traversal.Nodes[0].DocumentPath = "/mutated/path.FCStd"
	*input.Traversal.Nodes[1].ObjectName = "mutated"
	input.Traversal.Edges[0].Kind = "external_file_reference"
	input.Provenance.Evidence[0].Ref = "mutated"

	if !reflect.DeepEqual(record, recordBeforeMutation) {
		t.Fatalf("previously returned record changed after input mutation:\nbefore: %#v\nafter:  %#v", recordBeforeMutation, record)
	}
}

// --- 17: provenance evidence dedup / conflict -------------------------------

func TestMapReferenceTraversalProvenanceEvidenceDedupe(t *testing.T) {
	t.Run("matching evidence is not duplicated", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.EvidenceDigestSHA256 = referenceTraversalDigestA()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestA()},
		}

		record := mapReferenceTraversalRecord(t, input)
		if count := countReferenceTraversalEvidence(record.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef()); count != 1 {
			t.Fatalf("Provenance.Evidence = %#v, want one traversal reference", record.Provenance.Evidence)
		}
	})

	t.Run("empty existing digest is enriched without conflict", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.EvidenceDigestSHA256 = referenceTraversalDigestA()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef()},
		}

		first := mapReferenceTraversalRecord(t, input)
		second := mapReferenceTraversalRecord(t, input)
		if count := countReferenceTraversalEvidence(first.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef()); count != 1 {
			t.Fatalf("Provenance.Evidence = %#v, want one traversal reference", first.Provenance.Evidence)
		}
		if !hasReferenceTraversalEvidence(first.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef(), referenceTraversalDigestA()) {
			t.Fatalf("Provenance.Evidence = %#v, want enriched digest", first.Provenance.Evidence)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("digest enrichment is not deterministic:\nfirst:  %#v\nsecond: %#v", first, second)
		}
	})

	t.Run("conflicting non-empty digest fails", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.EvidenceDigestSHA256 = referenceTraversalDigestA()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestB()},
		}

		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})
}

// --- 18/19: zero-edge behavior ----------------------------------------------

func TestMapReferenceTraversalZeroEdgesReturnsNilRecordNoError(t *testing.T) {
	input := validReferenceTraversalInput()
	input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{}

	record, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
	if err != nil {
		t.Fatalf("MapReferenceTraversalToReferenceRecord(zero edges) returned error: %v", err)
	}
	if record != nil {
		t.Fatalf("record = %#v, want nil", record)
	}

	output, err := recordmap.MapReferenceTraversal(input)
	if err != nil {
		t.Fatalf("MapReferenceTraversal(zero edges) returned error: %v", err)
	}
	if output.ReferenceRecord != nil {
		t.Fatalf("output.ReferenceRecord = %#v, want nil", output.ReferenceRecord)
	}
}

func TestMapReferenceTraversalZeroEdgesStillValidatesProvenance(t *testing.T) {
	t.Run("conflicting digest fails even with zero edges", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{}
		input.EvidenceDigestSHA256 = referenceTraversalDigestA()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestB()},
		}

		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("malformed unrelated evidence fails even with zero edges", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{}
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "custom-evidence", Ref: ""},
		}

		_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		assertInvalidReferenceTraversalMapping(t, err)
	})
}

// --- 20/21: normalized duplicate collapse ------------------------------------

func TestMapReferenceTraversalDuplicateCollapsePropertyAndMechanismLoss(t *testing.T) {
	input := validReferenceTraversalInput()
	edgeA := recordmap.ReferenceTraversalRuntimeEdge{
		Source:             "object:raw-runtime-node-source",
		Target:             "object:raw-runtime-node-target",
		Kind:               "document_internal_reference",
		State:              "resolved",
		SourceProperty:     strPtr("Support"),
		ReferenceMechanism: strPtr("App::PropertyLink"),
	}
	edgeB := recordmap.ReferenceTraversalRuntimeEdge{
		Source:             "object:raw-runtime-node-source",
		Target:             "object:raw-runtime-node-target",
		Kind:               "document_internal_reference",
		State:              "resolved",
		SourceProperty:     strPtr("Placement"),
		ReferenceMechanism: strPtr("App::PropertyXLink"),
	}
	input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{edgeA, edgeB}
	if len(input.Traversal.Edges) != 2 {
		t.Fatalf("raw edge fixture count = %d, want 2 distinct raw edges", len(input.Traversal.Edges))
	}

	record := mapReferenceTraversalRecord(t, input)
	if len(record.Reference.Edges) != 1 {
		t.Fatalf("len(Edges) = %d, want 1 (normalized duplicates collapsed)", len(record.Reference.Edges))
	}
	if record.Reference.Edges[0].Role != "" {
		t.Fatalf("Role = %q, want empty despite raw property/mechanism data", record.Reference.Edges[0].Role)
	}
}

func TestMapReferenceTraversalDuplicateCollapseStateCompression(t *testing.T) {
	input := validReferenceTraversalInput()
	edgeA := referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", "document_internal_reference", "missing")
	edgeB := referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", "document_internal_reference", "failed")
	input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{edgeA, edgeB}

	record := mapReferenceTraversalRecord(t, input)
	if len(record.Reference.Edges) != 1 {
		t.Fatalf("len(Edges) = %d, want 1 (missing/failed both compress to unresolved)", len(record.Reference.Edges))
	}
	if record.Reference.Edges[0].Resolution != recordcontract.ReferenceResolutionUnresolved {
		t.Fatalf("Resolution = %q, want unresolved", record.Reference.Edges[0].Resolution)
	}
}

// --- 22: distinct normalized edges remain distinct ---------------------------

func TestMapReferenceTraversalDistinctEdgesRemainDistinct(t *testing.T) {
	t.Run("different target endpoint", func(t *testing.T) {
		input := validReferenceTraversalInput()
		thirdNode := referenceTraversalObjectNode("object:raw-runtime-node-third", "Assemblies/Main.FCStd", "Washer")
		input.Traversal.Nodes = append(input.Traversal.Nodes, thirdNode)
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
			referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", "document_internal_reference", "resolved"),
			referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-third", "document_internal_reference", "resolved"),
		}
		record := mapReferenceTraversalRecord(t, input)
		if len(record.Reference.Edges) != 2 {
			t.Fatalf("len(Edges) = %d, want 2 distinct target endpoints", len(record.Reference.Edges))
		}
	})

	t.Run("different kind", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
			referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", "document_internal_reference", "resolved"),
			referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", "external_document_reference", "resolved"),
		}
		record := mapReferenceTraversalRecord(t, input)
		if len(record.Reference.Edges) != 2 {
			t.Fatalf("len(Edges) = %d, want 2 distinct kinds", len(record.Reference.Edges))
		}
	})

	t.Run("different resolution", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
			referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", "document_internal_reference", "resolved"),
			referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", "document_internal_reference", "missing"),
		}
		record := mapReferenceTraversalRecord(t, input)
		if len(record.Reference.Edges) != 2 {
			t.Fatalf("len(Edges) = %d, want 2 distinct resolutions", len(record.Reference.Edges))
		}
	})
}

// --- 24: mapping helper parity -----------------------------------------------

func TestMapReferenceTraversalWrapperParity(t *testing.T) {
	t.Run("valid input", func(t *testing.T) {
		input := validReferenceTraversalInput()
		direct, directErr := recordmap.MapReferenceTraversalToReferenceRecord(input)
		wrapped, wrappedErr := recordmap.MapReferenceTraversal(input)

		if directErr != nil || wrappedErr != nil {
			t.Fatalf("errors = %v / %v, want nil", directErr, wrappedErr)
		}
		if !reflect.DeepEqual(direct, wrapped.ReferenceRecord) {
			t.Fatalf("wrapper parity mismatch:\ndirect:  %#v\nwrapped: %#v", direct, wrapped.ReferenceRecord)
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.RecordKey = ""

		_, directErr := recordmap.MapReferenceTraversalToReferenceRecord(input)
		_, wrappedErr := recordmap.MapReferenceTraversal(input)

		assertInvalidReferenceTraversalMapping(t, directErr)
		assertInvalidReferenceTraversalMapping(t, wrappedErr)
	})
}

// --- 25: sentinel error wrapping ---------------------------------------------

func TestMapReferenceTraversalSentinelErrorWrapping(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*recordmap.ReferenceTraversalMappingInput)
	}{
		{"missing record key", func(input *recordmap.ReferenceTraversalMappingInput) { input.RecordKey = "  " }},
		{"unsupported schema version", func(input *recordmap.ReferenceTraversalMappingInput) { input.Traversal.SchemaVersion = "1.0" }},
		{"nil nodes", func(input *recordmap.ReferenceTraversalMappingInput) { input.Traversal.Nodes = nil }},
		{"nil edges", func(input *recordmap.ReferenceTraversalMappingInput) { input.Traversal.Edges = nil }},
		{"malformed digest", func(input *recordmap.ReferenceTraversalMappingInput) { input.EvidenceDigestSHA256 = "not-hex" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validReferenceTraversalInput()
			tt.mutate(&input)
			_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
			assertInvalidReferenceTraversalMapping(t, err)
		})
	}
}

// --- required record key -----------------------------------------------------

func TestMapReferenceTraversalRequiredRecordKey(t *testing.T) {
	for _, recordKey := range []string{"", "   "} {
		t.Run("record key "+recordKey, func(t *testing.T) {
			input := validReferenceTraversalInput()
			input.RecordKey = recordKey
			_, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
			assertInvalidReferenceTraversalMapping(t, err)
		})
	}
}

// --- 27: canonical linkage reconciliation ------------------------------------

func TestMapReferenceTraversalLinkageReconciliation(t *testing.T) {
	tests := []struct {
		name              string
		mappingLinkage    recordmap.ReferenceTraversalMappingLinkage
		provenanceLinkage recordcontract.LinkageProvenance
		want              recordcontract.ReferenceLinkage
	}{
		{
			name:           "mapping-only jobId",
			mappingLinkage: recordmap.ReferenceTraversalMappingLinkage{JobID: "job-a"},
			want:           recordcontract.ReferenceLinkage{JobID: "job-a"},
		},
		{
			name:           "mapping-only productKey",
			mappingLinkage: recordmap.ReferenceTraversalMappingLinkage{ProductKey: "product-a"},
			want:           recordcontract.ReferenceLinkage{ProductKey: "product-a"},
		},
		{
			name:           "mapping-only stepRef",
			mappingLinkage: recordmap.ReferenceTraversalMappingLinkage{StepRef: "step-a"},
			want:           recordcontract.ReferenceLinkage{StepRef: "step-a"},
		},
		{
			name:              "provenance-only jobId",
			provenanceLinkage: recordcontract.LinkageProvenance{JobID: "job-a"},
			want:              recordcontract.ReferenceLinkage{JobID: "job-a"},
		},
		{
			name:              "provenance-only productKey",
			provenanceLinkage: recordcontract.LinkageProvenance{ProductKey: "product-a"},
			want:              recordcontract.ReferenceLinkage{ProductKey: "product-a"},
		},
		{
			name:              "provenance-only stepRef",
			provenanceLinkage: recordcontract.LinkageProvenance{StepRef: "step-a"},
			want:              recordcontract.ReferenceLinkage{StepRef: "step-a"},
		},
		{
			name:              "equal jobId, whitespace on mapping side",
			mappingLinkage:    recordmap.ReferenceTraversalMappingLinkage{JobID: " job-a "},
			provenanceLinkage: recordcontract.LinkageProvenance{JobID: "job-a"},
			want:              recordcontract.ReferenceLinkage{JobID: "job-a"},
		},
		{
			name:              "equal productKey, whitespace on mapping side",
			mappingLinkage:    recordmap.ReferenceTraversalMappingLinkage{ProductKey: " product-a "},
			provenanceLinkage: recordcontract.LinkageProvenance{ProductKey: "product-a"},
			want:              recordcontract.ReferenceLinkage{ProductKey: "product-a"},
		},
		{
			name:              "equal stepRef, whitespace on mapping side",
			mappingLinkage:    recordmap.ReferenceTraversalMappingLinkage{StepRef: " step-a "},
			provenanceLinkage: recordcontract.LinkageProvenance{StepRef: "step-a"},
			want:              recordcontract.ReferenceLinkage{StepRef: "step-a"},
		},
		{
			name: "empty linkage on both sides remains empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validReferenceTraversalInput()
			input.Linkage = tt.mappingLinkage
			input.Provenance.Linkage = tt.provenanceLinkage

			record := mapReferenceTraversalRecord(t, input)
			edge := record.Reference.Edges[0]

			if edge.Linkage != tt.want {
				t.Fatalf("edge.Linkage = %#v, want %#v", edge.Linkage, tt.want)
			}
			wantProvenanceLinkage := recordcontract.LinkageProvenance{
				JobID: tt.want.JobID, ProductKey: tt.want.ProductKey, StepRef: tt.want.StepRef,
			}
			if record.Provenance.Linkage != wantProvenanceLinkage {
				t.Fatalf("record.Provenance.Linkage = %#v, want %#v", record.Provenance.Linkage, wantProvenanceLinkage)
			}
		})
	}
}

func TestMapReferenceTraversalLinkageConflictRejected(t *testing.T) {
	tests := []struct {
		name              string
		mappingLinkage    recordmap.ReferenceTraversalMappingLinkage
		provenanceLinkage recordcontract.LinkageProvenance
	}{
		{
			name:              "conflicting jobId",
			mappingLinkage:    recordmap.ReferenceTraversalMappingLinkage{JobID: "job-a"},
			provenanceLinkage: recordcontract.LinkageProvenance{JobID: "job-b"},
		},
		{
			name:              "conflicting productKey",
			mappingLinkage:    recordmap.ReferenceTraversalMappingLinkage{ProductKey: "product-a"},
			provenanceLinkage: recordcontract.LinkageProvenance{ProductKey: "product-b"},
		},
		{
			name:              "conflicting stepRef",
			mappingLinkage:    recordmap.ReferenceTraversalMappingLinkage{StepRef: "step-a"},
			provenanceLinkage: recordcontract.LinkageProvenance{StepRef: "step-b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validReferenceTraversalInput()
			input.Linkage = tt.mappingLinkage
			input.Provenance.Linkage = tt.provenanceLinkage

			record, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
			if record != nil {
				t.Fatalf("record = %#v, want nil", record)
			}
			assertInvalidReferenceTraversalMapping(t, err)
		})
	}
}

// --- 28: canonical traversal evidence digest resolution ----------------------

func TestMapReferenceTraversalCanonicalEvidenceDigestResolution(t *testing.T) {
	t.Run("input digest only", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.EvidenceDigestSHA256 = referenceTraversalDigestA()

		record := mapReferenceTraversalRecord(t, input)
		if record.Reference.Edges[0].Evidence.DigestSHA256 != referenceTraversalDigestA() {
			t.Fatalf("edge digest = %q, want %q", record.Reference.Edges[0].Evidence.DigestSHA256, referenceTraversalDigestA())
		}
		if count := countReferenceTraversalEvidence(record.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef()); count != 1 {
			t.Fatalf("canonical evidence count = %d, want 1: %#v", count, record.Provenance.Evidence)
		}
		if !hasReferenceTraversalEvidence(record.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef(), referenceTraversalDigestA()) {
			t.Fatalf("Provenance.Evidence = %#v, want digest %q", record.Provenance.Evidence, referenceTraversalDigestA())
		}
	})

	// Regression guard: before canonicalization, the mapper only used the caller's
	// EvidenceDigestSHA256 field for edges and ignored an already-supplied canonical
	// traversal provenance evidence digest.
	t.Run("provenance digest only propagates to every edge", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.EvidenceDigestSHA256 = ""
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestA()},
		}

		record := mapReferenceTraversalRecord(t, input)
		if record.Reference.Edges[0].Evidence.DigestSHA256 != referenceTraversalDigestA() {
			t.Fatalf("edge digest = %q, want %q (provenance-only digest must propagate to edges)", record.Reference.Edges[0].Evidence.DigestSHA256, referenceTraversalDigestA())
		}
		if !hasReferenceTraversalEvidence(record.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef(), referenceTraversalDigestA()) {
			t.Fatalf("Provenance.Evidence = %#v, want digest %q", record.Provenance.Evidence, referenceTraversalDigestA())
		}
	})

	t.Run("equal input and provenance digest", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.EvidenceDigestSHA256 = referenceTraversalDigestA()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestA()},
		}

		record := mapReferenceTraversalRecord(t, input)
		if record.Reference.Edges[0].Evidence.DigestSHA256 != referenceTraversalDigestA() {
			t.Fatalf("edge digest = %q, want %q", record.Reference.Edges[0].Evidence.DigestSHA256, referenceTraversalDigestA())
		}
		if count := countReferenceTraversalEvidence(record.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef()); count != 1 {
			t.Fatalf("canonical evidence count = %d, want 1: %#v", count, record.Provenance.Evidence)
		}
	})

	t.Run("duplicate canonical evidence with equal digest collapses", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestA()},
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestA()},
		}

		record := mapReferenceTraversalRecord(t, input)
		if count := countReferenceTraversalEvidence(record.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef()); count != 1 {
			t.Fatalf("canonical evidence count = %d, want exactly 1: %#v", count, record.Provenance.Evidence)
		}
		if !hasReferenceTraversalEvidence(record.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef(), referenceTraversalDigestA()) {
			t.Fatalf("Provenance.Evidence = %#v, want digest %q", record.Provenance.Evidence, referenceTraversalDigestA())
		}
	})

	t.Run("duplicate canonical evidence, one empty and one equal digest, resolves canonically", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: ""},
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestA()},
		}

		record := mapReferenceTraversalRecord(t, input)
		if count := countReferenceTraversalEvidence(record.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef()); count != 1 {
			t.Fatalf("canonical evidence count = %d, want exactly 1: %#v", count, record.Provenance.Evidence)
		}
		if !hasReferenceTraversalEvidence(record.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef(), referenceTraversalDigestA()) {
			t.Fatalf("Provenance.Evidence = %#v, want resolved digest %q", record.Provenance.Evidence, referenceTraversalDigestA())
		}
		if record.Reference.Edges[0].Evidence.DigestSHA256 != referenceTraversalDigestA() {
			t.Fatalf("edge digest = %q, want %q", record.Reference.Edges[0].Evidence.DigestSHA256, referenceTraversalDigestA())
		}
	})

	t.Run("input and provenance digest conflict fails", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.EvidenceDigestSHA256 = referenceTraversalDigestA()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestB()},
		}

		record, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		if record != nil {
			t.Fatalf("record = %#v, want nil", record)
		}
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("provenance and provenance digest conflict fails", func(t *testing.T) {
		input := validReferenceTraversalInput()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestA()},
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestB()},
		}

		record, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		if record != nil {
			t.Fatalf("record = %#v, want nil", record)
		}
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("empty digest remains valid", func(t *testing.T) {
		record := mapReferenceTraversalRecord(t, validReferenceTraversalInput())
		if record.Reference.Edges[0].Evidence.DigestSHA256 != "" {
			t.Fatalf("edge digest = %q, want empty", record.Reference.Edges[0].Evidence.DigestSHA256)
		}
		if !hasReferenceTraversalEvidence(record.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef(), "") {
			t.Fatalf("Provenance.Evidence = %#v, want empty canonical digest", record.Provenance.Evidence)
		}
	})
}

func TestMapReferenceTraversalEveryEdgeSharesCanonicalEvidence(t *testing.T) {
	input := validReferenceTraversalInput()
	thirdNode := referenceTraversalObjectNode("object:raw-runtime-node-third", "Assemblies/Main.FCStd", "Washer")
	input.Traversal.Nodes = append(input.Traversal.Nodes, thirdNode)
	input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{
		referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-target", "document_internal_reference", "resolved"),
		referenceTraversalEdge("object:raw-runtime-node-source", "object:raw-runtime-node-third", "document_internal_reference", "resolved"),
	}
	input.EvidenceDigestSHA256 = referenceTraversalDigestA()

	record := mapReferenceTraversalRecord(t, input)
	if len(record.Reference.Edges) != 2 {
		t.Fatalf("len(Edges) = %d, want 2 distinct normalized edges", len(record.Reference.Edges))
	}
	for i, edge := range record.Reference.Edges {
		if edge.Evidence.SourceKind != "reference-traversal" ||
			edge.Evidence.SourceRef != referenceTraversalCanonicalRef() ||
			edge.Evidence.DigestSHA256 != referenceTraversalDigestA() {
			t.Fatalf("edges[%d].Evidence = %#v, want canonical evidence with digest %q", i, edge.Evidence, referenceTraversalDigestA())
		}
	}
}

// --- 29: zero-edge canonical context validation -------------------------------

func TestMapReferenceTraversalZeroEdgeCanonicalContextValidation(t *testing.T) {
	zeroEdgeInput := func() recordmap.ReferenceTraversalMappingInput {
		input := validReferenceTraversalInput()
		input.Traversal.Edges = []recordmap.ReferenceTraversalRuntimeEdge{}
		return input
	}

	t.Run("valid one-sided linkage and provenance-only digest returns nil record no error", func(t *testing.T) {
		input := zeroEdgeInput()
		input.Linkage = recordmap.ReferenceTraversalMappingLinkage{JobID: "job-a", ProductKey: "product-a", StepRef: "step-a"}
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestA()},
		}

		record, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if record != nil {
			t.Fatalf("record = %#v, want nil", record)
		}
	})

	t.Run("linkage conflict fails even with zero edges", func(t *testing.T) {
		input := zeroEdgeInput()
		input.Linkage = recordmap.ReferenceTraversalMappingLinkage{JobID: "job-a"}
		input.Provenance.Linkage = recordcontract.LinkageProvenance{JobID: "job-b"}

		record, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		if record != nil {
			t.Fatalf("record = %#v, want nil", record)
		}
		assertInvalidReferenceTraversalMapping(t, err)
	})

	t.Run("digest conflict fails even with zero edges", func(t *testing.T) {
		input := zeroEdgeInput()
		input.EvidenceDigestSHA256 = referenceTraversalDigestA()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestB()},
		}

		record, err := recordmap.MapReferenceTraversalToReferenceRecord(input)
		if record != nil {
			t.Fatalf("record = %#v, want nil", record)
		}
		assertInvalidReferenceTraversalMapping(t, err)
	})
}

// --- 30: unrelated provenance evidence preservation ---------------------------

func TestMapReferenceTraversalUnrelatedProvenanceEvidencePreservedExactlyOnce(t *testing.T) {
	input := validReferenceTraversalInput()
	input.EvidenceDigestSHA256 = referenceTraversalDigestA()
	input.Provenance.Evidence = []recordcontract.EvidenceReference{
		{Kind: "observed", Ref: "raw/runtime/parametron.observed.json", DigestSHA256: referenceTraversalDigestB()},
		{Kind: "report", Ref: "raw/report.json"},
	}

	record := mapReferenceTraversalRecord(t, input)
	if !hasReferenceTraversalEvidence(record.Provenance.Evidence, "observed", "raw/runtime/parametron.observed.json", referenceTraversalDigestB()) {
		t.Fatalf("unrelated observed evidence not preserved: %#v", record.Provenance.Evidence)
	}
	if !hasReferenceTraversalEvidence(record.Provenance.Evidence, "report", "raw/report.json", "") {
		t.Fatalf("unrelated report evidence not preserved: %#v", record.Provenance.Evidence)
	}
	if count := countReferenceTraversalEvidence(record.Provenance.Evidence, "reference-traversal", referenceTraversalCanonicalRef()); count != 1 {
		t.Fatalf("canonical evidence count = %d, want exactly 1: %#v", count, record.Provenance.Evidence)
	}
	if len(record.Provenance.Evidence) != 3 {
		t.Fatalf("len(Provenance.Evidence) = %d, want 3 (2 unrelated + 1 canonical): %#v", len(record.Provenance.Evidence), record.Provenance.Evidence)
	}
}

// --- 31: canonical-context equivalence (mapper-level, not repeated-run) -------

// TestMapReferenceTraversalEquivalentCanonicalContextsProduceEqualRecords proves that
// two different accepted representations of the same canonical linkage/evidence
// resolve to an identical normalized record identity, provenance, and summary.
// This is pure mapper canonicalization coverage: it does not exercise repeated
// FreeCAD/CLI/Executor runs and does not complete the later Phase 4 item that
// adds repeated normal-run determinism tests for reference record output.
func TestMapReferenceTraversalEquivalentCanonicalContextsProduceEqualRecords(t *testing.T) {
	inputA := validReferenceTraversalInput()
	inputA.Linkage = recordmap.ReferenceTraversalMappingLinkage{
		JobID: "job-a", ProductKey: "product-a", StepRef: "step-a",
	}
	inputA.EvidenceDigestSHA256 = referenceTraversalDigestA()

	inputB := validReferenceTraversalInput()
	inputB.Provenance.Linkage = recordcontract.LinkageProvenance{
		JobID: "job-a", ProductKey: "product-a", StepRef: "step-a",
	}
	inputB.Provenance.Evidence = []recordcontract.EvidenceReference{
		{Kind: "reference-traversal", Ref: referenceTraversalCanonicalRef(), DigestSHA256: referenceTraversalDigestA()},
	}

	recordA := mapReferenceTraversalRecord(t, inputA)
	recordB := mapReferenceTraversalRecord(t, inputB)

	if recordA.Identity != recordB.Identity {
		t.Fatalf("Identity mismatch:\nA: %#v\nB: %#v", recordA.Identity, recordB.Identity)
	}
	if !reflect.DeepEqual(recordA.Provenance, recordB.Provenance) {
		t.Fatalf("Provenance mismatch:\nA: %#v\nB: %#v", recordA.Provenance, recordB.Provenance)
	}
	if !reflect.DeepEqual(recordA.Reference, recordB.Reference) {
		t.Fatalf("Reference mismatch:\nA: %#v\nB: %#v", recordA.Reference, recordB.Reference)
	}
}
