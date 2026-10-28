package recordcontract

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"parametron/internal/engine/resourcebinding"
)

// ReferenceKind is a normalized reference graph edge kind vocabulary value.
type ReferenceKind string

const (
	ReferenceKindDocument  ReferenceKind = "document"
	ReferenceKindComponent ReferenceKind = "component"
	ReferenceKindDrawing   ReferenceKind = "drawing"
	ReferenceKindExternal  ReferenceKind = "external"
)

// ReferenceResolutionState is a normalized reference resolution state vocabulary value.
type ReferenceResolutionState string

const (
	ReferenceResolutionResolved   ReferenceResolutionState = "resolved"
	ReferenceResolutionUnresolved ReferenceResolutionState = "unresolved"
	ReferenceResolutionMissing    ReferenceResolutionState = "missing"
	ReferenceResolutionSkipped    ReferenceResolutionState = "skipped"
	ReferenceResolutionFailed     ReferenceResolutionState = "failed"
)

// ReferenceResource carries the existing declared resource identity tuple.
type ReferenceResource struct {
	ProjectID string                       `json:"projectId"`
	Kind      resourcebinding.ResourceKind `json:"kind"`
	LogicalID string                       `json:"logicalId"`
}

// ReferenceEndpoint identifies a native entity within a declared resource, or
// explicitly retains an unqualified contract-relative target coordinate.
type ReferenceEndpoint struct {
	Resource   *ReferenceResource `json:"resource,omitempty"`
	NativeKind string             `json:"nativeKind,omitempty"`
	// Qualification is declared, undeclared, or unsupported. Resolution describes
	// native evidence independently of membership in the declared inventory.
	Qualification string                   `json:"qualification,omitempty"`
	Resolution    ReferenceResolutionState `json:"resolution,omitempty"`
	ID            string                   `json:"id,omitempty"`
	Name          string                   `json:"name,omitempty"`
	Path          string                   `json:"path,omitempty"`
	AssetID       string                   `json:"assetId,omitempty"`
	RevisionID    string                   `json:"revisionId,omitempty"`
	DigestSHA256  string                   `json:"digestSha256,omitempty"`
}

// ReferenceLinkage captures normalized job/product/step linkage material.
type ReferenceLinkage struct {
	JobID      string `json:"jobId,omitempty"`
	ProductKey string `json:"productKey,omitempty"`
	StepRef    string `json:"stepRef,omitempty"`
}

// ReferenceEvidence captures normalized reference evidence references.
type ReferenceEvidence struct {
	SourceKind   string `json:"sourceKind,omitempty"`
	SourceRef    string `json:"sourceRef,omitempty"`
	DigestSHA256 string `json:"digestSha256,omitempty"`
}

// ReferenceEdge captures one normalized reference graph edge.
type ReferenceEdge struct {
	// NativeKind is a neutral discriminator. The historical coarse component
	// kind does not establish an assembly occurrence or physical instance.
	NativeKind         string  `json:"nativeKind,omitempty"`
	SourceProperty     *string `json:"sourceProperty,omitempty"`
	ReferenceMechanism *string `json:"referenceMechanism,omitempty"`

	Kind       ReferenceKind            `json:"kind"`
	Source     ReferenceEndpoint        `json:"source"`
	Target     ReferenceEndpoint        `json:"target"`
	Role       string                   `json:"role,omitempty"`
	Resolution ReferenceResolutionState `json:"resolution"`
	Linkage    ReferenceLinkage         `json:"linkage"`
	Evidence   ReferenceEvidence        `json:"evidence"`
}

// ReferenceSummary captures normalized reference graph payload material.
type ReferenceSummary struct {
	// Status retains succeeded/partial/failed runtime status. Completeness is
	// complete/incomplete/unavailable for supported scope and resource joins;
	// it does not claim recursive native dependency closure.
	Status       string             `json:"status,omitempty"`
	Completeness string             `json:"completeness,omitempty"`
	Unsupported  bool               `json:"unsupported,omitempty"`
	Source       *ReferenceEndpoint `json:"source,omitempty"`

	Edges []ReferenceEdge `json:"edges"`
}

// ReferenceRecord is the Engine-produced normalized reference record contract payload.
type ReferenceRecord struct {
	Family     Family           `json:"family"`
	Version    string           `json:"version"`
	RecordKey  string           `json:"recordKey"`
	Identity   Identity         `json:"identity"`
	Provenance Provenance       `json:"provenance"`
	Reference  ReferenceSummary `json:"reference"`
}

// ReferenceRecordInput carries caller material used to build a normalized reference record.
type ReferenceRecordInput struct {
	RecordKey  string
	Provenance Provenance
	Reference  ReferenceSummary
}

type canonicalReferenceEndpoint struct {
	Resource      *ReferenceResource       `json:"resource,omitempty"`
	NativeKind    string                   `json:"nativeKind,omitempty"`
	Qualification string                   `json:"qualification,omitempty"`
	Resolution    ReferenceResolutionState `json:"resolution,omitempty"`

	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	Path         string `json:"path,omitempty"`
	AssetID      string `json:"assetId,omitempty"`
	RevisionID   string `json:"revisionId,omitempty"`
	DigestSHA256 string `json:"digestSha256,omitempty"`
}

type canonicalReferenceLinkage struct {
	JobID      string `json:"jobId,omitempty"`
	ProductKey string `json:"productKey,omitempty"`
	StepRef    string `json:"stepRef,omitempty"`
}

type canonicalReferenceEvidence struct {
	SourceKind   string `json:"sourceKind,omitempty"`
	SourceRef    string `json:"sourceRef,omitempty"`
	DigestSHA256 string `json:"digestSha256,omitempty"`
}

type canonicalReferenceEdge struct {
	NativeKind         string  `json:"nativeKind,omitempty"`
	SourceProperty     *string `json:"sourceProperty,omitempty"`
	ReferenceMechanism *string `json:"referenceMechanism,omitempty"`

	Kind       string                     `json:"kind"`
	Source     canonicalReferenceEndpoint `json:"source"`
	Target     canonicalReferenceEndpoint `json:"target"`
	Role       string                     `json:"role,omitempty"`
	Resolution string                     `json:"resolution"`
	Linkage    canonicalReferenceLinkage  `json:"linkage"`
	Evidence   canonicalReferenceEvidence `json:"evidence"`
}

type canonicalReferenceSummary struct {
	Status       string             `json:"status,omitempty"`
	Completeness string             `json:"completeness,omitempty"`
	Unsupported  bool               `json:"unsupported,omitempty"`
	Source       *ReferenceEndpoint `json:"source,omitempty"`

	Edges []canonicalReferenceEdge `json:"edges"`
}

// BuildReferenceRecord normalizes input, derives identity, and returns a validated reference record.
func BuildReferenceRecord(input ReferenceRecordInput) (ReferenceRecord, error) {
	normalized := normalizeReferenceRecordInput(input)

	identity, err := deriveReferenceIdentity(normalized)
	if err != nil {
		return ReferenceRecord{}, err
	}

	record := ReferenceRecord{
		Family:     FamilyReference,
		Version:    CurrentVersion,
		RecordKey:  normalized.RecordKey,
		Identity:   identity,
		Provenance: normalized.Provenance,
		Reference:  normalized.Reference,
	}

	if err := ValidateReferenceRecord(record); err != nil {
		return ReferenceRecord{}, err
	}

	return record, nil
}

// NormalizeReferenceRecord returns a deterministic, copy-safe normalized reference record.
func NormalizeReferenceRecord(record ReferenceRecord) ReferenceRecord {
	return ReferenceRecord{
		Family:     NormalizeFamily(record.Family),
		Version:    trimString(record.Version),
		RecordKey:  trimString(record.RecordKey),
		Identity:   record.Identity,
		Provenance: NormalizeProvenance(record.Provenance),
		Reference:  normalizeReferenceSummary(record.Reference),
	}
}

// ValidateReferenceRecord rejects malformed normalized reference record material.
func ValidateReferenceRecord(record ReferenceRecord) error {
	normalized := NormalizeReferenceRecord(record)

	if normalized.Family != FamilyReference {
		return fmt.Errorf("%w: family must be reference", ErrInvalidReferenceRecord)
	}
	if err := ValidateVersion(normalized.Version); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidReferenceRecord, err)
	}
	if normalized.RecordKey == "" {
		return fmt.Errorf("%w: record key is required", ErrInvalidReferenceRecord)
	}
	if err := ValidateProvenance(normalized.Provenance); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidReferenceRecord, err)
	}
	if err := validateStoredIdentity(normalized.Identity); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidReferenceRecord, err)
	}
	if normalized.Identity.Family != FamilyReference {
		return fmt.Errorf("%w: identity family must be reference", ErrInvalidReferenceRecord)
	}
	if normalized.Identity.Version != CurrentVersion {
		return fmt.Errorf("%w: identity version must be %q", ErrInvalidReferenceRecord, CurrentVersion)
	}
	if err := validateReferenceSummary(normalized.Reference); err != nil {
		return err
	}

	expectedIdentity, err := deriveReferenceIdentity(normalizedReferenceRecordInput{
		RecordKey:  normalized.RecordKey,
		Provenance: normalized.Provenance,
		Reference:  normalized.Reference,
	})
	if err != nil {
		return err
	}
	if normalized.Identity != expectedIdentity {
		return fmt.Errorf("%w: identity does not match derived identity", ErrInvalidReferenceRecord)
	}

	return nil
}

type normalizedReferenceRecordInput struct {
	RecordKey  string
	Provenance Provenance
	Reference  ReferenceSummary
}

func normalizeReferenceRecordInput(input ReferenceRecordInput) normalizedReferenceRecordInput {
	return normalizedReferenceRecordInput{
		RecordKey:  trimString(input.RecordKey),
		Provenance: NormalizeProvenance(input.Provenance),
		Reference:  normalizeReferenceSummary(input.Reference),
	}
}

func normalizeReferenceSummary(summary ReferenceSummary) ReferenceSummary {
	return ReferenceSummary{
		Status: summary.Status, Completeness: summary.Completeness, Unsupported: summary.Unsupported, Source: cloneReferenceEndpoint(summary.Source),
		Edges: normalizeReferenceEdges(summary.Edges),
	}
}

func normalizeReferenceEdges(edges []ReferenceEdge) []ReferenceEdge {
	if len(edges) == 0 {
		return []ReferenceEdge{}
	}

	out := make([]ReferenceEdge, len(edges))
	for i, edge := range edges {
		out[i] = normalizeReferenceEdge(edge)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].NativeKind == "" && out[j].NativeKind == "" {
			if n := slices.Compare(legacyReferenceOrder(out[i]), legacyReferenceOrder(out[j])); n != 0 {
				return n < 0
			}
		}
		left, _ := referenceEdgeKey(out[i])
		right, _ := referenceEdgeKey(out[j])
		if left != right {
			return left < right
		}
		l, _ := json.Marshal(canonicalReferenceEdgeFromNormalized(out[i]))
		r, _ := json.Marshal(canonicalReferenceEdgeFromNormalized(out[j]))
		return string(l) < string(r)
	})

	return out
}

func normalizeReferenceEdge(edge ReferenceEdge) ReferenceEdge {
	if edge.NativeKind != "" {
		edge.Source = normalizeReferenceEndpoint(edge.Source)
		edge.Target = normalizeReferenceEndpoint(edge.Target)
		edge.SourceProperty = cloneReferenceString(edge.SourceProperty)
		edge.ReferenceMechanism = cloneReferenceString(edge.ReferenceMechanism)
		edge.Linkage = normalizeReferenceLinkage(edge.Linkage)
		edge.Evidence = normalizeReferenceEvidence(edge.Evidence)
		return edge
	}
	return ReferenceEdge{
		NativeKind: edge.NativeKind, SourceProperty: cloneReferenceString(edge.SourceProperty), ReferenceMechanism: cloneReferenceString(edge.ReferenceMechanism),
		Kind:       NormalizeReferenceKind(edge.Kind),
		Source:     normalizeReferenceEndpoint(edge.Source),
		Target:     normalizeReferenceEndpoint(edge.Target),
		Role:       trimString(edge.Role),
		Resolution: NormalizeReferenceResolutionState(edge.Resolution),
		Linkage:    normalizeReferenceLinkage(edge.Linkage),
		Evidence:   normalizeReferenceEvidence(edge.Evidence),
	}
}

func normalizeReferenceEndpoint(endpoint ReferenceEndpoint) ReferenceEndpoint {
	if endpoint.NativeKind != "" {
		endpoint.Resource = cloneReferenceResource(endpoint.Resource)
		return endpoint
	}
	return ReferenceEndpoint{
		Resource: cloneReferenceResource(endpoint.Resource), Qualification: endpoint.Qualification, Resolution: endpoint.Resolution,
		ID:           trimString(endpoint.ID),
		Name:         trimString(endpoint.Name),
		Path:         trimString(endpoint.Path),
		AssetID:      trimString(endpoint.AssetID),
		RevisionID:   trimString(endpoint.RevisionID),
		DigestSHA256: trimString(endpoint.DigestSHA256),
	}
}

func normalizeReferenceLinkage(linkage ReferenceLinkage) ReferenceLinkage {
	return ReferenceLinkage{
		JobID:      trimString(linkage.JobID),
		ProductKey: trimString(linkage.ProductKey),
		StepRef:    trimString(linkage.StepRef),
	}
}

func normalizeReferenceEvidence(evidence ReferenceEvidence) ReferenceEvidence {
	return ReferenceEvidence{
		SourceKind:   trimString(evidence.SourceKind),
		SourceRef:    trimString(evidence.SourceRef),
		DigestSHA256: trimString(evidence.DigestSHA256),
	}
}

// NormalizeReferenceKind trims and preserves unknown values for validation to reject.
func NormalizeReferenceKind(kind ReferenceKind) ReferenceKind {
	return ReferenceKind(strings.TrimSpace(string(kind)))
}

// NormalizeReferenceResolutionState trims and preserves unknown values for validation to reject.
func NormalizeReferenceResolutionState(state ReferenceResolutionState) ReferenceResolutionState {
	return ReferenceResolutionState(strings.TrimSpace(string(state)))
}

func knownReferenceKind(kind ReferenceKind) bool {
	switch NormalizeReferenceKind(kind) {
	case ReferenceKindDocument, ReferenceKindComponent, ReferenceKindDrawing, ReferenceKindExternal:
		return true
	default:
		return false
	}
}

func knownReferenceResolutionState(state ReferenceResolutionState) bool {
	switch state {
	case ReferenceResolutionResolved, ReferenceResolutionUnresolved, ReferenceResolutionMissing, ReferenceResolutionSkipped, ReferenceResolutionFailed:
		return true
	default:
		return false
	}
}

func hasReferenceEndpointIdentity(endpoint ReferenceEndpoint) bool {
	return endpoint.Resource != nil || endpoint.ID != "" ||
		endpoint.Path != "" ||
		endpoint.AssetID != "" ||
		endpoint.Name != ""
}

func validateReferenceSummary(summary ReferenceSummary) error {
	if len(summary.Edges) == 0 && summary.Status == "" {
		return fmt.Errorf("%w: reference must contain at least one edge", ErrInvalidReferenceRecord)
	}

	if err := validateReferenceTraversalSummary(summary); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(summary.Edges))
	for i, edge := range summary.Edges {
		if err := validateReferenceEdge(edge, i); err != nil {
			return err
		}

		key, err := referenceEdgeKey(edge)
		if err != nil {
			return err
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: edge %d is a duplicate", ErrInvalidReferenceRecord, i)
		}
		seen[key] = struct{}{}
	}

	return nil
}

func validateReferenceEdge(edge ReferenceEdge, index int) error {
	if err := validateNativeReferenceEdge(edge); err != nil {
		return err
	}

	if edge.Kind == "" {
		return fmt.Errorf("%w: edge %d kind is required", ErrInvalidReferenceRecord, index)
	}
	if !knownReferenceKind(edge.Kind) {
		return fmt.Errorf("%w: edge %d kind %q is invalid", ErrInvalidReferenceRecord, index, edge.Kind)
	}
	if edge.Resolution == "" {
		return fmt.Errorf("%w: edge %d resolution is required", ErrInvalidReferenceRecord, index)
	}
	if !knownReferenceResolutionState(edge.Resolution) {
		return fmt.Errorf("%w: edge %d resolution %q is invalid", ErrInvalidReferenceRecord, index, edge.Resolution)
	}
	if !hasReferenceEndpointIdentity(edge.Source) {
		return fmt.Errorf("%w: edge %d source id, path, assetId, or name is required", ErrInvalidReferenceRecord, index)
	}
	if !hasReferenceEndpointIdentity(edge.Target) {
		return fmt.Errorf("%w: edge %d target id, path, assetId, or name is required", ErrInvalidReferenceRecord, index)
	}
	if err := validateReferenceEndpointDigests(edge.Source, index, "source"); err != nil {
		return err
	}
	if err := validateReferenceEndpointDigests(edge.Target, index, "target"); err != nil {
		return err
	}
	if err := validateReferenceEvidence(edge.Evidence, index); err != nil {
		return err
	}

	return nil
}

func validateReferenceEndpointDigests(endpoint ReferenceEndpoint, edgeIndex int, side string) error {
	if err := validateReferenceDigestSHA256(
		fmt.Sprintf("edge %d %s.digestSha256", edgeIndex, side),
		endpoint.DigestSHA256,
	); err != nil {
		return err
	}
	return nil
}

func validateReferenceEvidence(evidence ReferenceEvidence, edgeIndex int) error {
	if err := validateReferenceDigestSHA256(
		fmt.Sprintf("edge %d evidence.digestSha256", edgeIndex),
		evidence.DigestSHA256,
	); err != nil {
		return err
	}
	if evidence.SourceKind != "" && evidence.SourceRef == "" {
		return fmt.Errorf("%w: edge %d evidence.sourceRef is required when sourceKind is present", ErrInvalidReferenceRecord, edgeIndex)
	}
	if evidence.SourceRef != "" && evidence.SourceKind == "" {
		return fmt.Errorf("%w: edge %d evidence.sourceKind is required when sourceRef is present", ErrInvalidReferenceRecord, edgeIndex)
	}
	return nil
}

func validateReferenceDigestSHA256(field, digest string) error {
	if digest == "" {
		return nil
	}
	if len(digest) != 64 || !isLowerHex64(digest) {
		return fmt.Errorf("%w: %s must be a 64-character lowercase hexadecimal digest", ErrInvalidReferenceRecord, field)
	}
	return nil
}

func deriveReferenceIdentity(input normalizedReferenceRecordInput) (Identity, error) {
	parts, err := referenceIdentityParts(input)
	if err != nil {
		return Identity{}, err
	}

	identity, err := DeriveIdentity(IdentityInput{
		Family:     FamilyReference,
		Version:    CurrentVersion,
		RecordKey:  input.RecordKey,
		Provenance: input.Provenance,
		Parts:      parts,
	})
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrInvalidReferenceRecord, err)
	}

	return identity, nil
}

func referenceIdentityParts(input normalizedReferenceRecordInput) ([]IdentityPart, error) {
	referenceJSON, err := json.Marshal(canonicalReferenceSummaryFromNormalized(input.Reference))
	if err != nil {
		return nil, fmt.Errorf("%w: marshal reference identity material: %v", ErrInvalidReferenceRecord, err)
	}

	return []IdentityPart{
		{Key: "reference", Value: string(referenceJSON)},
	}, nil
}

func canonicalReferenceSummaryFromNormalized(summary ReferenceSummary) canonicalReferenceSummary {
	return canonicalReferenceSummary{
		Status: summary.Status, Completeness: summary.Completeness, Unsupported: summary.Unsupported, Source: cloneReferenceEndpoint(summary.Source),
		Edges: canonicalReferenceEdgesFromNormalized(summary.Edges),
	}
}

func canonicalReferenceEdgesFromNormalized(edges []ReferenceEdge) []canonicalReferenceEdge {
	out := make([]canonicalReferenceEdge, len(edges))
	for i, edge := range edges {
		out[i] = canonicalReferenceEdgeFromNormalized(edge)
	}
	return out
}

func canonicalReferenceEdgeFromNormalized(edge ReferenceEdge) canonicalReferenceEdge {
	return canonicalReferenceEdge{
		NativeKind: edge.NativeKind, SourceProperty: cloneReferenceString(edge.SourceProperty), ReferenceMechanism: cloneReferenceString(edge.ReferenceMechanism),
		Kind:       string(edge.Kind),
		Source:     canonicalReferenceEndpointFromNormalized(edge.Source),
		Target:     canonicalReferenceEndpointFromNormalized(edge.Target),
		Role:       edge.Role,
		Resolution: string(edge.Resolution),
		Linkage: canonicalReferenceLinkage{
			JobID:      edge.Linkage.JobID,
			ProductKey: edge.Linkage.ProductKey,
			StepRef:    edge.Linkage.StepRef,
		},
		Evidence: canonicalReferenceEvidence{
			SourceKind:   edge.Evidence.SourceKind,
			SourceRef:    edge.Evidence.SourceRef,
			DigestSHA256: edge.Evidence.DigestSHA256,
		},
	}
}

func canonicalReferenceEndpointFromNormalized(endpoint ReferenceEndpoint) canonicalReferenceEndpoint {
	return canonicalReferenceEndpoint{
		Resource: cloneReferenceResource(endpoint.Resource), NativeKind: endpoint.NativeKind, Qualification: endpoint.Qualification, Resolution: endpoint.Resolution,
		ID:           endpoint.ID,
		Name:         endpoint.Name,
		Path:         endpoint.Path,
		AssetID:      endpoint.AssetID,
		RevisionID:   endpoint.RevisionID,
		DigestSHA256: endpoint.DigestSHA256,
	}
}

func referenceEdgeDuplicateKey(edge ReferenceEdge) (string, error) {
	data, err := json.Marshal(canonicalReferenceEdgeFromNormalized(edge))
	if err != nil {
		return "", fmt.Errorf("%w: marshal edge duplicate key material: %v", ErrInvalidReferenceRecord, err)
	}
	return string(data), nil
}

func cloneReferenceString(v *string) *string {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}
func cloneReferenceResource(v *ReferenceResource) *ReferenceResource {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}
func cloneReferenceEndpoint(v *ReferenceEndpoint) *ReferenceEndpoint {
	if v == nil {
		return nil
	}
	copy := normalizeReferenceEndpoint(*v)
	return &copy
}
func exactReferenceString(v string) bool {
	return v != "" && strings.TrimSpace(v) == v && !strings.ContainsRune(v, '\x00')
}
func invalidReference(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidReferenceRecord, message)
}

func validateNativeReferenceEndpoint(e ReferenceEndpoint) error {
	switch e.NativeKind {
	case "object":
		if !exactReferenceString(e.Name) {
			return invalidReference("native object name is required")
		}
	case "document", "external_document", "external_file":
		if e.Name != "" {
			return invalidReference("non-object endpoint has object name")
		}
	default:
		return invalidReference("invalid native entity kind")
	}
	if !knownReferenceResolutionState(e.Resolution) {
		return invalidReference("invalid native entity resolution")
	}
	switch e.Qualification {
	case "declared":
		if e.Resource == nil || !exactReferenceString(e.Resource.ProjectID) || !exactReferenceString(e.Resource.LogicalID) || e.Resource.Kind != resourcebinding.ResourceKindModel || e.Path != "" {
			return invalidReference("invalid declared resource endpoint")
		}
	case "undeclared", "unsupported":
		if e.Resource != nil || resourcebinding.ValidateCoordinate(e.Path) != nil {
			return invalidReference("invalid unqualified endpoint")
		}
	default:
		return invalidReference("invalid resource qualification")
	}
	if e.ID != "" || e.AssetID != "" || e.RevisionID != "" || e.DigestSHA256 != "" {
		return invalidReference("native identity cannot use unrelated endpoint identifiers")
	}
	return nil
}

func validateNativeReferenceEdge(e ReferenceEdge) error {
	if e.NativeKind == "" {
		if e.SourceProperty != nil || e.ReferenceMechanism != nil || e.Source.NativeKind != "" || e.Target.NativeKind != "" || e.Source.Resource != nil || e.Target.Resource != nil || e.Source.Qualification != "" || e.Target.Qualification != "" || e.Source.Resolution != "" || e.Target.Resolution != "" {
			return invalidReference("native relationship kind is required")
		}
		return nil
	}
	expected := ReferenceKindExternal
	switch e.NativeKind {
	case "document_internal_reference":
		expected = ReferenceKindComponent
	case "external_document_reference", "external_file_reference":
	default:
		return invalidReference("invalid native relationship kind")
	}
	if e.Kind != expected || e.Role != "" {
		return invalidReference("inconsistent native relationship kind or role")
	}
	if !knownReferenceResolutionState(e.Resolution) {
		return invalidReference("invalid native relationship resolution")
	}
	for _, endpoint := range []ReferenceEndpoint{e.Source, e.Target} {
		if err := validateNativeReferenceEndpoint(endpoint); err != nil {
			return err
		}
	}
	if e.Source.Qualification != "declared" || (e.Resolution == ReferenceResolutionResolved && (e.Source.Resolution != ReferenceResolutionResolved || e.Target.Resolution != ReferenceResolutionResolved)) || (e.Resolution == ReferenceResolutionMissing && e.Target.Resolution == ReferenceResolutionResolved) {
		return invalidReference("inconsistent relationship endpoint state")
	}
	if e.NativeKind == "document_internal_reference" && (e.Target.Resource == nil || *e.Source.Resource != *e.Target.Resource) {
		return invalidReference("internal relationship must remain in one declared resource")
	}
	if e.NativeKind != "document_internal_reference" && e.Target.Resource != nil && *e.Source.Resource == *e.Target.Resource {
		return invalidReference("external relationship must target another resource")
	}
	if e.NativeKind == "external_file_reference" && e.Target.NativeKind != "external_file" {
		return invalidReference("external file relationship requires file endpoint")
	}
	for _, value := range []*string{e.SourceProperty, e.ReferenceMechanism} {
		if value != nil && !exactReferenceString(*value) {
			return invalidReference("invalid native discriminator")
		}
	}
	return nil
}

func validateReferenceTraversalSummary(s ReferenceSummary) error {
	if s.Status == "" {
		if s.Source != nil || s.Completeness != "" || s.Unsupported {
			return invalidReference("traversal status is required")
		}
		for _, edge := range s.Edges {
			if edge.NativeKind != "" {
				return invalidReference("native traversal requires scope status")
			}
		}
		return nil
	}
	if s.Source == nil {
		return invalidReference("traversal source is required")
	}
	if err := validateNativeReferenceEndpoint(*s.Source); err != nil {
		return err
	}
	if s.Source.Qualification != "declared" || s.Source.NativeKind != "document" {
		return invalidReference("traversal source must be a declared document")
	}
	if s.Status != "failed" && s.Source.Resolution != ReferenceResolutionResolved {
		return invalidReference("usable traversal requires resolved source document")
	}
	switch s.Status {
	case "succeeded":
		if s.Completeness != "complete" && s.Completeness != "incomplete" {
			return invalidReference("invalid succeeded completeness")
		}
	case "partial":
		if s.Completeness != "incomplete" {
			return invalidReference("partial traversal must be incomplete")
		}
	case "failed":
		if s.Completeness != "unavailable" || len(s.Edges) != 0 {
			return invalidReference("failed traversal has no usable relationships")
		}
	default:
		return invalidReference("invalid traversal status")
	}
	if s.Unsupported && s.Completeness == "complete" {
		return invalidReference("unsupported scope cannot be complete")
	}
	for _, edge := range s.Edges {
		if edge.NativeKind == "" {
			return invalidReference("traversal requires native relationships")
		}
		if edge.Target.Qualification != "declared" && s.Completeness == "complete" {
			return invalidReference("unqualified target requires incomplete scope")
		}
		if edge.Source.Resource == nil || *edge.Source.Resource != *s.Source.Resource {
			return invalidReference("relationship source disagrees with traversal source")
		}
	}
	return nil
}

// ReferenceRelationshipKey is canonical relationship identity, distinct from
// evidence-sensitive record identity. State, linkage, sequence and evidence digest
// are excluded. Unqualified targets use only accepted contract-relative coordinates.
func ReferenceRelationshipKey(edge ReferenceEdge) (string, error) {
	if edge.NativeKind == "" {
		return "", invalidReference("resource-qualified native relationship is required")
	}
	if err := validateNativeReferenceEdge(edge); err != nil {
		return "", err
	}
	type endpointKey struct {
		Resource      *ReferenceResource `json:"resource,omitempty"`
		Qualification string             `json:"qualification"`
		Kind          string             `json:"kind"`
		Name          string             `json:"name,omitempty"`
		Coordinate    string             `json:"coordinate,omitempty"`
	}
	endpoint := func(e ReferenceEndpoint) endpointKey {
		return endpointKey{e.Resource, e.Qualification, e.NativeKind, e.Name, e.Path}
	}
	value := struct {
		Source    endpointKey `json:"source"`
		Target    endpointKey `json:"target"`
		Kind      string      `json:"kind"`
		Property  *string     `json:"sourceProperty"`
		Mechanism *string     `json:"referenceMechanism"`
	}{endpoint(edge.Source), endpoint(edge.Target), edge.NativeKind, edge.SourceProperty, edge.ReferenceMechanism}
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("%w: relationship identity: %v", ErrInvalidReferenceRecord, err)
	}
	return string(data), nil
}

// Legacy generic reference records retain their established duplicate semantics.
// New traversal records always use the resource-qualified relationship key.
func referenceEdgeKey(edge ReferenceEdge) (string, error) {
	if edge.NativeKind == "" {
		return referenceEdgeDuplicateKey(edge)
	}
	return ReferenceRelationshipKey(edge)
}

func legacyReferenceOrder(e ReferenceEdge) []string {
	return []string{string(e.Kind), e.Source.ID, e.Source.Path, e.Source.AssetID, e.Source.Name,
		e.Target.ID, e.Target.Path, e.Target.AssetID, e.Target.Name, e.Role, string(e.Resolution),
		e.Linkage.ProductKey, e.Linkage.JobID, e.Linkage.StepRef,
		e.Evidence.SourceKind, e.Evidence.SourceRef, e.Evidence.DigestSHA256}
}
