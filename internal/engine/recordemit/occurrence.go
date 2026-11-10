package recordemit

import (
	"fmt"
	"strings"

	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
)

// OccurrenceRunEvidence is explicit request plus exact bytes from the selected
// verified outcome. It does not assert that a native producer is installed.
type OccurrenceRunEvidence struct {
	Input cadruntime.OccurrenceMappingInput
}

func appendOccurrenceRecords(records []recordpackage.Record, evidence *CADRuntimeRunEvidence, recordKey string, base recordcontract.Provenance) ([]recordpackage.Record, error) {
	if evidence.Occurrence == nil {
		return records, nil
	}
	summary, err := cadruntime.MapOccurrenceEvidence(evidence.Occurrence.Input)
	if err != nil {
		return nil, fmt.Errorf("recordemit: occurrence evidence: %w", err)
	}
	if summary == nil {
		return records, nil
	}
	provenance := recordcontract.NormalizeProvenance(base)
	facts := recordcontract.ObservationSummary{Occurrences: summary}
	index := -1
	for i, r := range records {
		if r.Observation != nil {
			index = i
			facts.Facts = r.Observation.Observation.Facts
			provenance = r.Observation.Provenance
			break
		}
	}
	// Reconcile execution linkage and exact-byte evidence with existing observation
	// provenance; conflicting supplied values fail instead of being overwritten.
	linkage := &provenance.Linkage
	for _, pair := range []struct {
		existing *string
		value    string
	}{{&linkage.JobID, evidence.JobID}, {&linkage.ProductKey, evidence.ProductKey}, {&linkage.StepRef, evidence.StepRef}} {
		value := strings.TrimSpace(pair.value)
		if *pair.existing != "" && value != "" && *pair.existing != value {
			return nil, fmt.Errorf("recordemit: conflicting occurrence linkage")
		}
		if value != "" {
			*pair.existing = value
		}
	}
	if summary.Evidence.SourceRef != "" {
		refs := make([]recordcontract.EvidenceReference, 0, len(provenance.Evidence)+1)
		for _, ref := range provenance.Evidence {
			if ref.Kind == summary.Evidence.SourceKind && ref.Ref == summary.Evidence.SourceRef {
				if ref.DigestSHA256 != "" && ref.DigestSHA256 != summary.Evidence.DigestSHA256 {
					return nil, fmt.Errorf("recordemit: conflicting occurrence evidence digest")
				}
			} else {
				refs = append(refs, ref)
			}
		}
		provenance.Evidence = append(refs, recordcontract.EvidenceReference{Kind: summary.Evidence.SourceKind, Ref: summary.Evidence.SourceRef, DigestSHA256: summary.Evidence.DigestSHA256})
	}
	record, err := recordcontract.BuildObservationRecord(recordcontract.ObservationRecordInput{RecordKey: recordKey + ":observation", Provenance: provenance, Observation: facts})
	if err != nil {
		return nil, err
	}
	wrapped := recordpackage.ObservationRecord(record)
	if index >= 0 {
		records[index] = wrapped
	} else {
		records = append(records, wrapped)
	}
	return records, nil
}
