package recordcontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type canonicalIdentityMaterial struct {
	Algorithm  string              `json:"algorithm"`
	Family     string              `json:"family"`
	Version    string              `json:"version"`
	RecordKey  string              `json:"recordKey"`
	Provenance canonicalProvenance `json:"provenance"`
	Parts      []canonicalPart     `json:"parts"`
}

type canonicalProvenance struct {
	SourceRevision canonicalSourceRevision    `json:"sourceRevision"`
	Inputs         []canonicalProvenanceInput `json:"inputs"`
	Plan           canonicalPlanProvenance    `json:"plan"`
	Linkage        canonicalLinkageProvenance `json:"linkage"`
	Evidence       []canonicalEvidenceRef     `json:"evidence"`
	Runtime        canonicalRuntimeProvenance `json:"runtime"`
	// Integrity is omitted when absent so identities of records without
	// declared resource integrity provenance are unchanged.
	Integrity *canonicalIntegrityProvenance `json:"integrity,omitempty"`
}

type canonicalIntegrityProvenance struct {
	Lock      string                        `json:"lock"`
	Resources []canonicalResourceProvenance `json:"resources"`
}

type canonicalResourceProvenance struct {
	LogicalID                   string `json:"logicalId"`
	SourceCoordinate            string `json:"sourceCoordinate"`
	MaterializedCoordinate      string `json:"materializedCoordinate"`
	PersistedResultCoordinate   string `json:"persistedResultCoordinate"`
	SourceDigestSHA256          string `json:"sourceDigestSha256"`
	LockState                   string `json:"lockState"`
	BaselineDigestSHA256        string `json:"baselineDigestSha256"`
	MaterializationStatus       string `json:"materializationStatus"`
	RuntimeBaselineDigestSHA256 string `json:"runtimeBaselineDigestSha256"`
	RuntimeBaselineStatus       string `json:"runtimeBaselineStatus"`
	PersistedResultDigestSHA256 string `json:"persistedResultDigestSha256"`
	ResultStatus                string `json:"resultStatus"`
}

type canonicalSourceRevision struct {
	RevisionID   string `json:"revisionId"`
	AssetID      string `json:"assetId"`
	DigestSHA256 string `json:"digestSha256"`
}

type canonicalProvenanceInput struct {
	Kind         string `json:"kind"`
	Identity     string `json:"identity"`
	DigestSHA256 string `json:"digestSha256"`
}

type canonicalPlanProvenance struct {
	PlanID   string `json:"planId"`
	PlanHash string `json:"planHash"`
}

type canonicalLinkageProvenance struct {
	ProductKey string `json:"productKey"`
	JobID      string `json:"jobId"`
	StepRef    string `json:"stepRef"`
}

type canonicalEvidenceRef struct {
	Kind         string `json:"kind"`
	Ref          string `json:"ref"`
	DigestSHA256 string `json:"digestSha256"`
}

type canonicalRuntimeProvenance struct {
	ToolID    string `json:"toolId"`
	RuntimeID string `json:"runtimeId"`
	Adapter   string `json:"adapter"`
}

type canonicalPart struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func canonicalIdentityMaterialFromInput(algorithm IdentityAlgorithm, input IdentityInput) canonicalIdentityMaterial {
	normalized := normalizeIdentityInput(input)
	return canonicalIdentityMaterial{
		Algorithm:  string(algorithm),
		Family:     string(normalized.Family),
		Version:    normalized.Version,
		RecordKey:  normalized.RecordKey,
		Provenance: canonicalProvenanceFromNormalized(normalized.Provenance),
		Parts:      canonicalPartsFromNormalized(normalized.Parts),
	}
}

func canonicalProvenanceFromNormalized(p Provenance) canonicalProvenance {
	return canonicalProvenance{
		SourceRevision: canonicalSourceRevision{
			RevisionID:   p.SourceRevision.RevisionID,
			AssetID:      p.SourceRevision.AssetID,
			DigestSHA256: p.SourceRevision.DigestSHA256,
		},
		Inputs: canonicalProvenanceInputsFromNormalized(p.Inputs),
		Plan: canonicalPlanProvenance{
			PlanID:   p.Plan.PlanID,
			PlanHash: p.Plan.PlanHash,
		},
		Linkage: canonicalLinkageProvenance{
			ProductKey: p.Linkage.ProductKey,
			JobID:      p.Linkage.JobID,
			StepRef:    p.Linkage.StepRef,
		},
		Evidence: canonicalEvidenceRefsFromNormalized(p.Evidence),
		Runtime: canonicalRuntimeProvenance{
			ToolID:    p.Runtime.ToolID,
			RuntimeID: p.Runtime.RuntimeID,
			Adapter:   p.Runtime.Adapter,
		},
		Integrity: canonicalIntegrityProvenanceFromNormalized(p.Integrity),
	}
}

func canonicalIntegrityProvenanceFromNormalized(p *IntegrityProvenance) *canonicalIntegrityProvenance {
	if p == nil {
		return nil
	}
	out := &canonicalIntegrityProvenance{
		Lock:      string(p.Lock),
		Resources: make([]canonicalResourceProvenance, 0, len(p.Resources)),
	}
	for _, resource := range p.Resources {
		out.Resources = append(out.Resources, canonicalResourceProvenance{
			LogicalID:                   resource.LogicalID,
			SourceCoordinate:            resource.SourceCoordinate,
			MaterializedCoordinate:      resource.MaterializedCoordinate,
			PersistedResultCoordinate:   resource.PersistedResultCoordinate,
			SourceDigestSHA256:          resource.SourceDigestSHA256,
			LockState:                   string(resource.LockState),
			BaselineDigestSHA256:        resource.BaselineDigestSHA256,
			MaterializationStatus:       string(resource.MaterializationStatus),
			RuntimeBaselineDigestSHA256: resource.RuntimeBaselineDigestSHA256,
			RuntimeBaselineStatus:       string(resource.RuntimeBaselineStatus),
			PersistedResultDigestSHA256: resource.PersistedResultDigestSHA256,
			ResultStatus:                string(resource.ResultStatus),
		})
	}
	return out
}

func canonicalProvenanceInputsFromNormalized(inputs []ProvenanceInput) []canonicalProvenanceInput {
	out := make([]canonicalProvenanceInput, len(inputs))
	for i, input := range inputs {
		out[i] = canonicalProvenanceInput{
			Kind:         input.Kind,
			Identity:     input.Identity,
			DigestSHA256: input.DigestSHA256,
		}
	}
	return out
}

func canonicalEvidenceRefsFromNormalized(evidence []EvidenceReference) []canonicalEvidenceRef {
	out := make([]canonicalEvidenceRef, len(evidence))
	for i, item := range evidence {
		out[i] = canonicalEvidenceRef{
			Kind:         item.Kind,
			Ref:          item.Ref,
			DigestSHA256: item.DigestSHA256,
		}
	}
	return out
}

func canonicalPartsFromNormalized(parts []IdentityPart) []canonicalPart {
	out := make([]canonicalPart, len(parts))
	for i, part := range parts {
		out[i] = canonicalPart{
			Key:   part.Key,
			Value: part.Value,
		}
	}
	return out
}

func hashCanonicalIdentityMaterial(material canonicalIdentityMaterial) (string, error) {
	data, err := json.Marshal(material)
	if err != nil {
		return "", fmt.Errorf("marshal canonical identity material: %w", err)
	}

	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func isLowerHex64(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}
