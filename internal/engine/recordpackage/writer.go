package recordpackage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PackageInput carries validated material used to materialize a local record package.
type PackageInput struct {
	// PackageRoot is the filesystem path to the package root directory. When
	// UseCanonicalDirectoryName is true, PackageRoot is treated as the parent
	// directory and the writer creates PackageDirectoryName beneath it.
	PackageRoot string

	// UseCanonicalDirectoryName requests creation of parametron-record-package/
	// under PackageRoot instead of writing directly into PackageRoot.
	UseCanonicalDirectoryName bool

	// PackageKey is required package identity metadata recorded in the manifest.
	PackageKey string

	// Records carries one or more validated Engine-produced normalized records.
	Records []Record

	// ArtifactFiles carries optional artifact payload bytes under artifacts/files.
	ArtifactFiles []ArtifactFile

	// RawEvidenceFiles carries optional raw evidence bytes at canonical raw paths.
	RawEvidenceFiles []RawEvidenceFile

	// OverwriteExisting allows replacing an existing package manifest and other
	// layout-owned files in a non-empty package root.
	OverwriteExisting bool
}

// Writer materializes local Engine record packages on disk.
type Writer struct{}

// NewWriter constructs a stateless record package writer.
func NewWriter() *Writer {
	return &Writer{}
}

// WritePackage materializes input at the resolved package root.
func (w *Writer) WritePackage(input PackageInput) error {
	return WritePackage(input)
}

// WritePackage materializes input at the resolved package root.
func WritePackage(input PackageInput) error {
	packageKey := strings.TrimSpace(input.PackageKey)
	if packageKey == "" {
		return fmt.Errorf("%w: package key is required", ErrInvalidPackageInput)
	}

	packageRoot, err := resolvePackageRoot(input.PackageRoot, input.UseCanonicalDirectoryName)
	if err != nil {
		return err
	}

	records, err := validateRecords(input.Records)
	if err != nil {
		return err
	}

	artifacts, err := validateArtifactFiles(input.ArtifactFiles)
	if err != nil {
		return err
	}

	rawEvidence, err := validateRawEvidenceFiles(input.RawEvidenceFiles)
	if err != nil {
		return err
	}

	layout, err := NewLayout(packageRoot)
	if err != nil {
		return err
	}

	if err := checkDestinationWritable(layout, input.OverwriteExisting); err != nil {
		return err
	}

	if err := os.MkdirAll(layout.Root, packageDirPerm); err != nil {
		return fmt.Errorf("create package root %q: %w", layout.Root, err)
	}

	return writeValidatedPackage(layout, packageKey, records, artifacts, rawEvidence)
}

func resolvePackageRoot(root string, useCanonicalDirectoryName bool) (string, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return "", fmt.Errorf("%w: package root is empty", ErrInvalidPackageInput)
	}

	cleaned := filepath.Clean(trimmed)
	if useCanonicalDirectoryName {
		cleaned = filepath.Join(cleaned, PackageDirectoryName)
	}

	if !filepath.IsAbs(cleaned) {
		abs, err := filepath.Abs(cleaned)
		if err != nil {
			return "", fmt.Errorf("recordpackage: resolve package root: %w", err)
		}
		cleaned = abs
	}

	return cleaned, nil
}
