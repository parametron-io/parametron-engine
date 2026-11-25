package semantic

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"parametron/internal/engine/cad"
)

// These tests consume committed real FreeCAD capture outputs from
// parametron-freecad #93/#97 (copied verbatim under
// testdata/projects/freecad/drawings). They prove that Engine validates and
// projects zero, single, multi-sheet, and multiple technical drawing captures
// deterministically without structural repair.

type expectedDrawing struct {
	id       string
	topology string
	sheets   []expectedSheet
}

type expectedSheet struct {
	id        string
	order     int
	nativeRef string
}

func TestRealFreeCADDrawingCapturesProjectDeterministically(t *testing.T) {
	cases := []struct {
		name     string
		project  string
		expected []expectedDrawing
	}{
		{
			name:    "zero-drawings",
			project: "rehearsal/cube",
		},
		{
			name:    "single-sheet",
			project: "drawings/single-sheet",
			expected: []expectedDrawing{
				{
					id:       "drw.manufacturing",
					topology: "embedded",
					sheets: []expectedSheet{
						{id: "sht.manufacturing.assembly", order: 1, nativeRef: "Page"},
					},
				},
			},
		},
		{
			name:    "multi-sheet",
			project: "drawings/multi-sheet",
			expected: []expectedDrawing{
				{
					id:       "drw.manufacturing",
					topology: "embedded",
					sheets: []expectedSheet{
						{id: "sht.manufacturing.assembly", order: 1, nativeRef: "PageA"},
						{id: "sht.manufacturing.detail", order: 2, nativeRef: "PageB"},
					},
				},
			},
		},
		{
			name:    "multiple-drawings",
			project: "drawings/multiple-drawings",
			expected: []expectedDrawing{
				{
					id:       "drw.inspection",
					topology: "embedded",
					sheets: []expectedSheet{
						{id: "sht.inspection.detail", order: 1, nativeRef: "PageB"},
					},
				},
				{
					id:       "drw.manufacturing",
					topology: "embedded",
					sheets: []expectedSheet{
						{id: "sht.manufacturing.assembly", order: 1, nativeRef: "PageA"},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "testdata", "projects", "freecad", tc.project, "prm.cad.json")
			contract, err := cad.Load(path)
			if err != nil {
				t.Fatalf("cad.Load returned error: %v", err)
			}

			firstModel, err := BuildFromCapture(contract)
			if err != nil {
				t.Fatalf("BuildFromCapture(first) returned error: %v", err)
			}
			if err := Validate(firstModel); err != nil {
				t.Fatalf("Validate returned error: %v", err)
			}
			secondModel, err := BuildFromCapture(contract)
			if err != nil {
				t.Fatalf("BuildFromCapture(second) returned error: %v", err)
			}

			first, err := CanonicalJSON(firstModel)
			if err != nil {
				t.Fatalf("CanonicalJSON(first) returned error: %v", err)
			}
			second, err := CanonicalJSON(secondModel)
			if err != nil {
				t.Fatalf("CanonicalJSON(second) returned error: %v", err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("repeated projection was not byte-stable\nfirst:  %s\nsecond: %s", first, second)
			}
			if len(tc.expected) == 0 && !bytes.Contains(first, []byte(`"drawings":[]`)) {
				t.Fatalf("expected zero drawings to serialize as [], got %s", first)
			}

			byID := make(map[string]Drawing, len(firstModel.Drawings))
			for _, drawing := range firstModel.Drawings {
				byID[drawing.ID] = drawing
			}
			if len(firstModel.Drawings) != len(tc.expected) {
				t.Fatalf("unexpected drawings: %#v", firstModel.Drawings)
			}
			for _, want := range tc.expected {
				got, ok := byID[want.id]
				if !ok {
					t.Fatalf("missing expected drawing %q in %#v", want.id, firstModel.Drawings)
				}
				if got.Resource.Topology != want.topology {
					t.Fatalf("drawing %q topology = %q, want %q", want.id, got.Resource.Topology, want.topology)
				}
				if !reflect.DeepEqual(idsFromSemanticSheets(got.Sheets), idsFromExpectedSheets(want.sheets)) {
					t.Fatalf("drawing %q sheets = %#v, want %#v", want.id, got.Sheets, want.sheets)
				}
				for i, wantSheet := range want.sheets {
					if got.Sheets[i].Order != wantSheet.order {
						t.Fatalf("sheet %q order = %d, want %d", wantSheet.id, got.Sheets[i].Order, wantSheet.order)
					}
					if got.Sheets[i].NativeRef != wantSheet.nativeRef {
						t.Fatalf("sheet %q native ref = %q, want %q", wantSheet.id, got.Sheets[i].NativeRef, wantSheet.nativeRef)
					}
					if got.Sheets[i].NativeType != "TechDraw::DrawPage" {
						t.Fatalf("sheet %q native type = %q", wantSheet.id, got.Sheets[i].NativeType)
					}
				}
			}

			for _, excluded := range []string{"nativeRef", "NativeRef", "PageA", "PageB"} {
				if bytes.Contains(first, []byte(excluded)) {
					t.Fatalf("internal native mapping leaked into JSON: %s", first)
				}
			}

			reordered, err := cad.Load(path)
			if err != nil {
				t.Fatalf("cad.Load(reordered) returned error: %v", err)
			}
			slices.Reverse(reordered.Entities.Drawings)
			for i := range reordered.Entities.Drawings {
				slices.Reverse(reordered.Entities.Drawings[i].ComponentIDs)
				slices.Reverse(reordered.Entities.Drawings[i].Sheets)
			}
			reorderedModel, err := BuildFromCapture(reordered)
			if err != nil {
				t.Fatalf("BuildFromCapture(reordered) returned error: %v", err)
			}
			reorderedJSON, err := CanonicalJSON(reorderedModel)
			if err != nil {
				t.Fatalf("CanonicalJSON(reordered) returned error: %v", err)
			}
			if !bytes.Equal(first, reorderedJSON) {
				t.Fatalf("projection depended on capture ordering\nfirst:     %s\nreordered: %s", first, reorderedJSON)
			}
		})
	}
}

func TestRealFreeCADDrawingCapturesHaveResolvableComponents(t *testing.T) {
	for _, project := range []string{"drawings/single-sheet", "drawings/multi-sheet", "drawings/multiple-drawings"} {
		t.Run(project, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "testdata", "projects", "freecad", project, "prm.cad.json")
			contract, err := cad.Load(path)
			if err != nil {
				t.Fatalf("cad.Load returned error: %v", err)
			}
			model, err := BuildFromCapture(contract)
			if err != nil {
				t.Fatalf("BuildFromCapture returned error: %v", err)
			}

			componentIDs := make(map[string]struct{}, len(model.Components))
			for _, component := range model.Components {
				componentIDs[component.ID] = struct{}{}
			}
			for _, drawing := range model.Drawings {
				if len(drawing.ComponentIDs) == 0 {
					t.Fatalf("drawing %q has no component IDs", drawing.ID)
				}
				for _, componentID := range drawing.ComponentIDs {
					if _, ok := componentIDs[componentID]; !ok {
						t.Fatalf("drawing %q component %q does not resolve", drawing.ID, componentID)
					}
				}
				matched := model.DrawingsForComponent(drawing.ComponentIDs[0])
				if len(matched) == 0 {
					t.Fatalf("query path did not return drawing %q", drawing.ID)
				}
			}
		})
	}
}

func TestRepositoryHasNoUnparseableDrawingCaptureFixture(t *testing.T) {
	// Guard against fixture copies that were saved with a byte-order mark or
	// trailing noise; the real captures must remain strict JSON.
	for _, project := range []string{"drawings/single-sheet", "drawings/multi-sheet", "drawings/multiple-drawings"} {
		path := filepath.Join("..", "..", "..", "testdata", "projects", "freecad", project, "prm.cad.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		if strings.HasPrefix(string(data), "\ufeff") {
			t.Fatalf("%s must not start with a byte-order mark", path)
		}
		if _, err := cad.Parse(data); err != nil {
			t.Fatalf("cad.Parse(%q) returned error: %v", path, err)
		}
	}
}

func idsFromSemanticSheets(sheets []DrawingSheet) []string {
	out := make([]string, 0, len(sheets))
	for _, sheet := range sheets {
		out = append(out, sheet.ID)
	}
	return out
}

func idsFromExpectedSheets(sheets []expectedSheet) []string {
	out := make([]string, 0, len(sheets))
	for _, sheet := range sheets {
		out = append(out, sheet.id)
	}
	return out
}
