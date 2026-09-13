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
	}
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
