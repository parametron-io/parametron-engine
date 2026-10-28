package recordmap

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/resourcebinding"
)

const (
	referenceTraversalSchemaVersion = "1.0"
	referenceTraversalEvidenceKind  = "reference-traversal"
)

var referenceTraversalEvidenceRef = recordpackage.RawRuntimeReferenceTraversalContractPath()

// ReferenceTraversalRuntimeEvidence carries supplied FreeCAD traversal evidence.
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
	Resources resourcebinding.Inventory
	// ExpectedSourceDocument, when supplied by execution, correlates evidence
	// with the selected prepared source rather than any other staged resource.
	ExpectedSourceDocument string
	Traversal              ReferenceTraversalRuntimeEvidence
	RecordKey              string
	Provenance             recordcontract.Provenance
	Linkage                ReferenceTraversalMappingLinkage
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

// MapReferenceTraversal converts supplied traversal evidence into an optional reference record.
func MapReferenceTraversal(input ReferenceTraversalMappingInput) (ReferenceTraversalMappingOutput, error) {
	record, err := MapReferenceTraversalToReferenceRecord(input)
	if err != nil {
		return ReferenceTraversalMappingOutput{}, err
	}
	return ReferenceTraversalMappingOutput{ReferenceRecord: record}, nil
}

// MapReferenceTraversalToReferenceRecord maps traversal edges into the current normalized contract.
// Empty traversals retain source qualification and scope completeness.
func MapReferenceTraversalToReferenceRecord(input ReferenceTraversalMappingInput) (*recordcontract.ReferenceRecord, error) {
	nodes, err := validateReferenceTraversalMappingInput(input)
	if err != nil {
		return nil, err
	}
	context, err := resolveReferenceTraversalMappingContext(input)
	if err != nil {
		return nil, err
	}
	edges := make([]recordcontract.ReferenceEdge, 0, len(input.Traversal.Edges))
	seen := make(map[string]string, len(input.Traversal.Edges))
	source, err := referenceTraversalEndpoint(ReferenceTraversalRuntimeNode{Kind: "document", State: "resolved", DocumentPath: input.Traversal.SourceDocument}, input.Resources, true)
	if err != nil {
		return nil, fmt.Errorf("%w: source resource: %w", ErrInvalidReferenceTraversalMapping, err)
	}
	summary := recordcontract.ReferenceSummary{Status: input.Traversal.Status, Completeness: "complete", Source: &source}
	if summary.Status == "partial" {
		summary.Completeness = "incomplete"
	}
	if summary.Status == "failed" {
		summary.Completeness = "unavailable"
		// Aggregate failure does not prove that opening the document was attempted.
		summary.Source.Resolution = recordcontract.ReferenceResolutionUnresolved
	}
	for _, diagnostic := range input.Traversal.Diagnostics {
		if diagnostic.Code == "unsupported_reference_value_shape" {
			summary.Unsupported = true
		}
	}
	// Stable processing order also makes conflict selection independent of raw order.
	rawEdges := append([]ReferenceTraversalRuntimeEdge(nil), input.Traversal.Edges...)
	sort.Slice(rawEdges, func(i, j int) bool {
		return traversalEdgeEvidenceKey(rawEdges[i], nodes) < traversalEdgeEvidenceKey(rawEdges[j], nodes)
	})
	for _, raw := range rawEdges {
		edge, err := mapReferenceTraversalEdge(raw, nodes, context, input.Resources)
		if err != nil {
			return nil, fmt.Errorf("%w: relationship: %w", ErrInvalidReferenceTraversalMapping, err)
		}
		key, err := recordcontract.ReferenceRelationshipKey(edge)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidReferenceTraversalMapping, err)
		}
		evidenceKey := traversalEdgeEvidenceKey(raw, nodes)
		if prior, duplicate := seen[key]; duplicate {
			if prior != evidenceKey {
				return nil, fmt.Errorf("%w: conflicting relationship evidence for %s", ErrInvalidReferenceTraversalMapping, key)
			}
			continue
		}
		seen[key] = evidenceKey
		if edge.Target.Qualification != "declared" {
			summary.Completeness = "incomplete"
		}
		if edge.Target.Qualification == "unsupported" {
			summary.Unsupported = true
		}
		edges = append(edges, edge)
	}
	summary.Edges = edges

	record, err := recordcontract.BuildReferenceRecord(recordcontract.ReferenceRecordInput{
		RecordKey:  context.recordKey,
		Provenance: context.provenance,
		Reference:  summary,
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
	if err := validateTraversalScope(input); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidReferenceTraversalMapping, err)
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
	orderedNodes := append([]ReferenceTraversalRuntimeNode(nil), input.Traversal.Nodes...)
	sort.Slice(orderedNodes, func(i, j int) bool {
		left, right := traversalNodeEvidenceKey(orderedNodes[i]), traversalNodeEvidenceKey(orderedNodes[j])
		if left != right {
			return left < right
		}
		return orderedNodes[i].ID < orderedNodes[j].ID
	})
	nativeNodes := make(map[string]string)
	for _, node := range orderedNodes {
		if err := validateReferenceTraversalNode(node, len(input.Traversal.Diagnostics) > 0); err != nil {
			return nil, fmt.Errorf("%w: node: %w", ErrInvalidReferenceTraversalMapping, err)
		}
		if node.Kind == "document" && node.DocumentPath == input.Traversal.SourceDocument && input.Traversal.Status != "failed" && node.State != "resolved" {
			return nil, fmt.Errorf("%w: usable traversal requires resolved source document", ErrInvalidReferenceTraversalMapping)
		}
		if _, exists := nodes[node.ID]; exists {
			if traversalNodeEvidenceKey(nodes[node.ID]) != traversalNodeEvidenceKey(node) {
				return nil, fmt.Errorf("%w: conflicting duplicate node id %q", ErrInvalidReferenceTraversalMapping, node.ID)
			}
			continue
		}
		keyBytes, _ := json.Marshal([]any{node.Kind, node.DocumentPath, node.ObjectName})
		key := string(keyBytes)
		if prior, exists := nativeNodes[key]; exists && prior != traversalNodeEvidenceKey(node) {
			return nil, fmt.Errorf("%w: conflicting native node evidence for %s", ErrInvalidReferenceTraversalMapping, key)
		}
		nativeNodes[key] = traversalNodeEvidenceKey(node)
		nodes[node.ID] = node
	}
	orderedEdges := append([]ReferenceTraversalRuntimeEdge(nil), input.Traversal.Edges...)
	sort.Slice(orderedEdges, func(i, j int) bool {
		return traversalEdgeEvidenceKey(orderedEdges[i], nodes) < traversalEdgeEvidenceKey(orderedEdges[j], nodes)
	})
	for _, edge := range orderedEdges {
		if err := validateReferenceTraversalEdge(edge, nodes, len(input.Traversal.Diagnostics) > 0); err != nil {
			return nil, fmt.Errorf("%w: edge: %w", ErrInvalidReferenceTraversalMapping, err)
		}
		if nodes[edge.Source].DocumentPath != input.Traversal.SourceDocument || (edge.State == "resolved" && nodes[edge.Source].State != "resolved") {
			return nil, fmt.Errorf("%w: relationship source disagrees with sourceDocument or resolved edge", ErrInvalidReferenceTraversalMapping)
		}
	}
	return nodes, nil
}

func validateReferenceTraversalNode(node ReferenceTraversalRuntimeNode, hasScopeDiagnostic bool) error {
	if !isExactNonEmpty(node.ID) {
		return fmt.Errorf("node id must be an exact non-empty string")
	}
	if err := resourcebinding.ValidateCoordinate(node.DocumentPath); err != nil {
		return err
	}
	for _, value := range []*string{node.ObjectType, node.Label, node.Diagnostic} {
		if err := validateTraversalRawText(value); err != nil {
			return err
		}
	}
	if err := validateTraversalDiagnostic(node.Diagnostic); err != nil {
		return err
	}
	if (node.State == "skipped" || node.State == "failed") && node.Diagnostic == nil && !hasScopeDiagnostic {
		return fmt.Errorf("%s node requires diagnostic evidence", node.State)
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

func validateReferenceTraversalEdge(edge ReferenceTraversalRuntimeEdge, nodes map[string]ReferenceTraversalRuntimeNode, hasScopeDiagnostic bool) error {
	for _, value := range []*string{edge.SourceProperty, edge.ReferenceMechanism} {
		if err := validateTraversalOptional(value); err != nil {
			return err
		}
	}
	if err := validateTraversalDiagnostic(edge.Diagnostic); err != nil {
		return err
	}
	if (edge.State == "skipped" || edge.State == "failed") && edge.Diagnostic == nil && !hasScopeDiagnostic {
		return fmt.Errorf("%s edge requires diagnostic evidence", edge.State)
	}
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
	if (edge.State == "resolved" && nodes[edge.Target].State != "resolved") || (edge.State == "missing" && nodes[edge.Target].State == "resolved") {
		return fmt.Errorf("edge and target resolution disagree")
	}
	if edge.Kind == "document_internal_reference" && nodes[edge.Source].DocumentPath != nodes[edge.Target].DocumentPath {
		return fmt.Errorf("internal relationship crosses documents")
	}
	if edge.Kind != "document_internal_reference" && nodes[edge.Source].DocumentPath == nodes[edge.Target].DocumentPath {
		return fmt.Errorf("external relationship targets source document")
	}
	if edge.Kind == "external_file_reference" && nodes[edge.Target].Kind != "external_file" {
		return fmt.Errorf("external file relationship requires file endpoint")
	}
	return nil
}

func mapReferenceTraversalEdge(raw ReferenceTraversalRuntimeEdge, nodes map[string]ReferenceTraversalRuntimeNode, context referenceTraversalMappingContext, resources resourcebinding.Inventory) (recordcontract.ReferenceEdge, error) {
	kind, err := mapReferenceTraversalKind(raw.Kind)
	if err != nil {
		return recordcontract.ReferenceEdge{}, err
	}
	resolution, err := mapReferenceTraversalResolution(raw.State)
	if err != nil {
		return recordcontract.ReferenceEdge{}, err
	}
	source, err := referenceTraversalEndpoint(nodes[raw.Source], resources, true)
	if err != nil {
		return recordcontract.ReferenceEdge{}, err
	}
	target, err := referenceTraversalEndpoint(nodes[raw.Target], resources, false)
	if err != nil {
		return recordcontract.ReferenceEdge{}, err
	}
	return recordcontract.ReferenceEdge{
		NativeKind: raw.Kind, SourceProperty: raw.SourceProperty, ReferenceMechanism: raw.ReferenceMechanism,
		Kind:       kind,
		Source:     source,
		Target:     target,
		Resolution: resolution,
		Linkage:    context.linkage,
		Evidence:   context.evidence,
	}, nil
}

func referenceTraversalEndpoint(node ReferenceTraversalRuntimeNode, resources resourcebinding.Inventory, required bool) (recordcontract.ReferenceEndpoint, error) {
	endpoint := recordcontract.ReferenceEndpoint{NativeKind: node.Kind, Resolution: recordcontract.ReferenceResolutionState(node.State)}
	binding, err := resources.LookupCoordinate(node.DocumentPath, true)
	if err != nil {
		if required {
			return endpoint, err
		}
		endpoint.Qualification = "undeclared"
		if node.Kind == "external_file" {
			endpoint.Qualification = "unsupported"
		}
		endpoint.Path = node.DocumentPath
	} else {
		endpoint.Qualification = "declared"
		endpoint.Resource = &recordcontract.ReferenceResource{ProjectID: binding.ProjectID, Kind: binding.Kind, LogicalID: binding.LogicalID}
	}
	if node.Kind == "object" {
		endpoint.Name = *node.ObjectName
	}
	return endpoint, nil
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
	case "resolved", "missing", "unresolved", "skipped", "failed":
		return recordcontract.ReferenceResolutionState(state), nil
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

func validateTraversalOptional(value *string) error {
	if value != nil && !isExactNonEmpty(*value) {
		return fmt.Errorf("nullable native values must be exact non-empty strings when present")
	}
	return nil
}

func validateTraversalRawText(value *string) error {
	if value != nil && *value == "" {
		return fmt.Errorf("nullable raw text must be non-empty when present")
	}
	return nil
}

func validateTraversalDiagnostic(value *string) error {
	if err := validateTraversalRawText(value); err != nil {
		return err
	}
	if value != nil && strings.Contains(*value, "Traceback (most recent call last):") {
		return fmt.Errorf("raw stack trace is not accepted diagnostic evidence")
	}
	return nil
}

// Evidence comparison deliberately ignores lookup IDs and incidental sequences,
// but retains raw-only fields until duplicate/conflict validation is complete.
func traversalNodeEvidenceKey(node ReferenceTraversalRuntimeNode) string {
	node.Sequence = 0
	node.ID = ""
	data, _ := json.Marshal(node)
	return string(data)
}

func traversalEdgeEvidenceKey(edge ReferenceTraversalRuntimeEdge, nodes map[string]ReferenceTraversalRuntimeNode) string {
	edge.Sequence = 0
	source, target := edge.Source, edge.Target
	edge.Source, edge.Target = "", ""
	if _, ok := nodes[source]; !ok {
		edge.Source = source
	}
	if _, ok := nodes[target]; !ok {
		edge.Target = target
	}
	data, _ := json.Marshal(struct {
		Edge   ReferenceTraversalRuntimeEdge
		Source string
		Target string
	}{edge, traversalNodeEvidenceKey(nodes[source]), traversalNodeEvidenceKey(nodes[target])})
	return string(data)
}

func validateTraversalScope(input ReferenceTraversalMappingInput) error {
	t := input.Traversal
	if t.Kind != "raw_reference_traversal" || t.Boundary != "reference_traversal_entrypoint" || t.Operation != "reference_traversal" {
		return fmt.Errorf("invalid traversal kind, boundary, or operation")
	}
	if err := resourcebinding.ValidateCoordinate(t.SourceDocument); err != nil {
		return err
	}
	if input.ExpectedSourceDocument != "" && input.ExpectedSourceDocument != t.SourceDocument {
		return fmt.Errorf("sourceDocument disagrees with selected prepared source")
	}
	if _, err := input.Resources.LookupCoordinate(t.SourceDocument, true); err != nil {
		return fmt.Errorf("sourceDocument must identify a completed declared resource: %w", err)
	}
	for _, binding := range input.Resources.Bindings() {
		if binding.MaterializedCoordinate == "" {
			return fmt.Errorf("resource inventory must contain completed materialization coordinates")
		}
	}
	if t.Diagnostics == nil {
		return fmt.Errorf("diagnostics are required")
	}
	for _, d := range t.Diagnostics {
		if !isExactNonEmpty(d.Code) || d.Message == "" {
			return fmt.Errorf("invalid traversal diagnostic")
		}
		if err := validateTraversalDiagnostic(&d.Message); err != nil {
			return err
		}
		if err := validateTraversalOptional(d.Stage); err != nil {
			return err
		}
		switch d.Severity {
		case "info", "warning", "error":
		default:
			return fmt.Errorf("invalid traversal diagnostic severity")
		}
		if d.Code == "unsupported_reference_value_shape" && (t.Status != "partial" || d.Severity != "warning" || d.Stage == nil || *d.Stage != "reference_discovery") {
			return fmt.Errorf("unsupported reference evidence requires partial discovery scope")
		}
	}
	switch t.Status {
	case "succeeded":
	case "partial":
		opened := false
		for _, node := range t.Nodes {
			if node.Kind == "document" && node.DocumentPath == t.SourceDocument && node.State == "resolved" {
				opened = true
			}
		}
		if !opened || len(t.Diagnostics) == 0 {
			return fmt.Errorf("partial traversal requires opened source and explanatory diagnostics")
		}
	case "failed":
		if len(t.Edges) != 0 || len(t.Diagnostics) == 0 {
			return fmt.Errorf("failed traversal requires diagnostics and no usable relationships")
		}
	default:
		return fmt.Errorf("invalid traversal status %q", t.Status)
	}
	return nil
}
