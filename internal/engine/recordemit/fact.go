package recordemit

import (
	"fmt"
	"strings"

	"parametron/internal/engine/cadruntime"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
)

// FactRunEvidence is explicit request plus exact bytes from the selected
// verified outcome. It does not assert that a native producer is installed.
type FactRunEvidence struct {
	Input cadruntime.FactMappingInput
}

// appendFactRecords merges normalized facts into the single observation record
// family. Facts live inside observation.facts, so the occurrence merge that
// copies the Facts field cannot drop them.
func appendFactRecords(records []recordpackage.Record, evidence *CADRuntimeRunEvidence, recordKey string, base recordcontract.Provenance) ([]recordpackage.Record, error) {
	if evidence.Fact == nil {
		return records, nil
	}
	facts, err := cadruntime.MapFactEvidence(evidence.Fact.Input)
	if err != nil {
		return nil, fmt.Errorf("recordemit: fact evidence: %w", err)
	}
	if len(facts) == 0 {
		return records, nil
	}
	provenance := recordcontract.NormalizeProvenance(base)
	summary := recordcontract.ObservationSummary{Facts: facts}
	index := -1
	for i, r := range records {
		if r.Observation != nil {
			index = i
			summary.Facts = append(r.Observation.Observation.Facts, facts...)
			summary.Occurrences = r.Observation.Observation.Occurrences
			provenance = r.Observation.Provenance
			break
		}
	}
	// Reconcile execution linkage and exact-byte evidence with existing
	// observation provenance; conflicting supplied values fail instead of being
	// overwritten.
	linkage := &provenance.Linkage
	for _, pair := range []struct {
		existing *string
		value    string
	}{{&linkage.JobID, evidence.JobID}, {&linkage.ProductKey, evidence.ProductKey}, {&linkage.StepRef, evidence.StepRef}} {
		value := strings.TrimSpace(pair.value)
		if *pair.existing != "" && value != "" && *pair.existing != value {
			return nil, fmt.Errorf("recordemit: conflicting fact linkage")
		}
		if value != "" {
			*pair.existing = value
		}
	}
	factEvidence := cadruntime.FactEvidenceProvenance(facts)
	if factEvidence.SourceRef != "" {
		refs := make([]recordcontract.EvidenceReference, 0, len(provenance.Evidence)+1)
		for _, ref := range provenance.Evidence {
			if ref.Kind == factEvidence.SourceKind && ref.Ref == factEvidence.SourceRef {
				if ref.DigestSHA256 != "" && ref.DigestSHA256 != factEvidence.DigestSHA256 {
					return nil, fmt.Errorf("recordemit: conflicting fact evidence digest")
				}
			} else {
				refs = append(refs, ref)
			}
		}
		provenance.Evidence = append(refs, recordcontract.EvidenceReference{Kind: factEvidence.SourceKind, Ref: factEvidence.SourceRef, DigestSHA256: factEvidence.DigestSHA256})
	}
	record, err := recordcontract.BuildObservationRecord(recordcontract.ObservationRecordInput{RecordKey: recordKey + ":observation", Provenance: provenance, Observation: summary})
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
