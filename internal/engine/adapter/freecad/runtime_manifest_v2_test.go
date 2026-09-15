package freecad

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
)

// Phase 5 Task 11 attempt-local + parity permanent contract: attempt-local
// manifest materialization (ComposeFreeCADRuntimeManifest) shares the single
// projector / serializer used by the product-level path, keeps schema contract
// failures on the typed `manifest_validation` stage, and never changes the
// canonical runtime manifest filename when the schema version moves to 2.0.

func alignedV2ManifestRequest(t *testing.T, mut func(*planner.WriteExportManifestPayload)) adapter.CADRuntimeOrchestrationRequest {
	t.Helper()
	req := validFreeCADRuntimeManifestRequest(t)
	req.Manifest.ManifestProjectionMode = planner.ExportManifestProjectionModeFreeCADRuntimeNative
	req.Manifest.SchemaVersion = planner.FreeCADRuntimeMutationManifestSchemaVersion
	req.Manifest.PartMutations = &planner.ExportManifestMutationCollection{
		Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
	}
	if mut != nil {
		mut(&req.Manifest)
	}
	return req
}

// ---------------------------------------------------------------------------
// PART L — attempt-local validation
// ---------------------------------------------------------------------------

func TestTask11Runtime_AttemptLocalSchema2MutationPath(t *testing.T) {
	req := alignedV2ManifestRequest(t, func(m *planner.WriteExportManifestPayload) {
		m.AssemblyMutations = &planner.ExportManifestMutationCollection{
			Deletion: []planner.ExportManifestDeletionMutation{{Object: "SubAsm"}},
		}
	})
	got, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Manifest.SchemaVersion != "2.0" {
		t.Fatalf("schemaVersion: %q", got.Manifest.SchemaVersion)
	}
	if got.Manifest.PartMutations == nil || len(got.Manifest.PartMutations.Suppression) != 1 {
		t.Fatalf("part suppression missing: %#v", got.Manifest.PartMutations)
	}
	if got.Manifest.AssemblyMutations == nil || len(got.Manifest.AssemblyMutations.Deletion) != 1 {
		t.Fatalf("assembly deletion missing: %#v", got.Manifest.AssemblyMutations)
	}
	if !bytes.Contains(got.JSON, []byte(`"partMutations"`)) || !bytes.Contains(got.JSON, []byte(`"assemblyMutations"`)) {
		t.Fatalf("serialized sections missing: %s", got.JSON)
	}
	assertPathAbsent(t, req.ProductDir)
}

func TestTask11Runtime_AttemptLocalSchema1ValidPath(t *testing.T) {
	req := validFreeCADRuntimeManifestRequest(t)
	req.Manifest.ManifestProjectionMode = planner.ExportManifestProjectionModeFreeCADRuntimeNative
	got, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Manifest.SchemaVersion != "1.0" {
		t.Fatalf("schemaVersion: %q", got.Manifest.SchemaVersion)
	}
	if got.Manifest.AssemblyMutations != nil || got.Manifest.PartMutations != nil {
		t.Fatalf("schema-1 attempt manifest carries sections: %#v", got.Manifest)
	}
}

func TestTask11Runtime_AttemptLocalSchemaFailuresUseManifestValidationStage(t *testing.T) {
	t.Run("schema 1.0 with runtime mutation", func(t *testing.T) {
		req := alignedV2ManifestRequest(t, nil)
		req.Manifest.SchemaVersion = planner.ExportManifestSchemaVersion
		_, err := ComposeFreeCADRuntimeManifest(req)
		me := requireRuntimeManifestError(t, err, FreeCADRuntimeManifestStageManifestValidation)
		if me.Field != "Manifest.SchemaVersion" {
			t.Fatalf("field: %q", me.Field)
		}
		if !strings.Contains(err.Error(), "1.0 does not support runtime target mutations") {
			t.Fatalf("unexpected diagnostic: %v", err)
		}
		assertPathAbsent(t, req.ProductDir)
	})
	t.Run("unsupported schema version", func(t *testing.T) {
		req := alignedV2ManifestRequest(t, nil)
		req.Manifest.SchemaVersion = "3.0"
		_, err := ComposeFreeCADRuntimeManifest(req)
		requireRuntimeManifestError(t, err, FreeCADRuntimeManifestStageManifestValidation)
		if !strings.Contains(err.Error(), `"3.0" is not supported`) {
			t.Fatalf("unexpected diagnostic: %v", err)
		}
		assertPathAbsent(t, req.ProductDir)
	})
}

func TestTask11Runtime_AttemptLocalValidationFailsBeforeMaterialization(t *testing.T) {
	req := alignedV2ManifestRequest(t, nil)
	req.Manifest.SchemaVersion = "3.0"
	writeAttemptSource(t, req, []byte("source"))
	attempt, err := ComputeFreeCADRuntimeAttempt(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFreeCADRuntimeManifest(req); err == nil {
		t.Fatal("expected write to fail on schema validation")
	}
	assertPathAbsent(t, attempt.Layout.ManifestPath)
	assertLaterRuntimeFilesAbsent(t, attempt)
}

// ---------------------------------------------------------------------------
// PART M — native-only outputs=[] with schema 2.0
// ---------------------------------------------------------------------------

func TestTask11Runtime_NativeOnlyRuntimeMutationManifests(t *testing.T) {
	families := map[string]func(*planner.WriteExportManifestPayload){
		"suppression": func(m *planner.WriteExportManifestPayload) {
			m.PartMutations = &planner.ExportManifestMutationCollection{Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}}}
		},
		"visibility": func(m *planner.WriteExportManifestPayload) {
			m.PartMutations = &planner.ExportManifestMutationCollection{Visibility: []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}}}
		},
		"deletion": func(m *planner.WriteExportManifestPayload) {
			m.AssemblyMutations = &planner.ExportManifestMutationCollection{Deletion: []planner.ExportManifestDeletionMutation{{Object: "SubAsm"}}}
		},
	}
	for name, apply := range families {
		t.Run(name, func(t *testing.T) {
			req := alignedV2ManifestRequest(t, func(m *planner.WriteExportManifestPayload) {
				m.PartMutations = nil
				m.AssemblyMutations = nil
				m.Outputs = nil
				apply(m)
			})
			got, err := ComposeFreeCADRuntimeManifest(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Manifest.SchemaVersion != "2.0" {
				t.Fatalf("schemaVersion: %q", got.Manifest.SchemaVersion)
			}
			if got.Manifest.Outputs == nil || len(got.Manifest.Outputs) != 0 {
				t.Fatalf("expected non-nil empty Outputs, got %#v", got.Manifest.Outputs)
			}
			if !bytes.Contains(got.JSON, []byte(`"outputs": []`)) {
				t.Fatalf("expected outputs:[] in JSON: %s", got.JSON)
			}
			if got.Manifest.PartMutations == nil && got.Manifest.AssemblyMutations == nil {
				t.Fatalf("expected a runtime mutation section: %s", got.JSON)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PART N — product-level / attempt-local parity
// ---------------------------------------------------------------------------

func TestTask11Runtime_ProductAndAttemptLocalParity(t *testing.T) {
	req := alignedV2ManifestRequest(t, func(m *planner.WriteExportManifestPayload) {
		m.PartMutations = &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
		}
		m.AssemblyMutations = &planner.ExportManifestMutationCollection{
			Deletion: []planner.ExportManifestDeletionMutation{{Object: "SubAsm"}},
		}
	})

	attemptLocal, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatalf("attempt-local compose: %v", err)
	}

	// Product-level path over the same logical payload, with the SourceDocument
	// canonicalized to the same value the attempt layout resolved. This is the
	// only field the attempt path rewrites; everything else must be byte-identical.
	productPayload := req.Manifest
	productPayload.SourceDocument = attemptLocal.Manifest.SourceDocument
	productManifest, err := ProjectFreeCADRuntimeExportManifest(productPayload)
	if err != nil {
		t.Fatalf("product-level project: %v", err)
	}
	productJSON, err := MarshalFreeCADRuntimeExportManifestJSON(productManifest)
	if err != nil {
		t.Fatalf("product-level marshal: %v", err)
	}

	if !bytes.Equal(productJSON, attemptLocal.JSON) {
		t.Fatalf("product-level and attempt-local JSON diverge:\nproduct:\n%s\nattempt:\n%s", productJSON, attemptLocal.JSON)
	}

	// Neither path emits nested Parameters / Properties inside the mutation
	// sections.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(attemptLocal.JSON, &top); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"assemblyMutations", "partMutations"} {
		if strings.Contains(string(top[key]), `"parameters"`) || strings.Contains(string(top[key]), `"properties"`) {
			t.Fatalf("%s leaked nested metadata: %s", key, top[key])
		}
	}
}

func TestTask11Runtime_ProductAndAttemptLocalParityIsDeterministic(t *testing.T) {
	req := alignedV2ManifestRequest(t, func(m *planner.WriteExportManifestPayload) {
		m.AssemblyMutations = &planner.ExportManifestMutationCollection{
			Visibility: []planner.ExportManifestVisibilityMutation{{Object: "SubAsm", Visible: true}},
		}
	})
	first, err := ComposeFreeCADRuntimeManifest(req)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		got, err := ComposeFreeCADRuntimeManifest(cloneFreeCADRuntimeAttemptRequest(req))
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if !bytes.Equal(got.JSON, first.JSON) {
			t.Fatalf("iteration %d: attempt-local bytes drifted:\n%s\nvs\n%s", i, got.JSON, first.JSON)
		}
	}
}

// ---------------------------------------------------------------------------
// PART S — manifest filename
// ---------------------------------------------------------------------------

func TestTask11Runtime_ManifestFilenameUnchangedBySchemaVersion(t *testing.T) {
	if planner.FreeCADRuntimeExportManifestFilename != "prm.export-manifest.json" {
		t.Fatalf("canonical filename changed: %q", planner.FreeCADRuntimeExportManifestFilename)
	}
	if planner.ExportManifestFilename != planner.FreeCADRuntimeExportManifestFilename {
		t.Fatalf("active filename diverged from canonical: %q", planner.ExportManifestFilename)
	}
	if strings.Contains(planner.FreeCADRuntimeExportManifestFilename, "v2") {
		t.Fatalf("schema 2.0 must not introduce a v2 filename: %q", planner.FreeCADRuntimeExportManifestFilename)
	}
}
