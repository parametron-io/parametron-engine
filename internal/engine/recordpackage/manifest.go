package recordpackage

import (
	"encoding/json"
	"fmt"
	"sort"

	"parametron/internal/engine/recordcontract"
)

const packageManifestSchemaVersion = "1.0"

type packageManifest struct {
	SchemaVersion string                    `json:"schemaVersion"`
	PackageKey    string                    `json:"packageKey"`
	LayoutVersion string                    `json:"layoutVersion"`
	Ownership     packageManifestOwnership  `json:"ownership"`
	Records       []packageManifestRecord   `json:"records"`
	Artifacts     []packageManifestArtifact `json:"artifacts,omitempty"`
	RawEvidence   []packageManifestRawEntry `json:"rawEvidence,omitempty"`
}

type packageManifestOwnership struct {
	Producer            string `json:"producer"`
	LocalOutputEmitter  string `json:"localOutputEmitter"`
	IngestionTarget     string `json:"ingestionTarget"`
	DurableStorageOwner string `json:"durableStorageOwner"`
}

type packageManifestRecord struct {
	Family       string `json:"family"`
	ContractPath string `json:"contractPath"`
	RecordKey    string `json:"recordKey"`
	IdentityID   string `json:"identityId"`
}

type packageManifestArtifact struct {
	ContractPath string `json:"contractPath"`
}

type packageManifestRawEntry struct {
	ContractPath string `json:"contractPath"`
}

func buildPackageManifest(packageKey string, records []validatedRecord, artifacts []validatedArtifactFile, rawEvidence []validatedRawEvidenceFile) (packageManifest, error) {
	ownership := recordcontract.EngineProducedOwnership()
	if err := recordcontract.ValidateOwnership(ownership); err != nil {
		return packageManifest{}, fmt.Errorf("recordpackage: ownership: %w", err)
	}

	manifest := packageManifest{
		SchemaVersion: packageManifestSchemaVersion,
		PackageKey:    packageKey,
		LayoutVersion: recordcontract.CurrentVersion,
		Ownership: packageManifestOwnership{
			Producer:            string(ownership.Producer),
			LocalOutputEmitter:  string(ownership.LocalOutputEmitter),
			IngestionTarget:     string(ownership.IngestionTarget),
			DurableStorageOwner: string(ownership.DurableStorageOwner),
		},
		Records: make([]packageManifestRecord, 0, len(records)),
	}

	for _, record := range records {
		manifest.Records = append(manifest.Records, packageManifestRecord{
			Family:       string(record.family),
			ContractPath: record.contractPath,
			RecordKey:    record.recordKey,
			IdentityID:   record.identityID,
		})
	}

	if len(artifacts) > 0 {
		manifest.Artifacts = make([]packageManifestArtifact, 0, len(artifacts))
		for _, artifact := range artifacts {
			manifest.Artifacts = append(manifest.Artifacts, packageManifestArtifact{
				ContractPath: artifact.contractPath,
			})
		}
	}

	if len(rawEvidence) > 0 {
		manifest.RawEvidence = make([]packageManifestRawEntry, 0, len(rawEvidence))
		for _, item := range rawEvidence {
			manifest.RawEvidence = append(manifest.RawEvidence, packageManifestRawEntry{
				ContractPath: item.contractPath,
			})
		}
	}

	return manifest, nil
}

func marshalPackageManifest(manifest packageManifest) ([]byte, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal package manifest: %w", err)
	}
	return append(data, '\n'), nil
}

func marshalRecordPayload(payload any) ([]byte, error) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal record payload: %w", err)
	}
	return append(data, '\n'), nil
}

func sortValidatedArtifactFiles(files []validatedArtifactFile) {
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].contractPath < files[j].contractPath
	})
}

func sortValidatedRawEvidenceFiles(files []validatedRawEvidenceFile) {
	order := rawEvidenceContractPathOrder()
	handoffPosition := handoffRawEvidenceOrderPosition()
	sort.SliceStable(files, func(i, j int) bool {
		left := rawEvidenceSortPosition(files[i].contractPath, order, handoffPosition)
		right := rawEvidenceSortPosition(files[j].contractPath, order, handoffPosition)
		if left != right {
			return left < right
		}
		return files[i].contractPath < files[j].contractPath
	})
}

func rawEvidenceSortPosition(path string, order map[string]int, handoffPosition int) int {
	if position, ok := order[path]; ok {
		return position
	}
	if IsRawHandoffEvidenceFileContractPath(path) {
		return handoffPosition
	}
	return len(order) + 1
}

func rawEvidenceContractPathOrder() map[string]int {
	entries := RawEvidenceEntries()
	order := make(map[string]int, len(entries))
	for i, entry := range entries {
		if entry.Kind == EntryKindFile {
			order[entry.ContractPath] = i
		}
	}
	return order
}

func handoffRawEvidenceOrderPosition() int {
	for i, entry := range RawEvidenceEntries() {
		if entry.ContractPath == RawHandoffDirectoryContractPath() {
			return i
		}
	}
	panic("recordpackage: missing handoff raw evidence entry")
}
