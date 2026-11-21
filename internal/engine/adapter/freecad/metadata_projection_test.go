package freecad

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
)

// Issue #72 adapter contract: resolved native metadata mutations project into
// the canonical schema-1.0 runtime manifest as a dedicated `metadata` family
// under `partMutations` / `assemblyMutations`. Each entry carries exactly the
// native owner object selector, native property key, typed value, and value
// kind. Semantic target names, semantic metadata keys, display labels, and
// mutation policy never reach the runtime manifest.

func TestProjectFreeCADRuntimeExportManifest_MetadataEntryShapes(t *testing.T) {
	cases := []struct {
		name      string
		mutation  planner.ExportManifestMetadataMutation
		wantValue any
	}{
		{
			name:      "string",
			mutation:  planner.ExportManifestMetadataMutation{Object: "Body01", Property: "part_number", Value: "PN-1001", ValueKind: "string"},
			wantValue: "PN-1001",
		},
		{
			name:      "boolean",
			mutation:  planner.ExportManifestMetadataMutation{Object: "Body01", Property: "tagged", Value: true, ValueKind: "boolean"},
			wantValue: true,
		},
		{
			name:      "number",
			mutation:  planner.ExportManifestMetadataMutation{Object: "Assembly01", Property: "mass", Value: 12.5, ValueKind: "number"},
			wantValue: 12.5,
		},
		{
			name:      "integer",
			mutation:  planner.ExportManifestMetadataMutation{Object: "MountingBracket01", Property: "revision", Value: float64(7), ValueKind: "integer"},
			wantValue: float64(7),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
				p.PartMutations = &planner.ExportManifestMutationCollection{Metadata: []planner.ExportManifestMetadataMutation{tc.mutation}}
			})
			manifest, data := mustProjectCanonical(t, payload)

			if manifest.PartMutations == nil || len(manifest.PartMutations.Metadata) != 1 {
				t.Fatalf("metadata family not projected: %#v", manifest.PartMutations)
			}
			got := manifest.PartMutations.Metadata[0]
			if got.Object != tc.mutation.Object || got.Property != tc.mutation.Property || got.ValueKind != tc.mutation.ValueKind || got.Value != tc.wantValue {
				t.Fatalf("metadata mutation: got %#v", got)
			}

			var top map[string]json.RawMessage
			if err := json.Unmarshal(data, &top); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			var section map[string]json.RawMessage
			if err := json.Unmarshal(top["partMutations"], &section); err != nil {
				t.Fatalf("invalid section: %v", err)
			}
			entries, ok := section["metadata"]
			if !ok {
				t.Fatalf("missing metadata family: %s", top["partMutations"])
			}
			var decoded []map[string]any
			if err := json.Unmarshal(entries, &decoded); err != nil || len(decoded) != 1 {
				t.Fatalf("metadata entries: %v (%s)", err, entries)
			}
			want := map[string]any{
				"object":    tc.mutation.Object,
				"property":  tc.mutation.Property,
				"value":     tc.wantValue,
				"valueKind": tc.mutation.ValueKind,
			}
			if !reflectDeepEqual(decoded[0], want) {
				t.Fatalf("metadata entry JSON: got %#v want %#v", decoded[0], want)
			}
			for _, forbidden := range []string{"semanticId", "semanticTarget", "metadataKey", "displayName", "targetKind", "scope", "policy"} {
				if _, exists := decoded[0][forbidden]; exists {
					t.Fatalf("metadata entry leaked semantic field %q: %s", forbidden, entries)
				}
			}
		})
	}
}

func TestProjectFreeCADRuntimeExportManifest_MetadataPreservesInputOrder(t *testing.T) {
	payload := canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
		p.PartMutations = &planner.ExportManifestMutationCollection{
			Metadata: []planner.ExportManifestMetadataMutation{
				{Object: "Zulu01", Property: "part_number", Value: "Z", ValueKind: "string"},
				{Object: "Alpha01", Property: "part_number", Value: "A", ValueKind: "string"},
			},
		}
	})
	manifest, _ := mustProjectCanonical(t, payload)
	if manifest.PartMutations == nil || len(manifest.PartMutations.Metadata) != 2 {
		t.Fatalf("metadata family not projected: %#v", manifest.PartMutations)
	}
	if manifest.PartMutations.Metadata[0].Object != "Zulu01" || manifest.PartMutations.Metadata[1].Object != "Alpha01" {
		t.Fatalf("adapter must not independently reorder metadata: %#v", manifest.PartMutations.Metadata)
	}
}

func TestProjectFreeCADRuntimeExportManifest_MetadataOnlySection(t *testing.T) {
	payload := canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
		p.PartMutations = &planner.ExportManifestMutationCollection{
			Metadata: []planner.ExportManifestMetadataMutation{{Object: "Body01", Property: "part_number", Value: "PN-1001", ValueKind: "string"}},
		}
	})
	manifest, data := mustProjectCanonical(t, payload)

	if manifest.PartMutations == nil || len(manifest.PartMutations.Metadata) != 1 {
		t.Fatalf("metadata-only section not projected: %#v", manifest.PartMutations)
	}
	if manifest.AssemblyMutations != nil {
		t.Fatalf("unexpected assembly section: %#v", manifest.AssemblyMutations)
	}
	keys := topLevelKeys(t, data)
	if !hasKey(keys, "partMutations") || hasKey(keys, "assemblyMutations") {
		t.Fatalf("unexpected top-level keys: %v", keys)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	var section map[string]json.RawMessage
	if err := json.Unmarshal(top["partMutations"], &section); err != nil {
		t.Fatalf("invalid section: %v", err)
	}
	if len(section) != 1 {
		t.Fatalf("metadata-only section must carry exactly one family: %s", top["partMutations"])
	}
	if _, ok := section["metadata"]; !ok {
		t.Fatalf("missing metadata family: %s", top["partMutations"])
	}
}

func TestProjectFreeCADRuntimeExportManifest_MetadataValidation(t *testing.T) {
	validValue := planner.ExportManifestMetadataMutation{Object: "Body01", Property: "part_number", Value: "PN-1001", ValueKind: "string"}

	cases := []struct {
		name    string
		mutate  func(*planner.ExportManifestMetadataMutation)
		wantErr string
	}{
		{
			name:    "empty object",
			mutate:  func(m *planner.ExportManifestMetadataMutation) { m.Object = "" },
			wantErr: "partMutations.metadata[0].object is required for native owner object selector",
		},
		{
			name:    "empty property",
			mutate:  func(m *planner.ExportManifestMetadataMutation) { m.Property = "" },
			wantErr: "partMutations.metadata[0].property is required for native property key",
		},
		{
			name:    "object with surrounding whitespace",
			mutate:  func(m *planner.ExportManifestMetadataMutation) { m.Object = " Body01" },
			wantErr: "partMutations.metadata[0].object must be an exact native owner object selector without surrounding whitespace",
		},
		{
			name:    "property with surrounding whitespace",
			mutate:  func(m *planner.ExportManifestMetadataMutation) { m.Property = "part_number " },
			wantErr: "partMutations.metadata[0].property must be an exact native property key without surrounding whitespace",
		},
		{
			name:    "object with NUL",
			mutate:  func(m *planner.ExportManifestMetadataMutation) { m.Object = "Body\x0001" },
			wantErr: "partMutations.metadata[0].object must not contain NUL",
		},
		{
			name:    "property with NUL",
			mutate:  func(m *planner.ExportManifestMetadataMutation) { m.Property = "part\x00number" },
			wantErr: "partMutations.metadata[0].property must not contain NUL",
		},
		{
			name:    "missing valueKind",
			mutate:  func(m *planner.ExportManifestMetadataMutation) { m.ValueKind = "" },
			wantErr: `partMutations.metadata[0].valueKind "" is not supported`,
		},
		{
			name:    "unsupported valueKind",
			mutate:  func(m *planner.ExportManifestMetadataMutation) { m.ValueKind = "date" },
			wantErr: `partMutations.metadata[0].valueKind "date" is not supported`,
		},
		{
			name:    "string kind with boolean value",
			mutate:  func(m *planner.ExportManifestMetadataMutation) { m.Value = true },
			wantErr: `partMutations.metadata[0].value must be a string for valueKind "string"`,
		},
		{
			name:    "boolean kind with string value",
			mutate:  func(m *planner.ExportManifestMetadataMutation) { m.ValueKind = "boolean" },
			wantErr: `partMutations.metadata[0].value must be a boolean for valueKind "boolean"`,
		},
		{
			name:    "number kind with string value",
			mutate:  func(m *planner.ExportManifestMetadataMutation) { m.ValueKind = "number" },
			wantErr: `partMutations.metadata[0].value must be a number for valueKind "number"`,
		},
		{
			name: "integer kind with non-integral value",
			mutate: func(m *planner.ExportManifestMetadataMutation) {
				m.ValueKind = "integer"
				m.Value = 1.5
			},
			wantErr: `partMutations.metadata[0].value must be an exact integral number for valueKind "integer"`,
		},
		{
			name: "number kind with non-finite value",
			mutate: func(m *planner.ExportManifestMetadataMutation) {
				m.ValueKind = "number"
				m.Value = math.Inf(1)
			},
			wantErr: "partMutations.metadata[0].value must be finite",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutation := validValue
			if tc.mutate != nil {
				tc.mutate(&mutation)
			}
			payload := canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
				p.PartMutations = &planner.ExportManifestMutationCollection{Metadata: []planner.ExportManifestMetadataMutation{mutation}}
			})
			_, err := ProjectFreeCADRuntimeExportManifest(payload)
			if err == nil {
				t.Fatal("expected projection to fail")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}

	t.Run("duplicate native object and property", func(t *testing.T) {
		payload := canonicalMutationPayload(func(p *planner.WriteExportManifestPayload) {
			p.PartMutations = &planner.ExportManifestMutationCollection{Metadata: []planner.ExportManifestMetadataMutation{
				validValue,
				{Object: "Body01", Property: "part_number", Value: "PN-2002", ValueKind: "string"},
			}}
		})
		_, err := ProjectFreeCADRuntimeExportManifest(payload)
		if err == nil {
			t.Fatal("expected duplicate native metadata projection to fail")
		}
		want := `partMutations.metadata[1] duplicates partMutations.metadata[0] for native object "Body01" and property "part_number"`
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error containing %q, got %v", want, err)
		}
	})
}

func TestFreeCADAdapter_RejectsInvalidMetadataBeforeWrite(t *testing.T) {
	outputDir := t.TempDir()
	adp := NewFreeCADAdapter(outputDir)
	manifestPath := filepath.Join(outputDir, planner.ExportManifestFilename)

	step := planner.Step{
		Type: planner.StepWriteExportManifest,
		Payload: planner.WriteExportManifestPayload{
			ProductKey:             "widget",
			ManifestFilename:       planner.ExportManifestFilename,
			ManifestProjectionMode: planner.ExportManifestProjectionModeFreeCADRuntimeNative,
			SchemaVersion:          planner.ExportManifestSchemaVersion,
			PlanHash:               "abc123",
			Adapter:                "freecad",
			Product:                planner.ExportManifestProduct{ID: "widget"},
			SourceDocument:         "source/model.FCStd",
			Outputs:                []planner.ExportManifestOutput{},
			PartMutations: &planner.ExportManifestMutationCollection{
				Metadata: []planner.ExportManifestMetadataMutation{{Object: "Body01", Property: "part_number", Value: "PN-1001", ValueKind: "date"}},
			},
		},
	}

	err := adp.Run(context.Background(), step)
	if err == nil {
		t.Fatal("expected invalid metadata to fail before write")
	}
	if !strings.Contains(err.Error(), "project FreeCAD runtime export manifest") {
		t.Fatalf("expected projection error context, got %v", err)
	}
	if _, statErr := os.Stat(manifestPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no manifest file to be written, stat err=%v", statErr)
	}
}

// TestMetadataProjection_RealPlannerSemanticTargetDiffersFromNativeObject runs
// the real planner pipeline so the semantic target name and the native object
// selector are demonstrably different, then proves only the native selector and
// property reach the runtime manifest.
func TestMetadataProjection_RealPlannerSemanticTargetDiffersFromNativeObject(t *testing.T) {
	payload := metadataProjectionManifestPayload(t, "    target bracket:\n        metadata material = \"S235JR\"")
	manifest, err := ProjectFreeCADRuntimeExportManifest(payload)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if manifest.PartMutations == nil || len(manifest.PartMutations.Metadata) != 1 {
		t.Fatalf("metadata family not projected: %#v", manifest.PartMutations)
	}
	got := manifest.PartMutations.Metadata[0]
	want := FreeCADRuntimeMetadataMutation{Object: "Body01", Property: "material", Value: "S235JR", ValueKind: "string"}
	if !reflectDeepEqual(got, want) {
		t.Fatalf("metadata mutation: got %#v want %#v", got, want)
	}

	data, err := MarshalFreeCADRuntimeExportManifestJSON(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, forbidden := range []string{"bracket", "cmp.leg", "meta.bracket.material", "Material"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("runtime manifest leaked semantic provenance %q: %s", forbidden, data)
		}
	}
	if strings.Contains(string(data), `"parameters"`) || strings.Contains(string(data), `"properties"`) {
		t.Fatalf("runtime manifest leaked filtered nested intent: %s", data)
	}
}

func TestComposeFreeCADRuntimeManifest_ProjectsMetadata(t *testing.T) {
	req := alignedMutationManifestRequest(t, func(m *planner.WriteExportManifestPayload) {
		m.PartMutations = &planner.ExportManifestMutationCollection{
			Metadata: []planner.ExportManifestMetadataMutation{
				{Object: "Body01", Property: "part_number", Value: "PN-1001", ValueKind: "string"},
				{Object: "Body01", Property: "revision", Value: float64(7), ValueKind: "integer"},
			},
		}
	})

	materialization, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if materialization.Manifest.PartMutations == nil || len(materialization.Manifest.PartMutations.Metadata) != 2 {
		t.Fatalf("attempt-local metadata projection missing: %#v", materialization.Manifest.PartMutations)
	}
	if !strings.Contains(string(materialization.JSON), `"metadata"`) {
		t.Fatalf("attempt-local JSON missing metadata family: %s", materialization.JSON)
	}

	// Projection is deterministic: repeated composition produces identical bytes.
	again, err := ComposeFreeCADRuntimeManifest(cloneFreeCADRuntimeAttemptRequest(req))
	if err != nil {
		t.Fatalf("second compose: %v", err)
	}
	if string(again.JSON) != string(materialization.JSON) {
		t.Fatalf("metadata projection is not deterministic:\nfirst:\n%s\nsecond:\n%s", materialization.JSON, again.JSON)
	}
}

func TestComposeFreeCADRuntimeManifest_MetadataProjectionFailureIsTyped(t *testing.T) {
	req := alignedMutationManifestRequest(t, func(m *planner.WriteExportManifestPayload) {
		m.PartMutations = &planner.ExportManifestMutationCollection{
			Metadata: []planner.ExportManifestMetadataMutation{{Object: "Body01", Property: "part_number", Value: "PN-1001", ValueKind: "date"}},
		}
	})

	manifestErr := requireRuntimeManifestError(t, composeMetadataError(req), FreeCADRuntimeManifestStageManifestProjection)
	if manifestErr.Field != "partMutations.metadata[0].valueKind" {
		t.Fatalf("projection field: got %q want %q", manifestErr.Field, "partMutations.metadata[0].valueKind")
	}
	if !strings.Contains(manifestErr.Error(), `"date" is not supported`) {
		t.Fatalf("expected unsupported valueKind detail, got %v", manifestErr)
	}
	assertPathAbsent(t, req.ProductDir)
}

func composeMetadataError(req adapter.CADRuntimeOrchestrationRequest) error {
	_, err := ComposeFreeCADRuntimeManifest(req)
	return err
}

func metadataProjectionModel() *semantic.Model {
	model := mutationDeterminismModel()
	model.Components[1].Name = "bracket"
	model.Components[1].NativeRef = "Body01"
	model.Metadata = []semantic.Metadata{
		{
			ID:          "meta.bracket.material",
			OwnerKind:   semantic.OwnerKindComponent,
			OwnerID:     "cmp.leg",
			ComponentID: "cmp.leg",
			Key:         "material",
			DisplayName: "Material",
			ValueType:   "string",
			NativeType:  "App::PropertyString",
			Observable:  true,
			Writable:    true,
		},
	}
	return model
}

func metadataProjectionContract() *semanticmap.SemanticMap {
	contract := mutationDeterminismContract()
	contract.CaptureToSemantic.Metadata[0].NativeValueKinds = map[string]string{
		"App::PropertyString": "string",
	}
	return contract
}

func metadataProjectionManifestPayload(t *testing.T, body string) planner.WriteExportManifestPayload {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "adapter_metadata_projection_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp DSL: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(mutationDeterminismProduct(body))); err != nil {
		t.Fatalf("failed to write temp DSL: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp DSL: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("failed to validate DSL: %v", err)
	}

	intentModel, err := semantic.InjectDSLIntent(semantic.Clone(metadataProjectionModel()), ast)
	if err != nil {
		t.Fatalf("failed to inject DSL intent: %v", err)
	}

	plan, err := planner.CreatePlanWithTablesAndSemanticModel(ast, map[string]string{}, nil, intentModel, metadataProjectionContract())
	if err != nil {
		t.Fatalf("failed to create capture-backed plan: %v", err)
	}
	for _, step := range plan.Steps {
		if payload, ok := step.Payload.(planner.WriteExportManifestPayload); ok {
			return payload
		}
	}
	t.Fatal("no WriteExportManifestPayload step found")
	return planner.WriteExportManifestPayload{}
}

// reflectDeepEqual is a small local helper that avoids importing reflect in
// this file's hot path while still providing exact structural comparison.
func reflectDeepEqual(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}
