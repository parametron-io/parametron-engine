package freecad

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
)

// isPathUnder returns true if candidate is strictly contained under parent,
// determined via filepath.Rel to avoid string-prefix ambiguity.
func isPathUnder(parent, candidate string) bool {
	rel, err := filepath.Rel(parent, candidate)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." {
		return false
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if filepath.IsAbs(rel) {
		return false
	}
	return true
}

// --- Valid layout shape ---

func TestComputeFreeCADRuntimeWorkingCopyLayout_ValidShape(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")

	req := FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "myproduct",
		PlanHash:   "plan-hash-1",
		SourcePath: sourcePath,
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	absProductDir := filepath.Clean(productDir)

	if layout.ProductDir != absProductDir {
		t.Errorf("ProductDir = %q, want %q", layout.ProductDir, absProductDir)
	}

	expectedWorkingRoot := filepath.Join(absProductDir, "_working")
	if layout.WorkingRoot != expectedWorkingRoot {
		t.Errorf("WorkingRoot = %q, want %q", layout.WorkingRoot, expectedWorkingRoot)
	}

	if !isPathUnder(layout.WorkingRoot, layout.WorkingCopyDir) {
		t.Errorf("WorkingCopyDir %q is not under WorkingRoot %q", layout.WorkingCopyDir, layout.WorkingRoot)
	}

	expectedSourceDir := filepath.Join(layout.WorkingCopyDir, "source")
	if layout.SourceDir != expectedSourceDir {
		t.Errorf("SourceDir = %q, want %q", layout.SourceDir, expectedSourceDir)
	}

	expectedSourceDocumentPath := filepath.Join(layout.SourceDir, "model.FCStd")
	if layout.SourceDocumentPath != expectedSourceDocumentPath {
		t.Errorf("SourceDocumentPath = %q, want %q", layout.SourceDocumentPath, expectedSourceDocumentPath)
	}

	if layout.SourceDocument != "source/model.FCStd" {
		t.Errorf("SourceDocument = %q, want %q", layout.SourceDocument, "source/model.FCStd")
	}

	expectedManifestPath := filepath.Join(layout.WorkingCopyDir, planner.FreeCADRuntimeExportManifestFilename)
	if layout.ManifestPath != expectedManifestPath {
		t.Errorf("ManifestPath = %q, want %q", layout.ManifestPath, expectedManifestPath)
	}

	expectedResultPath := filepath.Join(layout.WorkingCopyDir, "result.json")
	if layout.ResultPath != expectedResultPath {
		t.Errorf("ResultPath = %q, want %q", layout.ResultPath, expectedResultPath)
	}

	expectedOutputDir := filepath.Join(layout.WorkingCopyDir, "outputs")
	if layout.OutputDir != expectedOutputDir {
		t.Errorf("OutputDir = %q, want %q", layout.OutputDir, expectedOutputDir)
	}

	if layout.OutputDirRelativePath != "outputs" {
		t.Errorf("OutputDirRelativePath = %q, want %q", layout.OutputDirRelativePath, "outputs")
	}
}

// --- Deterministic identity ---

func TestComputeFreeCADRuntimeWorkingCopyLayout_SameRequestSameID(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")
	req := FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "product-a",
		PlanHash:   "hash-001",
		SourcePath: sourcePath,
	}

	l1, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	l2, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if l1.WorkingCopyID != l2.WorkingCopyID {
		t.Errorf("same request yielded different WorkingCopyID: %q vs %q", l1.WorkingCopyID, l2.WorkingCopyID)
	}
	if l1.WorkingCopyDir != l2.WorkingCopyDir {
		t.Errorf("same request yielded different WorkingCopyDir: %q vs %q", l1.WorkingCopyDir, l2.WorkingCopyDir)
	}
}

func TestComputeFreeCADRuntimeWorkingCopyLayout_DifferentPlanHashDifferentID(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")
	base := FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "product-a",
		PlanHash:   "hash-001",
		SourcePath: sourcePath,
	}
	alt := base
	alt.PlanHash = "hash-999"

	l1, err := ComputeFreeCADRuntimeWorkingCopyLayout(base)
	if err != nil {
		t.Fatalf("base: %v", err)
	}
	l2, err := ComputeFreeCADRuntimeWorkingCopyLayout(alt)
	if err != nil {
		t.Fatalf("alt: %v", err)
	}

	if l1.WorkingCopyID == l2.WorkingCopyID {
		t.Errorf("different PlanHash produced same WorkingCopyID: %q", l1.WorkingCopyID)
	}
}

func TestComputeFreeCADRuntimeWorkingCopyLayout_DifferentSourcePathDifferentID(t *testing.T) {
	productDir := t.TempDir()
	base := FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "product-a",
		PlanHash:   "hash-001",
		SourcePath: filepath.Join(productDir, "model-a.FCStd"),
	}
	alt := base
	alt.SourcePath = filepath.Join(productDir, "model-b.FCStd")

	l1, err := ComputeFreeCADRuntimeWorkingCopyLayout(base)
	if err != nil {
		t.Fatalf("base: %v", err)
	}
	l2, err := ComputeFreeCADRuntimeWorkingCopyLayout(alt)
	if err != nil {
		t.Fatalf("alt: %v", err)
	}

	if l1.WorkingCopyID == l2.WorkingCopyID {
		t.Errorf("different SourcePath produced same WorkingCopyID: %q", l1.WorkingCopyID)
	}
}

func TestComputeFreeCADRuntimeWorkingCopyLayout_RelativeEquivalentToAbsolute(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "design.FCStd")

	cwd, err := os.Getwd()
	if err != nil {
		t.Skipf("cannot determine working directory: %v", err)
	}

	relProductDir, err := filepath.Rel(cwd, productDir)
	if err != nil {
		t.Skipf("cannot compute relative product dir: %v", err)
	}
	relSourcePath, err := filepath.Rel(cwd, sourcePath)
	if err != nil {
		t.Skipf("cannot compute relative source path: %v", err)
	}

	absReq := FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "rel-test",
		PlanHash:   "plan-rel",
		SourcePath: sourcePath,
	}
	relReq := FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: relProductDir,
		ProductKey: "rel-test",
		PlanHash:   "plan-rel",
		SourcePath: relSourcePath,
	}

	absLayout, err := ComputeFreeCADRuntimeWorkingCopyLayout(absReq)
	if err != nil {
		t.Fatalf("absolute request: %v", err)
	}
	relLayout, err := ComputeFreeCADRuntimeWorkingCopyLayout(relReq)
	if err != nil {
		t.Fatalf("relative request: %v", err)
	}

	if absLayout.WorkingCopyID != relLayout.WorkingCopyID {
		t.Errorf("relative vs absolute: WorkingCopyID mismatch: %q vs %q", absLayout.WorkingCopyID, relLayout.WorkingCopyID)
	}
	if absLayout.WorkingCopyDir != relLayout.WorkingCopyDir {
		t.Errorf("relative vs absolute: WorkingCopyDir mismatch: %q vs %q", absLayout.WorkingCopyDir, relLayout.WorkingCopyDir)
	}
}

// --- Path containment ---

func TestComputeFreeCADRuntimeWorkingCopyLayout_PathContainment(t *testing.T) {
	productDir := t.TempDir()
	req := FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "containment-test",
		PlanHash:   "hash-containment",
		SourcePath: filepath.Join(productDir, "model.FCStd"),
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !isPathUnder(layout.ProductDir, layout.WorkingRoot) {
		t.Errorf("WorkingRoot %q not under ProductDir %q", layout.WorkingRoot, layout.ProductDir)
	}

	if !isPathUnder(layout.WorkingRoot, layout.WorkingCopyDir) {
		t.Errorf("WorkingCopyDir %q not under WorkingRoot %q", layout.WorkingCopyDir, layout.WorkingRoot)
	}

	containedUnderWorkingCopy := []struct {
		name string
		path string
	}{
		{"SourceDir", layout.SourceDir},
		{"SourceDocumentPath", layout.SourceDocumentPath},
		{"ManifestPath", layout.ManifestPath},
		{"ResultPath", layout.ResultPath},
		{"OutputDir", layout.OutputDir},
	}
	for _, cp := range containedUnderWorkingCopy {
		if !isPathUnder(layout.WorkingCopyDir, cp.path) {
			t.Errorf("%s %q not contained under WorkingCopyDir %q", cp.name, cp.path, layout.WorkingCopyDir)
		}
	}
}

// --- Manifest-facing SourceDocument ---

func TestComputeFreeCADRuntimeWorkingCopyLayout_SourceDocumentShape(t *testing.T) {
	tests := []struct {
		name           string
		sourceBasename string
		wantDocument   string
	}{
		{
			name:           "standard FCStd extension",
			sourceBasename: "widget.FCStd",
			wantDocument:   "source/widget.FCStd",
		},
		{
			name:           "lowercase extension",
			sourceBasename: "part.fcstd",
			wantDocument:   "source/part.fcstd",
		},
		{
			name:           "no extension",
			sourceBasename: "design",
			wantDocument:   "source/design",
		},
		{
			name:           "multiple dots in name",
			sourceBasename: "my.file.FCStd",
			wantDocument:   "source/my.file.FCStd",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			productDir := t.TempDir()
			req := FreeCADRuntimeWorkingCopyLayoutRequest{
				ProductDir: productDir,
				ProductKey: "src-doc-test",
				PlanHash:   "plan-src",
				SourcePath: filepath.Join(productDir, tc.sourceBasename),
			}

			layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			doc := layout.SourceDocument

			if doc != tc.wantDocument {
				t.Errorf("SourceDocument = %q, want %q", doc, tc.wantDocument)
			}

			if filepath.IsAbs(doc) {
				t.Errorf("SourceDocument %q must be relative, but IsAbs returned true", doc)
			}
			if strings.HasPrefix(doc, "/") {
				t.Errorf("SourceDocument %q must not begin with /", doc)
			}
			if strings.Contains(doc, "\\") {
				t.Errorf("SourceDocument %q must not contain backslashes", doc)
			}
			for _, seg := range strings.Split(doc, "/") {
				if seg == ".." {
					t.Errorf("SourceDocument %q must not contain parent traversal", doc)
				}
			}

			// Verify source/<basename> shape
			parts := strings.SplitN(doc, "/", 2)
			if len(parts) != 2 || parts[0] != "source" || parts[1] != tc.sourceBasename {
				t.Errorf("SourceDocument %q does not match source/<basename> shape; want source/%s", doc, tc.sourceBasename)
			}
		})
	}
}

func TestComputeFreeCADRuntimeWorkingCopyLayout_SourceDocumentStableAcrossPlanHash(t *testing.T) {
	productDir := t.TempDir()
	basename := "assembly.FCStd"

	make := func(planHash string) FreeCADRuntimeWorkingCopyLayoutRequest {
		return FreeCADRuntimeWorkingCopyLayoutRequest{
			ProductDir: productDir,
			ProductKey: "stability-test",
			PlanHash:   planHash,
			SourcePath: filepath.Join(productDir, basename),
		}
	}

	l1, err := ComputeFreeCADRuntimeWorkingCopyLayout(make("hash-a"))
	if err != nil {
		t.Fatalf("call 1: %v", err)
	}
	l2, err := ComputeFreeCADRuntimeWorkingCopyLayout(make("hash-b"))
	if err != nil {
		t.Fatalf("call 2: %v", err)
	}

	if l1.SourceDocument != l2.SourceDocument {
		t.Errorf("SourceDocument changed with PlanHash: %q vs %q", l1.SourceDocument, l2.SourceDocument)
	}
}

// --- Input validation ---

func TestComputeFreeCADRuntimeWorkingCopyLayout_InvalidInputs(t *testing.T) {
	validProductDir := t.TempDir()
	validSourcePath := filepath.Join(validProductDir, "model.FCStd")

	tests := []struct {
		name            string
		productDir      string
		sourcePath      string
		wantErrContains string
	}{
		{
			name:            "empty ProductDir",
			productDir:      "",
			sourcePath:      validSourcePath,
			wantErrContains: "product directory",
		},
		{
			name:            "whitespace-only ProductDir",
			productDir:      "   ",
			sourcePath:      validSourcePath,
			wantErrContains: "product directory",
		},
		{
			name:            "empty SourcePath",
			productDir:      validProductDir,
			sourcePath:      "",
			wantErrContains: "source path",
		},
		{
			name:            "whitespace-only SourcePath",
			productDir:      validProductDir,
			sourcePath:      "   ",
			wantErrContains: "source path",
		},
		{
			name:            "null byte in ProductDir",
			productDir:      validProductDir + "\x00/extra",
			sourcePath:      validSourcePath,
			wantErrContains: "product directory",
		},
		{
			name:            "null byte in SourcePath",
			productDir:      validProductDir,
			sourcePath:      validSourcePath + "\x00extra",
			wantErrContains: "source path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := FreeCADRuntimeWorkingCopyLayoutRequest{
				ProductDir: tc.productDir,
				ProductKey: "validation-test",
				PlanHash:   "plan-hash",
				SourcePath: tc.sourcePath,
			}

			layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
			if err == nil {
				t.Fatalf("expected error, got nil (layout = %+v)", layout)
			}

			errMsg := err.Error()
			if errMsg == "" {
				t.Error("error message is empty")
			}
			if !strings.Contains(errMsg, tc.wantErrContains) {
				t.Errorf("error %q does not mention %q", errMsg, tc.wantErrContains)
			}

			var zero FreeCADRuntimeWorkingCopyLayout
			if layout != zero {
				t.Errorf("expected zero-value layout on error, got %+v", layout)
			}
		})
	}
}

// --- Filesystem side-effect-free behavior ---

func TestComputeFreeCADRuntimeWorkingCopyLayout_NoFilesystemSideEffects(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")

	req := FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "side-effect-test",
		PlanHash:   "hash-se",
		SourcePath: sourcePath,
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	noCreateDirs := []struct {
		name string
		path string
	}{
		{"WorkingRoot", layout.WorkingRoot},
		{"WorkingCopyDir", layout.WorkingCopyDir},
		{"SourceDir", layout.SourceDir},
		{"OutputDir", layout.OutputDir},
	}
	for _, d := range noCreateDirs {
		if _, statErr := os.Stat(d.path); statErr == nil {
			t.Errorf("layout computation unexpectedly created directory %s at %q", d.name, d.path)
		}
	}

	noCreateFiles := []struct {
		name string
		path string
	}{
		{"ManifestPath", layout.ManifestPath},
		{"ResultPath", layout.ResultPath},
		{"SourceDocumentPath", layout.SourceDocumentPath},
	}
	for _, f := range noCreateFiles {
		if _, statErr := os.Stat(f.path); statErr == nil {
			t.Errorf("layout computation unexpectedly created file %s at %q", f.name, f.path)
		}
	}
}

// --- Execution request construction ---

func TestFreeCADRuntimeWorkingCopyLayout_ExecutionRequest_FieldsFromLayout(t *testing.T) {
	productDir := t.TempDir()
	req := FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "exec-req-test",
		PlanHash:   "plan-exec",
		SourcePath: filepath.Join(productDir, "model.FCStd"),
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	runtimeCommand := "/usr/local/bin/parametron-freecad"
	execReq := layout.ExecutionRequest(runtimeCommand)

	if execReq.RuntimeCommand != runtimeCommand {
		t.Errorf("RuntimeCommand = %q, want %q", execReq.RuntimeCommand, runtimeCommand)
	}
	if execReq.WorkingCopyDir != layout.WorkingCopyDir {
		t.Errorf("WorkingCopyDir = %q, want %q", execReq.WorkingCopyDir, layout.WorkingCopyDir)
	}
	if execReq.ManifestPath != layout.ManifestPath {
		t.Errorf("ManifestPath = %q, want %q", execReq.ManifestPath, layout.ManifestPath)
	}
	if execReq.ResultPath != layout.ResultPath {
		t.Errorf("ResultPath = %q, want %q", execReq.ResultPath, layout.ResultPath)
	}
	if execReq.OutputDir != layout.OutputDir {
		t.Errorf("OutputDir = %q, want %q", execReq.OutputDir, layout.OutputDir)
	}
}

func TestFreeCADRuntimeWorkingCopyLayout_ExecutionRequest_RuntimeCommandPreserved(t *testing.T) {
	productDir := t.TempDir()
	req := FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "exec-multi-test",
		PlanHash:   "plan-exec-multi",
		SourcePath: filepath.Join(productDir, "model.FCStd"),
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd1 := layout.ExecutionRequest("/usr/bin/freecad-a")
	cmd2 := layout.ExecutionRequest("/usr/bin/freecad-b")

	if cmd1.RuntimeCommand != "/usr/bin/freecad-a" {
		t.Errorf("cmd1.RuntimeCommand = %q, want %q", cmd1.RuntimeCommand, "/usr/bin/freecad-a")
	}
	if cmd2.RuntimeCommand != "/usr/bin/freecad-b" {
		t.Errorf("cmd2.RuntimeCommand = %q, want %q", cmd2.RuntimeCommand, "/usr/bin/freecad-b")
	}

	// Layout paths must be identical across both calls.
	if cmd1.WorkingCopyDir != cmd2.WorkingCopyDir {
		t.Errorf("WorkingCopyDir differs: %q vs %q", cmd1.WorkingCopyDir, cmd2.WorkingCopyDir)
	}
	if cmd1.ManifestPath != cmd2.ManifestPath {
		t.Errorf("ManifestPath differs: %q vs %q", cmd1.ManifestPath, cmd2.ManifestPath)
	}
	if cmd1.ResultPath != cmd2.ResultPath {
		t.Errorf("ResultPath differs: %q vs %q", cmd1.ResultPath, cmd2.ResultPath)
	}
	if cmd1.OutputDir != cmd2.OutputDir {
		t.Errorf("OutputDir differs: %q vs %q", cmd1.OutputDir, cmd2.OutputDir)
	}
}

func TestFreeCADRuntimeWorkingCopyLayout_ExecutionRequest_DoesNotExecute(t *testing.T) {
	// ExecutionRequest must not invoke any subprocess. Verify by passing a
	// non-existent command and confirming the call returns without error and
	// without creating any runtime directories.
	productDir := t.TempDir()
	req := FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "no-exec-test",
		PlanHash:   "plan-no-exec",
		SourcePath: filepath.Join(productDir, "model.FCStd"),
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	execReq := layout.ExecutionRequest("/nonexistent-freecad-binary-should-never-run-xyz")
	if execReq.RuntimeCommand == "" {
		t.Error("ExecutionRequest returned empty RuntimeCommand")
	}

	// No runtime directories must have been created.
	if _, statErr := os.Stat(layout.WorkingRoot); statErr == nil {
		t.Errorf("ExecutionRequest created working root at %q", layout.WorkingRoot)
	}
	if _, statErr := os.Stat(layout.WorkingCopyDir); statErr == nil {
		t.Errorf("ExecutionRequest created working copy dir at %q", layout.WorkingCopyDir)
	}
}

// --- Legacy isolation ---

func TestComputeFreeCADRuntimeWorkingCopyLayout_WorkingCopyIDDistinctFromBridgeFormat(t *testing.T) {
	// bridge.ComputeWorkingCopyPath produces "<stem>.<hashprefix><ext>" filenames
	// (e.g., "model.abc123456def.FCStd"). The aligned layout must not use that format.
	productDir := t.TempDir()
	req := FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "legacy-isolation-test",
		PlanHash:   "plan-legacy",
		SourcePath: filepath.Join(productDir, "model.FCStd"),
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The bridge format appends the source extension to the working copy basename.
	if strings.HasSuffix(layout.WorkingCopyID, ".FCStd") {
		t.Errorf("WorkingCopyID %q looks like a bridge-format identifier (has source extension suffix)", layout.WorkingCopyID)
	}
}

func TestComputeFreeCADRuntimeWorkingCopyLayout_WorkingRootNameIsAligned(t *testing.T) {
	// Both bridge.ComputeWorkingCopyPath and the aligned layout helper use "_working"
	// as the working root directory name. Verify the constant is preserved by the layout.
	productDir := t.TempDir()
	req := FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "working-root-name-test",
		PlanHash:   "plan-wr",
		SourcePath: filepath.Join(productDir, "model.FCStd"),
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if filepath.Base(layout.WorkingRoot) != "_working" {
		t.Errorf("WorkingRoot base name = %q, want %q", filepath.Base(layout.WorkingRoot), "_working")
	}
}

// ============================================================
// Preparation: successful materialization
// ============================================================

func TestPrepareFreeCADRuntimeWorkingCopy_SuccessfulMaterialization(t *testing.T) {
	productDir := t.TempDir()
	sourceContent := []byte("FreeCAD source document content for test")
	sourcePath := filepath.Join(productDir, "box.FCStd")
	if err := os.WriteFile(sourcePath, sourceContent, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "prep-test",
		PlanHash:   "plan-prep",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	result, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
		Layout:     layout,
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Working-copy directory must exist.
	if _, statErr := os.Stat(layout.WorkingCopyDir); statErr != nil {
		t.Errorf("WorkingCopyDir not created: %v", statErr)
	}

	// Source directory must exist inside the working-copy directory.
	if _, statErr := os.Stat(layout.SourceDir); statErr != nil {
		t.Errorf("SourceDir not created: %v", statErr)
	}

	// Destination source document must contain the original bytes.
	destBytes, readErr := os.ReadFile(layout.SourceDocumentPath)
	if readErr != nil {
		t.Fatalf("read destination source document: %v", readErr)
	}
	if !bytes.Equal(destBytes, sourceContent) {
		t.Errorf("destination content = %q, want %q", destBytes, sourceContent)
	}

	// Original source file must be unmodified.
	origBytes, readErr := os.ReadFile(sourcePath)
	if readErr != nil {
		t.Fatalf("re-read original source: %v", readErr)
	}
	if !bytes.Equal(origBytes, sourceContent) {
		t.Errorf("original source file was modified")
	}

	// Manifest-facing SourceDocument must match the layout expectation.
	if result.Layout.SourceDocument != "source/box.FCStd" {
		t.Errorf("SourceDocument = %q, want %q", result.Layout.SourceDocument, "source/box.FCStd")
	}

	// No manifest file.
	if _, statErr := os.Stat(layout.ManifestPath); statErr == nil {
		t.Errorf("manifest file unexpectedly created at %q", layout.ManifestPath)
	}

	// No result file.
	if _, statErr := os.Stat(layout.ResultPath); statErr == nil {
		t.Errorf("result file unexpectedly created at %q", layout.ResultPath)
	}

	// Output directory must be created by preparation.
	outputDirInfo, statErr := os.Stat(layout.OutputDir)
	if statErr != nil {
		t.Errorf("OutputDir not created by preparation: %v", statErr)
	} else if !outputDirInfo.IsDir() {
		t.Errorf("OutputDir %q is not a directory", layout.OutputDir)
	}

	// Result layout must equal the request layout exactly.
	if result.Layout != layout {
		t.Errorf("result.Layout differs from request layout")
	}
}

func TestPrepareFreeCADRuntimeWorkingCopy_WorkingCopySourceAndOutputDirsCreated(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "part.FCStd")
	if err := os.WriteFile(sourcePath, []byte("part data"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "dirs-test",
		PlanHash:   "plan-dirs",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	if _, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
		Layout:     layout,
		SourcePath: sourcePath,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// WorkingRoot (parent of WorkingCopyDir) is created by MkdirAll as a side effect.
	if _, statErr := os.Stat(layout.WorkingRoot); statErr != nil {
		t.Errorf("WorkingRoot not created: %v", statErr)
	}
	if _, statErr := os.Stat(layout.WorkingCopyDir); statErr != nil {
		t.Errorf("WorkingCopyDir not created: %v", statErr)
	}
	if _, statErr := os.Stat(layout.SourceDir); statErr != nil {
		t.Errorf("SourceDir not created: %v", statErr)
	}

	// Output directory must be created by preparation.
	outputDirInfo, statErr := os.Stat(layout.OutputDir)
	if statErr != nil {
		t.Errorf("OutputDir not created by preparation: %v", statErr)
	} else if !outputDirInfo.IsDir() {
		t.Errorf("OutputDir %q is not a directory", layout.OutputDir)
	}
}

// ============================================================
// Preparation: repeated preparation / deterministic overwrite
// ============================================================

func TestPrepareFreeCADRuntimeWorkingCopy_RepeatedPreparation(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "design.FCStd")

	contentV1 := []byte("version 1 content")
	if err := os.WriteFile(sourcePath, contentV1, 0o644); err != nil {
		t.Fatalf("write v1: %v", err)
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "repeat-test",
		PlanHash:   "plan-repeat",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	prepReq := FreeCADRuntimeWorkingCopyPreparationRequest{
		Layout:     layout,
		SourcePath: sourcePath,
	}

	// First preparation.
	if _, err := PrepareFreeCADRuntimeWorkingCopy(prepReq); err != nil {
		t.Fatalf("first preparation: %v", err)
	}
	firstBytes, err := os.ReadFile(layout.SourceDocumentPath)
	if err != nil {
		t.Fatalf("read after first preparation: %v", err)
	}
	if !bytes.Equal(firstBytes, contentV1) {
		t.Errorf("after first preparation: destination = %q, want %q", firstBytes, contentV1)
	}

	// Overwrite source with new content.
	contentV2 := []byte("version 2 - updated content is longer")
	if err := os.WriteFile(sourcePath, contentV2, 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}

	// Second preparation.
	if _, err := PrepareFreeCADRuntimeWorkingCopy(prepReq); err != nil {
		t.Fatalf("second preparation: %v", err)
	}
	secondBytes, err := os.ReadFile(layout.SourceDocumentPath)
	if err != nil {
		t.Fatalf("read after second preparation: %v", err)
	}
	if !bytes.Equal(secondBytes, contentV2) {
		t.Errorf("after second preparation: destination = %q, want %q", secondBytes, contentV2)
	}

	// No temporary files must remain in the source directory.
	entries, err := os.ReadDir(layout.SourceDir)
	if err != nil {
		t.Fatalf("read source dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("temporary file %q remains in source directory after preparation", entry.Name())
		}
	}

	// Manifest-facing source path must remain stable across preparations.
	if layout.SourceDocument != "source/design.FCStd" {
		t.Errorf("SourceDocument = %q, want %q", layout.SourceDocument, "source/design.FCStd")
	}
}

func TestPrepareFreeCADRuntimeWorkingCopy_NoTemporaryFilesAfterSuccess(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "assembly.FCStd")
	if err := os.WriteFile(sourcePath, []byte("assembly data"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "tmp-test",
		PlanHash:   "plan-tmp",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	if _, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
		Layout:     layout,
		SourcePath: sourcePath,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, err := os.ReadDir(layout.SourceDir)
	if err != nil {
		t.Fatalf("read source dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("temporary file %q remains after successful preparation", entry.Name())
		}
	}
}

// ============================================================
// Preparation: source input validation
// ============================================================

func TestPrepareFreeCADRuntimeWorkingCopy_SourceInputValidation(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "src-val-test",
		PlanHash:   "plan-src-val",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	tests := []struct {
		name        string
		sourcePath  string
		wantErrFrag string
	}{
		{
			name:        "empty source path",
			sourcePath:  "",
			wantErrFrag: "source path",
		},
		{
			name:        "whitespace-only source path",
			sourcePath:  "   ",
			wantErrFrag: "source path",
		},
		{
			name:        "missing source file",
			sourcePath:  filepath.Join(productDir, "nonexistent.FCStd"),
			wantErrFrag: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
				Layout:     layout,
				SourcePath: tc.sourcePath,
			})
			if err == nil {
				t.Fatalf("expected error for source path %q, got nil", tc.sourcePath)
			}
			if tc.wantErrFrag != "" && !strings.Contains(err.Error(), tc.wantErrFrag) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErrFrag)
			}
		})
	}
}

func TestPrepareFreeCADRuntimeWorkingCopy_SourcePathIsDirectory(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "src-dir-test",
		PlanHash:   "plan-src-dir",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	// Use a real directory as the source path.
	dirPath := t.TempDir()
	_, err = PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
		Layout:     layout,
		SourcePath: dirPath,
	})
	if err == nil {
		t.Fatal("expected error when source path is a directory, got nil")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("error %q does not contain %q", err.Error(), "not a regular file")
	}
}

// ============================================================
// Preparation: layout validation
// ============================================================

func TestPrepareFreeCADRuntimeWorkingCopy_LayoutValidation(t *testing.T) {
	baseProductDir := t.TempDir()
	baseSourceFile := filepath.Join(baseProductDir, "base.FCStd")
	if err := os.WriteFile(baseSourceFile, []byte("base content"), 0o644); err != nil {
		t.Fatalf("write base source: %v", err)
	}

	baseLayout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: baseProductDir,
		ProductKey: "layout-val-test",
		PlanHash:   "plan-layout-val",
		SourcePath: baseSourceFile,
	})
	if err != nil {
		t.Fatalf("compute base layout: %v", err)
	}

	tests := []struct {
		name        string
		mutate      func(l FreeCADRuntimeWorkingCopyLayout) FreeCADRuntimeWorkingCopyLayout
		wantErrFrag string
	}{
		{
			name: "empty working-copy directory",
			mutate: func(l FreeCADRuntimeWorkingCopyLayout) FreeCADRuntimeWorkingCopyLayout {
				l.WorkingCopyDir = ""
				return l
			},
			wantErrFrag: "working-copy directory",
		},
		{
			name: "empty source directory",
			mutate: func(l FreeCADRuntimeWorkingCopyLayout) FreeCADRuntimeWorkingCopyLayout {
				l.SourceDir = ""
				return l
			},
			wantErrFrag: "source directory",
		},
		{
			name: "empty source document path",
			mutate: func(l FreeCADRuntimeWorkingCopyLayout) FreeCADRuntimeWorkingCopyLayout {
				l.SourceDocumentPath = ""
				return l
			},
			wantErrFrag: "source document path",
		},
		{
			name: "empty source document",
			mutate: func(l FreeCADRuntimeWorkingCopyLayout) FreeCADRuntimeWorkingCopyLayout {
				l.SourceDocument = ""
				return l
			},
			wantErrFrag: "source document",
		},
		{
			name: "source document path outside working-copy",
			mutate: func(l FreeCADRuntimeWorkingCopyLayout) FreeCADRuntimeWorkingCopyLayout {
				l.SourceDocumentPath = filepath.Join(baseProductDir, "outside.FCStd")
				return l
			},
			wantErrFrag: "source document path",
		},
		{
			name: "source directory outside working-copy",
			mutate: func(l FreeCADRuntimeWorkingCopyLayout) FreeCADRuntimeWorkingCopyLayout {
				l.SourceDir = baseProductDir
				return l
			},
			wantErrFrag: "source directory",
		},
		{
			name: "mismatched SourceDocument and SourceDocumentPath",
			mutate: func(l FreeCADRuntimeWorkingCopyLayout) FreeCADRuntimeWorkingCopyLayout {
				l.SourceDocument = "source/other.FCStd"
				return l
			},
			wantErrFrag: "source document path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mutatedLayout := tc.mutate(baseLayout)
			_, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
				Layout:     mutatedLayout,
				SourcePath: baseSourceFile,
			})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if tc.wantErrFrag != "" && !strings.Contains(err.Error(), tc.wantErrFrag) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErrFrag)
			}
		})
	}
}

// ============================================================
// Preparation: no side effects on failure
// ============================================================

func TestPrepareFreeCADRuntimeWorkingCopy_NoSideEffectsOnValidationFailure(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "no-side-effects",
		PlanHash:   "plan-nse",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	t.Run("empty source path", func(t *testing.T) {
		_, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
			Layout:     layout,
			SourcePath: "",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if _, statErr := os.Stat(layout.WorkingCopyDir); statErr == nil {
			t.Errorf("WorkingCopyDir created despite validation failure: %q", layout.WorkingCopyDir)
		}
		if _, statErr := os.Stat(layout.SourceDir); statErr == nil {
			t.Errorf("SourceDir created despite validation failure: %q", layout.SourceDir)
		}
		if _, statErr := os.Stat(layout.SourceDocumentPath); statErr == nil {
			t.Errorf("SourceDocumentPath created despite validation failure: %q", layout.SourceDocumentPath)
		}
	})

	t.Run("missing source file", func(t *testing.T) {
		_, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
			Layout:     layout,
			SourcePath: filepath.Join(productDir, "nonexistent.FCStd"),
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if _, statErr := os.Stat(layout.WorkingCopyDir); statErr == nil {
			t.Errorf("WorkingCopyDir created despite missing source file: %q", layout.WorkingCopyDir)
		}
		if _, statErr := os.Stat(layout.SourceDocumentPath); statErr == nil {
			t.Errorf("SourceDocumentPath created despite missing source file: %q", layout.SourceDocumentPath)
		}
	})

	t.Run("invalid layout does not create directories", func(t *testing.T) {
		mutatedLayout := layout
		mutatedLayout.WorkingCopyDir = ""
		_, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
			Layout:     mutatedLayout,
			SourcePath: sourcePath,
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		// ProductDir should be otherwise untouched; working root must not exist.
		if _, statErr := os.Stat(layout.WorkingRoot); statErr == nil {
			t.Errorf("WorkingRoot created despite invalid layout: %q", layout.WorkingRoot)
		}
	})
}

// ============================================================
// ValidatePreparedFreeCADRuntimeSourceDocument
// ============================================================

func TestValidatePreparedFreeCADRuntimeSourceDocument_SuccessAfterPreparation(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "box.FCStd")
	if err := os.WriteFile(sourcePath, []byte("FreeCAD source content"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "validate-success",
		PlanHash:   "plan-vs",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	if _, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
		Layout:     layout,
		SourcePath: sourcePath,
	}); err != nil {
		t.Fatalf("prepare working copy: %v", err)
	}

	if err := ValidatePreparedFreeCADRuntimeSourceDocument(layout); err != nil {
		t.Fatalf("ValidatePreparedFreeCADRuntimeSourceDocument failed: %v", err)
	}

	if layout.SourceDocument != "source/box.FCStd" {
		t.Errorf("SourceDocument = %q, want %q", layout.SourceDocument, "source/box.FCStd")
	}

	if _, statErr := os.Stat(layout.SourceDocumentPath); statErr != nil {
		t.Errorf("materialized source document not found at %q: %v", layout.SourceDocumentPath, statErr)
	}

	if _, statErr := os.Stat(layout.ManifestPath); statErr == nil {
		t.Errorf("validation unexpectedly created manifest file at %q", layout.ManifestPath)
	}
	if _, statErr := os.Stat(layout.ResultPath); statErr == nil {
		t.Errorf("validation unexpectedly created result file at %q", layout.ResultPath)
	}

	// OutputDir is created by preparation; validation must not delete or mutate it.
	outputDirInfo, statErr := os.Stat(layout.OutputDir)
	if statErr != nil {
		t.Errorf("OutputDir expected to exist after preparation: %v", statErr)
	} else if !outputDirInfo.IsDir() {
		t.Errorf("OutputDir %q is not a directory after validation", layout.OutputDir)
	}
}

func TestValidatePreparedFreeCADRuntimeSourceDocument_InvalidSourceDocument(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "box.FCStd")

	baseLayout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "invalid-src-doc",
		PlanHash:   "plan-isd",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute base layout: %v", err)
	}

	tests := []struct {
		name           string
		sourceDocument string
	}{
		{name: "empty", sourceDocument: ""},
		{name: "whitespace only", sourceDocument: "   "},
		{name: "absolute Unix path", sourceDocument: "/box.FCStd"},
		{name: "Windows drive path", sourceDocument: "C:/box.FCStd"},
		{name: "parent traversal", sourceDocument: "../box.FCStd"},
		{name: "dot", sourceDocument: "."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mutated := baseLayout
			mutated.SourceDocument = tc.sourceDocument
			if err := ValidatePreparedFreeCADRuntimeSourceDocument(mutated); err == nil {
				t.Fatalf("expected error for SourceDocument %q, got nil", tc.sourceDocument)
			}
		})
	}
}

func TestValidatePreparedFreeCADRuntimeSourceDocument_DestinationMismatch(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "box.FCStd")
	if err := os.WriteFile(sourcePath, []byte("box content"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "dest-mismatch",
		PlanHash:   "plan-dm",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	if _, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
		Layout:     layout,
		SourcePath: sourcePath,
	}); err != nil {
		t.Fatalf("prepare working copy: %v", err)
	}

	// Mutate both SourceDocument and SourceDocumentPath consistently to point
	// to a different contained path that was not materialized. The layout is
	// internally consistent but the named file does not exist.
	mutated := layout
	mutated.SourceDocument = "source/other.FCStd"
	mutated.SourceDocumentPath = filepath.Join(layout.WorkingCopyDir, "source", "other.FCStd")

	if err := ValidatePreparedFreeCADRuntimeSourceDocument(mutated); err == nil {
		t.Fatal("expected validation failure for destination mismatch, got nil")
	}
}

func TestValidatePreparedFreeCADRuntimeSourceDocument_MissingPreparedFile(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "missing-file",
		PlanHash:   "plan-mf",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	// Do not call PrepareFreeCADRuntimeWorkingCopy; the working copy does not exist.
	if err := ValidatePreparedFreeCADRuntimeSourceDocument(layout); err == nil {
		t.Fatal("expected validation failure for missing prepared file, got nil")
	}
}

func TestValidatePreparedFreeCADRuntimeSourceDocument_MissingPreparedFileAfterRemoval(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")
	if err := os.WriteFile(sourcePath, []byte("model data"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "missing-after-removal",
		PlanHash:   "plan-mar",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	if _, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
		Layout:     layout,
		SourcePath: sourcePath,
	}); err != nil {
		t.Fatalf("prepare working copy: %v", err)
	}

	if err := os.Remove(layout.SourceDocumentPath); err != nil {
		t.Fatalf("remove materialized source document: %v", err)
	}

	if err := ValidatePreparedFreeCADRuntimeSourceDocument(layout); err == nil {
		t.Fatal("expected validation failure after source document removal, got nil")
	}
}

func TestValidatePreparedFreeCADRuntimeSourceDocument_DirectoryInsteadOfFile(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "dir-not-file",
		PlanHash:   "plan-dnf",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	// Create a directory at the path where the source document should be.
	if err := os.MkdirAll(layout.SourceDocumentPath, 0o755); err != nil {
		t.Fatalf("create directory at source document path: %v", err)
	}

	err = ValidatePreparedFreeCADRuntimeSourceDocument(layout)
	if err == nil {
		t.Fatal("expected validation failure when source document path is a directory, got nil")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("error %q does not mention %q", err.Error(), "not a regular file")
	}
}

func TestValidatePreparedFreeCADRuntimeSourceDocument_SymlinkEscape(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "symlink-escape",
		PlanHash:   "plan-se",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	if err := os.MkdirAll(layout.SourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	// Create a real file outside the working copy as the symlink target.
	escapeTarget := filepath.Join(productDir, "escape-target.FCStd")
	if err := os.WriteFile(escapeTarget, []byte("escape target"), 0o644); err != nil {
		t.Fatalf("write escape target: %v", err)
	}

	if err := os.Symlink(escapeTarget, layout.SourceDocumentPath); err != nil {
		t.Skipf("cannot create symlink (platform limitation): %v", err)
	}

	err = ValidatePreparedFreeCADRuntimeSourceDocument(layout)
	if err == nil {
		t.Fatal("expected validation failure for symlink escape, got nil")
	}
	if !strings.Contains(err.Error(), "not contained under") {
		t.Errorf("error %q does not mention %q", err.Error(), "not contained under")
	}
}

func TestPrepareFreeCADRuntimeWorkingCopy_ValidationInvokedAfterCopy(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "assembly.FCStd")
	if err := os.WriteFile(sourcePath, []byte("assembly data"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "post-copy-validate",
		PlanHash:   "plan-pcv",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	result, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
		Layout:     layout,
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("prepare working copy: %v", err)
	}

	// The validator must accept the post-preparation state directly, proving
	// that preparation leaves the working copy in a valid prepared state.
	if err := ValidatePreparedFreeCADRuntimeSourceDocument(result.Layout); err != nil {
		t.Errorf("ValidatePreparedFreeCADRuntimeSourceDocument rejected post-preparation state: %v", err)
	}
}

func TestValidatePreparedFreeCADRuntimeSourceDocument_SideEffectFree(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "widget.FCStd")
	if err := os.WriteFile(sourcePath, []byte("widget data"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "side-effect-free",
		PlanHash:   "plan-sef",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	if _, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
		Layout:     layout,
		SourcePath: sourcePath,
	}); err != nil {
		t.Fatalf("prepare working copy: %v", err)
	}

	beforeEntries, err := os.ReadDir(layout.WorkingCopyDir)
	if err != nil {
		t.Fatalf("read working copy dir before validation: %v", err)
	}

	if err := ValidatePreparedFreeCADRuntimeSourceDocument(layout); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	afterEntries, err := os.ReadDir(layout.WorkingCopyDir)
	if err != nil {
		t.Fatalf("read working copy dir after validation: %v", err)
	}

	// Validation must not create, remove, or rename entries inside the working copy.
	if len(beforeEntries) != len(afterEntries) {
		t.Errorf("working copy directory entry count changed during validation: before %d, after %d",
			len(beforeEntries), len(afterEntries))
	}

	if _, statErr := os.Stat(layout.ManifestPath); statErr == nil {
		t.Errorf("validation created manifest file at %q", layout.ManifestPath)
	}
	if _, statErr := os.Stat(layout.ResultPath); statErr == nil {
		t.Errorf("validation created result file at %q", layout.ResultPath)
	}
	// OutputDir exists because preparation created it; that is expected and must not be
	// confused with validation creating it. The entry-count assertion above proves that
	// validation itself added no new entries.
}

// ============================================================
// Output directory preparation
// ============================================================

func TestPrepareFreeCADRuntimeOutputDirectories_OutputDirCreated(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")
	if err := os.WriteFile(sourcePath, []byte("model data"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "output-dir-created",
		PlanHash:   "plan-odc",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	if _, err := PrepareFreeCADRuntimeWorkingCopy(FreeCADRuntimeWorkingCopyPreparationRequest{
		Layout:     layout,
		SourcePath: sourcePath,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outputDirInfo, statErr := os.Stat(layout.OutputDir)
	if statErr != nil {
		t.Fatalf("OutputDir not created by preparation: %v", statErr)
	}
	if !outputDirInfo.IsDir() {
		t.Errorf("OutputDir %q is not a directory", layout.OutputDir)
	}

	if _, statErr := os.Stat(layout.ManifestPath); statErr == nil {
		t.Errorf("manifest file unexpectedly created at %q", layout.ManifestPath)
	}
	if _, statErr := os.Stat(layout.ResultPath); statErr == nil {
		t.Errorf("result file unexpectedly created at %q", layout.ResultPath)
	}

	entries, err := os.ReadDir(layout.OutputDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("output dir has %d unexpected entries after preparation", len(entries))
	}
}

func TestPrepareFreeCADRuntimeWorkingCopy_IdempotentOutputDir(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")
	if err := os.WriteFile(sourcePath, []byte("model data"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "idempotent-output",
		PlanHash:   "plan-idem-out",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	prepReq := FreeCADRuntimeWorkingCopyPreparationRequest{
		Layout:     layout,
		SourcePath: sourcePath,
	}

	if _, err := PrepareFreeCADRuntimeWorkingCopy(prepReq); err != nil {
		t.Fatalf("first preparation: %v", err)
	}

	sentinelPath := filepath.Join(layout.OutputDir, "sentinel.txt")
	if err := os.WriteFile(sentinelPath, []byte("sentinel"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	if _, err := PrepareFreeCADRuntimeWorkingCopy(prepReq); err != nil {
		t.Fatalf("second preparation: %v", err)
	}

	outputDirInfo, statErr := os.Stat(layout.OutputDir)
	if statErr != nil {
		t.Fatalf("OutputDir missing after second preparation: %v", statErr)
	}
	if !outputDirInfo.IsDir() {
		t.Errorf("OutputDir %q is not a directory after second preparation", layout.OutputDir)
	}

	if _, statErr := os.Stat(layout.SourceDocumentPath); statErr != nil {
		t.Errorf("source document missing after second preparation: %v", statErr)
	}

	if _, statErr := os.Stat(sentinelPath); statErr != nil {
		t.Errorf("sentinel file removed by second preparation: %v", statErr)
	}
}

func TestPrepareFreeCADRuntimeOutputDirectories_FileConflict(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "file-conflict",
		PlanHash:   "plan-fc",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	if err := os.MkdirAll(layout.WorkingCopyDir, 0o755); err != nil {
		t.Fatalf("create working copy dir: %v", err)
	}
	if err := os.WriteFile(layout.OutputDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("create file at output dir path: %v", err)
	}

	err = PrepareFreeCADRuntimeOutputDirectories(layout)
	if err == nil {
		t.Fatal("expected error for file conflict at output path, got nil")
	}
	if !strings.Contains(err.Error(), "output directory") {
		t.Errorf("error %q does not mention output directory", err.Error())
	}

	if _, statErr := os.Stat(layout.ManifestPath); statErr == nil {
		t.Errorf("manifest file created despite conflict: %q", layout.ManifestPath)
	}
	if _, statErr := os.Stat(layout.ResultPath); statErr == nil {
		t.Errorf("result file created despite conflict: %q", layout.ResultPath)
	}
}

func TestPrepareFreeCADRuntimeOutputDirectories_OutputDirOutsideWorkingCopy(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "outside-wc",
		PlanHash:   "plan-owc",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	mutated := layout
	mutated.OutputDir = filepath.Join(productDir, "outputs")

	err = PrepareFreeCADRuntimeOutputDirectories(mutated)
	if err == nil {
		t.Fatal("expected error for output directory outside working copy, got nil")
	}
	if !strings.Contains(err.Error(), "output directory") {
		t.Errorf("error %q does not mention output directory", err.Error())
	}

	if _, statErr := os.Stat(mutated.OutputDir); statErr == nil {
		t.Errorf("external output directory unexpectedly created at %q", mutated.OutputDir)
	}
}

func TestPrepareFreeCADRuntimeOutputDirectories_MutatedOutputRelativePath(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "rel-path-mut",
		PlanHash:   "plan-rpm",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	cases := []struct {
		name string
		path string
	}{
		{"parent traversal", "../outputs"},
		{"nested subdirectory", "nested/outputs"},
		{"parent in middle", "outputs/.."},
		{"empty string", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := layout
			mutated.OutputDirRelativePath = tc.path
			if err := PrepareFreeCADRuntimeOutputDirectories(mutated); err == nil {
				t.Fatalf("expected error for OutputDirRelativePath %q, got nil", tc.path)
			}
		})
	}
}

func TestPrepareFreeCADRuntimeOutputDirectories_SymlinkEscape(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "symlink-output",
		PlanHash:   "plan-slo",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	if err := os.MkdirAll(layout.WorkingCopyDir, 0o755); err != nil {
		t.Fatalf("create working copy dir: %v", err)
	}
	externalDir := t.TempDir()
	if err := os.Symlink(externalDir, layout.OutputDir); err != nil {
		t.Skipf("cannot create symlink (platform limitation): %v", err)
	}

	err = PrepareFreeCADRuntimeOutputDirectories(layout)
	if err == nil {
		t.Fatal("expected error for symlink at output directory path, got nil")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("error %q does not mention symbolic link", err.Error())
	}

	entries, readErr := os.ReadDir(externalDir)
	if readErr != nil {
		t.Fatalf("read external dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("external directory has %d unexpected entries; preparation must not write through the symlink", len(entries))
	}
}

func TestPrepareFreeCADRuntimeOutputDirectories_ExistingRealDir(t *testing.T) {
	productDir := t.TempDir()
	sourcePath := filepath.Join(productDir, "model.FCStd")

	layout, err := ComputeFreeCADRuntimeWorkingCopyLayout(FreeCADRuntimeWorkingCopyLayoutRequest{
		ProductDir: productDir,
		ProductKey: "existing-output-dir",
		PlanHash:   "plan-eod",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("compute layout: %v", err)
	}

	if err := os.MkdirAll(layout.OutputDir, 0o755); err != nil {
		t.Fatalf("pre-create output dir: %v", err)
	}
	existingFile := filepath.Join(layout.OutputDir, "existing.json")
	if err := os.WriteFile(existingFile, []byte(`{"existing":true}`), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	if err := PrepareFreeCADRuntimeOutputDirectories(layout); err != nil {
		t.Fatalf("unexpected error for pre-existing output directory: %v", err)
	}

	outputDirInfo, statErr := os.Stat(layout.OutputDir)
	if statErr != nil {
		t.Fatalf("OutputDir missing after preparation: %v", statErr)
	}
	if !outputDirInfo.IsDir() {
		t.Errorf("OutputDir %q is not a directory after preparation", layout.OutputDir)
	}

	if _, statErr := os.Stat(existingFile); statErr != nil {
		t.Errorf("existing file removed by preparation: %v", statErr)
	}
}
