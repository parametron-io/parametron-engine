package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/artifact"
)

// fileWritingAdapter writes real files to productDir so RecordExisting can find them.
// It is intentionally minimal: no FreeCAD, no Python, no external dependencies.
type fileWritingAdapter struct {
	productDir string
}

func (a *fileWritingAdapter) Run(_ context.Context, step planner.Step) error {
	switch step.Type {
	case planner.StepWriteCSV:
		payload := step.Payload.(planner.WriteCSVPayload)
		path := filepath.Join(a.productDir, payload.Filename)
		return os.WriteFile(path, []byte("param\n100\n"), 0644)
	case planner.StepWriteExportManifest:
		payload := step.Payload.(planner.WriteExportManifestPayload)
		path := filepath.Join(a.productDir, payload.ManifestFilename)
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return os.WriteFile(path, append(data, '\n'), 0644)
	}
	return nil
}

// TestNewWithArtifacts_RegistersCSVAndManifest verifies that after executing a
// two-step product plan (WriteCSV + WriteExportManifest) with a store-aware executor,
// the artifact store contains CSV/JSON artifacts and the run-level manifest is non-empty.
func TestNewWithArtifacts_RegistersCSVAndManifest(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	runRoot := filepath.Join(tmpDir, "runroot")
	productDir := planner.BuildProductDir(runRoot, "widget")
	if err := os.MkdirAll(productDir, 0755); err != nil {
		t.Fatalf("failed to create productDir: %v", err)
	}

	store := artifact.NewFileSystemStore(runRoot)
	adp := &fileWritingAdapter{productDir: productDir}

	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					Filename: "widget.csv",
					Headers:  []string{"width"},
					Values:   []interface{}{100.0},
				},
			},
			{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ProductKey:       "widget",
					ManifestFilename: planner.ExportManifestFilename,
					SchemaVersion:    planner.ExportManifestSchemaVersion,
					PlanHash:         "hash",
					Product: planner.ExportManifestProduct{
						ID: "widget",
					},
					Inputs: planner.ExportManifestInputs{},
					Outputs: []planner.ExportManifestOutput{
						{
							Type:     "step",
							Filename: "widget.step",
						},
					},
				},
			},
		},
	}

	exec := NewWithArtifacts(adp, store, productDir)
	if err := exec.Execute(context.Background(), plan); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	artifacts, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d: %+v", len(artifacts), artifacts)
	}

	types := map[artifact.ArtifactType]bool{}
	for _, a := range artifacts {
		types[a.Type] = true
		if a.ProductID != "widget" {
			t.Errorf("expected productId %q, got %q", "widget", a.ProductID)
		}
		if a.Class != artifact.ArtifactClassExecutionOutput {
			t.Errorf("expected execution_output class for %s, got %q", a.Filename, a.Class)
		}
		if a.ChecksumSHA256 == "" {
			t.Errorf("expected checksum to be set for artifact %s", a.Filename)
		}
		if a.SizeBytes == 0 {
			t.Errorf("expected non-zero size for artifact %s", a.Filename)
		}
		if a.Path == "" {
			t.Errorf("expected non-empty path for artifact %s", a.Filename)
		}
	}
	if !types[artifact.ArtifactTypeCSV] {
		t.Fatal("expected CSV artifact in store")
	}
	if !types[artifact.ArtifactTypeJSON] {
		t.Fatal("expected JSON artifact in store")
	}

	if err := store.WriteManifest(); err != nil {
		t.Fatalf("WriteManifest returned error: %v", err)
	}
	manifestPath := filepath.Join(runRoot, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}
	if !strings.Contains(string(data), "widget.csv") {
		t.Fatalf("expected manifest to reference widget.csv, got:\n%s", string(data))
	}
	if !strings.Contains(string(data), planner.ExportManifestFilename) {
		t.Fatalf("expected manifest to reference %s, got:\n%s", planner.ExportManifestFilename, string(data))
	}
}

// TestNewWithArtifacts_MultiProduct_NoConcurrentDataRace verifies that two products
// executing in parallel both register their artifacts safely (no map data races).
// Run with -race to detect concurrent access violations.
func TestNewWithArtifacts_MultiProduct_NoConcurrentDataRace(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	runRoot := filepath.Join(tmpDir, "runroot")
	store := artifact.NewFileSystemStore(runRoot)

	products := []string{"alpha", "beta"}

	type result struct {
		name string
		err  error
	}
	results := make(chan result, len(products))

	for _, name := range products {
		name := name
		productDir := planner.BuildProductDir(runRoot, name)
		if err := os.MkdirAll(productDir, 0755); err != nil {
			t.Fatalf("failed to create productDir for %s: %v", name, err)
		}

		go func() {
			adp := &fileWritingAdapter{productDir: productDir}
			plan := &planner.ExecutionPlan{
				Steps: []planner.Step{
					{
						Type: planner.StepWriteCSV,
						Payload: planner.WriteCSVPayload{
							Filename: name + ".csv",
							Headers:  []string{"x"},
							Values:   []interface{}{1.0},
						},
					},
					{
						Type: planner.StepWriteExportManifest,
						Payload: planner.WriteExportManifestPayload{
							ProductKey:       name,
							ManifestFilename: planner.ExportManifestFilename,
							SchemaVersion:    planner.ExportManifestSchemaVersion,
							PlanHash:         "hash",
							Product: planner.ExportManifestProduct{
								ID: name,
							},
							Inputs: planner.ExportManifestInputs{},
							Outputs: []planner.ExportManifestOutput{
								{
									Type:     "step",
									Filename: name + ".step",
								},
							},
						},
					},
				},
			}
			exec := NewWithArtifacts(adp, store, productDir)
			results <- result{name: name, err: exec.Execute(context.Background(), plan)}
		}()
	}

	for range products {
		r := <-results
		if r.err != nil {
			t.Errorf("product %s execution failed: %v", r.name, r.err)
		}
	}

	artifacts, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	// 2 products × 2 artifacts (CSV + JSON) = 4 total
	if len(artifacts) != 4 {
		t.Fatalf("expected 4 artifacts, got %d: %+v", len(artifacts), artifacts)
	}
}

// TestNewWithArtifacts_StoreNil_NoPanic verifies that an executor constructed
// without a store (via New) behaves exactly as before this change.
func TestNewWithArtifacts_StoreNil_NoPanic(t *testing.T) {
	t.Parallel()

	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type:    planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{Filename: "x.csv"},
			},
		},
	}
	exec := New(&recordingAdapter{})
	if err := exec.Execute(context.Background(), plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRecordExisting_ArtifactPathStoredRelativeToRunRoot checks that artifacts
// written to productDir have store-relative paths like "products/widget/widget.csv".
func TestRecordExisting_ArtifactPathStoredRelativeToRunRoot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	runRoot := filepath.Join(tmpDir, "run")
	productDir := planner.BuildProductDir(runRoot, "widget")
	if err := os.MkdirAll(productDir, 0755); err != nil {
		t.Fatal(err)
	}

	csvPath := filepath.Join(productDir, "widget.csv")
	if err := os.WriteFile(csvPath, []byte("x\n1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := artifact.NewFileSystemStore(runRoot)
	a, err := store.RecordExisting(context.Background(), csvPath, artifact.Artifact{
		Type:      artifact.ArtifactTypeCSV,
		ProductID: "widget",
		StepID:    "0",
		Filename:  "widget.csv",
	})
	if err != nil {
		t.Fatalf("RecordExisting returned error: %v", err)
	}

	wantPath := "products/widget/widget.csv"
	if a.Path != wantPath {
		t.Fatalf("expected store-relative path %q, got %q", wantPath, a.Path)
	}

	gotAbsPath, ok := store.GetPath(a.ID)
	if !ok {
		t.Fatal("GetPath returned false")
	}
	absCSV, _ := filepath.Abs(csvPath)
	if gotAbsPath != absCSV {
		t.Fatalf("GetPath: expected %q, got %q", absCSV, gotAbsPath)
	}
}

func TestNewWithArtifacts_ProductKeyDeterminesProductID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	runRoot := filepath.Join(tmpDir, "runroot")
	// Deliberately use pattern-like filename that is not equal to product key.
	productDir := planner.BuildProductDir(runRoot, "A")
	if err := os.MkdirAll(productDir, 0755); err != nil {
		t.Fatalf("failed to create productDir: %v", err)
	}

	store := artifact.NewFileSystemStore(runRoot)
	adp := &fileWritingAdapter{productDir: productDir}

	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					ProductKey: "A",
					Filename:   "Dev_A_hash_1.csv",
					Headers:    []string{"x"},
					Values:     []interface{}{1.0},
				},
			},
			{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ProductKey:       "A",
					ManifestFilename: planner.ExportManifestFilename,
					SchemaVersion:    planner.ExportManifestSchemaVersion,
					PlanHash:         "hash",
					Product: planner.ExportManifestProduct{
						ID: "A",
					},
					Inputs: planner.ExportManifestInputs{},
					Outputs: []planner.ExportManifestOutput{
						{
							Type:     "step",
							Filename: "Dev_A_hash_1.step",
						},
					},
				},
			},
		},
	}

	exec := NewWithArtifacts(adp, store, productDir)
	if err := exec.Execute(context.Background(), plan); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	artifacts, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(artifacts))
	}

	for _, a := range artifacts {
		if a.ProductID != "A" {
			t.Fatalf("expected ProductID to remain product key 'A', got %q", a.ProductID)
		}
		if a.Class != artifact.ArtifactClassExecutionOutput {
			t.Fatalf("expected execution_output class for %s, got %q", a.Filename, a.Class)
		}
	}
}
