package recordmap

import (
	"fmt"
	"strings"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
)

const (
	referenceTraversalSchemaVersion = "2.0"
	referenceTraversalEvidenceKind  = "reference-traversal"
)

var referenceTraversalEvidenceRef = recordpackage.RawRuntimeReferenceTraversalContractPath()

// ReferenceTraversalRuntimeEvidence carries supplied FreeCAD schema-2 traversal evidence.
type ReferenceTraversalRuntimeEvidence struct {
	SchemaVersion  string                                `json:"schemaVersion"`
	Kind           string                                `json:"kind"`
	Boundary       string                                `json:"boundary"`
	Operation      string                                `json:"operation"`
	Status         string                                `json:"status"`
	SourceDocument string                                `json:"sourceDocument"`
	Nodes          []ReferenceTraversalRuntimeNode       `json:"nodes"`
	Edges          []ReferenceTraversalRuntimeEdge       `json:"edges"`
	Diagnostics    []ReferenceTraversalRuntimeDiagnostic `json:"diagnostics"`
}

// ReferenceTraversalRuntimeNode carries one raw traversal node.
type ReferenceTraversalRuntimeNode struct {
	Sequence     int     `json:"sequence"`
	ID           string  `json:"id"`
	Kind         string  `json:"kind"`
	State        string  `json:"state"`
	DocumentPath string  `json:"documentPath"`
	ObjectName   *string `json:"objectName"`
	ObjectType   *string `json:"objectType"`
	Label        *string `json:"label"`
	Diagnostic   *string `json:"diagnostic"`
}

// ReferenceTraversalRuntimeEdge carries one directed raw traversal edge.
type ReferenceTraversalRuntimeEdge struct {
	Sequence           int     `json:"sequence"`
	Source             string  `json:"source"`
	Target             string  `json:"target"`
	Kind               string  `json:"kind"`
	SourceProperty     *string `json:"sourceProperty"`
	ReferenceMechanism *string `json:"referenceMechanism"`
	State              string  `json:"state"`
	Diagnostic         *string `json:"diagnostic"`
}

// ReferenceTraversalRuntimeDiagnostic carries one structured raw diagnostic.
type ReferenceTraversalRuntimeDiagnostic struct {
	Sequence int     `json:"sequence"`
	Severity string  `json:"severity"`
	Code     string  `json:"code"`
	Message  string  `json:"message"`
	Stage    *string `json:"stage"`
}

// ReferenceTraversalMappingLinkage carries caller-supplied deterministic linkage.
type ReferenceTraversalMappingLinkage struct {
	JobID      string
	ProductKey string
	StepRef    string
}

// ReferenceTraversalMappingInput carries raw traversal evidence and caller-owned identity.
type ReferenceTraversalMappingInput struct {
	Traversal  ReferenceTraversalRuntimeEvidence
	RecordKey  string
	Provenance recordcontract.Provenance
	Linkage    ReferenceTraversalMappingLinkage
	// EvidenceDigestSHA256 is an optional digest of the canonical raw traversal evidence.
	EvidenceDigestSHA256 string
}

// ReferenceTraversalMappingOutput carries an optional normalized reference record.
type ReferenceTraversalMappingOutput struct {
	ReferenceRecord *recordcontract.ReferenceRecord
}

type referenceTraversalMappingContext struct {
	recordKey  string
	provenance recordcontract.Provenance
	linkage    recordcontract.ReferenceLinkage
	evidence   recordcontract.ReferenceEvidence
}

// MapReferenceTraversal converts supplied schema-2 traversal evidence into an optional reference record.
func MapReferenceTraversal(input ReferenceTraversalMappingInput) (ReferenceTraversalMappingOutput, error) {
	record, err := MapReferenceTraversalToReferenceRecord(input)
	if err != nil {
		return ReferenceTraversalMappingOutput{}, err
	}
	return ReferenceTraversalMappingOutput{ReferenceRecord: record}, nil
}

// MapReferenceTraversalToReferenceRecord maps traversal edges into the current normalized contract.
// It returns nil when valid traversal evidence contains no edges.
func MapReferenceTraversalToReferenceRecord(input ReferenceTraversalMappingInput) (*recordcontract.ReferenceRecord, error) {
	nodes, err := validateReferenceTraversalMappingInput(input)
	if err != nil {
		return nil, err
	}
	context, err := resolveReferenceTraversalMappingContext(input)
	if err != nil {
		return nil, err
	}
	if len(input.Traversal.Edges) == 0 {
		return nil, nil
	}

	edges := make([]recordcontract.ReferenceEdge, 0, len(input.Traversal.Edges))
	seen := make(map[recordcontract.ReferenceEdge]struct{}, len(input.Traversal.Edges))
	for i, raw := range input.Traversal.Edges {
		edge, err := mapReferenceTraversalEdge(raw, nodes, context)
		if err != nil {
			return nil, fmt.Errorf("%w: edges[%d]: %w", ErrInvalidReferenceTraversalMapping, i, err)
		}
		if _, duplicate := seen[edge]; duplicate {
			continue
		}
		seen[edge] = struct{}{}
		edges = append(edges, edge)
	}

	record, err := recordcontract.BuildReferenceRecord(recordcontract.ReferenceRecordInput{
		RecordKey:  context.recordKey,
		Provenance: context.provenance,
		Reference:  recordcontract.ReferenceSummary{Edges: edges},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: build reference record: %w", ErrInvalidReferenceTraversalMapping, err)
	}
	return &record, nil
}

func validateReferenceTraversalMappingInput(input ReferenceTraversalMappingInput) (map[string]ReferenceTraversalRuntimeNode, error) {
	if strings.TrimSpace(input.RecordKey) == "" {
		return nil, fmt.Errorf("%w: record key is required", ErrInvalidReferenceTraversalMapping)
	}
	if input.Traversal.SchemaVersion != referenceTraversalSchemaVersion {
		return nil, fmt.Errorf("%w: unsupported traversal schema version %q", ErrInvalidReferenceTraversalMapping, input.Traversal.SchemaVersion)
	}
	if err := validateReferenceTraversalDigest(input.EvidenceDigestSHA256); err != nil {
		return nil, err
	}
	if input.Traversal.Nodes == nil {
		return nil, fmt.Errorf("%w: nodes are required", ErrInvalidReferenceTraversalMapping)
	}
	if input.Traversal.Edges == nil {
		return nil, fmt.Errorf("%w: edges are required", ErrInvalidReferenceTraversalMapping)
	}

	nodes := make(map[string]ReferenceTraversalRuntimeNode, len(input.Traversal.Nodes))
	for i, node := range input.Traversal.Nodes {
		if err := validateReferenceTraversalNode(node); err != nil {
			return nil, fmt.Errorf("%w: nodes[%d]: %w", ErrInvalidReferenceTraversalMapping, i, err)
		}
		if _, exists := nodes[node.ID]; exists {
			return nil, fmt.Errorf("%w: nodes[%d]: duplicate node id %q", ErrInvalidReferenceTraversalMapping, i, node.ID)
		}
		nodes[node.ID] = node
	}
	for i, edge := range input.Traversal.Edges {
		if err := validateReferenceTraversalEdge(edge, nodes); err != nil {
			return nil, fmt.Errorf("%w: edges[%d]: %w", ErrInvalidReferenceTraversalMapping, i, err)
		}
	}
	return nodes, nil
}

func validateReferenceTraversalNode(node ReferenceTraversalRuntimeNode) error {
	if !isExactNonEmpty(node.ID) {
		return fmt.Errorf("node id must be an exact non-empty string")
	}
	if !isExactNonEmpty(node.DocumentPath) {
		return fmt.Errorf("documentPath must be an exact non-empty string")
	}
	if _, err := mapReferenceTraversalResolution(node.State); err != nil {
		return fmt.Errorf("node state: %w", err)
	}
	switch node.Kind {
	case "object":
		if node.ObjectName == nil || !isExactNonEmpty(*node.ObjectName) {
			return fmt.Errorf("objectName is required for object nodes")
		}
	case "document", "external_document", "external_file":
		if node.ObjectName != nil {
			return fmt.Errorf("objectName must be absent for %s nodes", node.Kind)
		}
	default:
		return fmt.Errorf("unsupported node kind %q", node.Kind)
	}
	return nil
}

func validateReferenceTraversalEdge(edge ReferenceTraversalRuntimeEdge, nodes map[string]ReferenceTraversalRuntimeNode) error {
	if !isExactNonEmpty(edge.Source) {
		return fmt.Errorf("source must be an exact non-empty node id")
	}
	if !isExactNonEmpty(edge.Target) {
		return fmt.Errorf("target must be an exact non-empty node id")
	}
	if _, ok := nodes[edge.Source]; !ok {
		return fmt.Errorf("source node %q is not supplied", edge.Source)
	}
	if _, ok := nodes[edge.Target]; !ok {
		return fmt.Errorf("target node %q is not supplied", edge.Target)
	}
	if _, err := mapReferenceTraversalKind(edge.Kind); err != nil {
		return err
	}
	if _, err := mapReferenceTraversalResolution(edge.State); err != nil {
		return err
	}
	return nil
}

func mapReferenceTraversalEdge(raw ReferenceTraversalRuntimeEdge, nodes map[string]ReferenceTraversalRuntimeNode, context referenceTraversalMappingContext) (recordcontract.ReferenceEdge, error) {
	kind, err := mapReferenceTraversalKind(raw.Kind)
	if err != nil {
		return recordcontract.ReferenceEdge{}, err
	}
	resolution, err := mapReferenceTraversalResolution(raw.State)
	if err != nil {
		return recordcontract.ReferenceEdge{}, err
	}
	return recordcontract.ReferenceEdge{
		Kind:       kind,
		Source:     referenceTraversalEndpoint(nodes[raw.Source]),
		Target:     referenceTraversalEndpoint(nodes[raw.Target]),
		Resolution: resolution,
		Linkage:    context.linkage,
		Evidence:   context.evidence,
	}, nil
}

func referenceTraversalEndpoint(node ReferenceTraversalRuntimeNode) recordcontract.ReferenceEndpoint {
	endpoint := recordcontract.ReferenceEndpoint{Path: node.DocumentPath}
	if node.Kind == "object" {
		endpoint.Name = *node.ObjectName
	}
	return endpoint
}

func mapReferenceTraversalKind(kind string) (recordcontract.ReferenceKind, error) {
	switch kind {
	case "document_internal_reference":
		return recordcontract.ReferenceKindComponent, nil
	case "external_document_reference", "external_file_reference":
		return recordcontract.ReferenceKindExternal, nil
	default:
		return "", fmt.Errorf("unsupported edge kind %q", kind)
	}
}

func mapReferenceTraversalResolution(state string) (recordcontract.ReferenceResolutionState, error) {
	switch state {
	case "resolved":
		return recordcontract.ReferenceResolutionResolved, nil
	case "missing", "unresolved", "skipped", "failed":
		return recordcontract.ReferenceResolutionUnresolved, nil
	default:
		return "", fmt.Errorf("unsupported traversal state %q", state)
	}
}

func resolveReferenceTraversalMappingContext(input ReferenceTraversalMappingInput) (referenceTraversalMappingContext, error) {
	linkage, err := resolveReferenceTraversalLinkage(input.Linkage, input.Provenance.Linkage)
	if err != nil {
		return referenceTraversalMappingContext{}, err
	}
	provenance, digest, err := resolveReferenceTraversalProvenance(input.Provenance, input.EvidenceDigestSHA256, linkage)
	if err != nil {
		return referenceTraversalMappingContext{}, err
	}
	return referenceTraversalMappingContext{
		recordKey:  strings.TrimSpace(input.RecordKey),
		provenance: provenance,
		linkage:    linkage,
		evidence: recordcontract.ReferenceEvidence{
			SourceKind:   referenceTraversalEvidenceKind,
			SourceRef:    referenceTraversalEvidenceRef,
			DigestSHA256: digest,
		},
	}, nil
}

func resolveReferenceTraversalLinkage(mapping ReferenceTraversalMappingLinkage, provenance recordcontract.LinkageProvenance) (recordcontract.ReferenceLinkage, error) {
	jobID, err := resolveReferenceTraversalLinkageField("jobId", mapping.JobID, provenance.JobID)
	if err != nil {
		return recordcontract.ReferenceLinkage{}, err
	}
	productKey, err := resolveReferenceTraversalLinkageField("productKey", mapping.ProductKey, provenance.ProductKey)
	if err != nil {
		return recordcontract.ReferenceLinkage{}, err
	}
	stepRef, err := resolveReferenceTraversalLinkageField("stepRef", mapping.StepRef, provenance.StepRef)
	if err != nil {
		return recordcontract.ReferenceLinkage{}, err
	}
	return recordcontract.ReferenceLinkage{JobID: jobID, ProductKey: productKey, StepRef: stepRef}, nil
}

func resolveReferenceTraversalLinkageField(name, mapping, provenance string) (string, error) {
	mapping = strings.TrimSpace(mapping)
	provenance = strings.TrimSpace(provenance)
	if mapping != "" && provenance != "" && mapping != provenance {
		return "", fmt.Errorf("%w: conflicting %s linkage", ErrInvalidReferenceTraversalMapping, name)
	}
	if mapping != "" {
		return mapping, nil
	}
	return provenance, nil
}

func resolveReferenceTraversalProvenance(base recordcontract.Provenance, digest string, linkage recordcontract.ReferenceLinkage) (recordcontract.Provenance, string, error) {
	provenance := copyProvenance(base)
	provenance.Linkage = recordcontract.LinkageProvenance{
		JobID: linkage.JobID, ProductKey: linkage.ProductKey, StepRef: linkage.StepRef,
	}
	resolvedDigest := strings.TrimSpace(digest)
	evidence := make([]recordcontract.EvidenceReference, 0, len(provenance.Evidence)+1)
	for _, item := range provenance.Evidence {
		if strings.TrimSpace(item.Kind) != referenceTraversalEvidenceKind || strings.TrimSpace(item.Ref) != referenceTraversalEvidenceRef {
			evidence = append(evidence, item)
			continue
		}
		existing := strings.TrimSpace(item.DigestSHA256)
		if existing != "" && resolvedDigest != "" && existing != resolvedDigest {
			return recordcontract.Provenance{}, "", fmt.Errorf("%w: conflicting digest for traversal evidence reference", ErrInvalidReferenceTraversalMapping)
		}
		if resolvedDigest == "" {
			resolvedDigest = existing
		}
	}
	evidence = append(evidence, recordcontract.EvidenceReference{Kind: referenceTraversalEvidenceKind, Ref: referenceTraversalEvidenceRef, DigestSHA256: resolvedDigest})
	provenance.Evidence = evidence
	normalized := recordcontract.NormalizeProvenance(provenance)
	if err := recordcontract.ValidateProvenance(normalized); err != nil {
		return recordcontract.Provenance{}, "", fmt.Errorf("%w: validate provenance: %w", ErrInvalidReferenceTraversalMapping, err)
	}
	return normalized, resolvedDigest, nil
}

func validateReferenceTraversalDigest(digest string) error {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil
	}
	if len(digest) != 64 || !isMappingLowerHex64(digest) {
		return fmt.Errorf("%w: evidenceDigestSha256 must be a 64-character lowercase hexadecimal digest", ErrInvalidReferenceTraversalMapping)
	}
	return nil
}

func isExactNonEmpty(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsRune(value, '\x00')
}
