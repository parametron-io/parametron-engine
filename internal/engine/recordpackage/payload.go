package recordpackage

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// ArtifactFile carries artifact payload bytes at a layout-relative contract path
// under artifacts/files.
type ArtifactFile struct {
	ContractPath string
	Content      []byte
}

// RawEvidenceFile carries raw evidence bytes at a canonical raw evidence contract path.
type RawEvidenceFile struct {
	ContractPath string
	Content      []byte
}

type validatedArtifactFile struct {
	contractPath string
	content      []byte
}

type validatedRawEvidenceFile struct {
	contractPath string
	content      []byte
}

func validateArtifactFiles(files []ArtifactFile) ([]validatedArtifactFile, error) {
	if len(files) == 0 {
		return nil, nil
	}

	prefix := ArtifactFilesDirectoryContractPath() + "/"
	validated := make([]validatedArtifactFile, 0, len(files))
	seen := make(map[string]struct{}, len(files))

	for i, file := range files {
		contractPath := strings.TrimSpace(file.ContractPath)
		if contractPath == "" {
			return nil, fmt.Errorf("artifactFiles[%d]: %w: contract path is required", i, ErrInvalidLayoutPath)
		}
		if err := ValidateContractPath(contractPath); err != nil {
			return nil, fmt.Errorf("artifactFiles[%d]: %w", i, err)
		}
		if !strings.HasPrefix(contractPath, prefix) {
			return nil, fmt.Errorf("artifactFiles[%d]: %w: contract path %q must be under %q", i, ErrInvalidLayoutPath, contractPath, prefix)
		}
		if len(file.Content) == 0 {
			return nil, fmt.Errorf("artifactFiles[%d]: %w: content is required", i, ErrInvalidPackageInput)
		}
		if _, exists := seen[contractPath]; exists {
			return nil, fmt.Errorf("artifactFiles[%d]: %w: duplicate contract path %q", i, ErrInvalidLayoutPath, contractPath)
		}
		seen[contractPath] = struct{}{}

		validated = append(validated, validatedArtifactFile{
			contractPath: contractPath,
			content:      append([]byte(nil), file.Content...),
		})
	}

	sortValidatedArtifactFiles(validated)
	return validated, nil
}

func validateRawEvidenceFiles(files []RawEvidenceFile) ([]validatedRawEvidenceFile, error) {
	if len(files) == 0 {
		return nil, nil
	}

	validated := make([]validatedRawEvidenceFile, 0, len(files))
	seen := make(map[string]struct{}, len(files))

	for i, file := range files {
		contractPath := strings.TrimSpace(file.ContractPath)
		if contractPath == "" {
			return nil, fmt.Errorf("rawEvidenceFiles[%d]: %w: contract path is required", i, ErrInvalidLayoutPath)
		}
		if err := ValidateContractPath(contractPath); err != nil {
			return nil, fmt.Errorf("rawEvidenceFiles[%d]: %w", i, err)
		}
		if !IsRawEvidenceFileContractPath(contractPath) {
			return nil, fmt.Errorf("rawEvidenceFiles[%d]: %w: contract path %q is not a raw evidence file contract path", i, ErrInvalidLayoutPath, contractPath)
		}
		if len(file.Content) == 0 {
			return nil, fmt.Errorf("rawEvidenceFiles[%d]: %w: content is required", i, ErrInvalidPackageInput)
		}
		if _, exists := seen[contractPath]; exists {
			return nil, fmt.Errorf("rawEvidenceFiles[%d]: %w: duplicate contract path %q", i, ErrInvalidLayoutPath, contractPath)
		}
		seen[contractPath] = struct{}{}

		validated = append(validated, validatedRawEvidenceFile{
			contractPath: contractPath,
			content:      append([]byte(nil), file.Content...),
		})
	}

	sortValidatedRawEvidenceFiles(validated)
	return validated, nil
}

func ensurePackageDirectories(layout Layout, artifacts []validatedArtifactFile, rawEvidence []validatedRawEvidenceFile) error {
	required := map[string]struct{}{
		RecordsDirectoryContractPath():         {},
		ArtifactRecordsDirectoryContractPath(): {},
		ArtifactsDirectoryContractPath():       {},
		ArtifactFilesDirectoryContractPath():   {},
		RawEvidenceDirectoryContractPath():     {},
	}

	for _, entry := range RawEvidenceEntries() {
		if entry.Kind == EntryKindDirectory {
			required[entry.ContractPath] = struct{}{}
		}
	}

	for _, artifact := range artifacts {
		dir, err := contractPathParent(artifact.contractPath)
		if err != nil {
			return err
		}
		required[dir] = struct{}{}
	}
	for _, item := range rawEvidence {
		dir, err := contractPathParent(item.contractPath)
		if err != nil {
			return err
		}
		required[dir] = struct{}{}
	}

	paths := make([]string, 0, len(required))
	for contractPath := range required {
		paths = append(paths, contractPath)
	}
	sort.Strings(paths)

	for _, contractPath := range paths {
		fsPath, err := layout.FSPath(contractPath)
		if err != nil {
			return err
		}
		if err := ensureDirectory(fsPath); err != nil {
			return err
		}
	}

	return nil
}

func checkDestinationWritable(layout Layout, overwrite bool) error {
	root := layout.Root
	info, err := os.Stat(root)
	rootExists := false
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat package root %q: %w", root, err)
		}
	} else {
		if !info.IsDir() {
			return fmt.Errorf("%w: package root %q is not a directory", ErrPackageDestinationExists, root)
		}
		rootExists = true
	}

	manifestPath, err := layout.FSPath(PackageManifestContractPath())
	if err != nil {
		return err
	}

	if _, err := os.Stat(manifestPath); err == nil {
		if !overwrite {
			return fmt.Errorf("%w: %s", ErrPackageManifestExists, PackageManifestContractPath())
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat package manifest %q: %w", manifestPath, err)
	}

	if !rootExists {
		return nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read package root %q: %w", root, err)
	}
	if len(entries) == 0 {
		return nil
	}
	if overwrite {
		return nil
	}

	return fmt.Errorf("%w: package root %q is not empty", ErrPackageDestinationExists, root)
}

func writeValidatedPackage(layout Layout, packageKey string, records []validatedRecord, artifacts []validatedArtifactFile, rawEvidence []validatedRawEvidenceFile) error {
	if err := ensurePackageDirectories(layout, artifacts, rawEvidence); err != nil {
		return err
	}

	for _, record := range records {
		data, err := marshalRecordPayload(record.payload)
		if err != nil {
			return fmt.Errorf("records[%s]: %w", record.family, err)
		}
		fsPath, err := layout.FSPath(record.contractPath)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(fsPath, data); err != nil {
			return fmt.Errorf("write record %q: %w", record.contractPath, err)
		}
	}

	for _, artifact := range artifacts {
		fsPath, err := layout.FSPath(artifact.contractPath)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(fsPath, artifact.content); err != nil {
			return fmt.Errorf("write artifact %q: %w", artifact.contractPath, err)
		}
	}

	for _, item := range rawEvidence {
		fsPath, err := layout.FSPath(item.contractPath)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(fsPath, item.content); err != nil {
			return fmt.Errorf("write raw evidence %q: %w", item.contractPath, err)
		}
	}

	manifest, err := buildPackageManifest(packageKey, records, artifacts, rawEvidence)
	if err != nil {
		return err
	}
	manifestData, err := marshalPackageManifest(manifest)
	if err != nil {
		return err
	}

	manifestPath, err := layout.FSPath(PackageManifestContractPath())
	if err != nil {
		return err
	}
	if err := writeFileAtomic(manifestPath, manifestData); err != nil {
		return fmt.Errorf("write package manifest: %w", err)
	}

	return nil
}

func contractPathParent(contractPath string) (string, error) {
	idx := strings.LastIndex(contractPath, "/")
	if idx <= 0 {
		return "", fmt.Errorf("%w: contract path %q has no parent directory", ErrInvalidLayoutPath, contractPath)
	}
	parent := contractPath[:idx]
	if err := ValidateContractPath(parent); err != nil {
		return "", err
	}
	return parent, nil
}
