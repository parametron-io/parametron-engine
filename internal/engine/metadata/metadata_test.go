package metadata

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"parametron/internal/engine/artifact"
	"parametron/internal/engine/projectinput"
)

func TestWrite_CreatesMetadataJSON(t *testing.T) {
	runRoot := t.TempDir()

	m := Build(BuildInput{
		PlanHash:          "plan-hash",
		DSLHash:           "dsl-hash",
		StartedAt:         time.Unix(100, 0).UTC(),
		EndedAt:           time.Unix(101, 0).UTC(),
		WorkerCount:       2,
		MaxRetries:        2,
		StepTimeoutSecs:   30,
		ParametronVersion: "dev",
		GoVersion:         "go1.test",
	})
	if err := Write(runRoot, m); err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	path := filepath.Join(runRoot, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s to be created: %v", FileName, err)
	}
	if len(data) == 0 {
		t.Fatalf("expected %s to be non-empty", FileName)
	}

	if _, err := os.Stat(filepath.Join(runRoot, "metadata.json")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy metadata.json to not be written, stat err: %v", err)
	}
}

func TestBuild_DeterministicOrdering(t *testing.T) {
	artifacts := []artifact.Artifact{
		{
			Type:           artifact.ArtifactTypeSTEP,
			Path:           "products/widget_b/widget_b.step",
			ChecksumSHA256: "ccc",
			SizeBytes:      30,
			ProductID:      "widget_b",
		},
		{
			Type:           artifact.ArtifactTypeCSV,
			Path:           "products/widget_a/widget_a.csv",
			ChecksumSHA256: "aaa",
			SizeBytes:      10,
			ProductID:      "widget_a",
		},
		{
			Type:           artifact.ArtifactTypeSTEP,
			Path:           "products/widget_a/widget_a.step",
			ChecksumSHA256: "bbb",
			SizeBytes:      20,
			ProductID:      "widget_a",
		},
	}

	m := Build(BuildInput{
		PlanHash:          "plan-hash",
		DSLHash:           "dsl-hash",
		ProfileName:       "Prod",
		ProfileSettings:   map[string]any{"z": true, "a": "x"},
		Artifacts:         artifacts,
		StartedAt:         time.Unix(100, 0).UTC(),
		EndedAt:           time.Unix(101, 0).UTC(),
		WorkerCount:       2,
		MaxRetries:        2,
		StepTimeoutSecs:   30,
		ParametronVersion: "dev",
		GoVersion:         "go1.test",
	})

	if got := len(m.Products); got != 2 {
		t.Fatalf("expected 2 products, got %d", got)
	}
	if m.Products[0].ID != "widget_a" || m.Products[1].ID != "widget_b" {
		t.Fatalf("expected products sorted by ID, got %+v", m.Products)
	}
	if got := len(m.Products[0].Artifacts); got != 2 {
		t.Fatalf("expected 2 artifacts for widget_a, got %d", got)
	}
	if m.Products[0].Artifacts[0].Path != "products/widget_a/widget_a.csv" {
		t.Fatalf("expected artifact paths sorted by path, got %+v", m.Products[0].Artifacts)
	}

	first, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal metadata (first): %v", err)
	}
	second, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal metadata (second): %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("metadata JSON must be deterministic across repeated marshaling")
	}

	jsonStr := string(first)
	aPos := strings.Index(jsonStr, `"a":"x"`)
	zPos := strings.Index(jsonStr, `"z":true`)
	if aPos == -1 || zPos == -1 || aPos > zPos {
		t.Fatalf("expected profile settings to be sorted by key in JSON, got: %s", jsonStr)
	}
}

func TestBuild_ProfilePresenceAndOmission(t *testing.T) {
	withProfile := Build(BuildInput{
		PlanHash:          "plan-hash",
		DSLHash:           "dsl-hash",
		ProfileName:       "Debug",
		ProfileSettings:   map[string]any{"enabled": true},
		StartedAt:         time.Unix(100, 0).UTC(),
		EndedAt:           time.Unix(101, 0).UTC(),
		WorkerCount:       1,
		MaxRetries:        2,
		StepTimeoutSecs:   30,
		ParametronVersion: "dev",
		GoVersion:         "go1.test",
	})
	withJSON, err := json.Marshal(withProfile)
	if err != nil {
		t.Fatalf("failed to marshal metadata with profile: %v", err)
	}
	if !strings.Contains(string(withJSON), `"profile"`) {
		t.Fatalf("expected profile field when profile selected: %s", string(withJSON))
	}

	withoutProfile := Build(BuildInput{
		PlanHash:          "plan-hash",
		DSLHash:           "dsl-hash",
		StartedAt:         time.Unix(100, 0).UTC(),
		EndedAt:           time.Unix(101, 0).UTC(),
		WorkerCount:       1,
		MaxRetries:        2,
		StepTimeoutSecs:   30,
		ParametronVersion: "dev",
		GoVersion:         "go1.test",
	})
	withoutJSON, err := json.Marshal(withoutProfile)
	if err != nil {
		t.Fatalf("failed to marshal metadata without profile: %v", err)
	}
	if strings.Contains(string(withoutJSON), `"profile"`) {
		t.Fatalf("expected profile field to be omitted when unused: %s", string(withoutJSON))
	}
}

func TestBuild_TableSectionOmittedWhenNoTablesProvided(t *testing.T) {
	m := Build(BuildInput{
		PlanHash:          "plan-hash",
		DSLHash:           "dsl-hash",
		StartedAt:         time.Unix(100, 0).UTC(),
		EndedAt:           time.Unix(101, 0).UTC(),
		WorkerCount:       1,
		MaxRetries:        2,
		StepTimeoutSecs:   30,
		ParametronVersion: "dev",
		GoVersion:         "go1.test",
	})

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal metadata: %v", err)
	}
	if strings.Contains(string(data), `"tables"`) {
		t.Fatalf("expected tables field to be omitted, got: %s", string(data))
	}
}

func TestBuild_TableSectionDeterministicAndPreserved(t *testing.T) {
	tables := []TableInputMetadata{
		{LogicalID: "labels", Name: "label_catalog", Fingerprint: "bbb"},
		{LogicalID: "fasteners", Name: "fastener_catalog", Fingerprint: "aaa"},
	}

	m := Build(BuildInput{
		PlanHash:          "plan-hash",
		DSLHash:           "dsl-hash",
		Tables:            tables,
		StartedAt:         time.Unix(100, 0).UTC(),
		EndedAt:           time.Unix(101, 0).UTC(),
		WorkerCount:       1,
		MaxRetries:        2,
		StepTimeoutSecs:   30,
		ParametronVersion: "dev",
		GoVersion:         "go1.test",
	})

	if len(m.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(m.Tables))
	}
	if m.Tables[0].LogicalID != "fasteners" || m.Tables[0].Name != "fastener_catalog" || m.Tables[0].Fingerprint != "aaa" {
		t.Fatalf("unexpected first table metadata: %+v", m.Tables[0])
	}
	if m.Tables[1].LogicalID != "labels" || m.Tables[1].Name != "label_catalog" || m.Tables[1].Fingerprint != "bbb" {
		t.Fatalf("unexpected second table metadata: %+v", m.Tables[1])
	}

	first, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal metadata (first): %v", err)
	}
	second, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal metadata (second): %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("metadata JSON must be deterministic across repeated marshaling")
	}

	jsonStr := string(first)
	if !strings.Contains(jsonStr, `"logicalId":"labels"`) || !strings.Contains(jsonStr, `"name":"label_catalog"`) || !strings.Contains(jsonStr, `"fingerprint":"bbb"`) {
		t.Fatalf("expected labels table metadata to be preserved, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"logicalId":"fasteners"`) || !strings.Contains(jsonStr, `"name":"fastener_catalog"`) || !strings.Contains(jsonStr, `"fingerprint":"aaa"`) {
		t.Fatalf("expected fasteners table metadata to be preserved, got: %s", jsonStr)
	}
}

func TestBuild_ProjectInputsDeterministicAndPreserved(t *testing.T) {
	inputs := &projectinput.CapturedResources{
		DSL: projectinput.CapturedResource{
			ResolvedPath: "/tmp/project.dsl",
			Signature:    "dsl-hash",
			Kind:         projectinput.ResourceKindDSL,
		},
		Models: []projectinput.CapturedResource{
			{
				LogicalID:    "z_model",
				ResolvedPath: "/tmp/z.FCStd",
				Signature:    "hash-z",
				Kind:         projectinput.ResourceKindModel,
			},
			{
				LogicalID:    "a_model",
				ResolvedPath: "/tmp/a.FCStd",
				Signature:    "hash-a",
				Kind:         projectinput.ResourceKindModel,
			},
		},
		Tables: []projectinput.CapturedResource{
			{
				LogicalID:    "labels",
				ResolvedPath: "/tmp/labels.json",
				Signature:    "fp-labels",
				Kind:         projectinput.ResourceKindTable,
			},
		},
	}

	m := Build(BuildInput{
		PlanHash:          "plan-hash",
		DSLHash:           "dsl-hash",
		ProjectInputs:     inputs,
		StartedAt:         time.Unix(100, 0).UTC(),
		EndedAt:           time.Unix(101, 0).UTC(),
		WorkerCount:       1,
		MaxRetries:        2,
		StepTimeoutSecs:   30,
		ParametronVersion: "dev",
		GoVersion:         "go1.test",
	})

	if m.ProjectInputs == nil {
		t.Fatal("expected project inputs to be preserved")
	}
	if got, want := m.ProjectInputs.Models[0].LogicalID, "a_model"; got != want {
		t.Fatalf("expected sorted project model ordering, got %q want %q", got, want)
	}

	first, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal metadata (first): %v", err)
	}
	second, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal metadata (second): %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("metadata JSON must be deterministic across repeated marshaling")
	}
	if !strings.Contains(string(first), `"projectInputs"`) {
		t.Fatalf("expected projectInputs field, got: %s", string(first))
	}
}

func TestBuild_ArtifactsMatchManifest(t *testing.T) {
	runRoot := t.TempDir()
	productDirA := filepath.Join(runRoot, "products", "widget_a")
	productDirB := filepath.Join(runRoot, "products", "widget_b")
	if err := os.MkdirAll(productDirA, 0755); err != nil {
		t.Fatalf("failed to create product dir A: %v", err)
	}
	if err := os.MkdirAll(productDirB, 0755); err != nil {
		t.Fatalf("failed to create product dir B: %v", err)
	}

	csvA := filepath.Join(productDirA, "widget_a.csv")
	stepA := filepath.Join(productDirA, "widget_a.step")
	stepB := filepath.Join(productDirB, "widget_b.step")
	if err := os.WriteFile(csvA, []byte("a"), 0644); err != nil {
		t.Fatalf("failed to write csvA: %v", err)
	}
	if err := os.WriteFile(stepA, []byte("b"), 0644); err != nil {
		t.Fatalf("failed to write stepA: %v", err)
	}
	if err := os.WriteFile(stepB, []byte("c"), 0644); err != nil {
		t.Fatalf("failed to write stepB: %v", err)
	}

	store := artifact.NewFileSystemStore(runRoot)
	_, err := store.RecordExisting(context.Background(), stepB, artifact.Artifact{
		Type:      artifact.ArtifactTypeSTEP,
		ProductID: "widget_b",
		StepID:    "1",
		Filename:  "widget_b.step",
	})
	if err != nil {
		t.Fatalf("failed to record stepB: %v", err)
	}
	_, err = store.RecordExisting(context.Background(), csvA, artifact.Artifact{
		Type:      artifact.ArtifactTypeCSV,
		ProductID: "widget_a",
		StepID:    "0",
		Filename:  "widget_a.csv",
	})
	if err != nil {
		t.Fatalf("failed to record csvA: %v", err)
	}
	_, err = store.RecordExisting(context.Background(), stepA, artifact.Artifact{
		Type:      artifact.ArtifactTypeSTEP,
		ProductID: "widget_a",
		StepID:    "1",
		Filename:  "widget_a.step",
	})
	if err != nil {
		t.Fatalf("failed to record stepA: %v", err)
	}

	if err := store.WriteManifest(); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(runRoot, "manifest.json"))
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}
	var manifest artifact.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("failed to list artifacts: %v", err)
	}
	m := Build(BuildInput{
		PlanHash:          "plan-hash",
		DSLHash:           "dsl-hash",
		Artifacts:         list,
		StartedAt:         time.Unix(100, 0).UTC(),
		EndedAt:           time.Unix(101, 0).UTC(),
		WorkerCount:       2,
		MaxRetries:        2,
		StepTimeoutSecs:   30,
		ParametronVersion: "dev",
		GoVersion:         "go1.test",
	})

	flat := make(map[string]ArtifactSummary)
	for _, product := range m.Products {
		for _, item := range product.Artifacts {
			flat[item.Path] = item
		}
	}
	if len(flat) != len(manifest.Artifacts) {
		t.Fatalf("metadata artifact count mismatch: got %d want %d", len(flat), len(manifest.Artifacts))
	}
	for _, a := range manifest.Artifacts {
		got, ok := flat[a.Path]
		if !ok {
			t.Fatalf("artifact path missing in metadata: %s", a.Path)
		}
		if got.Type != string(a.Type) || got.ChecksumSHA256 != a.ChecksumSHA256 || got.SizeBytes != a.SizeBytes {
			t.Fatalf("artifact mismatch for path %s: got %+v want type=%s checksum=%s size=%d", a.Path, got, a.Type, a.ChecksumSHA256, a.SizeBytes)
		}
	}
}
