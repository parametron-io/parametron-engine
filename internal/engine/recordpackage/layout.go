package recordpackage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"parametron/internal/engine/recordcontract"
)

const (
	// PackageDirectoryName is the canonical local record package root directory name.
	PackageDirectoryName = "parametron-record-package"

	// PackageManifestFileName is the canonical package manifest filename at the package root.
	PackageManifestFileName = "parametron.record-package.json"

	// RecordsDirectoryName is the canonical normalized records directory.
	RecordsDirectoryName = "records"

	// ArtifactsDirectoryName is the canonical packaged artifacts directory.
	ArtifactsDirectoryName = "artifacts"

	// ArtifactFilesDirectoryName is the canonical artifact payload files subdirectory.
	ArtifactFilesDirectoryName = "files"

	// RawEvidenceDirectoryName is the canonical raw evidence / operational outputs directory.
	RawEvidenceDirectoryName = "raw"

	rawReportFileName                    = "report.json"
	rawMetadataFileName                  = "metadata.json"
	rawArtifactStoreDirectoryName        = "artifact-store"
	rawArtifactStoreManifestName         = "manifest.json"
	rawHandoffDirectoryName              = "handoff"
	rawObservedDirectoryName             = "observed"
	rawObservedFileName                  = "parametron.observed.json"
	rawVerificationDirectoryName         = "verification"
	rawVerificationFileName              = "parametron.verification.json"
	rawRuntimeDirectoryName              = "runtime"
	rawRuntimeResultFileName             = "result.json"
	rawRuntimeReferenceTraversalFileName = "parametron.reference-traversal.json"
)

// EntryKind distinguishes file and directory layout entries.
type EntryKind string

const (
	EntryKindFile      EntryKind = "file"
	EntryKindDirectory EntryKind = "directory"
)

// EntryRole distinguishes the purpose of a layout entry.
type EntryRole string

const (
	EntryRolePackageManifest  EntryRole = "package-manifest"
	EntryRoleNormalizedRecord EntryRole = "normalized-record"
	EntryRoleArtifactPayload  EntryRole = "artifact-payload"
	EntryRoleRawEvidence      EntryRole = "raw-evidence"
)

// Entry describes one deterministic layout location in the local record package.
type Entry struct {
	ContractPath string
	Kind         EntryKind
	Role         EntryRole
	Family       recordcontract.Family
}

// PackageManifestContractPath returns the canonical package manifest contract path.
func PackageManifestContractPath() string {
	return PackageManifestFileName
}

// RecordsDirectoryContractPath returns the canonical normalized records directory contract path.
func RecordsDirectoryContractPath() string {
	return RecordsDirectoryName
}

// ArtifactsDirectoryContractPath returns the canonical artifacts directory contract path.
func ArtifactsDirectoryContractPath() string {
	return ArtifactsDirectoryName
}

// ArtifactFilesDirectoryContractPath returns the canonical artifact payload directory contract path.
func ArtifactFilesDirectoryContractPath() string {
	path, err := JoinContractPath(ArtifactsDirectoryName, ArtifactFilesDirectoryName)
	if err != nil {
		panic(fmt.Sprintf("recordpackage: internal artifact files path: %v", err))
	}
	return path
}

// RawEvidenceDirectoryContractPath returns the canonical raw evidence directory contract path.
func RawEvidenceDirectoryContractPath() string {
	return RawEvidenceDirectoryName
}

// RecordContractPath returns the canonical normalized record contract path for family.
func RecordContractPath(family recordcontract.Family) (string, bool) {
	fileName, ok := recordcontract.FileNameForFamily(family)
	if !ok {
		return "", false
	}
	path, err := JoinContractPath(RecordsDirectoryName, fileName)
	if err != nil {
		return "", false
	}
	return path, true
}

// MustRecordContractPath returns the canonical normalized record contract path for family
// or panics when family is unknown.
func MustRecordContractPath(family recordcontract.Family) string {
	path, ok := RecordContractPath(family)
	if !ok {
		panic(fmt.Sprintf("recordpackage: unknown record family %q", family))
	}
	return path
}

// RecordEntries returns normalized record layout entries in recordcontract.Definitions order.
func RecordEntries() []Entry {
	definitions := recordcontract.Definitions()
	out := make([]Entry, 0, len(definitions))
	for _, def := range definitions {
		path, ok := RecordContractPath(def.Family)
		if !ok {
			panic(fmt.Sprintf("recordpackage: missing record path for family %q", def.Family))
		}
		out = append(out, Entry{
			ContractPath: path,
			Kind:         EntryKindFile,
			Role:         EntryRoleNormalizedRecord,
			Family:       def.Family,
		})
	}
	return append([]Entry(nil), out...)
}

// RawEvidenceEntries returns raw evidence layout entries in deterministic order.
func RawEvidenceEntries() []Entry {
	rawPaths := []struct {
		path string
		kind EntryKind
	}{
		{RawReportContractPath(), EntryKindFile},
		{RawMetadataContractPath(), EntryKindFile},
		{RawArtifactStoreManifestContractPath(), EntryKindFile},
		{RawHandoffDirectoryContractPath(), EntryKindDirectory},
		{RawObservedContractPath(), EntryKindFile},
		{RawVerificationContractPath(), EntryKindFile},
		{RawRuntimeResultContractPath(), EntryKindFile},
		{RawRuntimeReferenceTraversalContractPath(), EntryKindFile},
	}

	out := make([]Entry, 0, len(rawPaths))
	for _, item := range rawPaths {
		out = append(out, Entry{
			ContractPath: item.path,
			Kind:         item.kind,
			Role:         EntryRoleRawEvidence,
		})
	}
	return append([]Entry(nil), out...)
}

// Entries returns the full deterministic local record package layout in stable order.
func Entries() []Entry {
	out := make([]Entry, 0, 1+1+len(RecordEntries())+2+1+len(RawEvidenceEntries()))

	out = append(out, Entry{
		ContractPath: PackageManifestContractPath(),
		Kind:         EntryKindFile,
		Role:         EntryRolePackageManifest,
	})
	out = append(out, Entry{
		ContractPath: RecordsDirectoryContractPath(),
		Kind:         EntryKindDirectory,
		Role:         EntryRoleNormalizedRecord,
	})
	out = append(out, RecordEntries()...)
	out = append(out, Entry{
		ContractPath: ArtifactsDirectoryContractPath(),
		Kind:         EntryKindDirectory,
		Role:         EntryRoleArtifactPayload,
	})
	out = append(out, Entry{
		ContractPath: ArtifactFilesDirectoryContractPath(),
		Kind:         EntryKindDirectory,
		Role:         EntryRoleArtifactPayload,
	})
	out = append(out, Entry{
		ContractPath: RawEvidenceDirectoryContractPath(),
		Kind:         EntryKindDirectory,
		Role:         EntryRoleRawEvidence,
	})
	out = append(out, RawEvidenceEntries()...)

	return append([]Entry(nil), out...)
}

// ValidateContractPath reports whether path is a safe layout-relative contract path.
func ValidateContractPath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: path is empty", ErrInvalidLayoutPath)
	}
	if strings.Contains(path, `\`) {
		return fmt.Errorf("%w: backslashes are not allowed in %q", ErrInvalidLayoutPath, path)
	}
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return fmt.Errorf("%w: absolute paths are not allowed in %q", ErrInvalidLayoutPath, path)
	}
	if strings.Contains(path, "//") {
		return fmt.Errorf("%w: repeated separators are not allowed in %q", ErrInvalidLayoutPath, path)
	}

	segments := strings.Split(path, "/")
	for _, segment := range segments {
		if segment == "" {
			return fmt.Errorf("%w: empty path segment in %q", ErrInvalidLayoutPath, path)
		}
		if segment == "." || segment == ".." {
			return fmt.Errorf("%w: traversal segment %q in %q", ErrInvalidLayoutPath, segment, path)
		}
	}
	return nil
}

// JoinContractPath joins layout-relative path parts using forward slashes.
func JoinContractPath(parts ...string) (string, error) {
	if len(parts) == 0 {
		return "", fmt.Errorf("%w: no path parts provided", ErrInvalidLayoutPath)
	}
	joined := strings.Join(parts, "/")
	if err := ValidateContractPath(joined); err != nil {
		return "", err
	}
	return joined, nil
}

// Layout resolves contract paths to filesystem paths under a package root.
type Layout struct {
	Root string
}

// NewLayout constructs a layout resolver for root without creating directories or files.
func NewLayout(root string) (Layout, error) {
	if strings.TrimSpace(root) == "" {
		return Layout{}, fmt.Errorf("%w: package root is empty", ErrInvalidLayoutPath)
	}
	cleaned := filepath.Clean(root)
	if !filepath.IsAbs(cleaned) {
		abs, err := filepath.Abs(cleaned)
		if err != nil {
			return Layout{}, fmt.Errorf("recordpackage: resolve package root: %w", err)
		}
		cleaned = abs
	}
	return Layout{Root: cleaned}, nil
}

// FSPath resolves a validated contract path to a filesystem path under Root.
func (l Layout) FSPath(contractPath string) (string, error) {
	if err := ValidateContractPath(contractPath); err != nil {
		return "", err
	}
	root := filepath.Clean(l.Root)
	resolved := filepath.Join(root, filepath.FromSlash(contractPath))
	resolved = filepath.Clean(resolved)

	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", fmt.Errorf("recordpackage: resolve contract path %q: %w", contractPath, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: contract path %q escapes package root", ErrInvalidLayoutPath, contractPath)
	}
	return resolved, nil
}

// RecordFSPath resolves the normalized record contract path for family to a filesystem path.
func (l Layout) RecordFSPath(family recordcontract.Family) (string, bool, error) {
	contractPath, ok := RecordContractPath(family)
	if !ok {
		return "", false, nil
	}
	fsPath, err := l.FSPath(contractPath)
	if err != nil {
		return "", true, err
	}
	return fsPath, true, nil
}

func mustJoinContractPath(parts ...string) string {
	path, err := JoinContractPath(parts...)
	if err != nil {
		panic(fmt.Sprintf("recordpackage: internal contract path: %v", err))
	}
	return path
}
