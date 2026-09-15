package recordmap

import (
	"encoding/json"
	"fmt"
	"strings"

	"parametron/internal/engine/observed"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
)

const (
	observedEvidenceKind             = "observed"
	observedReferenceRecordKeySuffix = ":reference"
	observedComponentKindKey         = "kind"
	observedStringValueKind          = "string"
)

var observedEvidenceRef = recordpackage.RawObservedContractPath()

// ObservedMappingLinkage carries caller-supplied deterministic linkage material.
// The mapper must not infer missing job/product/step identity from PDM or runtime paths.
type ObservedMappingLinkage struct {
	JobID      string
	ProductKey string
	StepRef    string
}

// ObservedMappingInput carries parsed observed material and caller-supplied record identity.
type ObservedMappingInput struct {
	Observed observed.Observed

	// Required stable key material supplied by the caller.
	// Do not invent source revision or PDM identity.
	RecordKey string

	// Optional stable key for the reference record.
	// If empty and references exist, it is derived deterministically from RecordKey.
	ReferenceRecordKey string

	// Optional base provenance supplied by the caller.
	// The mapper preserves it and appends observed raw evidence,
	// but must not fabricate unavailable PDM/source revision data.
	Provenance recordcontract.Provenance

	// Optional deterministic linkage supplied by the caller.
	Linkage ObservedMappingLinkage

	// Optional digest of raw/observed/prm.observed.json, if the caller has it.
	// Must be a lowercase 64-character SHA-256 hex digest if present.
	EvidenceDigestSHA256 string
}

// ObservedMappingOutput carries normalized observation and optional reference records.
type ObservedMappingOutput struct {
	ObservationRecord recordcontract.ObservationRecord
	ReferenceRecord   *recordcontract.ReferenceRecord
}

// MapObserved converts parsed observed material into normalized observation and reference records.
func MapObserved(input ObservedMappingInput) (ObservedMappingOutput, error) {
	observationRecord, err := MapObservedToObservationRecord(input)
	if err != nil {
		return ObservedMappingOutput{}, err
	}

	referenceRecord, err := MapObservedToReferenceRecord(input)
	if err != nil {
		return ObservedMappingOutput{}, err
	}

	return ObservedMappingOutput{
		ObservationRecord: observationRecord,
		ReferenceRecord:   referenceRecord,
	}, nil
}

// MapObservedToObservationRecord converts parsed observed material into a normalized observation record.
func MapObservedToObservationRecord(input ObservedMappingInput) (recordcontract.ObservationRecord, error) {
	if err := validateObservedMappingInput(input); err != nil {
		return recordcontract.ObservationRecord{}, err
	}

	provenance, err := enrichObservedProvenance(input)
	if err != nil {
		return recordcontract.ObservationRecord{}, err
	}

	facts, err := mapObservationFacts(input)
	if err != nil {
		return recordcontract.ObservationRecord{}, err
	}

	record, err := recordcontract.BuildObservationRecord(recordcontract.ObservationRecordInput{
		RecordKey:   strings.TrimSpace(input.RecordKey),
		Provenance:  provenance,
		Observation: recordcontract.ObservationSummary{Facts: facts},
	})
	if err != nil {
		return recordcontract.ObservationRecord{}, fmt.Errorf("%w: build observation record: %w", ErrInvalidObservedMapping, err)
	}

	return record, nil
}

// MapObservedToReferenceRecord converts observed references into a normalized reference record.
// It returns nil when there are no observed references, because a reference record requires at least one edge.
func MapObservedToReferenceRecord(input ObservedMappingInput) (*recordcontract.ReferenceRecord, error) {
	if err := validateObservedMappingInput(input); err != nil {
		return nil, err
	}

	references := input.Observed.Observation.References
	if len(references) == 0 {
		return nil, nil
	}

	provenance, err := enrichObservedProvenance(input)
	if err != nil {
		return nil, err
	}

	edges := mapReferenceEdges(input)

	record, err := recordcontract.BuildReferenceRecord(recordcontract.ReferenceRecordInput{
		RecordKey:  observedReferenceRecordKey(input),
		Provenance: provenance,
		Reference:  recordcontract.ReferenceSummary{Edges: edges},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: build reference record: %w", ErrInvalidObservedMapping, err)
	}

	return &record, nil
}

func validateObservedMappingInput(input ObservedMappingInput) error {
	if strings.TrimSpace(input.RecordKey) == "" {
		return fmt.Errorf("%w: record key is required", ErrInvalidObservedMapping)
	}
	if strings.TrimSpace(input.Observed.SchemaVersion) == "" {
		return fmt.Errorf("%w: observed schema version is required", ErrInvalidObservedMapping)
	}
	if input.Observed.SchemaVersion != observed.SchemaVersion {
		return fmt.Errorf("%w: unsupported observed schema version %q", ErrInvalidObservedMapping, input.Observed.SchemaVersion)
	}

	source := input.Observed
	if err := observed.Validate(&source); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidObservedMapping, err)
	}

	if err := validateObservedDigestSHA256("evidenceDigestSha256", input.EvidenceDigestSHA256); err != nil {
		return err
	}

	if input.ReferenceRecordKey != "" && strings.TrimSpace(input.ReferenceRecordKey) == "" {
		return fmt.Errorf("%w: reference record key must not be blank", ErrInvalidObservedMapping)
	}

	return nil
}

func mapObservationFacts(input ObservedMappingInput) ([]recordcontract.ObservationFact, error) {
	obs := input.Observed.Observation
	evidence := observedObservationEvidence(strings.TrimSpace(input.EvidenceDigestSHA256))
	linkage := observedObservationLinkage(input.Linkage)

	facts := make([]recordcontract.ObservationFact, 0, len(obs.Components)+len(obs.Parameters)+len(obs.Metadata)+len(obs.References))

	for _, component := range obs.Components {
		raw, err := canonicalObservedJSONString(component.Kind)
		if err != nil {
			return nil, err
		}
		facts = append(facts, recordcontract.ObservationFact{
			Kind: recordcontract.ObservationKindComponent,
			Subject: recordcontract.ObservationSubject{
				ID:       component.ID,
				Name:     component.Name,
				ParentID: component.ParentID,
			},
			Key:      observedComponentKindKey,
			Value:    recordcontract.ObservationValue{Kind: observedStringValueKind, Raw: raw},
			Linkage:  linkage,
			Evidence: evidence,
		})
	}

	for _, parameter := range obs.Parameters {
		facts = append(facts, recordcontract.ObservationFact{
			Kind: recordcontract.ObservationKindParameter,
			Subject: recordcontract.ObservationSubject{
				ID:      parameter.ID,
				Name:    parameter.Name,
				GroupID: parameter.GroupID,
			},
			Key:      parameter.Name,
			Value:    recordcontract.ObservationValue{Kind: parameter.ValueKind, Raw: string(parameter.Value.Raw())},
			Linkage:  linkage,
			Evidence: evidence,
		})
	}

	for _, entry := range obs.Metadata {
		facts = append(facts, recordcontract.ObservationFact{
			Kind: recordcontract.ObservationKindMetadata,
			Subject: recordcontract.ObservationSubject{
				ID:      entry.ID,
				Name:    entry.Key,
				OwnerID: entry.OwnerID,
			},
			Key:      entry.Key,
			Value:    recordcontract.ObservationValue{Kind: entry.ValueKind, Raw: string(entry.Value.Raw())},
			Linkage:  linkage,
			Evidence: evidence,
		})
	}

	for _, reference := range obs.References {
		raw, err := canonicalObservedJSONString(reference.Name)
		if err != nil {
			return nil, err
		}
		facts = append(facts, recordcontract.ObservationFact{
			Kind:     recordcontract.ObservationKindReference,
			Subject:  recordcontract.ObservationSubject{Name: reference.Name},
			Key:      reference.Kind,
			Value:    recordcontract.ObservationValue{Kind: observedStringValueKind, Raw: raw},
			Linkage:  linkage,
			Evidence: evidence,
		})
	}

	return facts, nil
}

func mapReferenceEdges(input ObservedMappingInput) []recordcontract.ReferenceEdge {
	references := input.Observed.Observation.References
	evidence := observedReferenceEvidence(strings.TrimSpace(input.EvidenceDigestSHA256))
	linkage := observedReferenceLinkage(input.Linkage)
	workingCopy := input.Observed.WorkingCopy

	edges := make([]recordcontract.ReferenceEdge, 0, len(references))
	for _, reference := range references {
		edges = append(edges, recordcontract.ReferenceEdge{
			Kind: recordcontract.ReferenceKindComponent,
			Source: recordcontract.ReferenceEndpoint{
				Path:         workingCopy.Path,
				DigestSHA256: workingCopy.SHA256,
			},
			Target:     recordcontract.ReferenceEndpoint{Name: reference.Name},
			Role:       reference.Kind,
			Resolution: recordcontract.ReferenceResolutionResolved,
			Linkage:    linkage,
			Evidence:   evidence,
		})
	}

	return edges
}

func enrichObservedProvenance(input ObservedMappingInput) (recordcontract.Provenance, error) {
	provenance := copyProvenance(input.Provenance)

	evidence, err := appendObservedEvidence(provenance.Evidence, strings.TrimSpace(input.EvidenceDigestSHA256))
	if err != nil {
		return recordcontract.Provenance{}, err
	}
	provenance.Evidence = evidence

	normalized := recordcontract.NormalizeProvenance(provenance)
	if err := recordcontract.ValidateProvenance(normalized); err != nil {
		return recordcontract.Provenance{}, fmt.Errorf("%w: validate provenance: %w", ErrInvalidObservedMapping, err)
	}

	return normalized, nil
}

func appendObservedEvidence(existing []recordcontract.EvidenceReference, digest string) ([]recordcontract.EvidenceReference, error) {
	out := copyEvidenceReferences(existing)
	for i, item := range out {
		if strings.TrimSpace(item.Kind) != observedEvidenceKind || strings.TrimSpace(item.Ref) != observedEvidenceRef {
			continue
		}
		existingDigest := strings.TrimSpace(item.DigestSHA256)
		if existingDigest != "" && digest != "" && existingDigest != digest {
			return nil, fmt.Errorf("%w: conflicting digest for observed evidence reference", ErrInvalidObservedMapping)
		}
		if existingDigest == "" && digest != "" {
			out[i].DigestSHA256 = digest
		}
		return out, nil
	}

	out = append(out, recordcontract.EvidenceReference{
		Kind:         observedEvidenceKind,
		Ref:          observedEvidenceRef,
		DigestSHA256: digest,
	})
	return out, nil
}

func observedReferenceRecordKey(input ObservedMappingInput) string {
	if key := strings.TrimSpace(input.ReferenceRecordKey); key != "" {
		return key
	}
	return strings.TrimSpace(input.RecordKey) + observedReferenceRecordKeySuffix
}

func observedObservationEvidence(digest string) recordcontract.ObservationEvidence {
	return recordcontract.ObservationEvidence{
		SourceKind:   observedEvidenceKind,
		SourceRef:    observedEvidenceRef,
		DigestSHA256: digest,
	}
}

func observedReferenceEvidence(digest string) recordcontract.ReferenceEvidence {
	return recordcontract.ReferenceEvidence{
		SourceKind:   observedEvidenceKind,
		SourceRef:    observedEvidenceRef,
		DigestSHA256: digest,
	}
}

func observedObservationLinkage(linkage ObservedMappingLinkage) recordcontract.ObservationLinkage {
	return recordcontract.ObservationLinkage{
		JobID:      strings.TrimSpace(linkage.JobID),
		ProductKey: strings.TrimSpace(linkage.ProductKey),
		StepRef:    strings.TrimSpace(linkage.StepRef),
	}
}

func observedReferenceLinkage(linkage ObservedMappingLinkage) recordcontract.ReferenceLinkage {
	return recordcontract.ReferenceLinkage{
		JobID:      strings.TrimSpace(linkage.JobID),
		ProductKey: strings.TrimSpace(linkage.ProductKey),
		StepRef:    strings.TrimSpace(linkage.StepRef),
	}
}

func canonicalObservedJSONString(value string) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("%w: encode observed value: %w", ErrInvalidObservedMapping, err)
	}
	return string(encoded), nil
}

func validateObservedDigestSHA256(field, digest string) error {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil
	}
	if len(digest) != 64 || !isMappingLowerHex64(digest) {
		return fmt.Errorf("%w: %s must be a 64-character lowercase hexadecimal digest", ErrInvalidObservedMapping, field)
	}
	return nil
}
