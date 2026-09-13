package recordcontract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
)

// ReferenceEndpoint captures normalized reference graph endpoint identity material.
type ReferenceEndpoint struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	Path         string `json:"path,omitempty"`
	AssetID      string `json:"assetId,omitempty"`
	RevisionID   string `json:"revisionId,omitempty"`
	DigestSHA256 string `json:"digestSha256,omitempty"`
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
	Kind       string                     `json:"kind"`
	Source     canonicalReferenceEndpoint `json:"source"`
	Target     canonicalReferenceEndpoint `json:"target"`
	Role       string                     `json:"role,omitempty"`
	Resolution string                     `json:"resolution"`
	Linkage    canonicalReferenceLinkage  `json:"linkage"`
	Evidence   canonicalReferenceEvidence `json:"evidence"`
}

type canonicalReferenceSummary struct {
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
		left := out[i]
		right := out[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Source.ID != right.Source.ID {
			return left.Source.ID < right.Source.ID
		}
		if left.Source.Path != right.Source.Path {
			return left.Source.Path < right.Source.Path
		}
		if left.Source.AssetID != right.Source.AssetID {
			return left.Source.AssetID < right.Source.AssetID
		}
		if left.Source.Name != right.Source.Name {
			return left.Source.Name < right.Source.Name
		}
		if left.Target.ID != right.Target.ID {
			return left.Target.ID < right.Target.ID
		}
		if left.Target.Path != right.Target.Path {
			return left.Target.Path < right.Target.Path
		}
		if left.Target.AssetID != right.Target.AssetID {
			return left.Target.AssetID < right.Target.AssetID
		}
		if left.Target.Name != right.Target.Name {
			return left.Target.Name < right.Target.Name
		}
		if left.Role != right.Role {
			return left.Role < right.Role
		}
		if left.Resolution != right.Resolution {
			return left.Resolution < right.Resolution
		}
		if left.Linkage.ProductKey != right.Linkage.ProductKey {
			return left.Linkage.ProductKey < right.Linkage.ProductKey
		}
		if left.Linkage.JobID != right.Linkage.JobID {
			return left.Linkage.JobID < right.Linkage.JobID
		}
		if left.Linkage.StepRef != right.Linkage.StepRef {
			return left.Linkage.StepRef < right.Linkage.StepRef
		}
		if left.Evidence.SourceKind != right.Evidence.SourceKind {
			return left.Evidence.SourceKind < right.Evidence.SourceKind
		}
		if left.Evidence.SourceRef != right.Evidence.SourceRef {
			return left.Evidence.SourceRef < right.Evidence.SourceRef
		}
		return left.Evidence.DigestSHA256 < right.Evidence.DigestSHA256
	})

	return out
}

func normalizeReferenceEdge(edge ReferenceEdge) ReferenceEdge {
	return ReferenceEdge{
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
	return ReferenceEndpoint{
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
	switch NormalizeReferenceResolutionState(state) {
	case ReferenceResolutionResolved, ReferenceResolutionUnresolved:
		return true
	default:
		return false
	}
}

func hasReferenceEndpointIdentity(endpoint ReferenceEndpoint) bool {
	return endpoint.ID != "" ||
		endpoint.Path != "" ||
		endpoint.AssetID != "" ||
		endpoint.Name != ""
}

func validateReferenceSummary(summary ReferenceSummary) error {
	if len(summary.Edges) == 0 {
		return fmt.Errorf("%w: reference must contain at least one edge", ErrInvalidReferenceRecord)
	}

	seen := make(map[string]struct{}, len(summary.Edges))
	for i, edge := range summary.Edges {
		if err := validateReferenceEdge(edge, i); err != nil {
			return err
		}

		key, err := referenceEdgeDuplicateKey(edge)
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
