package freecad

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"parametron/internal/authoring/planner"
)

const (
	freeCADRuntimeWorkingRootName       = "_working"
	freeCADRuntimeSourceDirName         = "source"
	freeCADRuntimeResultFilename        = "result.json"
	freeCADRuntimeWorkingCopyHashLength = 16
	freeCADRuntimeWorkingCopyPrefixMax  = 48
)

// FreeCADRuntimeWorkingCopyLayoutRequest contains the stable inputs used to
// derive an aligned external FreeCAD runtime working-copy layout.
type FreeCADRuntimeWorkingCopyLayoutRequest struct {
	ProductDir string
	ProductKey string
	PlanHash   string
	SourcePath string
}

// FreeCADRuntimeWorkingCopyLayout describes the host and manifest-facing paths
// for an aligned external FreeCAD runtime execution. Computing the layout has
// no filesystem side effects.
type FreeCADRuntimeWorkingCopyLayout struct {
	ProductDir            string
	WorkingRoot           string
	WorkingCopyDir        string
	WorkingCopyID         string
	SourceDir             string
	SourceDocument        string
	SourceDocumentPath    string
	ManifestPath          string
	ResultPath            string
	OutputDir             string
	OutputDirRelativePath string
}

// FreeCADRuntimeWorkingCopyPreparationRequest contains an already-computed
// aligned layout and the host source document to materialize into it.
type FreeCADRuntimeWorkingCopyPreparationRequest struct {
	Layout     FreeCADRuntimeWorkingCopyLayout
	SourcePath string
}

// FreeCADRuntimeWorkingCopyPreparationResult describes the prepared aligned
// working copy.
type FreeCADRuntimeWorkingCopyPreparationResult struct {
	Layout FreeCADRuntimeWorkingCopyLayout
}

// ComputeFreeCADRuntimeWorkingCopyLayout derives a deterministic aligned
// working-copy layout without creating any directories or files.
func ComputeFreeCADRuntimeWorkingCopyLayout(req FreeCADRuntimeWorkingCopyLayoutRequest) (FreeCADRuntimeWorkingCopyLayout, error) {
	productDir, err := normalizeFreeCADRuntimeHostPath("product directory", req.ProductDir)
	if err != nil {
		return FreeCADRuntimeWorkingCopyLayout{}, err
	}
	sourcePath, err := normalizeFreeCADRuntimeHostPath("source path", req.SourcePath)
	if err != nil {
		return FreeCADRuntimeWorkingCopyLayout{}, err
	}

	sourceBasename := filepath.Base(sourcePath)
	sourceStem := strings.TrimSuffix(sourceBasename, filepath.Ext(sourceBasename))
	prefix := sanitizeFreeCADRuntimeWorkingCopyPrefix(req.ProductKey)
	if prefix == "" {
		prefix = sanitizeFreeCADRuntimeWorkingCopyPrefix(sourceStem)
	}
	if prefix == "" {
		prefix = "working-copy"
	}

	workingCopyID := prefix + "-" + freeCADRuntimeWorkingCopyHash(req.ProductKey, req.PlanHash, sourcePath)
	return computeFreeCADRuntimeWorkingCopyLayoutWithID(
		productDir,
		sourcePath,
		workingCopyID,
		planner.FreeCADRuntimeExportManifestFilename,
		freeCADRuntimeResultFilename,
	)
}

// computeFreeCADRuntimeWorkingCopyLayoutWithID derives an aligned working-copy
// layout for an already-computed working-copy ID and declared handoff filenames.
// It creates no directories or files.
func computeFreeCADRuntimeWorkingCopyLayoutWithID(
	productDir string,
	sourcePath string,
	workingCopyID string,
	manifestFilename string,
	resultFilename string,
) (FreeCADRuntimeWorkingCopyLayout, error) {
	if err := requireCanonicalAbsoluteFreeCADRuntimeHostPath("product directory", productDir); err != nil {
		return FreeCADRuntimeWorkingCopyLayout{}, err
	}
	if err := requireCanonicalAbsoluteFreeCADRuntimeHostPath("source path", sourcePath); err != nil {
		return FreeCADRuntimeWorkingCopyLayout{}, err
	}
	if err := validateFreeCADRuntimeWorkingCopyID(workingCopyID); err != nil {
		return FreeCADRuntimeWorkingCopyLayout{}, err
	}
	if err := validateFreeCADRuntimeLogicalFilename("manifest filename", manifestFilename); err != nil {
		return FreeCADRuntimeWorkingCopyLayout{}, err
	}
	if err := validateFreeCADRuntimeLogicalFilename("result filename", resultFilename); err != nil {
		return FreeCADRuntimeWorkingCopyLayout{}, err
	}

	sourceBasename := filepath.Base(sourcePath)
	workingRoot := filepath.Join(productDir, freeCADRuntimeWorkingRootName)
	workingCopyDir := filepath.Join(workingRoot, workingCopyID)
	if workingCopyDir != filepath.Join(workingRoot, filepath.Base(workingCopyID)) {
		return FreeCADRuntimeWorkingCopyLayout{}, fmt.Errorf(
			"working-copy ID %q is not a single path segment under working root",
			workingCopyID,
		)
	}
	if err := requireFreeCADRuntimePathContained(workingRoot, workingCopyDir); err != nil {
		return FreeCADRuntimeWorkingCopyLayout{}, fmt.Errorf("working-copy directory: %w", err)
	}

	sourceDir := filepath.Join(workingCopyDir, freeCADRuntimeSourceDirName)
	sourceDocumentPath := filepath.Join(sourceDir, sourceBasename)
	sourceDocument := filepath.ToSlash(filepath.Join(freeCADRuntimeSourceDirName, sourceBasename))
	manifestPath := filepath.Join(workingCopyDir, manifestFilename)
	resultPath := filepath.Join(workingCopyDir, resultFilename)
	outputDir := filepath.Join(workingCopyDir, freeCADRuntimeOutputPathRoot)

	normalizedSourceDocument, err := normalizeFreeCADRuntimeSourceDocumentPath(sourceDocument)
	if err != nil {
		return FreeCADRuntimeWorkingCopyLayout{}, fmt.Errorf("derive manifest-facing source document: %w", err)
	}
	if normalizedSourceDocument != sourceDocument {
		return FreeCADRuntimeWorkingCopyLayout{}, fmt.Errorf("manifest-facing source document is not canonical: %q", sourceDocument)
	}

	containedPaths := []struct {
		label string
		path  string
	}{
		{label: "source directory", path: sourceDir},
		{label: "source document path", path: sourceDocumentPath},
		{label: "manifest path", path: manifestPath},
		{label: "result path", path: resultPath},
		{label: "output directory", path: outputDir},
	}
	for _, candidate := range containedPaths {
		if err := requireFreeCADRuntimePathContained(workingCopyDir, candidate.path); err != nil {
			return FreeCADRuntimeWorkingCopyLayout{}, fmt.Errorf("%s: %w", candidate.label, err)
		}
	}

	return FreeCADRuntimeWorkingCopyLayout{
		ProductDir:            productDir,
		WorkingRoot:           workingRoot,
		WorkingCopyDir:        workingCopyDir,
		WorkingCopyID:         workingCopyID,
		SourceDir:             sourceDir,
		SourceDocument:        sourceDocument,
		SourceDocumentPath:    sourceDocumentPath,
		ManifestPath:          manifestPath,
		ResultPath:            resultPath,
		OutputDir:             outputDir,
		OutputDirRelativePath: freeCADRuntimeOutputPathRoot,
	}, nil
}

func requireCanonicalAbsoluteFreeCADRuntimeHostPath(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.Contains(value, "\x00") {
		return fmt.Errorf("%s contains null byte", field)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s has surrounding whitespace", field)
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("%s must be absolute", field)
	}
	if filepath.Clean(value) != value {
		return fmt.Errorf("%s must be clean", field)
	}
	return nil
}

func validateFreeCADRuntimeWorkingCopyID(workingCopyID string) error {
	if workingCopyID == "" {
		return fmt.Errorf("working-copy ID is required")
	}
	if strings.TrimSpace(workingCopyID) != workingCopyID {
		return fmt.Errorf("working-copy ID has surrounding whitespace")
	}
	if strings.Contains(workingCopyID, "\x00") {
		return fmt.Errorf("working-copy ID contains null byte")
	}
	if workingCopyID == "." || workingCopyID == ".." {
		return fmt.Errorf("working-copy ID %q is not a single path segment", workingCopyID)
	}
	if strings.Contains(workingCopyID, "/") || strings.Contains(workingCopyID, `\`) {
		return fmt.Errorf("working-copy ID %q must not contain path separators", workingCopyID)
	}
	if filepath.IsAbs(workingCopyID) {
		return fmt.Errorf("working-copy ID %q must not be absolute", workingCopyID)
	}
	if filepath.Base(workingCopyID) != workingCopyID {
		return fmt.Errorf("working-copy ID %q is not a single path segment", workingCopyID)
	}
	return nil
}

func validateFreeCADRuntimeLogicalFilename(field, name string) error {
	if name == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("%s has surrounding whitespace", field)
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("%s contains whitespace", field)
	}
	if strings.Contains(name, "\x00") {
		return fmt.Errorf("%s contains null byte", field)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%s %q is not a single logical filename", field, name)
	}
	if strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return fmt.Errorf("%s %q must not contain path separators", field, name)
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("%s %q must not be absolute", field, name)
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("%s %q is not a single logical filename", field, name)
	}
	return nil
}

// PrepareFreeCADRuntimeWorkingCopy materializes the source document into an
// already-computed aligned working-copy layout.
func PrepareFreeCADRuntimeWorkingCopy(req FreeCADRuntimeWorkingCopyPreparationRequest) (FreeCADRuntimeWorkingCopyPreparationResult, error) {
	if err := validateFreeCADRuntimeWorkingCopyPreparationLayout(req.Layout); err != nil {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf("prepare FreeCAD runtime working copy: %w", err)
	}
	if err := validateExistingFreeCADRuntimeOutputDirectory(req.Layout); err != nil {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf("prepare FreeCAD runtime working copy: %w", err)
	}

	sourcePath, err := normalizeFreeCADRuntimeHostPath("source path", req.SourcePath)
	if err != nil {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf("prepare FreeCAD runtime working copy: %w", err)
	}
	if sourcePath == req.Layout.SourceDocumentPath {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf(
			"prepare FreeCAD runtime working copy: source path %q must differ from destination path",
			req.SourcePath,
		)
	}

	sourcePathInfo, err := os.Stat(sourcePath)
	if err != nil {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf(
			"prepare FreeCAD runtime working copy: inspect source path %q: %w",
			req.SourcePath,
			err,
		)
	}
	if !sourcePathInfo.Mode().IsRegular() {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf(
			"prepare FreeCAD runtime working copy: source path %q is not a regular file",
			req.SourcePath,
		)
	}

	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf(
			"prepare FreeCAD runtime working copy: open source path %q: %w",
			req.SourcePath,
			err,
		)
	}
	defer sourceFile.Close()

	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf(
			"prepare FreeCAD runtime working copy: inspect source path %q: %w",
			req.SourcePath,
			err,
		)
	}
	if !sourceInfo.Mode().IsRegular() {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf(
			"prepare FreeCAD runtime working copy: opened source path %q is not a regular file",
			req.SourcePath,
		)
	}
	destinationInfo, err := os.Stat(req.Layout.SourceDocumentPath)
	switch {
	case err == nil && os.SameFile(sourceInfo, destinationInfo):
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf(
			"prepare FreeCAD runtime working copy: source path %q must differ from destination path",
			req.SourcePath,
		)
	case err != nil && !os.IsNotExist(err):
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf(
			"prepare FreeCAD runtime working copy: inspect destination path %q: %w",
			req.Layout.SourceDocumentPath,
			err,
		)
	}

	if err := os.MkdirAll(req.Layout.WorkingCopyDir, 0o755); err != nil {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf(
			"prepare FreeCAD runtime working copy: create working-copy directory %q: %w",
			req.Layout.WorkingCopyDir,
			err,
		)
	}
	if err := os.MkdirAll(req.Layout.SourceDir, 0o755); err != nil {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf(
			"prepare FreeCAD runtime working copy: create source directory %q: %w",
			req.Layout.SourceDir,
			err,
		)
	}
	if err := copyFreeCADRuntimeSourceDocument(sourceFile, req.Layout.SourceDir, req.Layout.SourceDocumentPath); err != nil {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf("prepare FreeCAD runtime working copy: %w", err)
	}
	if err := ValidatePreparedFreeCADRuntimeSourceDocument(req.Layout); err != nil {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf("prepare FreeCAD runtime working copy: %w", err)
	}
	if err := PrepareFreeCADRuntimeOutputDirectories(req.Layout); err != nil {
		return FreeCADRuntimeWorkingCopyPreparationResult{}, fmt.Errorf("prepare FreeCAD runtime working copy: %w", err)
	}

	return FreeCADRuntimeWorkingCopyPreparationResult{Layout: req.Layout}, nil
}

// PrepareFreeCADRuntimeOutputDirectories creates the deterministic aligned
// runtime output directory without removing or rewriting existing contents.
func PrepareFreeCADRuntimeOutputDirectories(layout FreeCADRuntimeWorkingCopyLayout) error {
	if err := validateFreeCADRuntimeOutputDirectoryLayout(layout); err != nil {
		return fmt.Errorf("prepare FreeCAD runtime output directories: %w", err)
	}
	if err := validateExistingFreeCADRuntimeOutputDirectory(layout); err != nil {
		return fmt.Errorf("prepare FreeCAD runtime output directories: %w", err)
	}

	if err := os.MkdirAll(layout.WorkingCopyDir, 0o755); err != nil {
		return fmt.Errorf(
			"prepare FreeCAD runtime output directories: create working-copy directory %q: %w",
			layout.WorkingCopyDir,
			err,
		)
	}

	outputInfo, err := os.Lstat(layout.OutputDir)
	switch {
	case err == nil && outputInfo.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf(
			"prepare FreeCAD runtime output directories: output directory %q must not be a symbolic link",
			layout.OutputDir,
		)
	case err == nil && !outputInfo.IsDir():
		return fmt.Errorf(
			"prepare FreeCAD runtime output directories: output directory %q is not a directory",
			layout.OutputDir,
		)
	case err != nil && !os.IsNotExist(err):
		return fmt.Errorf(
			"prepare FreeCAD runtime output directories: inspect output directory %q: %w",
			layout.OutputDir,
			err,
		)
	case os.IsNotExist(err):
		if err := os.Mkdir(layout.OutputDir, 0o755); err != nil {
			return fmt.Errorf(
				"prepare FreeCAD runtime output directories: create output directory %q: %w",
				layout.OutputDir,
				err,
			)
		}
	}

	if err := validateExistingFreeCADRuntimeOutputDirectory(layout); err != nil {
		return fmt.Errorf("prepare FreeCAD runtime output directories: %w", err)
	}
	return nil
}

// ValidatePreparedFreeCADRuntimeSourceDocument proves that the manifest-facing
// sourceDocument resolves to the materialized regular file inside the prepared
// working copy. It has no filesystem side effects.
func ValidatePreparedFreeCADRuntimeSourceDocument(layout FreeCADRuntimeWorkingCopyLayout) error {
	if err := validateFreeCADRuntimeWorkingCopyPreparationLayout(layout); err != nil {
		return fmt.Errorf("validate prepared FreeCAD runtime source document: %w", err)
	}

	resolvedPath, err := normalizeFreeCADRuntimeHostPath(
		"resolved layout source document path",
		filepath.Join(layout.WorkingCopyDir, filepath.FromSlash(layout.SourceDocument)),
	)
	if err != nil {
		return fmt.Errorf("validate prepared FreeCAD runtime source document: %w", err)
	}
	if err := requireFreeCADRuntimePathContained(layout.WorkingCopyDir, resolvedPath); err != nil {
		return fmt.Errorf("validate prepared FreeCAD runtime source document: resolved source document: %w", err)
	}
	if resolvedPath != layout.SourceDocumentPath {
		return fmt.Errorf(
			"validate prepared FreeCAD runtime source document: resolved source document path %q does not match layout source document path %q",
			resolvedPath,
			layout.SourceDocumentPath,
		)
	}

	sourceInfo, err := os.Stat(resolvedPath)
	if err != nil {
		return fmt.Errorf(
			"validate prepared FreeCAD runtime source document: inspect resolved source document path %q: %w",
			resolvedPath,
			err,
		)
	}
	if !sourceInfo.Mode().IsRegular() {
		return fmt.Errorf(
			"validate prepared FreeCAD runtime source document: resolved source document path %q is not a regular file",
			resolvedPath,
		)
	}

	resolvedWorkingCopyDir, err := filepath.EvalSymlinks(layout.WorkingCopyDir)
	if err != nil {
		return fmt.Errorf(
			"validate prepared FreeCAD runtime source document: resolve working-copy directory %q: %w",
			layout.WorkingCopyDir,
			err,
		)
	}
	resolvedSourceDocumentPath, err := filepath.EvalSymlinks(resolvedPath)
	if err != nil {
		return fmt.Errorf(
			"validate prepared FreeCAD runtime source document: resolve source document path %q: %w",
			resolvedPath,
			err,
		)
	}
	if err := requireFreeCADRuntimePathContained(resolvedWorkingCopyDir, resolvedSourceDocumentPath); err != nil {
		return fmt.Errorf("validate prepared FreeCAD runtime source document: resolved filesystem source document: %w", err)
	}

	return nil
}

// ExecutionRequest builds the existing aligned runtime invocation request
// without invoking the runtime.
func (layout FreeCADRuntimeWorkingCopyLayout) ExecutionRequest(runtimeCommand string) FreeCADRuntimeExecutionRequest {
	return FreeCADRuntimeExecutionRequest{
		RuntimeCommand: runtimeCommand,
		WorkingCopyDir: layout.WorkingCopyDir,
		ManifestPath:   layout.ManifestPath,
		ResultPath:     layout.ResultPath,
		OutputDir:      layout.OutputDir,
	}
}

func validateFreeCADRuntimeWorkingCopyPreparationLayout(layout FreeCADRuntimeWorkingCopyLayout) error {
	workingCopyDir, err := normalizeFreeCADRuntimeHostPath("layout working-copy directory", layout.WorkingCopyDir)
	if err != nil {
		return err
	}
	if workingCopyDir != layout.WorkingCopyDir {
		return fmt.Errorf("layout working-copy directory is not canonical: %q", layout.WorkingCopyDir)
	}
	sourceDir, err := normalizeFreeCADRuntimeHostPath("layout source directory", layout.SourceDir)
	if err != nil {
		return err
	}
	if sourceDir != layout.SourceDir {
		return fmt.Errorf("layout source directory is not canonical: %q", layout.SourceDir)
	}
	sourceDocumentPath, err := normalizeFreeCADRuntimeHostPath("layout source document path", layout.SourceDocumentPath)
	if err != nil {
		return err
	}
	if sourceDocumentPath != layout.SourceDocumentPath {
		return fmt.Errorf("layout source document path is not canonical: %q", layout.SourceDocumentPath)
	}

	if err := requireFreeCADRuntimePathContained(workingCopyDir, sourceDir); err != nil {
		return fmt.Errorf("layout source directory: %w", err)
	}
	if err := requireFreeCADRuntimePathContained(workingCopyDir, sourceDocumentPath); err != nil {
		return fmt.Errorf("layout source document path: %w", err)
	}

	sourceDocument, err := normalizeFreeCADRuntimeSourceDocumentPath(layout.SourceDocument)
	if err != nil {
		return fmt.Errorf("layout source document: %w", err)
	}
	if sourceDocument != layout.SourceDocument {
		return fmt.Errorf("layout source document is not canonical: %q", layout.SourceDocument)
	}

	expectedSourceDocumentPath := filepath.Join(workingCopyDir, filepath.FromSlash(sourceDocument))
	if sourceDocumentPath != expectedSourceDocumentPath {
		return fmt.Errorf(
			"layout source document path %q does not correspond to source document %q under working-copy directory",
			layout.SourceDocumentPath,
			layout.SourceDocument,
		)
	}
	if sourceDir != filepath.Dir(sourceDocumentPath) {
		return fmt.Errorf(
			"layout source directory %q does not contain source document path %q",
			layout.SourceDir,
			layout.SourceDocumentPath,
		)
	}
	if err := validateFreeCADRuntimeOutputDirectoryLayout(layout); err != nil {
		return err
	}
	return nil
}

func validateFreeCADRuntimeOutputDirectoryLayout(layout FreeCADRuntimeWorkingCopyLayout) error {
	workingCopyDir, err := normalizeFreeCADRuntimeHostPath("layout working-copy directory", layout.WorkingCopyDir)
	if err != nil {
		return err
	}
	if workingCopyDir != layout.WorkingCopyDir {
		return fmt.Errorf("layout working-copy directory is not canonical: %q", layout.WorkingCopyDir)
	}

	outputDir, err := normalizeFreeCADRuntimeHostPath("layout output directory", layout.OutputDir)
	if err != nil {
		return err
	}
	if outputDir != layout.OutputDir {
		return fmt.Errorf("layout output directory is not canonical: %q", layout.OutputDir)
	}
	if err := requireFreeCADRuntimePathContained(workingCopyDir, outputDir); err != nil {
		return fmt.Errorf("layout output directory: %w", err)
	}

	outputDirRelativePath := layout.OutputDirRelativePath
	if strings.TrimSpace(outputDirRelativePath) == "" {
		return fmt.Errorf("layout output directory relative path is required")
	}
	if strings.TrimSpace(outputDirRelativePath) != outputDirRelativePath ||
		filepath.IsAbs(filepath.FromSlash(outputDirRelativePath)) ||
		filepath.ToSlash(filepath.Clean(filepath.FromSlash(outputDirRelativePath))) != outputDirRelativePath {
		return fmt.Errorf("layout output directory relative path is not canonical: %q", outputDirRelativePath)
	}
	if outputDirRelativePath != freeCADRuntimeOutputPathRoot {
		return fmt.Errorf(
			"layout output directory relative path %q must equal aligned runtime output root %q",
			outputDirRelativePath,
			freeCADRuntimeOutputPathRoot,
		)
	}

	expectedOutputDir := filepath.Join(workingCopyDir, filepath.FromSlash(outputDirRelativePath))
	if outputDir != expectedOutputDir {
		return fmt.Errorf(
			"layout output directory %q does not correspond to output directory relative path %q under working-copy directory",
			layout.OutputDir,
			layout.OutputDirRelativePath,
		)
	}
	return nil
}

func validateExistingFreeCADRuntimeOutputDirectory(layout FreeCADRuntimeWorkingCopyLayout) error {
	workingCopyInfo, err := os.Stat(layout.WorkingCopyDir)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("inspect working-copy directory %q: %w", layout.WorkingCopyDir, err)
	case !workingCopyInfo.IsDir():
		return fmt.Errorf("working-copy directory %q is not a directory", layout.WorkingCopyDir)
	}

	resolvedWorkingCopyDir, err := filepath.EvalSymlinks(layout.WorkingCopyDir)
	if err != nil {
		return fmt.Errorf("resolve working-copy directory %q: %w", layout.WorkingCopyDir, err)
	}

	outputInfo, err := os.Lstat(layout.OutputDir)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("inspect output directory %q: %w", layout.OutputDir, err)
	case outputInfo.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("output directory %q must not be a symbolic link", layout.OutputDir)
	case !outputInfo.IsDir():
		return fmt.Errorf("output directory %q is not a directory", layout.OutputDir)
	}

	resolvedOutputDir, err := filepath.EvalSymlinks(layout.OutputDir)
	if err != nil {
		return fmt.Errorf("resolve output directory %q: %w", layout.OutputDir, err)
	}
	if err := requireFreeCADRuntimePathContained(resolvedWorkingCopyDir, resolvedOutputDir); err != nil {
		return fmt.Errorf("resolved filesystem output directory: %w", err)
	}
	return nil
}

func copyFreeCADRuntimeSourceDocument(sourceFile *os.File, sourceDir, destinationPath string) (err error) {
	tempFile, err := os.CreateTemp(sourceDir, ".source-document-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary source document in %q: %w", sourceDir, err)
	}
	tempPath := tempFile.Name()
	defer func() {
		tempFile.Close()
		if err != nil {
			os.Remove(tempPath)
		}
	}()

	if err = tempFile.Chmod(0o644); err != nil {
		return fmt.Errorf("set temporary source document permissions: %w", err)
	}
	if _, err = io.Copy(tempFile, sourceFile); err != nil {
		return fmt.Errorf("copy source document to temporary path: %w", err)
	}
	if err = tempFile.Close(); err != nil {
		return fmt.Errorf("close temporary source document: %w", err)
	}
	if err = os.Rename(tempPath, destinationPath); err != nil {
		return fmt.Errorf("replace source document destination %q: %w", destinationPath, err)
	}
	return nil
}

func normalizeFreeCADRuntimeHostPath(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if strings.Contains(value, "\x00") {
		return "", fmt.Errorf("%s contains null byte", field)
	}

	absolutePath, err := filepath.Abs(filepath.Clean(trimmed))
	if err != nil {
		return "", fmt.Errorf("resolve %s %q: %w", field, value, err)
	}
	return filepath.Clean(absolutePath), nil
}

func sanitizeFreeCADRuntimeWorkingCopyPrefix(value string) string {
	var builder strings.Builder
	separatorPending := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			required := 1
			if separatorPending && builder.Len() > 0 {
				required++
			}
			if builder.Len()+required > freeCADRuntimeWorkingCopyPrefixMax {
				return strings.Trim(builder.String(), "-")
			}
			if required == 2 {
				builder.WriteByte('-')
			}
			builder.WriteRune(r)
			separatorPending = false
		default:
			separatorPending = builder.Len() > 0
		}
	}
	return strings.Trim(builder.String(), "-")
}

func freeCADRuntimeWorkingCopyHash(productKey, planHash, sourcePath string) string {
	hash := sha256.New()
	for _, value := range []string{
		strings.TrimSpace(productKey),
		strings.TrimSpace(planHash),
		sourcePath,
	} {
		fmt.Fprintf(hash, "%d:", len(value))
		hash.Write([]byte(value))
	}
	return hex.EncodeToString(hash.Sum(nil))[:freeCADRuntimeWorkingCopyHashLength]
}

func requireFreeCADRuntimePathContained(parent, candidate string) error {
	relativePath, err := filepath.Rel(parent, candidate)
	if err != nil {
		return fmt.Errorf("resolve containment: %w", err)
	}
	if relativePath == "." || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) || filepath.IsAbs(relativePath) {
		return fmt.Errorf("%q is not contained under %q", candidate, parent)
	}
	return nil
}
