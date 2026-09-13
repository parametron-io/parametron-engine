package planner

import (
	"testing"
)

// TestExportManifestFilenameConstantsUseCanonicalName locks the canonical
// runtime-facing filename and its active Engine alias.
func TestExportManifestFilenameConstantsUseCanonicalName(t *testing.T) {
	if FreeCADRuntimeExportManifestFilename != "export_manifest_v1.json" {
		t.Errorf("FreeCADRuntimeExportManifestFilename = %q, want %q", FreeCADRuntimeExportManifestFilename, "export_manifest_v1.json")
	}
	if ExportManifestFilename != FreeCADRuntimeExportManifestFilename {
		t.Errorf("ExportManifestFilename = %q, want FreeCADRuntimeExportManifestFilename %q", ExportManifestFilename, FreeCADRuntimeExportManifestFilename)
	}
}

// TestCreatePlan_WriteExportManifestPayloadFilenameUsesCurrentAlias proves that
// the planner sets WriteExportManifestPayload.ManifestFilename to ExportManifestFilename,
// which now resolves to the canonical underscore-form "export_manifest_v1.json".
func TestCreatePlan_WriteExportManifestPayloadFilenameUsesCurrentAlias(t *testing.T) {
	dslContent := `
product Widget {
    param width: number = 100
}
`
	_, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) < 2 {
		t.Fatalf("expected at least 2 steps, got %d", len(plan.Steps))
	}

	manifestPayload, ok := plan.Steps[1].Payload.(WriteExportManifestPayload)
	if !ok {
		t.Fatalf("expected WriteExportManifestPayload at step 1, got %T", plan.Steps[1].Payload)
	}

	if manifestPayload.ManifestFilename != ExportManifestFilename {
		t.Errorf("ManifestFilename = %q, want ExportManifestFilename %q", manifestPayload.ManifestFilename, ExportManifestFilename)
	}
	if manifestPayload.ManifestFilename != "export_manifest_v1.json" {
		t.Errorf("ManifestFilename = %q, want canonical underscore-form %q", manifestPayload.ManifestFilename, "export_manifest_v1.json")
	}
}

// TestCreatePlan_RunCADRuntimeManifestFieldUsesCurrentAlias proves that the RunCADRuntime
// step references ExportManifestFilename, which now resolves to the canonical underscore-form.
func TestCreatePlan_RunCADRuntimeManifestFieldUsesCurrentAlias(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box.FCStd"

    param label: string = "box"
}
`
	_, plan := parseValidateAndPlan(t, dslContent)
	if len(plan.Steps) != 3 {
		t.Fatalf("expected 3 steps for freecad adapter, got %d", len(plan.Steps))
	}

	runPayload, ok := plan.Steps[2].Payload.(RunCADRuntimePayload)
	if !ok {
		t.Fatalf("expected RunCADRuntimePayload at step 2, got %T", plan.Steps[2].Payload)
	}

	if runPayload.ManifestFilename != ExportManifestFilename {
		t.Errorf("RunCADRuntimePayload.ManifestFilename = %q, want ExportManifestFilename %q", runPayload.ManifestFilename, ExportManifestFilename)
	}
	if runPayload.ManifestFilename != "export_manifest_v1.json" {
		t.Errorf("RunCADRuntimePayload.ManifestFilename = %q, want canonical underscore-form %q", runPayload.ManifestFilename, "export_manifest_v1.json")
	}
}
