package projectinput

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"parametron/internal/engine/cad"
)

// #86 boundary coverage: project-mode capture loading accepts zero, one, and
// multiple technical drawings with both resource topologies without any
// prm.project.json schema change.

func TestLoadCaptureContractSupportsDrawingVariants(t *testing.T) {
	const embeddedDrawings = `[
    {
      "id": "drw.embedded",
      "name": "embedded",
      "displayName": "",
      "componentIds": ["cmp.root"],
      "resource": {"topology": "embedded"},
      "sheets": [
        {
          "id": "sht.embedded",
          "name": "sheet",
          "displayName": "",
          "order": 1,
          "cadType": "TechDraw::DrawPage",
          "identitySource": {"kind": "native_ref", "nativeRef": "Page"}
        }
      ]
    }
  ]`
	const multipleDrawings = `[
    {
      "id": "drw.alpha",
      "name": "alpha",
      "displayName": "",
      "componentIds": ["cmp.root"],
      "resource": {"topology": "embedded"},
      "sheets": [
        {
          "id": "sht.alpha",
          "name": "sheet",
          "displayName": "",
          "order": 1,
          "cadType": "TechDraw::DrawPage",
          "identitySource": {"kind": "native_ref", "nativeRef": "Page"}
        }
      ]
    },
    {
      "id": "drw.beta",
      "name": "beta",
      "displayName": "",
      "componentIds": ["cmp.root"],
      "resource": {"topology": "separate", "path": "drawings/beta.FCStd"},
      "sheets": [
        {
          "id": "sht.beta",
          "name": "sheet",
          "displayName": "",
          "order": 1,
          "cadType": "TechDraw::DrawPage",
          "identitySource": {"kind": "native_ref", "nativeRef": "Page"}
        }
      ]
    }
  ]`

	cases := []struct {
		name     string
		drawings string
		want     []string
	}{
		{name: "zero-drawings", drawings: `[]`, want: nil},
		{name: "single-embedded-drawing", drawings: embeddedDrawings, want: []string{"embedded"}},
		{name: "multiple-embedded-and-separate-drawings", drawings: multipleDrawings, want: []string{"embedded", "separate"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectRoot := writeDrawingCaptureProject(t, tc.drawings)

			contract, err := LoadCaptureContract(&Resolved{ProjectRoot: projectRoot})
			if err != nil {
				t.Fatalf("LoadCaptureContract returned error: %v", err)
			}
			if contract == nil {
				t.Fatal("LoadCaptureContract returned nil contract")
			}

			var got []string
			for _, drawing := range contract.Entities.Drawings {
				got = append(got, drawing.Resource.Topology)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("unexpected drawings %#v, want topologies %#v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("unexpected drawing topologies %#v, want %#v", got, tc.want)
				}
			}

			first, err := cad.CanonicalJSON(contract)
			if err != nil {
				t.Fatalf("CanonicalJSON(first) returned error: %v", err)
			}
			reloaded, err := LoadCaptureContract(&Resolved{ProjectRoot: projectRoot})
			if err != nil {
				t.Fatalf("second LoadCaptureContract returned error: %v", err)
			}
			second, err := cad.CanonicalJSON(reloaded)
			if err != nil {
				t.Fatalf("CanonicalJSON(second) returned error: %v", err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("project capture load was not deterministic\nfirst:  %s\nsecond: %s", first, second)
			}
			if tc.want == nil && !bytes.Contains(first, []byte(`"drawings":[]`)) {
				t.Fatalf("expected zero drawings to serialize as [], got %s", first)
			}
		})
	}
}

func TestLoadCaptureContractMissingFileReturnsNil(t *testing.T) {
	contract, err := LoadCaptureContract(&Resolved{ProjectRoot: t.TempDir()})
	if contract != nil || err != nil {
		t.Fatalf("missing capture contract: contract=%#v err=%v", contract, err)
	}
}

func writeDrawingCaptureProject(t *testing.T, drawings string) string {
	t.Helper()

	projectRoot := t.TempDir()
	payload := `{
  "schemaVersion": "1.0",
  "captureId": "cap.project.drawings",
  "adapter": {"name": "freecad", "version": ""},
  "cadSystem": {"name": "FreeCAD", "version": ""},
  "sourceDocument": {"logicalId": "m", "path": "input/M.FCStd", "fingerprint": "sha256:x"},
  "rootProduct": {"id": "cmp.root"},
  "annotations": {"description": "", "comment": "", "purpose": ""},
  "entities": {
    "components": [
      {
        "id": "cmp.root",
        "kind": "assembly",
        "name": "Root",
        "displayName": "Root",
        "cadType": "App::Part",
        "quantity": 1,
        "material": "",
        "identitySource": {"kind": "parent_scoped_path", "path": "cmp.root"},
        "targetability": {"suppress": false, "unsuppress": false, "hide": true, "unhide": true, "delete": false},
        "annotations": {"description": "", "comment": "", "purpose": ""}
      }
    ],
    "features": [],
    "relationships": [],
    "parameterGroups": [],
    "parameters": [],
    "metadata": [],
    "drawings": ` + drawings + `
  },
  "structure": {
    "rootComponentId": "cmp.root",
    "nodes": [
      {"componentId": "cmp.root", "parentComponentId": "", "children": []}
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(projectRoot, captureContractFileName), []byte(payload), 0o644); err != nil {
		t.Fatalf("failed to write capture contract: %v", err)
	}
	return projectRoot
}
