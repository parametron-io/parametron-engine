package recordcontract

import (
	"fmt"
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

func trimString(value string) string {
	return strings.TrimSpace(value)
}
