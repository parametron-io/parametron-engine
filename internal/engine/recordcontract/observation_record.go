package recordcontract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ObservationKind is a normalized observation fact kind vocabulary value.
type ObservationKind string

const (
	ObservationKindParameter ObservationKind = "parameter"
	ObservationKindMetadata  ObservationKind = "metadata"
	ObservationKindReference ObservationKind = "reference"
	ObservationKindComponent ObservationKind = "component"
)

// ObservationValue captures normalized observation value material.
type ObservationValue struct {
	Kind string `json:"kind,omitempty"`
	Raw  string `json:"raw,omitempty"`
}

// ObservationSubject captures normalized observation subject identity material.
type ObservationSubject struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	GroupID  string `json:"groupId,omitempty"`
	OwnerID  string `json:"ownerId,omitempty"`
	ParentID string `json:"parentId,omitempty"`
}

// ObservationLinkage captures normalized job/product/step linkage material.
type ObservationLinkage struct {
	JobID      string `json:"jobId,omitempty"`
	ProductKey string `json:"productKey,omitempty"`
	StepRef    string `json:"stepRef,omitempty"`
}

// ObservationEvidence captures normalized observation evidence references.
type ObservationEvidence struct {
	SourceKind   string `json:"sourceKind,omitempty"`
	SourceRef    string `json:"sourceRef,omitempty"`
	DigestSHA256 string `json:"digestSha256,omitempty"`
}

// ObservationFact captures one normalized observation fact.
type ObservationFact struct {
	Kind     ObservationKind     `json:"kind"`
	Subject  ObservationSubject  `json:"subject"`
	Key      string              `json:"key,omitempty"`
	Value    ObservationValue    `json:"value"`
	Linkage  ObservationLinkage  `json:"linkage"`
	Evidence ObservationEvidence `json:"evidence"`
}

// ObservationSummary captures normalized observation payload material.
type ObservationSummary struct {
	Facts []ObservationFact `json:"facts"`
}

// ObservationRecord is the Engine-produced normalized observation record contract payload.
type ObservationRecord struct {
	Family      Family             `json:"family"`
	Version     string             `json:"version"`
	RecordKey   string             `json:"recordKey"`
	Identity    Identity           `json:"identity"`
	Provenance  Provenance         `json:"provenance"`
	Observation ObservationSummary `json:"observation"`
}

// ObservationRecordInput carries caller material used to build a normalized observation record.
type ObservationRecordInput struct {
	RecordKey   string
	Provenance  Provenance
	Observation ObservationSummary
}

type canonicalObservationValue struct {
	Kind string `json:"kind,omitempty"`
	Raw  string `json:"raw,omitempty"`
}

type canonicalObservationSubject struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	GroupID  string `json:"groupId,omitempty"`
	OwnerID  string `json:"ownerId,omitempty"`
	ParentID string `json:"parentId,omitempty"`
}

type canonicalObservationLinkage struct {
	JobID      string `json:"jobId,omitempty"`
	ProductKey string `json:"productKey,omitempty"`
	StepRef    string `json:"stepRef,omitempty"`
}

type canonicalObservationEvidence struct {
	SourceKind   string `json:"sourceKind,omitempty"`
	SourceRef    string `json:"sourceRef,omitempty"`
	DigestSHA256 string `json:"digestSha256,omitempty"`
}

type canonicalObservationFact struct {
	Kind     string                       `json:"kind"`
	Subject  canonicalObservationSubject  `json:"subject"`
	Key      string                       `json:"key,omitempty"`
	Value    canonicalObservationValue    `json:"value"`
	Linkage  canonicalObservationLinkage  `json:"linkage"`
	Evidence canonicalObservationEvidence `json:"evidence"`
}

type canonicalObservationSummary struct {
	Facts []canonicalObservationFact `json:"facts"`
}

// BuildObservationRecord normalizes input, derives identity, and returns a validated observation record.
func BuildObservationRecord(input ObservationRecordInput) (ObservationRecord, error) {
	normalized := normalizeObservationRecordInput(input)

	identity, err := deriveObservationIdentity(normalized)
	if err != nil {
		return ObservationRecord{}, err
	}

	record := ObservationRecord{
		Family:      FamilyObservation,
		Version:     CurrentVersion,
		RecordKey:   normalized.RecordKey,
		Identity:    identity,
		Provenance:  normalized.Provenance,
		Observation: normalized.Observation,
	}

	if err := ValidateObservationRecord(record); err != nil {
		return ObservationRecord{}, err
	}

	return record, nil
}

// NormalizeObservationRecord returns a deterministic, copy-safe normalized observation record.
func NormalizeObservationRecord(record ObservationRecord) ObservationRecord {
	return ObservationRecord{
		Family:      NormalizeFamily(record.Family),
		Version:     trimString(record.Version),
		RecordKey:   trimString(record.RecordKey),
		Identity:    record.Identity,
		Provenance:  NormalizeProvenance(record.Provenance),
		Observation: normalizeObservationSummary(record.Observation),
	}
}

// ValidateObservationRecord rejects malformed normalized observation record material.
func ValidateObservationRecord(record ObservationRecord) error {
	normalized := NormalizeObservationRecord(record)

	if normalized.Family != FamilyObservation {
		return fmt.Errorf("%w: family must be observation", ErrInvalidObservationRecord)
	}
	if err := ValidateVersion(normalized.Version); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidObservationRecord, err)
	}
	if normalized.RecordKey == "" {
		return fmt.Errorf("%w: record key is required", ErrInvalidObservationRecord)
	}
	if err := ValidateProvenance(normalized.Provenance); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidObservationRecord, err)
	}
	if err := validateStoredIdentity(normalized.Identity); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidObservationRecord, err)
	}
	if normalized.Identity.Family != FamilyObservation {
		return fmt.Errorf("%w: identity family must be observation", ErrInvalidObservationRecord)
	}
	if normalized.Identity.Version != CurrentVersion {
		return fmt.Errorf("%w: identity version must be %q", ErrInvalidObservationRecord, CurrentVersion)
	}
	if err := validateObservationSummary(normalized.Observation); err != nil {
		return err
	}

	expectedIdentity, err := deriveObservationIdentity(normalizedObservationRecordInput{
		RecordKey:   normalized.RecordKey,
		Provenance:  normalized.Provenance,
		Observation: normalized.Observation,
	})
	if err != nil {
		return err
	}
	if normalized.Identity != expectedIdentity {
		return fmt.Errorf("%w: identity does not match derived identity", ErrInvalidObservationRecord)
	}

	return nil
}

type normalizedObservationRecordInput struct {
	RecordKey   string
	Provenance  Provenance
	Observation ObservationSummary
}

func normalizeObservationRecordInput(input ObservationRecordInput) normalizedObservationRecordInput {
	return normalizedObservationRecordInput{
		RecordKey:   trimString(input.RecordKey),
		Provenance:  NormalizeProvenance(input.Provenance),
		Observation: normalizeObservationSummary(input.Observation),
	}
}

func normalizeObservationSummary(summary ObservationSummary) ObservationSummary {
	return ObservationSummary{
		Facts: normalizeObservationFacts(summary.Facts),
	}
}

func normalizeObservationFacts(facts []ObservationFact) []ObservationFact {
	if len(facts) == 0 {
		return []ObservationFact{}
	}

	out := make([]ObservationFact, len(facts))
	for i, fact := range facts {
		out[i] = normalizeObservationFact(fact)
	}

	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Subject.ID != right.Subject.ID {
			return left.Subject.ID < right.Subject.ID
		}
		if left.Subject.Name != right.Subject.Name {
			return left.Subject.Name < right.Subject.Name
		}
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		if left.Value.Kind != right.Value.Kind {
			return left.Value.Kind < right.Value.Kind
		}
		if left.Value.Raw != right.Value.Raw {
			return left.Value.Raw < right.Value.Raw
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

func normalizeObservationFact(fact ObservationFact) ObservationFact {
	return ObservationFact{
		Kind:     NormalizeObservationKind(fact.Kind),
		Subject:  normalizeObservationSubject(fact.Subject),
		Key:      trimString(fact.Key),
		Value:    normalizeObservationValue(fact.Value),
		Linkage:  normalizeObservationLinkage(fact.Linkage),
		Evidence: normalizeObservationEvidence(fact.Evidence),
	}
}

func normalizeObservationSubject(subject ObservationSubject) ObservationSubject {
	return ObservationSubject{
		ID:       trimString(subject.ID),
		Name:     trimString(subject.Name),
		GroupID:  trimString(subject.GroupID),
		OwnerID:  trimString(subject.OwnerID),
		ParentID: trimString(subject.ParentID),
	}
}

func normalizeObservationValue(value ObservationValue) ObservationValue {
	return ObservationValue{
		Kind: trimString(value.Kind),
		Raw:  trimString(value.Raw),
	}
}

func normalizeObservationLinkage(linkage ObservationLinkage) ObservationLinkage {
	return ObservationLinkage{
		JobID:      trimString(linkage.JobID),
		ProductKey: trimString(linkage.ProductKey),
		StepRef:    trimString(linkage.StepRef),
	}
}

func normalizeObservationEvidence(evidence ObservationEvidence) ObservationEvidence {
	return ObservationEvidence{
		SourceKind:   trimString(evidence.SourceKind),
		SourceRef:    trimString(evidence.SourceRef),
		DigestSHA256: trimString(evidence.DigestSHA256),
	}
}

// NormalizeObservationKind trims and preserves unknown values for validation to reject.
func NormalizeObservationKind(kind ObservationKind) ObservationKind {
	return ObservationKind(strings.TrimSpace(string(kind)))
}

func knownObservationKind(kind ObservationKind) bool {
	switch NormalizeObservationKind(kind) {
	case ObservationKindParameter, ObservationKindMetadata, ObservationKindReference, ObservationKindComponent:
		return true
	default:
		return false
	}
}

func hasObservationSubjectIdentity(subject ObservationSubject) bool {
	return subject.ID != "" || subject.Name != ""
}

func validateObservationSummary(summary ObservationSummary) error {
	if len(summary.Facts) == 0 {
		return fmt.Errorf("%w: observation must contain at least one fact", ErrInvalidObservationRecord)
	}

	seen := make(map[string]struct{}, len(summary.Facts))
	for i, fact := range summary.Facts {
		if err := validateObservationFact(fact, i); err != nil {
			return err
		}

		key, err := observationFactDuplicateKey(fact)
		if err != nil {
			return err
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: fact %d is a duplicate", ErrInvalidObservationRecord, i)
		}
		seen[key] = struct{}{}
	}

	return nil
}

func validateObservationFact(fact ObservationFact, index int) error {
	if fact.Kind == "" {
		return fmt.Errorf("%w: fact %d kind is required", ErrInvalidObservationRecord, index)
	}
	if !knownObservationKind(fact.Kind) {
		return fmt.Errorf("%w: fact %d kind %q is invalid", ErrInvalidObservationRecord, index, fact.Kind)
	}
	if !hasObservationSubjectIdentity(fact.Subject) {
		return fmt.Errorf("%w: fact %d subject id or name is required", ErrInvalidObservationRecord, index)
	}

	switch NormalizeObservationKind(fact.Kind) {
	case ObservationKindParameter:
		if fact.Value.Kind == "" {
			return fmt.Errorf("%w: fact %d value.kind is required for parameter observations", ErrInvalidObservationRecord, index)
		}
		if fact.Value.Raw == "" {
			return fmt.Errorf("%w: fact %d value.raw is required for parameter observations", ErrInvalidObservationRecord, index)
		}
	case ObservationKindMetadata:
		if fact.Key == "" {
			return fmt.Errorf("%w: fact %d key is required for metadata observations", ErrInvalidObservationRecord, index)
		}
		if fact.Value.Kind == "" {
			return fmt.Errorf("%w: fact %d value.kind is required for metadata observations", ErrInvalidObservationRecord, index)
		}
		if fact.Value.Raw == "" {
			return fmt.Errorf("%w: fact %d value.raw is required for metadata observations", ErrInvalidObservationRecord, index)
		}
	case ObservationKindReference:
		if fact.Key == "" && fact.Value.Raw == "" {
			return fmt.Errorf("%w: fact %d key or value.raw is required for reference observations", ErrInvalidObservationRecord, index)
		}
	case ObservationKindComponent:
		// subject identity already validated above
	}

	if err := validateObservationEvidence(fact.Evidence, index); err != nil {
		return err
	}

	return nil
}

func validateObservationEvidence(evidence ObservationEvidence, factIndex int) error {
	if err := validateObservationDigestSHA256(
		fmt.Sprintf("fact %d evidence.digestSha256", factIndex),
		evidence.DigestSHA256,
	); err != nil {
		return err
	}
	if evidence.SourceKind != "" && evidence.SourceRef == "" {
		return fmt.Errorf("%w: fact %d evidence.sourceRef is required when sourceKind is present", ErrInvalidObservationRecord, factIndex)
	}
	if evidence.SourceRef != "" && evidence.SourceKind == "" {
		return fmt.Errorf("%w: fact %d evidence.sourceKind is required when sourceRef is present", ErrInvalidObservationRecord, factIndex)
	}
	return nil
}

func validateObservationDigestSHA256(field, digest string) error {
	if digest == "" {
		return nil
	}
	if len(digest) != 64 || !isLowerHex64(digest) {
		return fmt.Errorf("%w: %s must be a 64-character lowercase hexadecimal digest", ErrInvalidObservationRecord, field)
	}
	return nil
}

func deriveObservationIdentity(input normalizedObservationRecordInput) (Identity, error) {
	parts, err := observationIdentityParts(input)
	if err != nil {
		return Identity{}, err
	}

	identity, err := DeriveIdentity(IdentityInput{
		Family:     FamilyObservation,
		Version:    CurrentVersion,
		RecordKey:  input.RecordKey,
		Provenance: input.Provenance,
		Parts:      parts,
	})
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrInvalidObservationRecord, err)
	}

	return identity, nil
}

func observationIdentityParts(input normalizedObservationRecordInput) ([]IdentityPart, error) {
	observationJSON, err := json.Marshal(canonicalObservationSummaryFromNormalized(input.Observation))
	if err != nil {
		return nil, fmt.Errorf("%w: marshal observation identity material: %v", ErrInvalidObservationRecord, err)
	}

	return []IdentityPart{
		{Key: "observation", Value: string(observationJSON)},
	}, nil
}

func canonicalObservationSummaryFromNormalized(summary ObservationSummary) canonicalObservationSummary {
	return canonicalObservationSummary{
		Facts: canonicalObservationFactsFromNormalized(summary.Facts),
	}
}

func canonicalObservationFactsFromNormalized(facts []ObservationFact) []canonicalObservationFact {
	out := make([]canonicalObservationFact, len(facts))
	for i, fact := range facts {
		out[i] = canonicalObservationFactFromNormalized(fact)
	}
	return out
}

func canonicalObservationFactFromNormalized(fact ObservationFact) canonicalObservationFact {
	return canonicalObservationFact{
		Kind: string(fact.Kind),
		Subject: canonicalObservationSubject{
			ID:       fact.Subject.ID,
			Name:     fact.Subject.Name,
			GroupID:  fact.Subject.GroupID,
			OwnerID:  fact.Subject.OwnerID,
			ParentID: fact.Subject.ParentID,
		},
		Key: fact.Key,
		Value: canonicalObservationValue{
			Kind: fact.Value.Kind,
			Raw:  fact.Value.Raw,
		},
		Linkage: canonicalObservationLinkage{
			JobID:      fact.Linkage.JobID,
			ProductKey: fact.Linkage.ProductKey,
			StepRef:    fact.Linkage.StepRef,
		},
		Evidence: canonicalObservationEvidence{
			SourceKind:   fact.Evidence.SourceKind,
			SourceRef:    fact.Evidence.SourceRef,
			DigestSHA256: fact.Evidence.DigestSHA256,
		},
	}
}

func observationFactDuplicateKey(fact ObservationFact) (string, error) {
	data, err := json.Marshal(canonicalObservationFactFromNormalized(fact))
	if err != nil {
		return "", fmt.Errorf("%w: marshal fact duplicate key material: %v", ErrInvalidObservationRecord, err)
	}
	return string(data), nil
}
