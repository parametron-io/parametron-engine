package recordcontract

import (
	"fmt"
	"sort"
)

// IdentityAlgorithm names a deterministic record identity derivation algorithm.
type IdentityAlgorithm string

const IdentityAlgorithmSHA256CanonicalV1 IdentityAlgorithm = "sha256-canonical-json-v1"

// IdentityPart carries optional deterministic key/value identity material.
type IdentityPart struct {
	Key   string
	Value string
}

// IdentityInput carries normalized material used to derive a record identity.
type IdentityInput struct {
	Family     Family
	Version    string
	RecordKey  string
	Provenance Provenance
	Parts      []IdentityPart
}

// Identity is a derived deterministic record identity.
type Identity struct {
	ID        string
	Algorithm IdentityAlgorithm
	Family    Family
	Version   string
}

// DeriveIdentity validates and normalizes input material and returns a deterministic identity.
func DeriveIdentity(input IdentityInput) (Identity, error) {
	normalized, err := validateIdentityInput(input)
	if err != nil {
		return Identity{}, err
	}

	id, err := hashCanonicalIdentityMaterial(canonicalIdentityMaterialFromInput(IdentityAlgorithmSHA256CanonicalV1, normalized))
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrInvalidIdentity, err)
	}

	return Identity{
		ID:        id,
		Algorithm: IdentityAlgorithmSHA256CanonicalV1,
		Family:    normalized.Family,
		Version:   normalized.Version,
	}, nil
}

func validateIdentityInput(input IdentityInput) (IdentityInput, error) {
	normalized := normalizeIdentityInput(input)

	if err := ValidateFamily(normalized.Family); err != nil {
		return IdentityInput{}, fmt.Errorf("%w: %w", ErrInvalidIdentity, err)
	}
	if err := ValidateVersion(normalized.Version); err != nil {
		return IdentityInput{}, fmt.Errorf("%w: %w", ErrInvalidIdentity, err)
	}
	if normalized.RecordKey == "" {
		return IdentityInput{}, fmt.Errorf("%w: record key is required", ErrInvalidIdentity)
	}
	if err := ValidateProvenance(normalized.Provenance); err != nil {
		return IdentityInput{}, fmt.Errorf("%w: %w", ErrInvalidIdentity, err)
	}

	for i, part := range normalized.Parts {
		if part.Key == "" {
			return IdentityInput{}, fmt.Errorf("%w: identity part %d key is required", ErrInvalidIdentity, i)
		}
		if part.Value == "" {
			return IdentityInput{}, fmt.Errorf("%w: identity part %d value is required", ErrInvalidIdentity, i)
		}
	}

	return normalized, nil
}

func normalizeIdentityInput(input IdentityInput) IdentityInput {
	return IdentityInput{
		Family:     NormalizeFamily(input.Family),
		Version:    trimString(input.Version),
		RecordKey:  trimString(input.RecordKey),
		Provenance: NormalizeProvenance(input.Provenance),
		Parts:      normalizeIdentityParts(input.Parts),
	}
}

func normalizeIdentityParts(parts []IdentityPart) []IdentityPart {
	if len(parts) == 0 {
		return []IdentityPart{}
	}

	out := make([]IdentityPart, len(parts))
	for i, part := range parts {
		out[i] = IdentityPart{
			Key:   trimString(part.Key),
			Value: trimString(part.Value),
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Value < out[j].Value
	})

	return out
}
