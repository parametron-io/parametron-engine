package recordcontract

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// SourceRevisionProvenance captures normalized source revision and asset identity.
type SourceRevisionProvenance struct {
	RevisionID   string
	AssetID      string
	DigestSHA256 string
}

// ProvenanceInput captures normalized execution input identity material.
type ProvenanceInput struct {
	Kind         string
	Identity     string
	DigestSHA256 string
}

// PlanProvenance captures normalized plan identity material.
type PlanProvenance struct {
	PlanID   string
	PlanHash string
}

// LinkageProvenance captures normalized product, job, and step linkage.
type LinkageProvenance struct {
	ProductKey string
	JobID      string
	StepRef    string
}

// EvidenceReference captures normalized runtime evidence references.
type EvidenceReference struct {
	Kind         string
	Ref          string
	DigestSHA256 string
}

// RuntimeProvenance captures normalized tool and runtime identity material.
type RuntimeProvenance struct {
	ToolID    string
	RuntimeID string
	Adapter   string
}

// Provenance captures Engine-produced normalized provenance shared by record families.
// It is distinct from PDM-ready local output, future PDM Server ingestion identity,
// and raw runtime evidence bytes.
type Provenance struct {
	SourceRevision SourceRevisionProvenance
	Inputs         []ProvenanceInput
	Plan           PlanProvenance
	Linkage        LinkageProvenance
	Evidence       []EvidenceReference
	Runtime        RuntimeProvenance
	// Integrity is present only for runs that address declared CAD resources.
	// It is omitted from serialized provenance when absent so existing record
	// bytes and identities are unchanged for runs without declared resources.
	Integrity *IntegrityProvenance `json:"Integrity,omitempty"`
}

// LockVerificationState reports whether accepted project-lock source identities
// were verified. An unlocked workflow records not_performed and never claims
// verification.
type LockVerificationState string

const (
	LockVerificationVerified     LockVerificationState = "verified"
	LockVerificationMismatch     LockVerificationState = "mismatch"
	LockVerificationMissing      LockVerificationState = "missing"
	LockVerificationMalformed    LockVerificationState = "malformed"
	LockVerificationUnavailable  LockVerificationState = "unavailable"
	LockVerificationNotPerformed LockVerificationState = "not_performed"
)

// IntegrityStatus reports the outcome of one stage of the per-resource identity
// chain.
type IntegrityStatus string

const (
	IntegrityStatusVerified     IntegrityStatus = "verified"
	IntegrityStatusMismatch     IntegrityStatus = "mismatch"
	IntegrityStatusMissing      IntegrityStatus = "missing"
	IntegrityStatusMalformed    IntegrityStatus = "malformed"
	IntegrityStatusInaccessible IntegrityStatus = "inaccessible"
	IntegrityStatusUnavailable  IntegrityStatus = "unavailable"
	IntegrityStatusUnsupported  IntegrityStatus = "unsupported"
	IntegrityStatusNotPerformed IntegrityStatus = "not_performed"
	IntegrityStatusNotReported  IntegrityStatus = "not_reported"
)

// IntegrityProvenance is the normalized per-resource identity chain for declared
// CAD resources. Source, materialized baseline, runtime-reported baseline, and
// persisted configured-result identities are separate fields and are never
// collapsed into one content identity.
type IntegrityProvenance struct {
	Lock      LockVerificationState
	Resources []ResourceProvenance
}

// ResourceProvenance is the normalized identity chain of one declared resource.
type ResourceProvenance struct {
	LogicalID                   string
	SourceCoordinate            string
	MaterializedCoordinate      string
	PersistedResultCoordinate   string
	SourceDigestSHA256          string
	LockState                   LockVerificationState
	BaselineDigestSHA256        string
	MaterializationStatus       IntegrityStatus
	RuntimeBaselineDigestSHA256 string
	RuntimeBaselineStatus       IntegrityStatus
	PersistedResultDigestSHA256 string
	ResultStatus                IntegrityStatus
}

// NormalizeProvenance returns a deterministic, copy-safe normalized provenance value.
func NormalizeProvenance(p Provenance) Provenance {
	return Provenance{
		SourceRevision: normalizeSourceRevision(p.SourceRevision),
		Inputs:         normalizeProvenanceInputs(p.Inputs),
		Plan:           normalizePlanProvenance(p.Plan),
		Linkage:        normalizeLinkageProvenance(p.Linkage),
		Evidence:       normalizeEvidenceReferences(p.Evidence),
		Runtime:        normalizeRuntimeProvenance(p.Runtime),
		Integrity:      normalizeIntegrityProvenance(p.Integrity),
	}
}

// ValidateProvenance rejects malformed normalized provenance material.
func ValidateProvenance(p Provenance) error {
	normalized := NormalizeProvenance(p)

	if sourceRevisionPresent(normalized.SourceRevision) {
		if normalized.SourceRevision.RevisionID == "" && normalized.SourceRevision.AssetID == "" {
			return fmt.Errorf("%w: source revision requires revisionId or assetId", ErrInvalidProvenance)
		}
		if err := validateDigestSHA256("sourceRevision.digestSha256", normalized.SourceRevision.DigestSHA256); err != nil {
			return err
		}
	}

	for i, input := range normalized.Inputs {
		if input.Kind == "" {
			return fmt.Errorf("%w: provenance input %d kind is required", ErrInvalidProvenance, i)
		}
		if input.Identity == "" {
			return fmt.Errorf("%w: provenance input %d identity is required", ErrInvalidProvenance, i)
		}
		if err := validateDigestSHA256(fmt.Sprintf("provenance.inputs[%d].digestSha256", i), input.DigestSHA256); err != nil {
			return err
		}
	}

	if planProvenancePresent(normalized.Plan) {
		if normalized.Plan.PlanID == "" && normalized.Plan.PlanHash == "" {
			return fmt.Errorf("%w: plan provenance requires planId or planHash", ErrInvalidProvenance)
		}
	}

	for i, evidence := range normalized.Evidence {
		if evidence.Kind == "" {
			return fmt.Errorf("%w: evidence reference %d kind is required", ErrInvalidProvenance, i)
		}
		if evidence.Ref == "" {
			return fmt.Errorf("%w: evidence reference %d ref is required", ErrInvalidProvenance, i)
		}
		if err := validateDigestSHA256(fmt.Sprintf("provenance.evidence[%d].digestSha256", i), evidence.DigestSHA256); err != nil {
			return err
		}
	}

	if err := validateIntegrityProvenance(normalized.Integrity); err != nil {
		return err
	}

	return nil
}

func normalizeSourceRevision(p SourceRevisionProvenance) SourceRevisionProvenance {
	return SourceRevisionProvenance{
		RevisionID:   trimString(p.RevisionID),
		AssetID:      trimString(p.AssetID),
		DigestSHA256: trimString(p.DigestSHA256),
	}
}

func normalizePlanProvenance(p PlanProvenance) PlanProvenance {
	return PlanProvenance{
		PlanID:   trimString(p.PlanID),
		PlanHash: trimString(p.PlanHash),
	}
}

func normalizeLinkageProvenance(p LinkageProvenance) LinkageProvenance {
	return LinkageProvenance{
		ProductKey: trimString(p.ProductKey),
		JobID:      trimString(p.JobID),
		StepRef:    trimString(p.StepRef),
	}
}

func normalizeRuntimeProvenance(p RuntimeProvenance) RuntimeProvenance {
	return RuntimeProvenance{
		ToolID:    trimString(p.ToolID),
		RuntimeID: trimString(p.RuntimeID),
		Adapter:   trimString(p.Adapter),
	}
}

func normalizeProvenanceInputs(inputs []ProvenanceInput) []ProvenanceInput {
	if len(inputs) == 0 {
		return []ProvenanceInput{}
	}

	out := make([]ProvenanceInput, len(inputs))
	for i, input := range inputs {
		out[i] = ProvenanceInput{
			Kind:         trimString(input.Kind),
			Identity:     trimString(input.Identity),
			DigestSHA256: trimString(input.DigestSHA256),
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Identity != out[j].Identity {
			return out[i].Identity < out[j].Identity
		}
		return out[i].DigestSHA256 < out[j].DigestSHA256
	})

	return out
}

func normalizeEvidenceReferences(evidence []EvidenceReference) []EvidenceReference {
	if len(evidence) == 0 {
		return []EvidenceReference{}
	}

	out := make([]EvidenceReference, len(evidence))
	for i, item := range evidence {
		out[i] = EvidenceReference{
			Kind:         trimString(item.Kind),
			Ref:          trimString(item.Ref),
			DigestSHA256: trimString(item.DigestSHA256),
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Ref != out[j].Ref {
			return out[i].Ref < out[j].Ref
		}
		return out[i].DigestSHA256 < out[j].DigestSHA256
	})

	return out
}

func sourceRevisionPresent(p SourceRevisionProvenance) bool {
	return p.RevisionID != "" || p.AssetID != "" || p.DigestSHA256 != ""
}

func planProvenancePresent(p PlanProvenance) bool {
	return p.PlanID != "" || p.PlanHash != ""
}

func validateDigestSHA256(field, digest string) error {
	if digest == "" {
		return nil
	}
	if len(digest) != 64 || !isLowerHex64(digest) {
		return fmt.Errorf("%w: %s must be a 64-character lowercase hexadecimal digest", ErrInvalidProvenance, field)
	}
	return nil
}

func normalizeIntegrityProvenance(p *IntegrityProvenance) *IntegrityProvenance {
	if p == nil {
		return nil
	}
	out := &IntegrityProvenance{
		Lock:      LockVerificationState(trimString(string(p.Lock))),
		Resources: make([]ResourceProvenance, 0, len(p.Resources)),
	}
	for _, item := range p.Resources {
		out.Resources = append(out.Resources, ResourceProvenance{
			LogicalID:                   trimString(item.LogicalID),
			SourceCoordinate:            trimString(item.SourceCoordinate),
			MaterializedCoordinate:      trimString(item.MaterializedCoordinate),
			PersistedResultCoordinate:   trimString(item.PersistedResultCoordinate),
			SourceDigestSHA256:          trimString(item.SourceDigestSHA256),
			LockState:                   LockVerificationState(trimString(string(item.LockState))),
			BaselineDigestSHA256:        trimString(item.BaselineDigestSHA256),
			MaterializationStatus:       IntegrityStatus(trimString(string(item.MaterializationStatus))),
			RuntimeBaselineDigestSHA256: trimString(item.RuntimeBaselineDigestSHA256),
			RuntimeBaselineStatus:       IntegrityStatus(trimString(string(item.RuntimeBaselineStatus))),
			PersistedResultDigestSHA256: trimString(item.PersistedResultDigestSHA256),
			ResultStatus:                IntegrityStatus(trimString(string(item.ResultStatus))),
		})
	}
	sort.SliceStable(out.Resources, func(i, j int) bool { return out.Resources[i].LogicalID < out.Resources[j].LogicalID })
	return out
}

func validateIntegrityProvenance(p *IntegrityProvenance) error {
	if p == nil {
		return nil
	}
	if !knownLockVerificationState(p.Lock) {
		return fmt.Errorf("%w: provenance.integrity.lock %q is not a known lock verification state", ErrInvalidProvenance, p.Lock)
	}

	seen := make(map[string]struct{}, len(p.Resources))
	for i, resource := range p.Resources {
		field := func(name string) string { return fmt.Sprintf("provenance.integrity.resources[%d].%s", i, name) }
		if resource.LogicalID == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidProvenance, field("logicalId"))
		}
		if _, exists := seen[resource.LogicalID]; exists {
			return fmt.Errorf("%w: %s duplicates another resource identity", ErrInvalidProvenance, field("logicalId"))
		}
		seen[resource.LogicalID] = struct{}{}

		if !knownLockVerificationState(resource.LockState) {
			return fmt.Errorf("%w: %s %q is not a known lock verification state", ErrInvalidProvenance, field("lockState"), resource.LockState)
		}
		for _, status := range []struct {
			name  string
			value IntegrityStatus
		}{
			{"materializationStatus", resource.MaterializationStatus},
			{"runtimeBaselineStatus", resource.RuntimeBaselineStatus},
			{"resultStatus", resource.ResultStatus},
		} {
			if !knownIntegrityStatus(status.value) {
				return fmt.Errorf("%w: %s %q is not a known integrity status", ErrInvalidProvenance, field(status.name), status.value)
			}
		}

		if resource.SourceCoordinate == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidProvenance, field("sourceCoordinate"))
		}
		for _, coordinate := range []struct {
			name  string
			value string
		}{
			{"sourceCoordinate", resource.SourceCoordinate},
			{"materializedCoordinate", resource.MaterializedCoordinate},
			{"persistedResultCoordinate", resource.PersistedResultCoordinate},
		} {
			if coordinate.value == "" {
				continue
			}
			if !canonicalRelativeCoordinate(coordinate.value) {
				return fmt.Errorf("%w: %s %q must be a canonical relative coordinate", ErrInvalidProvenance, field(coordinate.name), coordinate.value)
			}
		}
		if resource.MaterializedCoordinate == "" &&
			(resource.MaterializationStatus == IntegrityStatusVerified || resource.MaterializationStatus == IntegrityStatusMismatch) {
			return fmt.Errorf("%w: %s is required for materialization status %q", ErrInvalidProvenance, field("materializedCoordinate"), resource.MaterializationStatus)
		}
		if resource.PersistedResultCoordinate == "" &&
			(resource.ResultStatus == IntegrityStatusVerified || resource.ResultStatus == IntegrityStatusMismatch || resource.ResultStatus == IntegrityStatusInaccessible) {
			return fmt.Errorf("%w: %s is required for result status %q", ErrInvalidProvenance, field("persistedResultCoordinate"), resource.ResultStatus)
		}

		for _, digest := range []struct {
			name  string
			value string
		}{
			{"sourceDigestSha256", resource.SourceDigestSHA256},
			{"baselineDigestSha256", resource.BaselineDigestSHA256},
			{"runtimeBaselineDigestSha256", resource.RuntimeBaselineDigestSHA256},
			{"persistedResultDigestSha256", resource.PersistedResultDigestSHA256},
		} {
			if err := validateDigestSHA256(field(digest.name), digest.value); err != nil {
				return err
			}
		}
	}

	return nil
}

func knownLockVerificationState(state LockVerificationState) bool {
	switch state {
	case LockVerificationVerified, LockVerificationMismatch, LockVerificationMissing,
		LockVerificationMalformed, LockVerificationUnavailable, LockVerificationNotPerformed:
		return true
	default:
		return false
	}
}

// knownIntegrityStatus reports whether status is a supported value.
func knownIntegrityStatus(status IntegrityStatus) bool {
	switch status {
	case IntegrityStatusVerified, IntegrityStatusMismatch, IntegrityStatusMissing,
		IntegrityStatusMalformed, IntegrityStatusInaccessible, IntegrityStatusUnavailable,
		IntegrityStatusUnsupported, IntegrityStatusNotPerformed, IntegrityStatusNotReported:
		return true
	default:
		return false
	}
}

func canonicalRelativeCoordinate(value string) bool {
	if value == "" || trimString(value) != value {
		return false
	}
	if strings.ContainsAny(value, "\\\x00") || strings.HasPrefix(value, "/") {
		return false
	}
	if path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	return value != "." && value != ".." && !strings.HasPrefix(value, "../")
}

func trimString(value string) string {
	return strings.TrimSpace(value)
}
