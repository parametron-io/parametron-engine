package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/engine/cad"
)

// #86 boundary coverage: CLI validate --project accepts project capture
// contracts with zero, one, and multiple technical drawings across both
// resource topologies without a prm.project.json schema change.

func TestValidateProjectAcceptsDrawingVariants(t *testing.T) {
	sourceProject := filepath.Join(findRepoRoot(t), "testdata", "projects", "freecad", "rehearsal", "cube")

	cases := []struct {
		name     string
		drawings []cad.Drawing
	}{
		{name: "zero-drawings"},
		{
			name:     "single-embedded-drawing",
			drawings: []cad.Drawing{cliBoundaryDrawing("drw.embedded", "embedded", []string{"cmp.root"}, cad.DrawingResource{Topology: "embedded"}, "sht.embedded")},
		},
		{
			name: "multiple-embedded-and-separate-drawings",
			drawings: []cad.Drawing{
				cliBoundaryDrawing("drw.embedded", "embedded", []string{"cmp.root"}, cad.DrawingResource{Topology: "embedded"}, "sht.embedded"),
				cliBoundaryDrawing("drw.separate", "separate", []string{"cmp.body"}, cad.DrawingResource{Topology: "separate", Path: "drawings/boundary.FCStd"}, "sht.separate"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := copyFixtureTree(t, sourceProject)
			if tc.drawings != nil {
				writeProjectDrawings(t, projectDir, tc.drawings)
			}

			for run := 0; run < 2; run++ {
				stdout, err := executeHarnessCommand(t, []string{"validate", "--project", projectDir})
				if err != nil {
					t.Fatalf("validate --project failed on run %d: %v", run, err)
				}
				if !strings.Contains(stdout, "Validation succeeded") {
					t.Fatalf("unexpected validate output on run %d: %q", run, stdout)
				}
			}
		})
	}
}

func TestValidateProjectRejectsUnresolvedDrawingComponent(t *testing.T) {
	sourceProject := filepath.Join(findRepoRoot(t), "testdata", "projects", "freecad", "rehearsal", "cube")
	projectDir := copyFixtureTree(t, sourceProject)
	writeProjectDrawings(t, projectDir, []cad.Drawing{
		cliBoundaryDrawing("drw.missing", "missing", []string{"cmp.missing"}, cad.DrawingResource{Topology: "embedded"}, "sht.missing"),
	})

	_, err := executeHarnessCommand(t, []string{"validate", "--project", projectDir})
	if err == nil {
		t.Fatal("expected validate --project to reject an unresolved drawing component")
	}
	if !strings.Contains(err.Error(), "must resolve to an existing component") {
		t.Fatalf("expected unresolved component diagnostic, got %v", err)
	}
}

func cliBoundaryDrawing(id, name string, componentIDs []string, resource cad.DrawingResource, sheetID string) cad.Drawing {
	return cad.Drawing{
		ID:           id,
		Name:         name,
		DisplayName:  "",
		ComponentIDs: componentIDs,
		Resource:     resource,
		Sheets: []cad.DrawingSheet{{
			ID:          sheetID,
			Name:        "sheet",
			DisplayName: "",
			Order:       1,
			CADType:     "TechDraw::DrawPage",
			IdentitySource: cad.IdentitySource{
				Kind:      "native_ref",
				NativeRef: "Page",
			},
		}},
	}
}

func writeProjectDrawings(t *testing.T, projectDir string, drawings []cad.Drawing) {
	t.Helper()

	path := filepath.Join(projectDir, "prm.cad.json")
	contract, err := cad.Load(path)
	if err != nil {
		t.Fatalf("cad.Load returned error: %v", err)
	}
	contract.Entities.Drawings = drawings
	data, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent returned error: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write capture contract: %v", err)
	}
}
