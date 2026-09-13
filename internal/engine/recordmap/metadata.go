package recordmap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"parametron/internal/engine/metadata"
	"parametron/internal/engine/recordcontract"
)

const (
	metadataSchemaVersion = "1.0"
	metadataEvidenceKind  = "metadata"
	metadataEvidenceRef   = "raw/metadata.json"
)

// MetadataMappingInput carries operational metadata material and caller-supplied provenance.
type MetadataMappingInput struct {
	Metadata metadata.Metadata

	// Optional base provenance supplied by the caller.
	// The mapper may enrich plan provenance, runtime provenance, input identity,
	// and evidence references from metadata fields, but must not fabricate
	// unavailable PDM/source revision data.
	Provenance recordcontract.Provenance
}

// MetadataMappingOutput carries normalized provenance and input identity surfaces.
type MetadataMappingOutput struct {
	Provenance recordcontract.Provenance
	Inputs     []recordcontract.ProvenanceInput
}

// MapMetadata converts operational metadata into normalized provenance and input identity surfaces.
func MapMetadata(input MetadataMappingInput) (MetadataMappingOutput, error) {
	if err := validateMetadataSchema(input.Metadata); err != nil {
		return MetadataMappingOutput{}, err
	}

	metadataInputs, err := MapMetadataToInputIdentitySurfaces(input.Metadata)
	if err != nil {
		return MetadataMappingOutput{}, err
	}

	provenance := copyProvenance(input.Provenance)
	mergedInputs, err := mergeProvenanceInputs(provenance.Inputs, metadataInputs)
	if err != nil {
		return MetadataMappingOutput{}, err
	}
	provenance.Inputs = mergedInputs
	provenance = enrichMetadataPlanProvenance(provenance, input.Metadata.PlanHash)
	provenance = enrichMetadataRuntimeProvenance(provenance, input.Metadata.Toolchain)
	provenance.Evidence = appendMetadataEvidence(provenance.Evidence)

	normalized := recordcontract.NormalizeProvenance(provenance)
	if err := recordcontract.ValidateProvenance(normalized); err != nil {
		return MetadataMappingOutput{}, fmt.Errorf("%w: validate provenance: %w", ErrInvalidMetadataMapping, err)
	}

	return MetadataMappingOutput{
		Provenance: normalized,
		Inputs:     normalized.Inputs,
	}, nil
}

// MapMetadataToProvenance converts operational metadata into normalized provenance.
func MapMetadataToProvenance(input MetadataMappingInput) (recordcontract.Provenance, error) {
	output, err := MapMetadata(input)
	if err != nil {
		return recordcontract.Provenance{}, err
	}
	return output.Provenance, nil
}

// MapMetadataToInputIdentitySurfaces derives metadata-owned input identity surfaces.
func MapMetadataToInputIdentitySurfaces(md metadata.Metadata) ([]recordcontract.ProvenanceInput, error) {
	if err := validateMetadataSchema(md); err != nil {
		return nil, err
	}

	inputs := make([]recordcontract.ProvenanceInput, 0, 8)

	dslInput, includeDSL, err := mapDSLInput(md)
	if err != nil {
		return nil, err
	}
	if includeDSL {
		inputs = append(inputs, dslInput)
	}

	modelInputs, err := mapModelInputs(md)
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, modelInputs...)

	tableInputs, err := mapTableInputs(md)
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, tableInputs...)

	profileInput, includeProfile, err := mapProfileInput(md.Profile)
	if err != nil {
		return nil, err
	}
	if includeProfile {
		inputs = append(inputs, profileInput)
	}

	merged, err := deduplicateProvenanceInputs(inputs)
	if err != nil {
		return nil, err
	}

	return recordcontract.NormalizeProvenance(recordcontract.Provenance{Inputs: merged}).Inputs, nil
}

func validateMetadataSchema(md metadata.Metadata) error {
	schemaVersion := strings.TrimSpace(md.SchemaVersion)
	if schemaVersion == "" {
		return fmt.Errorf("%w: metadata schema version is required", ErrInvalidMetadataMapping)
	}
	if schemaVersion != metadataSchemaVersion {
		return fmt.Errorf("%w: unsupported metadata schema version %q", ErrInvalidMetadataMapping, md.SchemaVersion)
	}
	return nil
}

func mapDSLInput(md metadata.Metadata) (recordcontract.ProvenanceInput, bool, error) {
	if md.ProjectInputs != nil {
		dsl := md.ProjectInputs.DSL
		signature := strings.TrimSpace(dsl.Signature)
		if signature != "" {
			if err := validateMappingDigestSHA256("dsl signature", signature); err != nil {
				return recordcontract.ProvenanceInput{}, false, err
			}

			identity := strings.TrimSpace(dsl.ResolvedPath)
			if identity == "" {
				identity = "dsl"
			}

			return recordcontract.ProvenanceInput{
				Kind:         "dsl",
				Identity:     identity,
				DigestSHA256: signature,
			}, true, nil
		}
	}

	dslHash := strings.TrimSpace(md.DSLHash)
	if dslHash != "" {
		if err := validateMappingDigestSHA256("dslHash", dslHash); err != nil {
			return recordcontract.ProvenanceInput{}, false, err
		}
	}

	return recordcontract.ProvenanceInput{
		Kind:         "dsl",
		Identity:     "dsl",
		DigestSHA256: dslHash,
	}, true, nil
}

func mapModelInputs(md metadata.Metadata) ([]recordcontract.ProvenanceInput, error) {
	if md.ProjectInputs == nil || len(md.ProjectInputs.Models) == 0 {
		return []recordcontract.ProvenanceInput{}, nil
	}

	out := make([]recordcontract.ProvenanceInput, 0, len(md.ProjectInputs.Models))
	for i, model := range md.ProjectInputs.Models {
		identity := strings.TrimSpace(model.LogicalID)
		if identity == "" {
			identity = strings.TrimSpace(model.ResolvedPath)
		}
		if identity == "" {
			return nil, fmt.Errorf("%w: model input %d identity is required", ErrInvalidMetadataMapping, i)
		}

		signature := strings.TrimSpace(model.Signature)
		if err := validateMappingDigestSHA256(fmt.Sprintf("model input %d signature", i), signature); err != nil {
			return nil, err
		}

		out = append(out, recordcontract.ProvenanceInput{
			Kind:         "model",
			Identity:     identity,
			DigestSHA256: signature,
		})
	}

	return out, nil
}

func mapTableInputs(md metadata.Metadata) ([]recordcontract.ProvenanceInput, error) {
	out := make([]recordcontract.ProvenanceInput, 0)
	byLogicalID := make(map[string]string)

	if md.ProjectInputs != nil {
		for i, table := range md.ProjectInputs.Tables {
			identity := strings.TrimSpace(table.LogicalID)
			if identity == "" {
				identity = strings.TrimSpace(table.ResolvedPath)
			}
			if identity == "" {
				return nil, fmt.Errorf("%w: project table input %d identity is required", ErrInvalidMetadataMapping, i)
			}

			signature := strings.TrimSpace(table.Signature)
			if err := validateMappingDigestSHA256(fmt.Sprintf("project table input %d signature", i), signature); err != nil {
				return nil, err
			}

			logicalID := strings.TrimSpace(table.LogicalID)
			if logicalID != "" {
				if existing, ok := byLogicalID[logicalID]; ok && existing != "" && signature != "" && existing != signature {
					return nil, fmt.Errorf("%w: conflicting digests for table logical id %q", ErrInvalidMetadataMapping, logicalID)
				}
				byLogicalID[logicalID] = signature
			}

			out = append(out, recordcontract.ProvenanceInput{
				Kind:         "table",
				Identity:     identity,
				DigestSHA256: signature,
			})
		}
	}

	for i, table := range md.Tables {
		logicalID := strings.TrimSpace(table.LogicalID)
		identity := logicalID
		if identity == "" {
			identity = strings.TrimSpace(table.Name)
		}
		if identity == "" {
			return nil, fmt.Errorf("%w: metadata table input %d identity is required", ErrInvalidMetadataMapping, i)
		}

		fingerprint := strings.TrimSpace(table.Fingerprint)
		if err := validateMappingDigestSHA256(fmt.Sprintf("metadata table input %d fingerprint", i), fingerprint); err != nil {
			return nil, err
		}

		if logicalID != "" {
			if existing, ok := byLogicalID[logicalID]; ok && existing != "" && fingerprint != "" && existing != fingerprint {
				return nil, fmt.Errorf("%w: conflicting digests for table logical id %q", ErrInvalidMetadataMapping, logicalID)
			} else if ok {
				continue
			}
			byLogicalID[logicalID] = fingerprint
		}

		out = append(out, recordcontract.ProvenanceInput{
			Kind:         "table",
			Identity:     identity,
			DigestSHA256: fingerprint,
		})
	}

	return out, nil
}

func mapProfileInput(profile metadata.ProfileMetadata) (recordcontract.ProvenanceInput, bool, error) {
	if profile.IsZero() {
		return recordcontract.ProvenanceInput{}, false, nil
	}

	profileJSON, err := profile.MarshalJSON()
	if err != nil {
		return recordcontract.ProvenanceInput{}, false, fmt.Errorf("%w: profile digest: %w", ErrInvalidMetadataMapping, err)
	}

	sum := sha256.Sum256(profileJSON)
	digest := hex.EncodeToString(sum[:])

	identity := strings.TrimSpace(profile.Name)
	if identity == "" {
		identity = "profile"
	}

	return recordcontract.ProvenanceInput{
		Kind:         "profile",
		Identity:     identity,
		DigestSHA256: digest,
	}, true, nil
}

func enrichMetadataPlanProvenance(provenance recordcontract.Provenance, planHash string) recordcontract.Provenance {
	if strings.TrimSpace(provenance.Plan.PlanHash) == "" {
		provenance.Plan.PlanHash = strings.TrimSpace(planHash)
	}
	return provenance
}

func enrichMetadataRuntimeProvenance(provenance recordcontract.Provenance, toolchain metadata.ToolchainMetadata) recordcontract.Provenance {
	parametronVersion := strings.TrimSpace(toolchain.ParametronVersion)
	goVersion := strings.TrimSpace(toolchain.GoVersion)

	if strings.TrimSpace(provenance.Runtime.ToolID) == "" && (parametronVersion != "" || goVersion != "") {
		provenance.Runtime.ToolID = "parametron"
	}

	if strings.TrimSpace(provenance.Runtime.RuntimeID) == "" {
		provenance.Runtime.RuntimeID = deriveMetadataRuntimeID(parametronVersion, goVersion)
	}

	return provenance
}

func deriveMetadataRuntimeID(parametronVersion, goVersion string) string {
	switch {
	case parametronVersion != "" && goVersion != "":
		return "parametron=" + parametronVersion + ";go=" + goVersion
	case parametronVersion != "":
		return "parametron=" + parametronVersion
	case goVersion != "":
		return "go=" + goVersion
	default:
		return ""
	}
}

func appendMetadataEvidence(existing []recordcontract.EvidenceReference) []recordcontract.EvidenceReference {
	for _, item := range existing {
		if item.Kind == metadataEvidenceKind && item.Ref == metadataEvidenceRef {
			return copyEvidenceReferences(existing)
		}
	}

	out := copyEvidenceReferences(existing)
	out = append(out, recordcontract.EvidenceReference{
		Kind: metadataEvidenceKind,
		Ref:  metadataEvidenceRef,
	})
	return out
}

func mergeProvenanceInputs(callerInputs, metadataInputs []recordcontract.ProvenanceInput) ([]recordcontract.ProvenanceInput, error) {
	merged := make([]recordcontract.ProvenanceInput, 0, len(callerInputs)+len(metadataInputs))
	merged = append(merged, copyProvenanceInputs(callerInputs)...)
	merged = append(merged, copyProvenanceInputs(metadataInputs)...)

	return deduplicateProvenanceInputs(merged)
}

func deduplicateProvenanceInputs(inputs []recordcontract.ProvenanceInput) ([]recordcontract.ProvenanceInput, error) {
	if len(inputs) == 0 {
		return []recordcontract.ProvenanceInput{}, nil
	}

	type inputKey struct {
		kind     string
		identity string
	}

	byKey := make(map[inputKey]recordcontract.ProvenanceInput, len(inputs))
	order := make([]inputKey, 0, len(inputs))

	for _, input := range inputs {
		key := inputKey{
			kind:     strings.TrimSpace(input.Kind),
			identity: strings.TrimSpace(input.Identity),
		}

		existing, ok := byKey[key]
		if !ok {
			byKey[key] = recordcontract.ProvenanceInput{
				Kind:         key.kind,
				Identity:     key.identity,
				DigestSHA256: strings.TrimSpace(input.DigestSHA256),
			}
			order = append(order, key)
			continue
		}

		existingDigest := strings.TrimSpace(existing.DigestSHA256)
		inputDigest := strings.TrimSpace(input.DigestSHA256)
		if existingDigest != "" && inputDigest != "" && existingDigest != inputDigest {
			return nil, fmt.Errorf("%w: conflicting digests for provenance input kind %q identity %q", ErrInvalidMetadataMapping, key.kind, key.identity)
		}

		if existingDigest == "" && inputDigest != "" {
			existing.DigestSHA256 = inputDigest
			byKey[key] = existing
		}
	}

	out := make([]recordcontract.ProvenanceInput, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}

	return out, nil
}

func validateMappingDigestSHA256(field, digest string) error {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil
	}
	if len(digest) != 64 || !isMappingLowerHex64(digest) {
		return fmt.Errorf("%w: %s must be a 64-character lowercase hexadecimal digest", ErrInvalidMetadataMapping, field)
	}
	return nil
}

func isMappingLowerHex64(value string) bool {
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

func copyProvenance(p recordcontract.Provenance) recordcontract.Provenance {
	return recordcontract.Provenance{
		SourceRevision: p.SourceRevision,
		Inputs:         copyProvenanceInputs(p.Inputs),
		Plan:           p.Plan,
		Linkage:        p.Linkage,
		Evidence:       copyEvidenceReferences(p.Evidence),
		Runtime:        p.Runtime,
	}
}

func copyProvenanceInputs(inputs []recordcontract.ProvenanceInput) []recordcontract.ProvenanceInput {
	if len(inputs) == 0 {
		return []recordcontract.ProvenanceInput{}
	}
	out := make([]recordcontract.ProvenanceInput, len(inputs))
	copy(out, inputs)
	return out
}

func copyEvidenceReferences(evidence []recordcontract.EvidenceReference) []recordcontract.EvidenceReference {
	if len(evidence) == 0 {
		return []recordcontract.EvidenceReference{}
	}
	out := make([]recordcontract.EvidenceReference, len(evidence))
	copy(out, evidence)
	return out
}
