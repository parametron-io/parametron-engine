package semantic

import (
	"bytes"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"parametron/internal/engine/cad"
)

// These tests pin the v1.0 semantic drawing projection from the #83 audit:
// capture drawings and ordered sheets project into semantic.Model, retain
// native selectors internally, and expose a deterministic component ->
// drawings -> ordered sheets -> native binding query path.

func drawingsCapture(t *testing.T) *cad.CADContract {
	t.Helper()
	return loadCaptureFixture(t, "smoke", "with-drawings")
}

func buildDrawingsModel(t *testing.T) *Model {
	t.Helper()
	model, err := BuildFromCapture(drawingsCapture(t))
	if err != nil {
		t.Fatalf("BuildFromCapture returned error: %v", err)
	}
	return model
}

func TestBuildFromCaptureProjectsDrawings(t *testing.T) {
	model := buildDrawingsModel(t)

	if err := Validate(model); err != nil {
		t.Fatalf("Validate(model) returned error: %v", err)
	}
	if got := idsFromDrawings(model.Drawings); !reflect.DeepEqual(got, []string{"drw.inspection", "drw.manufacturing"}) {
		t.Fatalf("unexpected drawings: %#v", got)
	}

	byID := drawingsByID(model.Drawings)
	manufacturing := byID["drw.manufacturing"]
	if manufacturing.NativeRef != "Drawing" {
		t.Fatalf("unexpected drawing native ref: %q", manufacturing.NativeRef)
	}
	if manufacturing.Name != "manufacturing" || manufacturing.DisplayName != "Manufacturing Drawing" {
		t.Fatalf("unexpected drawing identity: %#v", manufacturing)
	}
	if !reflect.DeepEqual(manufacturing.ComponentIDs, []string{"cmp.bracket", "cmp.root"}) {
		t.Fatalf("unexpected component IDs: %#v", manufacturing.ComponentIDs)
	}
	if manufacturing.Resource.Topology != "embedded" || manufacturing.Resource.Path != "" {
		t.Fatalf("unexpected embedded resource: %#v", manufacturing.Resource)
	}
	if len(manufacturing.Sheets) != 2 {
		t.Fatalf("unexpected sheets: %#v", manufacturing.Sheets)
	}
	first, second := manufacturing.Sheets[0], manufacturing.Sheets[1]
	if first.ID != "sht.manufacturing.assembly" || first.Order != 1 || first.NativeRef != "Page" || first.NativeType != "TechDraw::DrawPage" {
		t.Fatalf("unexpected first sheet: %#v", first)
	}
	if second.ID != "sht.manufacturing.detail" || second.Order != 2 || second.NativeRef != "Page001" || second.NativeType != "TechDraw::DrawPage" {
		t.Fatalf("unexpected second sheet: %#v", second)
	}

	inspection := byID["drw.inspection"]
	if inspection.NativeRef != "" {
		t.Fatalf("expected no drawing-level native ref, got %q", inspection.NativeRef)
	}
	if inspection.Resource.Topology != "separate" || inspection.Resource.Path != "drawings/inspection.FCStd" {
		t.Fatalf("unexpected separate resource: %#v", inspection.Resource)
	}
	if len(inspection.Sheets) != 1 || inspection.Sheets[0].NativeRef != "Page" {
		t.Fatalf("unexpected inspection sheets: %#v", inspection.Sheets)
	}
}

func TestBuildFromCaptureZeroDrawings(t *testing.T) {
	contract := loadCaptureFixture(t, "smoke", "minimal-valid")
	model, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture returned error: %v", err)
	}
	if model.Drawings == nil {
		t.Fatal("expected zero drawings to remain a non-nil empty slice")
	}
	if len(model.Drawings) != 0 {
		t.Fatalf("expected zero drawings, got %d", len(model.Drawings))
	}
	data, err := CanonicalJSON(model)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}
	if !bytes.Contains(data, []byte(`"drawings":[]`)) {
		t.Fatalf("expected canonical JSON to emit \"drawings\":[], got %s", data)
	}
	if got := model.CanonicalDrawings(); len(got) != 0 {
		t.Fatalf("expected empty canonical drawings, got %#v", got)
	}
	if got := model.DrawingsForComponent("cmp.root"); len(got) != 0 {
		t.Fatalf("expected no drawings for component, got %#v", got)
	}
}

func TestDrawingsNativeRefRetentionAndJSONExclusion(t *testing.T) {
	model := buildDrawingsModel(t)
	canonical, err := Canonicalize(model)
	if err != nil {
		t.Fatalf("Canonicalize returned error: %v", err)
	}

	for _, candidate := range []*Model{model, Clone(model), canonical} {
		byID := drawingsByID(candidate.Drawings)
		manufacturing := byID["drw.manufacturing"]
		if manufacturing.NativeRef != "Drawing" {
			t.Fatalf("drawing native ref was not retained: %#v", manufacturing)
		}
		if len(manufacturing.Sheets) != 2 ||
			manufacturing.Sheets[0].NativeRef != "Page" ||
			manufacturing.Sheets[1].NativeRef != "Page001" {
			t.Fatalf("sheet native refs were not retained: %#v", manufacturing.Sheets)
		}
		if byID["drw.inspection"].NativeRef != "" {
			t.Fatalf("absent drawing native ref acquired a value: %#v", byID["drw.inspection"])
		}

		data, err := CanonicalJSON(candidate)
		if err != nil {
			t.Fatalf("CanonicalJSON returned error: %v", err)
		}
		for _, excluded := range []string{"nativeRef", "NativeRef", "Page001"} {
			if bytes.Contains(data, []byte(excluded)) {
				t.Fatalf("internal native mapping leaked into JSON: %s", data)
			}
		}
	}
}

func TestDrawingsNativeRefHasNoFallback(t *testing.T) {
	contract := drawingsCapture(t)
	for i := range contract.Entities.Drawings {
		if contract.Entities.Drawings[i].ID == "drw.manufacturing" {
			contract.Entities.Drawings[i].IdentitySource = nil
		}
	}

	model, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture returned error: %v", err)
	}
	manufacturing := drawingsByID(model.Drawings)["drw.manufacturing"]
	if manufacturing.NativeRef != "" {
		t.Fatalf("absent drawing native ref acquired a fallback: %q", manufacturing.NativeRef)
	}
	for _, fallback := range []string{manufacturing.ID, manufacturing.Name, manufacturing.DisplayName} {
		if manufacturing.NativeRef == fallback && fallback != "" {
			t.Fatalf("drawing native ref fell back to %q", fallback)
		}
	}
}

func TestDrawingsProjectionIsDefensivelyCopied(t *testing.T) {
	contract := drawingsCapture(t)
	model, err := BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("BuildFromCapture returned error: %v", err)
	}
	before, err := CanonicalJSON(model)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}

	for i := range contract.Entities.Drawings {
		contract.Entities.Drawings[i].Name = "changed"
		contract.Entities.Drawings[i].ComponentIDs = append(contract.Entities.Drawings[i].ComponentIDs, "cmp.changed")
		for j := range contract.Entities.Drawings[i].Sheets {
			contract.Entities.Drawings[i].Sheets[j].IdentitySource.NativeRef = "Changed"
		}
	}

	after, err := CanonicalJSON(model)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("capture mutation leaked into built model\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestDrawingsForComponentQueryReadiness(t *testing.T) {
	model := buildDrawingsModel(t)

	bracketDrawings := model.DrawingsForComponent("cmp.bracket")
	if got := idsFromDrawings(bracketDrawings); !reflect.DeepEqual(got, []string{"drw.inspection", "drw.manufacturing"}) {
		t.Fatalf("unexpected drawings for component: %#v", got)
	}
	manufacturing := drawingsByID(bracketDrawings)["drw.manufacturing"]
	if len(manufacturing.Sheets) != 2 || manufacturing.Sheets[0].NativeRef != "Page" || manufacturing.Sheets[1].NativeRef != "Page001" {
		t.Fatalf("unexpected ordered sheets/native bindings: %#v", manufacturing.Sheets)
	}

	if got := idsFromDrawings(model.DrawingsForComponent("cmp.root")); !reflect.DeepEqual(got, []string{"drw.manufacturing"}) {
		t.Fatalf("unexpected root drawings: %#v", got)
	}
	if got := model.DrawingsForComponent("cmp.missing"); len(got) != 0 {
		t.Fatalf("expected no drawings for unknown component, got %#v", got)
	}

	// The helper must canonicalize regardless of model input order and return
	// defensive copies.
	model.Drawings = []Drawing{model.Drawings[1], model.Drawings[0]}
	for i := range model.Drawings {
		slices.Reverse(model.Drawings[i].ComponentIDs)
		slices.Reverse(model.Drawings[i].Sheets)
	}
	got := model.DrawingsForComponent("cmp.bracket")
	if ids := idsFromDrawings(got); !reflect.DeepEqual(ids, []string{"drw.inspection", "drw.manufacturing"}) {
		t.Fatalf("expected query helper to canonicalize drawing order, got %#v", ids)
	}
	got[0].ID = "drw.mutated"
	got[0].ComponentIDs = append(got[0].ComponentIDs, "cmp.mutated")
	got[0].Sheets[0].NativeRef = "mutated"
	if ids := idsFromDrawings(model.DrawingsForComponent("cmp.bracket")); !reflect.DeepEqual(ids, []string{"drw.inspection", "drw.manufacturing"}) {
		t.Fatalf("query helper did not return a defensive copy: %#v", ids)
	}
}

func TestDrawingsCanonicalOrderingAndInputOrderIndependence(t *testing.T) {
	left := drawingsCapture(t)
	right := drawingsCapture(t)
	slices.Reverse(right.Entities.Drawings)
	for i := range right.Entities.Drawings {
		slices.Reverse(right.Entities.Drawings[i].ComponentIDs)
		slices.Reverse(right.Entities.Drawings[i].Sheets)
	}

	leftModel, err := BuildFromCapture(left)
	if err != nil {
		t.Fatalf("BuildFromCapture(left) returned error: %v", err)
	}
	rightModel, err := BuildFromCapture(right)
	if err != nil {
		t.Fatalf("BuildFromCapture(right) returned error: %v", err)
	}

	first, err := CanonicalJSON(leftModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	repeated, err := CanonicalJSON(leftModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(repeated) returned error: %v", err)
	}
	reordered, err := CanonicalJSON(rightModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(reordered) returned error: %v", err)
	}
	if !bytes.Equal(first, repeated) {
		t.Fatalf("repeated canonical JSON was not byte-stable\nfirst:    %s\nrepeated: %s", first, repeated)
	}
	if !bytes.Equal(first, reordered) {
		t.Fatalf("canonical JSON depended on capture ordering\nfirst:     %s\nreordered: %s", first, reordered)
	}

	// Canonicalize must sort a directly-mutated model's drawings, componentIds,
	// and sheets.
	mutated := Clone(leftModel)
	slices.Reverse(mutated.Drawings)
	for i := range mutated.Drawings {
		slices.Reverse(mutated.Drawings[i].ComponentIDs)
		slices.Reverse(mutated.Drawings[i].Sheets)
	}
	canonical, err := Canonicalize(mutated)
	if err != nil {
		t.Fatalf("Canonicalize returned error: %v", err)
	}
	if got := idsFromDrawings(canonical.Drawings); !reflect.DeepEqual(got, []string{"drw.inspection", "drw.manufacturing"}) {
		t.Fatalf("unexpected canonical drawing order: %#v", got)
	}
	manufacturing := drawingsByID(canonical.Drawings)["drw.manufacturing"]
	if !reflect.DeepEqual(manufacturing.ComponentIDs, []string{"cmp.bracket", "cmp.root"}) {
		t.Fatalf("unexpected canonical componentIds: %#v", manufacturing.ComponentIDs)
	}
	if len(manufacturing.Sheets) != 2 || manufacturing.Sheets[0].Order != 1 || manufacturing.Sheets[1].Order != 2 {
		t.Fatalf("unexpected canonical sheet order: %#v", manufacturing.Sheets)
	}
}

func TestDrawingsAddRemoveDeterminism(t *testing.T) {
	baseModel := buildDrawingsModel(t)
	base, err := CanonicalJSON(baseModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(base) returned error: %v", err)
	}

	withoutInspection := drawingsCapture(t)
	withoutInspection.Entities.Drawings = slices.DeleteFunc(withoutInspection.Entities.Drawings, func(drawing cad.Drawing) bool {
		return drawing.ID == "drw.inspection"
	})
	first, err := BuildFromCapture(withoutInspection)
	if err != nil {
		t.Fatalf("BuildFromCapture(without inspection) returned error: %v", err)
	}
	second, err := BuildFromCapture(withoutInspection)
	if err != nil {
		t.Fatalf("BuildFromCapture(without inspection) returned error: %v", err)
	}
	firstJSON, err := CanonicalJSON(first)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) returned error: %v", err)
	}
	secondJSON, err := CanonicalJSON(second)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) returned error: %v", err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("removing a drawing was not deterministic\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
	if bytes.Equal(firstJSON, base) {
		t.Fatal("expected removing a drawing to change canonical JSON")
	}

	zero := drawingsCapture(t)
	zero.Entities.Drawings = []cad.Drawing{}
	zeroModel, err := BuildFromCapture(zero)
	if err != nil {
		t.Fatalf("BuildFromCapture(zero) returned error: %v", err)
	}
	zeroJSON, err := CanonicalJSON(zeroModel)
	if err != nil {
		t.Fatalf("CanonicalJSON(zero) returned error: %v", err)
	}
	if !bytes.Contains(zeroJSON, []byte(`"drawings":[]`)) {
		t.Fatalf("expected zero drawings to serialize as [], got %s", zeroJSON)
	}

	restored, err := BuildFromCapture(drawingsCapture(t))
	if err != nil {
		t.Fatalf("BuildFromCapture(restored) returned error: %v", err)
	}
	restoredJSON, err := CanonicalJSON(restored)
	if err != nil {
		t.Fatalf("CanonicalJSON(restored) returned error: %v", err)
	}
	if !bytes.Equal(restoredJSON, base) {
		t.Fatalf("re-adding the drawing did not restore canonical JSON\nbase:     %s\nrestored: %s", base, restoredJSON)
	}
}

func TestDrawingsValidationFailures(t *testing.T) {
	cases := []struct {
		name  string
		build func(*Model)
		want  string
	}{
		{
			name:  "blank-drawing-id",
			build: func(model *Model) { model.Drawings[0].ID = "" },
			want:  "drawings[0].id is required",
		},
		{
			name:  "blank-drawing-name",
			build: func(model *Model) { model.Drawings[0].Name = " " },
			want:  "drawings[0].name is required",
		},
		{
			name:  "duplicate-drawing-id",
			build: func(model *Model) { model.Drawings[1].ID = model.Drawings[0].ID },
			want:  `drawings[1].id duplicates stable ID "drw.inspection" already declared at drawings[0].id`,
		},
		{
			name:  "duplicate-drawing-name",
			build: func(model *Model) { model.Drawings[1].Name = model.Drawings[0].Name },
			want:  `drawings[1].name "inspection" duplicates drawing name`,
		},
		{
			name:  "sheet-id-collides-with-component",
			build: func(model *Model) { model.Drawings[0].Sheets[0].ID = "cmp.root" },
			want:  `drawings[0].sheets[0].id duplicates stable ID "cmp.root" already declared at components[`,
		},
		{
			name:  "duplicate-sheet-id-across-drawings",
			build: func(model *Model) { model.Drawings[1].Sheets[0].ID = model.Drawings[0].Sheets[0].ID },
			want:  `drawings[1].sheets[0].id duplicates stable ID "sht.inspection.sheetOne" already declared at drawings[0].sheets[0].id`,
		},
		{
			name:  "blank-sheet-id",
			build: func(model *Model) { model.Drawings[0].Sheets[0].ID = "" },
			want:  "drawings[0].sheets[0].id is required",
		},
		{
			name:  "blank-sheet-name",
			build: func(model *Model) { model.Drawings[0].Sheets[0].Name = "" },
			want:  "drawings[0].sheets[0].name is required",
		},
		{
			name:  "blank-sheet-native-type",
			build: func(model *Model) { model.Drawings[0].Sheets[0].NativeType = "" },
			want:  "drawings[0].sheets[0].nativeType is required",
		},
		{
			name:  "zero-sheet-order",
			build: func(model *Model) { model.Drawings[0].Sheets[0].Order = 0 },
			want:  "drawings[0].sheets[0].order must be a positive integer",
		},
		{
			name: "duplicate-sheet-name-within-drawing",
			build: func(model *Model) {
				model.Drawings[1].Sheets[1].Name = model.Drawings[1].Sheets[0].Name
			},
			want: `drawings[1].sheets[1].name "assembly" duplicates sheet name`,
		},
		{
			name: "duplicate-sheet-order",
			build: func(model *Model) {
				model.Drawings[1].Sheets[1].Order = model.Drawings[1].Sheets[0].Order
			},
			want: "duplicates sheet order",
		},
		{
			name:  "empty-component-ids",
			build: func(model *Model) { model.Drawings[0].ComponentIDs = []string{} },
			want:  "drawings[0].componentIds must not be empty",
		},
		{
			name:  "blank-component-id",
			build: func(model *Model) { model.Drawings[0].ComponentIDs = []string{""} },
			want:  "drawings[0].componentIds[0] is required",
		},
		{
			name:  "duplicate-component-id",
			build: func(model *Model) { model.Drawings[0].ComponentIDs = []string{"cmp.bracket", "cmp.bracket"} },
			want:  `drawings[0].componentIds[1] duplicates component ID "cmp.bracket"`,
		},
		{
			name:  "unresolved-component-id",
			build: func(model *Model) { model.Drawings[0].ComponentIDs = []string{"cmp.missing"} },
			want:  `drawings[0].componentIds[0] "cmp.missing" must resolve to an existing component`,
		},
		{
			name:  "empty-sheets",
			build: func(model *Model) { model.Drawings[0].Sheets = []DrawingSheet{} },
			want:  "drawings[0].sheets must not be empty",
		},
		{
			name: "embedded-with-path",
			build: func(model *Model) {
				model.Drawings[0].Resource = DrawingResource{Topology: "embedded", Path: "x.FCStd"}
			},
			want: `drawings[0].resource.path must be empty when topology is "embedded"`,
		},
		{
			name:  "separate-without-path",
			build: func(model *Model) { model.Drawings[0].Resource = DrawingResource{Topology: "separate"} },
			want:  `drawings[0].resource.path is required when topology is "separate"`,
		},
		{
			name: "separate-absolute-path",
			build: func(model *Model) {
				model.Drawings[0].Resource = DrawingResource{Topology: "separate", Path: "/abs/x.FCStd"}
			},
			want: "invalid canonical relative coordinate",
		},
		{
			name:  "unknown-topology",
			build: func(model *Model) { model.Drawings[0].Resource = DrawingResource{Topology: "linked"} },
			want:  `drawings[0].resource.topology must be one of "embedded" or "separate"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := Clone(buildDrawingsModel(t))
			tc.build(model)

			first := Validate(model)
			second := Validate(model)
			if first == nil || second == nil {
				t.Fatal("expected Validate to fail")
			}
			if !errors.Is(first, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", first)
			}
			if first.Error() != second.Error() {
				t.Fatalf("expected deterministic validation text\nfirst:  %q\nsecond: %q", first.Error(), second.Error())
			}
			if !strings.Contains(first.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, first)
			}
		})
	}
}

func TestDrawingsAreNotDslTargets(t *testing.T) {
	model := buildDrawingsModel(t)

	target, err := ResolveSemanticTargetByExactName(model, "RootAssembly")
	if err != nil {
		t.Fatalf("expected component target resolution to remain unchanged: %v", err)
	}
	if target.SemanticID != "cmp.root" {
		t.Fatalf("unexpected resolved target: %#v", target)
	}

	for _, drawing := range model.Drawings {
		if _, err := ResolveSemanticTargetByExactName(model, drawing.Name); err == nil {
			t.Fatalf("drawing name %q unexpectedly resolved as a DSL target", drawing.Name)
		}
	}
}

func idsFromDrawings(items []Drawing) []string {
	return collectTestIDs(len(items), func(i int) string { return items[i].ID })
}

func drawingsByID(items []Drawing) map[string]Drawing {
	byID := make(map[string]Drawing, len(items))
	for _, drawing := range items {
		byID[drawing.ID] = drawing
	}
	return byID
}
