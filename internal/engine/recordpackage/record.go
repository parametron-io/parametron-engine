package recordpackage

import (
	"fmt"
	"sort"

	"parametron/internal/engine/recordcontract"
)

// Record carries one validated Engine-produced normalized record payload.
// Exactly one family-specific field must be set.
type Record struct {
	Execution    *recordcontract.ExecutionRecord
	Artifact     *recordcontract.ArtifactRecord
	Observation  *recordcontract.ObservationRecord
	Reference    *recordcontract.ReferenceRecord
	Failure      *recordcontract.FailureRecord
	Verification *recordcontract.VerificationRecord
}

// ExecutionRecord wraps a normalized execution record payload.
func ExecutionRecord(record recordcontract.ExecutionRecord) Record {
	return Record{Execution: &record}
}

// ArtifactRecord wraps a normalized artifact record payload.
func ArtifactRecord(record recordcontract.ArtifactRecord) Record {
	return Record{Artifact: &record}
}

// ObservationRecord wraps a normalized observation record payload.
func ObservationRecord(record recordcontract.ObservationRecord) Record {
	return Record{Observation: &record}
}

// ReferenceRecord wraps a normalized reference record payload.
func ReferenceRecord(record recordcontract.ReferenceRecord) Record {
	return Record{Reference: &record}
}

// FailureRecord wraps a normalized failure record payload.
func FailureRecord(record recordcontract.FailureRecord) Record {
	return Record{Failure: &record}
}

// VerificationRecord wraps a normalized verification record payload.
func VerificationRecord(record recordcontract.VerificationRecord) Record {
	return Record{Verification: &record}
}

type validatedRecord struct {
	family       recordcontract.Family
	recordKey    string
	identityID   string
	contractPath string
	payload      any
}

func validateRecord(record Record) (validatedRecord, error) {
	var (
		family recordcontract.Family
		count  int
	)

	if record.Execution != nil {
		family = recordcontract.FamilyExecution
		count++
	}
	if record.Artifact != nil {
		family = recordcontract.FamilyArtifact
		count++
	}
	if record.Observation != nil {
		family = recordcontract.FamilyObservation
		count++
	}
	if record.Reference != nil {
		family = recordcontract.FamilyReference
		count++
	}
	if record.Failure != nil {
		family = recordcontract.FamilyFailure
		count++
	}
	if record.Verification != nil {
		family = recordcontract.FamilyVerification
		count++
	}

	if count == 0 {
		return validatedRecord{}, fmt.Errorf("%w: record payload is required", ErrInvalidRecord)
	}
	if count > 1 {
		return validatedRecord{}, fmt.Errorf("%w: exactly one record payload field may be set", ErrInvalidRecord)
	}

	normalized, recordKey, identityID, err := normalizeAndValidateRecordPayload(record, family)
	if err != nil {
		return validatedRecord{}, err
	}

	contractPath, ok := RecordContractPath(family)
	if !ok {
		return validatedRecord{}, fmt.Errorf("%w: unknown record family %q", ErrInvalidRecord, family)
	}

	return validatedRecord{
		family:       family,
		recordKey:    recordKey,
		identityID:   identityID,
		contractPath: contractPath,
		payload:      normalized,
	}, nil
}

func normalizeAndValidateRecordPayload(record Record, family recordcontract.Family) (any, string, string, error) {
	switch family {
	case recordcontract.FamilyExecution:
		normalized := recordcontract.NormalizeExecutionRecord(*record.Execution)
		if err := recordcontract.ValidateExecutionRecord(normalized); err != nil {
			return nil, "", "", fmt.Errorf("%w: %w", ErrInvalidRecord, err)
		}
		return normalized, normalized.RecordKey, normalized.Identity.ID, nil
	case recordcontract.FamilyArtifact:
		normalized := recordcontract.NormalizeArtifactRecord(*record.Artifact)
		if err := recordcontract.ValidateArtifactRecord(normalized); err != nil {
			return nil, "", "", fmt.Errorf("%w: %w", ErrInvalidRecord, err)
		}
		return normalized, normalized.RecordKey, normalized.Identity.ID, nil
	case recordcontract.FamilyObservation:
		normalized := recordcontract.NormalizeObservationRecord(*record.Observation)
		if err := recordcontract.ValidateObservationRecord(normalized); err != nil {
			return nil, "", "", fmt.Errorf("%w: %w", ErrInvalidRecord, err)
		}
		return normalized, normalized.RecordKey, normalized.Identity.ID, nil
	case recordcontract.FamilyReference:
		normalized := recordcontract.NormalizeReferenceRecord(*record.Reference)
		if err := recordcontract.ValidateReferenceRecord(normalized); err != nil {
			return nil, "", "", fmt.Errorf("%w: %w", ErrInvalidRecord, err)
		}
		return normalized, normalized.RecordKey, normalized.Identity.ID, nil
	case recordcontract.FamilyFailure:
		normalized := recordcontract.NormalizeFailureRecord(*record.Failure)
		if err := recordcontract.ValidateFailureRecord(normalized); err != nil {
			return nil, "", "", fmt.Errorf("%w: %w", ErrInvalidRecord, err)
		}
		return normalized, normalized.RecordKey, normalized.Identity.ID, nil
	case recordcontract.FamilyVerification:
		normalized := recordcontract.NormalizeVerificationRecord(*record.Verification)
		if err := recordcontract.ValidateVerificationRecord(normalized); err != nil {
			return nil, "", "", fmt.Errorf("%w: %w", ErrInvalidRecord, err)
		}
		return normalized, normalized.RecordKey, normalized.Identity.ID, nil
	default:
		return nil, "", "", fmt.Errorf("%w: unknown record family %q", ErrInvalidRecord, family)
	}
}

func validateRecords(records []Record) ([]validatedRecord, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("%w: at least one record is required", ErrInvalidPackageInput)
	}

	validated := make([]validatedRecord, 0, len(records))
	seenFamilies := make(map[recordcontract.Family]struct{}, len(records))

	for i, record := range records {
		item, err := validateRecord(record)
		if err != nil {
			return nil, fmt.Errorf("records[%d]: %w", i, err)
		}
		if _, exists := seenFamilies[item.family]; exists {
			return nil, fmt.Errorf("records[%d]: %w: duplicate family %q", i, ErrInvalidRecord, item.family)
		}
		seenFamilies[item.family] = struct{}{}
		validated = append(validated, item)
	}

	sortValidatedRecords(validated)
	return validated, nil
}

func sortValidatedRecords(records []validatedRecord) {
	familyOrder := make(map[recordcontract.Family]int, len(recordcontract.Definitions()))
	for i, def := range recordcontract.Definitions() {
		familyOrder[def.Family] = i
	}

	sort.SliceStable(records, func(i, j int) bool {
		return familyOrder[records[i].family] < familyOrder[records[j].family]
	})
}
