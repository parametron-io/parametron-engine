package projectinput

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/engine/tableloader"
)

func TestCaptureResources_DeterministicAcrossCallsAndEntrypoints(t *testing.T) {
	projectDir, expected := writeProjectCaptureFixture(t)
	projectFile := filepath.Join(projectDir, "parametron.project.json")

	fromDirResolved, err := ResolveProject(projectDir)
	if err != nil {
		t.Fatalf("ResolveProject(directory) returned error: %v", err)
	}
	firstDir, err := CaptureResources(fromDirResolved)
	if err != nil {
		t.Fatalf("CaptureResources(directory) returned error: %v", err)
	}
	secondDir, err := CaptureResources(fromDirResolved)
	if err != nil {
		t.Fatalf("CaptureResources(directory second) returned error: %v", err)
	}
	if !reflect.DeepEqual(firstDir, secondDir) {
		t.Fatalf("expected deterministic capture across repeated calls\nfirst:  %#v\nsecond: %#v", firstDir, secondDir)
	}

	fromFileResolved, err := ResolveProject(projectFile)
	if err != nil {
		t.Fatalf("ResolveProject(file) returned error: %v", err)
	}
	fromFile, err := CaptureResources(fromFileResolved)
	if err != nil {
		t.Fatalf("CaptureResources(file) returned error: %v", err)
	}

	if !reflect.DeepEqual(firstDir, fromFile) {
		t.Fatalf("expected equivalent capture across entrypoints\ndir:  %#v\nfile: %#v", firstDir, fromFile)
	}
	if !reflect.DeepEqual(firstDir, expected) {
		t.Fatalf("unexpected capture output\nwant: %#v\ngot:  %#v", expected, firstDir)
	}
}

func TestCaptureResources_OrdersModelsAndTablesByLogicalID(t *testing.T) {
	projectDir, _ := writeProjectCaptureFixture(t)

	resolved, err := ResolveProject(projectDir)
	if err != nil {
		t.Fatalf("ResolveProject returned error: %v", err)
	}
	captured, err := CaptureResources(resolved)
	if err != nil {
		t.Fatalf("CaptureResources returned error: %v", err)
	}

	if got, want := logicalIDsFromCaptured(captured.Models), []string{"box_model", "cover_model"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected model ordering: got %v want %v", got, want)
	}
	if got, want := logicalIDsFromCaptured(captured.Tables), []string{"fasteners", "labels"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected table ordering: got %v want %v", got, want)
	}
}

func TestCaptureResources_ChangeDetection(t *testing.T) {
	projectDir, baseline := writeProjectCaptureFixture(t)

	projectFile := filepath.Join(projectDir, "parametron.project.json")
	resolved, err := ResolveProject(projectFile)
	if err != nil {
		t.Fatalf("ResolveProject returned error: %v", err)
	}

	modelPath := resolved.ModelPaths["box_model"]
	if err := os.WriteFile(modelPath, []byte("model-a-updated"), 0o644); err != nil {
		t.Fatalf("failed to update model fixture: %v", err)
	}
	modelChanged, err := CaptureResources(resolved)
	if err != nil {
		t.Fatalf("CaptureResources(model changed) returned error: %v", err)
	}
	if modelChanged.Models[0].Signature == baseline.Models[0].Signature {
		t.Fatal("expected model signature to change after model file content update")
	}

	tablePath := resolved.TablePaths["fasteners"]
	if err := os.WriteFile(tablePath, []byte(`{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 9}
  ]
}`), 0o644); err != nil {
		t.Fatalf("failed to update table fixture: %v", err)
	}
	tableChanged, err := CaptureResources(resolved)
	if err != nil {
		t.Fatalf("CaptureResources(table changed) returned error: %v", err)
	}
	if tableChanged.Tables[0].Signature == modelChanged.Tables[0].Signature {
		t.Fatal("expected table signature to change after table content update")
	}

	if err := os.WriteFile(resolved.DSLPath, []byte(withVersionHeader(`
product Widget {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
    param label: string = table_cell("labels", sku, "label")
    param revision: number = 2
}
`)), 0o644); err != nil {
		t.Fatalf("failed to update DSL fixture: %v", err)
	}
	dslChanged, err := CaptureResources(resolved)
	if err != nil {
		t.Fatalf("CaptureResources(DSL changed) returned error: %v", err)
	}
	if dslChanged.DSL.Signature == tableChanged.DSL.Signature {
		t.Fatal("expected DSL signature to change after DSL content update")
	}
}

func TestResolveProject_EntrypointsEquivalentAndDeterministic(t *testing.T) {
	projectDir := filepath.Join(findRepoRoot(t), "testdata", "projects", "freecad", "smoke", "minimal-valid-project")
	projectFile := filepath.Join(projectDir, "parametron.project.json")

	firstDir, err := ResolveProject(projectDir)
	if err != nil {
		t.Fatalf("ResolveProject(directory) returned error: %v", err)
	}
	secondDir, err := ResolveProject(projectDir)
	if err != nil {
		t.Fatalf("ResolveProject(directory) second call returned error: %v", err)
	}
	if !reflect.DeepEqual(firstDir, secondDir) {
		t.Fatalf("expected deterministic directory resolution\nfirst: %#v\nsecond: %#v", firstDir, secondDir)
	}

	fromFile, err := ResolveProject(projectFile)
	if err != nil {
		t.Fatalf("ResolveProject(file) returned error: %v", err)
	}
	if !reflect.DeepEqual(firstDir, fromFile) {
		t.Fatalf("expected equivalent resolution across entrypoints\ndir:  %#v\nfile: %#v", firstDir, fromFile)
	}
}

func TestResolveProject_RelativeEntrypointsReturnAbsoluteResolvedPaths(t *testing.T) {
	repoRoot := findRepoRoot(t)
	projectDir := filepath.Join(repoRoot, "testdata", "projects", "freecad", "integration", "real-table-driven-project")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	relativeProjectDir, err := filepath.Rel(cwd, projectDir)
	if err != nil {
		t.Fatalf("filepath.Rel(projectDir) returned error: %v", err)
	}
	relativeProjectFile := filepath.Join(relativeProjectDir, "parametron.project.json")

	expectedProjectFile := filepath.Join(projectDir, "parametron.project.json")
	expectedDSLPath := filepath.Join(projectDir, "rehearsal.project.dsl")
	expectedModelPath := filepath.Join(projectDir, "input", "Coupling.FCStd")
	expectedTablePath := filepath.Join(projectDir, "tables", "variants.json")

	for _, entryPath := range []string{relativeProjectDir, relativeProjectFile} {
		entryPath := entryPath
		t.Run(filepath.Base(entryPath), func(t *testing.T) {
			resolved, err := ResolveProject(entryPath)
			if err != nil {
				t.Fatalf("ResolveProject(%q) returned error: %v", entryPath, err)
			}

			if resolved.ProjectFile != expectedProjectFile {
				t.Fatalf("expected absolute project file %q, got %q", expectedProjectFile, resolved.ProjectFile)
			}
			if resolved.ProjectRoot != projectDir {
				t.Fatalf("expected absolute project root %q, got %q", projectDir, resolved.ProjectRoot)
			}
			if resolved.DSLPath != expectedDSLPath {
				t.Fatalf("expected absolute DSL path %q, got %q", expectedDSLPath, resolved.DSLPath)
			}
			if got := resolved.ModelPaths["coupling"]; got != expectedModelPath {
				t.Fatalf("expected absolute model path %q, got %q", expectedModelPath, got)
			}
			if got := resolved.TablePaths["variants"]; got != expectedTablePath {
				t.Fatalf("expected absolute table path %q, got %q", expectedTablePath, got)
			}
		})
	}
}

func TestLoadProjectTables_Deterministic(t *testing.T) {
	projectDir := filepath.Join(findRepoRoot(t), "testdata", "projects", "freecad", "smoke", "valid-project-with-table")
	resolved, err := ResolveProject(projectDir)
	if err != nil {
		t.Fatalf("ResolveProject returned error: %v", err)
	}

	first, err := LoadProjectTables(resolved)
	if err != nil {
		t.Fatalf("LoadProjectTables first call returned error: %v", err)
	}
	second, err := LoadProjectTables(resolved)
	if err != nil {
		t.Fatalf("LoadProjectTables second call returned error: %v", err)
	}

	if got, want := sortedKeys(first), []string{"fasteners"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected logical IDs %v, got %v", want, got)
	}
	if !reflect.DeepEqual(sortedKeys(first), sortedKeys(second)) {
		t.Fatalf("expected deterministic logical IDs across calls, got %v and %v", sortedKeys(first), sortedKeys(second))
	}

	firstFingerprint, err := first["fasteners"].Fingerprint()
	if err != nil {
		t.Fatalf("first fingerprint failed: %v", err)
	}
	secondFingerprint, err := second["fasteners"].Fingerprint()
	if err != nil {
		t.Fatalf("second fingerprint failed: %v", err)
	}
	if firstFingerprint != secondFingerprint {
		t.Fatalf("expected deterministic fingerprint, got %q and %q", firstFingerprint, secondFingerprint)
	}
	if first["fasteners"].Name != second["fasteners"].Name {
		t.Fatalf("expected deterministic table name, got %q and %q", first["fasteners"].Name, second["fasteners"].Name)
	}
}

func TestApplyModelMappings_RewritesLogicalIDsToPhysicalPaths(t *testing.T) {
	projectDir := filepath.Join(findRepoRoot(t), "testdata", "projects", "freecad", "smoke", "minimal-valid-project")
	resolved, err := ResolveProject(projectDir)
	if err != nil {
		t.Fatalf("ResolveProject returned error: %v", err)
	}

	ast, err := dsl.Parse(resolved.DSLPath)
	if err != nil {
		t.Fatalf("dsl.Parse returned error: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("dsl.Validate returned error: %v", err)
	}
	if err := ApplyModelMappings(ast, resolved); err != nil {
		t.Fatalf("ApplyModelMappings returned error: %v", err)
	}

	product := ast.Products[0]
	literal, ok := product.SourceModel.(*dsl.LiteralExpression)
	if !ok {
		t.Fatalf("expected SourceModel literal after rewrite, got %T", product.SourceModel)
	}
	got, ok := literal.Value.(string)
	if !ok {
		t.Fatalf("expected rewritten source model to be string, got %T", literal.Value)
	}
	if got != resolved.ModelPaths["box_model"] {
		t.Fatalf("expected rewritten source model %q, got %q", resolved.ModelPaths["box_model"], got)
	}
	if got == "box_model" {
		t.Fatalf("expected physical path, got logical ID %q", got)
	}
}

func TestApplyModelMappings_MixedAdaptersLeaveAdapterNoneWithoutSourceModel(t *testing.T) {
	projectDir := filepath.Join(findRepoRoot(t), "testdata", "projects", "freecad", "smoke", "multi-adapter-project")
	resolved, err := ResolveProject(projectDir)
	if err != nil {
		t.Fatalf("ResolveProject returned error: %v", err)
	}

	ast, err := dsl.Parse(resolved.DSLPath)
	if err != nil {
		t.Fatalf("dsl.Parse returned error: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("dsl.Validate returned error: %v", err)
	}
	if err := ApplyModelMappings(ast, resolved); err != nil {
		t.Fatalf("ApplyModelMappings returned error: %v", err)
	}

	products := map[string]*dsl.ProductNode{}
	for _, product := range ast.Products {
		products[product.Name] = product
	}

	freecad := products["Box"]
	if freecad == nil {
		t.Fatalf("expected FreeCADBox product, got %v", sortedProductKeys(products))
	}
	literal, ok := freecad.SourceModel.(*dsl.LiteralExpression)
	if !ok {
		t.Fatalf("expected FreeCADBox SourceModel literal after rewrite, got %T", freecad.SourceModel)
	}
	if got := literal.Value.(string); got != resolved.ModelPaths["box_model"] {
		t.Fatalf("expected FreeCADBox source model %q, got %q", resolved.ModelPaths["box_model"], got)
	}

	noneAdapter := products["Spec"]
	if noneAdapter == nil {
		t.Fatalf("expected NoAdapterSummary product, got %v", sortedProductKeys(products))
	}
	if noneAdapter.SourceModel != nil {
		t.Fatalf("expected adapter=\"none\" product to keep nil SourceModel, got %T", noneAdapter.SourceModel)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd failed: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func sortedProductKeys(m map[string]*dsl.ProductNode) []string {
	return sortedKeys(m)
}

func logicalIDsFromCaptured(resources []CapturedResource) []string {
	logicalIDs := make([]string, 0, len(resources))
	for _, resource := range resources {
		logicalIDs = append(logicalIDs, resource.LogicalID)
	}
	return logicalIDs
}

func writeProjectCaptureFixture(t *testing.T) (string, *CapturedResources) {
	t.Helper()

	projectDir := t.TempDir()
	projectDSL := filepath.Join(projectDir, "project.dsl")
	modelDir := filepath.Join(projectDir, "models")
	tableDir := filepath.Join(projectDir, "tables")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.MkdirAll(tableDir, 0o755); err != nil {
		t.Fatalf("failed to create table dir: %v", err)
	}

	if err := os.WriteFile(projectDSL, []byte(withVersionHeader(`
const PRIMARY_MODEL = "box_model"

product Widget {
    adapter = "freecad"
    source_model = PRIMARY_MODEL
    outputs = ["step"]

    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
    param label: string = table_cell("labels", sku, "label")
}
`)), 0o644); err != nil {
		t.Fatalf("failed to write project DSL: %v", err)
	}

	modelAPath := filepath.Join(modelDir, "box.FCStd")
	modelBPath := filepath.Join(modelDir, "cover.FCStd")
	if err := os.WriteFile(modelAPath, []byte("model-a"), 0o644); err != nil {
		t.Fatalf("failed to write model A: %v", err)
	}
	if err := os.WriteFile(modelBPath, []byte("model-b"), 0o644); err != nil {
		t.Fatalf("failed to write model B: %v", err)
	}

	fastenersPath := filepath.Join(tableDir, "fasteners.json")
	labelsPath := filepath.Join(tableDir, "labels.json")
	if err := os.WriteFile(fastenersPath, []byte(`{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8}
  ]
}`), 0o644); err != nil {
		t.Fatalf("failed to write fasteners table: %v", err)
	}
	if err := os.WriteFile(labelsPath, []byte(`{
  "schemaVersion": "1.0",
  "name": "label_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "label", "type": "string", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "label": "M8 bolt"}
  ]
}`), 0o644); err != nil {
		t.Fatalf("failed to write labels table: %v", err)
	}

	projectFile := filepath.Join(projectDir, "parametron.project.json")
	if err := os.WriteFile(projectFile, []byte(`{
  "version": "1.0",
  "projectId": "capture-project",
  "dsl": "./project.dsl",
  "resources": {
    "models": {
      "cover_model": "./models/cover.FCStd",
      "box_model": "./models/box.FCStd"
    }
  },
  "tables": {
    "labels": "./tables/labels.json",
    "fasteners": "./tables/fasteners.json"
  }
}`), 0o644); err != nil {
		t.Fatalf("failed to write project map: %v", err)
	}

	expected := &CapturedResources{
		DSL: CapturedResource{
			ResolvedPath: projectDSL,
			Signature:    mustFileSHA256(t, projectDSL),
			Kind:         ResourceKindDSL,
		},
		Models: []CapturedResource{
			{
				LogicalID:    "box_model",
				ResolvedPath: modelAPath,
				Signature:    mustFileSHA256(t, modelAPath),
				Kind:         ResourceKindModel,
			},
			{
				LogicalID:    "cover_model",
				ResolvedPath: modelBPath,
				Signature:    mustFileSHA256(t, modelBPath),
				Kind:         ResourceKindModel,
			},
		},
		Tables: []CapturedResource{
			{
				LogicalID:    "fasteners",
				ResolvedPath: fastenersPath,
				Signature:    mustTableFingerprint(t, fastenersPath),
				Kind:         ResourceKindTable,
			},
			{
				LogicalID:    "labels",
				ResolvedPath: labelsPath,
				Signature:    mustTableFingerprint(t, labelsPath),
				Kind:         ResourceKindTable,
			},
		},
	}

	firstJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("failed to marshal expected capture: %v", err)
	}
	secondJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("failed to marshal expected capture second time: %v", err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatal("expected capture fixture JSON to marshal deterministically")
	}

	return projectDir, expected
}

func mustFileSHA256(t *testing.T, path string) string {
	t.Helper()
	hash, err := computeFileSHA256(path)
	if err != nil {
		t.Fatalf("computeFileSHA256(%s) failed: %v", path, err)
	}
	return hash
}

func mustTableFingerprint(t *testing.T, path string) string {
	t.Helper()
	projectTables, err := tableloader.LoadFiles(map[string]string{"table": path})
	if err != nil {
		t.Fatalf("LoadFiles(%s) failed: %v", path, err)
	}
	fingerprint, err := projectTables["table"].Fingerprint()
	if err != nil {
		t.Fatalf("Fingerprint(%s) failed: %v", path, err)
	}
	return fingerprint
}

func withVersionHeader(content string) string {
	trimmed := bytes.TrimSpace([]byte(content))
	if bytes.HasPrefix(trimmed, []byte("dsl v1.0")) || bytes.HasPrefix(trimmed, []byte("dsl 1.0")) {
		return content
	}
	return "dsl v1.0\n" + content
}
