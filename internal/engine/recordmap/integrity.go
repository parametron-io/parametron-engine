package recordmap

import (
	"fmt"

	"parametron/internal/engine/integrity"
	"parametron/internal/engine/recordcontract"
)

// MapIntegrityProvenance maps the joined per-resource identity chain into the
// normalized provenance contract.
//
// The four content identities stay separate: the accepted source digest, the
// materialized baseline digest, the runtime-reported baseline digest, and the
// persisted configured-result digest each map to their own field, and the lock
// and per-stage statuses are carried unchanged. Ordering is by declared logical
// resource identity, so equivalent evidence normalizes identically regardless of
// evidence order or absolute host paths.
func MapIntegrityProvenance(run *integrity.RunIntegrity) (*recordcontract.IntegrityProvenance, error) {
	if run == nil {
		return nil, nil
	}

	normalized := integrity.NormalizeRunIntegrity(run)
	provenance := &recordcontract.IntegrityProvenance{
		Lock:      recordcontract.LockVerificationState(normalized.Lock),
		Resources: make([]recordcontract.ResourceProvenance, 0, len(normalized.Resources)),
	}
	for _, resource := range normalized.Resources {
		provenance.Resources = append(provenance.Resources, recordcontract.ResourceProvenance{
			LogicalID:                   resource.LogicalID,
			SourceCoordinate:            resource.SourceCoordinate,
			MaterializedCoordinate:      resource.MaterializedCoordinate,
			PersistedResultCoordinate:   resource.PersistedResultCoordinate,
			SourceDigestSHA256:          resource.SourceDigest.String(),
			LockState:                   recordcontract.LockVerificationState(resource.LockState),
			BaselineDigestSHA256:        resource.MaterializedBaselineDigest.String(),
			MaterializationStatus:       recordcontract.IntegrityStatus(resource.MaterializationStatus),
			RuntimeBaselineDigestSHA256: resource.RuntimeBaselineDigest.String(),
			RuntimeBaselineStatus:       recordcontract.IntegrityStatus(resource.RuntimeBaselineStatus),
			PersistedResultDigestSHA256: resource.PersistedResultDigest.String(),
			ResultStatus:                recordcontract.IntegrityStatus(resource.ResultStatus),
		})
	}

	result := recordcontract.NormalizeProvenance(recordcontract.Provenance{Integrity: provenance}).Integrity
	if err := recordcontract.ValidateProvenance(recordcontract.Provenance{Integrity: result}); err != nil {
		return nil, fmt.Errorf("%w: integrity provenance: %w", ErrInvalidIntegrityMapping, err)
	}
	return result, nil
}

func copyIntegrityProvenance(in *recordcontract.IntegrityProvenance) *recordcontract.IntegrityProvenance {
	if in == nil {
		return nil
	}
	out := &recordcontract.IntegrityProvenance{
		Lock:      in.Lock,
		Resources: make([]recordcontract.ResourceProvenance, len(in.Resources)),
	}
	copy(out.Resources, in.Resources)
	return out
}
