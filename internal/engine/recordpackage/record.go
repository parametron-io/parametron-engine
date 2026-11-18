package recordpackage

import (
	"fmt"
	"sort"

	"parametron/internal/engine/recordcontract"
)

// Record carries one validated Engine-produced normalized record payload.
// Exactly one family-specific field must be set.
//
// OutcomeKey, when set, scopes a normalized record to one eligible execution
// outcome. It places the record under records/outcomes/<OutcomeKey>/ so plural
// outcome-scoped families keep distinct package paths. An empty OutcomeKey keeps
// the established singular family path.
type Record struct {
	Execution    *recordcontract.ExecutionRecord
	Artifact     *recordcontract.ArtifactRecord
	Observation  *recordcontract.ObservationRecord
	Reference    *recordcontract.ReferenceRecord
	Failure      *recordcontract.FailureRecord
	Verification *recordcontract.VerificationRecord
	OutcomeKey   string
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
	outcomeKey   string
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

	outcomeKey, err := NormalizeOutcomeKey(record.OutcomeKey)
	if err != nil {
		return validatedRecord{}, fmt.Errorf("%w: %w", ErrInvalidRecord, err)
	}
	if outcomeKey != "" && family == recordcontract.FamilyArtifact {
		return validatedRecord{}, fmt.Errorf("%w: artifact records use identity addresses, not outcome scopes", ErrInvalidRecord)
	}

	normalized, recordKey, identityID, err := normalizeAndValidateRecordPayload(record, family)
	if err != nil {
		return validatedRecord{}, err
	}

	var contractPath string
	if family == recordcontract.FamilyArtifact {
		contractPath, err = ArtifactRecordContractPath(identityID)
	} else if outcomeKey != "" {
		contractPath, err = OutcomeRecordContractPath(outcomeKey, family)
	} else {
		var ok bool
		contractPath, ok = RecordContractPath(family)
		if !ok {
			return validatedRecord{}, fmt.Errorf("%w: unknown record family %q", ErrInvalidRecord, family)
		}
	}
	if err != nil {
		return validatedRecord{}, fmt.Errorf("%w: %w", ErrInvalidRecord, err)
	}

	return validatedRecord{
		family:       family,
		recordKey:    recordKey,
		identityID:   identityID,
		outcomeKey:   outcomeKey,
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
	type familyScope struct {
		family     recordcontract.Family
		outcomeKey string
	}
	seenFamilyScopes := make(map[familyScope]struct{}, len(records))
	seenArtifactIdentities := make(map[string]struct{})
	seenContractPaths := make(map[string]struct{}, len(records))

	for i, record := range records {
		item, err := validateRecord(record)
		if err != nil {
			return nil, fmt.Errorf("records[%d]: %w", i, err)
		}
		if item.family == recordcontract.FamilyArtifact {
			if _, exists := seenArtifactIdentities[item.identityID]; exists {
				return nil, fmt.Errorf("records[%d]: %w: duplicate artifact identity %q", i, ErrInvalidRecord, item.identityID)
			}
			seenArtifactIdentities[item.identityID] = struct{}{}
		} else {
			scope := familyScope{family: item.family, outcomeKey: item.outcomeKey}
			if _, exists := seenFamilyScopes[scope]; exists {
				return nil, fmt.Errorf("records[%d]: %w: duplicate family %q in outcome scope %q", i, ErrInvalidRecord, item.family, item.outcomeKey)
			}
			seenFamilyScopes[scope] = struct{}{}
		}
		if _, exists := seenContractPaths[item.contractPath]; exists {
			return nil, fmt.Errorf("records[%d]: %w: duplicate contract path %q", i, ErrInvalidRecord, item.contractPath)
		}
		seenContractPaths[item.contractPath] = struct{}{}
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
		left, right := familyOrder[records[i].family], familyOrder[records[j].family]
		if left != right {
			return left < right
		}
		if records[i].family == recordcontract.FamilyArtifact {
			return records[i].identityID < records[j].identityID
		}
		// Outcome-scoped families order by outcome scope and then by canonical
		// record path. Singleton families keep their established path order.
		if records[i].outcomeKey != records[j].outcomeKey {
			return records[i].outcomeKey < records[j].outcomeKey
		}
		return records[i].contractPath < records[j].contractPath
	})
}
