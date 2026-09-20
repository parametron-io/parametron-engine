package planner

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
)

// Issue #16 planner permanent contract: every aligned FreeCAD runtime manifest
// the planner emits carries the single canonical schema "1.0". The optional
// Part / Assembly mutation collections (suppression / visibility / deletion)
// are structural: they are present exactly when target actions produce runtime
// mutations and absent otherwise, and their presence never influences the
// schema version. Internal Parameters / Properties mutation metadata never
// becomes a runtime mutation family and top-level parameterAssignments remains
// the only executable native scalar-write surface.
//
// These tests exercise the real production seam (createPlan ->
// buildExportManifestIntent) through parseValidateAndPlanCaptureBacked, plus
// the committed helper functions directly for the family-composition edge
// cases.

// runtimeMutationManifestModel is a capture-backed FreeCAD semantic model with a
// resolvable native parameter plus part-scope and assembly-scope features and
// components, so every one of the five canonical target actions can be authored
// against a real destination bucket in the same product.
func runtimeMutationManifestModel() *semantic.Model {
	return &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly"},
			{ID: "cmp.leg", Kind: "part", Name: "Leg", ParentID: "cmp.root", Targetability: allBitsTrue()},
			{ID: "cmp.cover", Kind: "part", Name: "Cover", ParentID: "cmp.root", Targetability: allBitsTrue()},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.root.width", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.root", ComponentID: "cmp.root", Name: "width", NativeType: "Length"},
		},
		Features: []semantic.Feature{
			{ID: "feat.pad", ComponentID: "cmp.leg", Name: "Pad", NativeType: "PartDesign::Pad", Targetability: allBitsTrue()},
			{ID: "feat.slot", ComponentID: "cmp.leg", Name: "Slot", NativeType: "PartDesign::Pocket", Targetability: allBitsTrue()},
			{ID: "feat.rail", ComponentID: "cmp.root", Name: "Rail", NativeType: "PartDesign::Pad", Targetability: allBitsTrue()},
			{ID: "feat.chamfer", ComponentID: "cmp.root", Name: "Chamfer", NativeType: "PartDesign::Chamfer", Targetability: allBitsTrue()},
			{ID: "feat.latch", ComponentID: "cmp.root", Name: "Latch", NativeType: "PartDesign::Pad", Targetability: allBitsTrue()},
		},
	}
}

func runtimeMutationManifestProduct(body string) string {
	return `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param width: number = 12
` + body + `
}
`
}

func runtimeManifestPayload(t *testing.T, body string) WriteExportManifestPayload {
	t.Helper()
	_, plan := parseValidateAndPlanCaptureBacked(t, runtimeMutationManifestProduct(body), runtimeMutationManifestModel(), testProjectionContract())
	return manifestPayloadFromPlan(t, plan)
}

// ---------------------------------------------------------------------------
// PART A — schema constants / domain
// ---------------------------------------------------------------------------

func TestTask11_SchemaConstants(t *testing.T) {
	if ExportManifestSchemaVersion != "1.0" {
		t.Fatalf("canonical schema constant: got %q want %q", ExportManifestSchemaVersion, "1.0")
	}
	if ExportManifestFilename != "prm.export-manifest.json" || FreeCADRuntimeExportManifestFilename != "prm.export-manifest.json" {
		t.Fatalf("canonical manifest filename drifted: %q / %q", ExportManifestFilename, FreeCADRuntimeExportManifestFilename)
	}
}

// ---------------------------------------------------------------------------
// PART B — planner canonical schema (real capture-backed path)
// ---------------------------------------------------------------------------

func TestTask11_PlannerCanonicalSchemaMatrix(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		wantMutation bool
		wantPart     *ExportManifestMutationCollection
		wantAsm      *ExportManifestMutationCollection
	}{
		{name: "mutation-less", body: ""},
		{name: "keep-only", body: "    target Pad: action = keep"},
		// width alone -> internal Parameters metadata only
		{name: "parameterAssignments-only", body: ""},
		{
			name: "suppress", body: "    target Pad: action = suppress", wantMutation: true,
			wantPart: &ExportManifestMutationCollection{Suppression: []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}}},
		},
		{
			name: "unsuppress", body: "    target Pad: action = unsuppress", wantMutation: true,
			wantPart: &ExportManifestMutationCollection{Suppression: []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: false}}},
		},
		{
			name: "hide", body: "    target Pad: action = hide", wantMutation: true,
			wantPart: &ExportManifestMutationCollection{Visibility: []ExportManifestVisibilityMutation{{Object: "Pad", Visible: false}}},
		},
		{
			name: "unhide", body: "    target Pad: action = unhide", wantMutation: true,
			wantPart: &ExportManifestMutationCollection{Visibility: []ExportManifestVisibilityMutation{{Object: "Pad", Visible: true}}},
		},
		{
			name: "delete", body: "    target Chamfer: action = delete", wantMutation: true,
			wantAsm: &ExportManifestMutationCollection{Deletion: []ExportManifestDeletionMutation{{Object: "Chamfer"}}},
		},
		{
			name: "mixed-part-and-assembly", body: "    target Pad: action = suppress\n    target Rail: action = hide\n    target Chamfer: action = delete", wantMutation: true,
			wantPart: &ExportManifestMutationCollection{Suppression: []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}}},
			wantAsm: &ExportManifestMutationCollection{
				Visibility: []ExportManifestVisibilityMutation{{Object: "Rail", Visible: false}},
				Deletion:   []ExportManifestDeletionMutation{{Object: "Chamfer"}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := runtimeManifestPayload(t, tc.body)
			if payload.SchemaVersion != "1.0" {
				t.Fatalf("schemaVersion: got %q want 1.0", payload.SchemaVersion)
			}
			if payload.ManifestFilename != "prm.export-manifest.json" {
				t.Fatalf("manifestFilename: got %q", payload.ManifestFilename)
			}
			has := hasRuntimeTargetMutations(payload.AssemblyMutations) || hasRuntimeTargetMutations(payload.PartMutations)
			if has != tc.wantMutation {
				t.Fatalf("runtime mutation presence: got %v want %v: assembly=%#v part=%#v", has, tc.wantMutation, payload.AssemblyMutations, payload.PartMutations)
			}
			// Schema 1.0 must still carry the mutation intent verbatim.
			assertRuntimeFamilies(t, "part", payload.PartMutations, tc.wantPart)
			assertRuntimeFamilies(t, "assembly", payload.AssemblyMutations, tc.wantAsm)
		})
	}
}

// keep-only authoring: no Task 8 intent, no runtime mutation sections, and the
// parameterAssignments surface is unaffected.
func TestTask11_KeepOnlyRetainsSchema1AndParameterAssignments(t *testing.T) {
	payload := runtimeManifestPayload(t, "    target Pad: action = keep")
	if payload.SchemaVersion != "1.0" {
		t.Fatalf("keep-only schemaVersion: got %q want 1.0", payload.SchemaVersion)
	}
	if len(payload.ParameterAssignments) != 1 || payload.ParameterAssignments[0].Name != "width" {
		t.Fatalf("keep-only lost parameterAssignments: %#v", payload.ParameterAssignments)
	}
	for _, c := range []*ExportManifestMutationCollection{payload.AssemblyMutations, payload.PartMutations} {
		if c == nil {
			continue
		}
		if len(c.Suppression)+len(c.Visibility)+len(c.Deletion) != 0 {
			t.Fatalf("keep-only produced runtime mutation families: %#v", c)
		}
	}
}

// Internal Parameters / Properties metadata never becomes a runtime mutation
// family; a single runtime family entry does.
func TestTask11_InternalMetadataIsNotRuntimeMutation(t *testing.T) {
	metaOnly := &ExportManifestMutationCollection{
		Parameters: []ExportManifestParameterMutation{{Object: "Box", Property: "Width", ValueParam: "width"}},
		Properties: []ExportManifestPropertyMutation{{Object: "Box", Property: "Label", Value: "primary"}},
	}
	if hasRuntimeTargetMutations(metaOnly) {
		t.Fatal("Parameters+Properties-only collection must not count as runtime target mutations")
	}
	withVisibility := &ExportManifestMutationCollection{
		Parameters: metaOnly.Parameters,
		Properties: metaOnly.Properties,
		Visibility: []ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
	}
	if !hasRuntimeTargetMutations(withVisibility) {
		t.Fatal("one Visibility entry must count as a runtime target mutation")
	}
}

// ---------------------------------------------------------------------------
// PART C — draft / final planner parity
// ---------------------------------------------------------------------------

// The planner runs two manifest constructions when file_pattern hashing is
// active: the draft plan that seeds the naming hash, then the final plan. Both
// emit canonical schema 1.0 over the identical buildExportManifestIntent
// output, so an equivalent mutation-bearing product built with file_pattern
// hashing active carries 1.0 with correctly composed families, and a
// mutation-less one carries 1.0 with none.
func TestTask11_FilePatternHashingKeepsCanonicalSchemaConsistent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		want     string
		wantPart *ExportManifestMutationCollection
		wantAsm  *ExportManifestMutationCollection
	}{
		{
			name:     "mutation-bearing",
			body:     "    target Pad: action = suppress\n    target Rail: action = delete",
			want:     "1.0",
			wantPart: &ExportManifestMutationCollection{Suppression: []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}}},
			wantAsm:  &ExportManifestMutationCollection{Deletion: []ExportManifestDeletionMutation{{Object: "Rail"}}},
		},
		{name: "mutation-less", body: "", want: "1.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			product := `
profile Prod {
    file_pattern = "{product}_{param:width}"
}
use profile Prod

product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param width: number = 12
` + tc.body + `
}
`
			_, plan := parseValidateAndPlanCaptureBacked(t, product, runtimeMutationManifestModel(), testProjectionContract())
			payload := manifestPayloadFromPlan(t, plan)
			if payload.SchemaVersion != tc.want {
				t.Fatalf("schemaVersion: got %q want %q", payload.SchemaVersion, tc.want)
			}
			assertRuntimeFamilies(t, "part", payload.PartMutations, tc.wantPart)
			assertRuntimeFamilies(t, "assembly", payload.AssemblyMutations, tc.wantAsm)
		})
	}
}

// ---------------------------------------------------------------------------
// PART D — planner mutation collection shape
// ---------------------------------------------------------------------------

func TestTask11_PlannerMutationCollectionShape(t *testing.T) {
	rt := reflect.TypeOf(ExportManifestMutationCollection{})
	want := []string{"Parameters", "Properties", "Suppression", "Visibility", "Deletion"}
	if rt.NumField() != len(want) {
		t.Fatalf("ExportManifestMutationCollection field count: got %d want %d", rt.NumField(), len(want))
	}
	for i, name := range want {
		if rt.Field(i).Name != name {
			t.Fatalf("ExportManifestMutationCollection field %d: got %q want %q", i, rt.Field(i).Name, name)
		}
	}

	vis := reflect.TypeOf(ExportManifestVisibilityMutation{})
	if vis.NumField() != 2 || vis.Field(0).Name != "Object" || vis.Field(1).Name != "Visible" {
		t.Fatalf("ExportManifestVisibilityMutation shape changed: %#v", vis)
	}
	if got := vis.Field(0).Tag.Get("json"); got != "object" {
		t.Fatalf("visibility object json tag: got %q", got)
	}
	if got := vis.Field(1).Tag.Get("json"); got != "visible" {
		t.Fatalf("visibility visible json tag: got %q", got)
	}

	del := reflect.TypeOf(ExportManifestDeletionMutation{})
	if del.NumField() != 1 || del.Field(0).Name != "Object" {
		t.Fatalf("ExportManifestDeletionMutation shape changed: %#v", del)
	}
	for _, forbidden := range []string{"Force", "Cascade", "SemanticID", "TargetKind", "Scope", "DependencyPolicy"} {
		if _, ok := del.FieldByName(forbidden); ok {
			t.Fatalf("ExportManifestDeletionMutation must not carry %q", forbidden)
		}
	}
}

// The semanticmap -> planner conversion retains all five families verbatim.
func TestTask11_SemanticmapToPlannerConversionRetainsAllFamilies(t *testing.T) {
	projected := &semanticmap.ManifestMutationCollection{
		Parameters:  []semanticmap.ManifestParameterMutation{{Object: "Box", Property: "Width", ValueParam: "width", Type: "number", Unit: "mm"}},
		Properties:  []semanticmap.ManifestPropertyMutation{{Object: "Box", Property: "Label", Value: "primary"}},
		Suppression: []semanticmap.ManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
		Visibility:  []semanticmap.ManifestVisibilityMutation{{Object: "Body", Visible: false}},
		Deletion:    []semanticmap.ManifestDeletionMutation{{Object: "Chamfer"}},
	}
	got := toExportManifestMutationCollection(projected)
	want := &ExportManifestMutationCollection{
		Parameters:  []ExportManifestParameterMutation{{Object: "Box", Property: "Width", ValueParam: "width", Type: "number", Unit: "mm"}},
		Properties:  []ExportManifestPropertyMutation{{Object: "Box", Property: "Label", Value: "primary"}},
		Suppression: []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
		Visibility:  []ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
		Deletion:    []ExportManifestDeletionMutation{{Object: "Chamfer"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("conversion dropped a family:\ngot:  %#v\nwant: %#v", got, want)
	}
}

// ---------------------------------------------------------------------------
// PART E — Task 10 family composition into the planner collection
// ---------------------------------------------------------------------------

func TestTask11_Task10CompositionPreservesMatureEntriesAndAppends(t *testing.T) {
	base := &ExportManifestMutationCollection{
		Parameters:  []ExportManifestParameterMutation{{Object: "Box", Property: "Width", ValueParam: "width"}},
		Properties:  []ExportManifestPropertyMutation{{Object: "Box", Property: "Label", Value: "primary"}},
		Suppression: []ExportManifestSuppressionMutation{{Object: "LegacyPad", Suppressed: true}},
	}
	routed := &semanticmap.ProjectedTargetMutationRouting{
		Part: &semanticmap.ManifestMutationCollection{
			Suppression: []semanticmap.ManifestSuppressionMutation{{Object: "NewPad", Suppressed: false}},
			Visibility:  []semanticmap.ManifestVisibilityMutation{{Object: "NewBody", Visible: false}},
			Deletion:    []semanticmap.ManifestDeletionMutation{{Object: "NewChamfer"}},
		},
		Assembly: &semanticmap.ManifestMutationCollection{
			Visibility: []semanticmap.ManifestVisibilityMutation{{Object: "AsmComp", Visible: true}},
		},
	}

	part := mergeExportManifestMutationCollections(base, routed, false)
	if !reflect.DeepEqual(part.Parameters, base.Parameters) {
		t.Fatalf("mature Parameters not preserved: %#v", part.Parameters)
	}
	if !reflect.DeepEqual(part.Properties, base.Properties) {
		t.Fatalf("mature Properties not preserved: %#v", part.Properties)
	}
	wantSuppression := []ExportManifestSuppressionMutation{{Object: "LegacyPad", Suppressed: true}, {Object: "NewPad", Suppressed: false}}
	if !reflect.DeepEqual(part.Suppression, wantSuppression) {
		t.Fatalf("composition order wrong: got %#v want %#v", part.Suppression, wantSuppression)
	}
	if !reflect.DeepEqual(part.Visibility, []ExportManifestVisibilityMutation{{Object: "NewBody", Visible: false}}) {
		t.Fatalf("Task 10 Visibility not appended to Part: %#v", part.Visibility)
	}
	if !reflect.DeepEqual(part.Deletion, []ExportManifestDeletionMutation{{Object: "NewChamfer"}}) {
		t.Fatalf("Task 10 Deletion not appended to Part: %#v", part.Deletion)
	}

	// Part / Assembly buckets remain isolated: the assembly route does not
	// leak into the part collection.
	for _, v := range part.Visibility {
		if v.Object == "AsmComp" {
			t.Fatal("assembly route leaked into part collection")
		}
	}
	assembly := mergeExportManifestMutationCollections(nil, routed, true)
	if len(assembly.Visibility) != 1 || assembly.Visibility[0].Object != "AsmComp" {
		t.Fatalf("assembly composition wrong: %#v", assembly)
	}
	if len(assembly.Suppression) != 0 || len(assembly.Deletion) != 0 {
		t.Fatalf("assembly bucket picked up part families: %#v", assembly)
	}
}

// Composition performs no dedup and no merge/normalization: the same Object may
// appear across families and repeated, and every entry survives verbatim.
func TestTask11_Task10CompositionNoDedupNoMerge(t *testing.T) {
	base := &ExportManifestMutationCollection{
		Suppression: []ExportManifestSuppressionMutation{{Object: "Shared", Suppressed: true}},
	}
	routed := &semanticmap.ProjectedTargetMutationRouting{
		Part: &semanticmap.ManifestMutationCollection{
			Suppression: []semanticmap.ManifestSuppressionMutation{{Object: "Shared", Suppressed: true}},
			Visibility:  []semanticmap.ManifestVisibilityMutation{{Object: "Shared", Visible: false}},
		},
	}
	got := mergeExportManifestMutationCollections(base, routed, false)
	if len(got.Suppression) != 2 {
		t.Fatalf("dedup collapsed repeated Suppression: %#v", got.Suppression)
	}
	if len(got.Visibility) != 1 || got.Visibility[0].Object != "Shared" {
		t.Fatalf("Suppression+Visibility on the same Object did not both survive: %#v", got)
	}
}

// ---------------------------------------------------------------------------
// PART Q — five-action planner -> payload integration (real path)
// ---------------------------------------------------------------------------

func TestTask11_FiveActionRuntimeProjection(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantPart   *ExportManifestMutationCollection
		wantAsm    *ExportManifestMutationCollection
		wantSchema string
	}{
		{
			name:       "suppress",
			body:       "    target Pad: action = suppress",
			wantPart:   &ExportManifestMutationCollection{Suppression: []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}}},
			wantSchema: "1.0",
		},
		{
			name:       "unsuppress",
			body:       "    target Pad: action = unsuppress",
			wantPart:   &ExportManifestMutationCollection{Suppression: []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: false}}},
			wantSchema: "1.0",
		},
		{
			name:       "hide",
			body:       "    target Pad: action = hide",
			wantPart:   &ExportManifestMutationCollection{Visibility: []ExportManifestVisibilityMutation{{Object: "Pad", Visible: false}}},
			wantSchema: "1.0",
		},
		{
			name:       "unhide",
			body:       "    target Pad: action = unhide",
			wantPart:   &ExportManifestMutationCollection{Visibility: []ExportManifestVisibilityMutation{{Object: "Pad", Visible: true}}},
			wantSchema: "1.0",
		},
		{
			name:       "delete",
			body:       "    target Rail: action = delete",
			wantAsm:    &ExportManifestMutationCollection{Deletion: []ExportManifestDeletionMutation{{Object: "Rail"}}},
			wantSchema: "1.0",
		},
		{
			name:       "keep",
			body:       "    target Pad: action = keep",
			wantSchema: "1.0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := runtimeManifestPayload(t, tc.body)
			if payload.SchemaVersion != tc.wantSchema {
				t.Fatalf("schemaVersion: got %q want %q", payload.SchemaVersion, tc.wantSchema)
			}
			assertRuntimeFamilies(t, "part", payload.PartMutations, tc.wantPart)
			assertRuntimeFamilies(t, "assembly", payload.AssemblyMutations, tc.wantAsm)
		})
	}
}

// assertRuntimeFamilies compares only the runtime families (suppression /
// visibility / deletion) of a projected planner collection, ignoring internal
// Parameters / Properties metadata that legitimately rides alongside.
func assertRuntimeFamilies(t *testing.T, bucket string, got, want *ExportManifestMutationCollection) {
	t.Helper()
	var gotS []ExportManifestSuppressionMutation
	var gotV []ExportManifestVisibilityMutation
	var gotD []ExportManifestDeletionMutation
	if got != nil {
		gotS, gotV, gotD = got.Suppression, got.Visibility, got.Deletion
	}
	var wantS []ExportManifestSuppressionMutation
	var wantV []ExportManifestVisibilityMutation
	var wantD []ExportManifestDeletionMutation
	if want != nil {
		wantS, wantV, wantD = want.Suppression, want.Visibility, want.Deletion
	}
	if !reflect.DeepEqual(gotS, wantS) || !reflect.DeepEqual(gotV, wantV) || !reflect.DeepEqual(gotD, wantD) {
		t.Fatalf("%s runtime families mismatch:\ngot  S=%#v V=%#v D=%#v\nwant S=%#v V=%#v D=%#v", bucket, gotS, gotV, gotD, wantS, wantV, wantD)
	}
}

// The native Object resolved before Task 11 survives verbatim into the payload;
// no semantic ID substitution, no re-routing.
func TestTask11_NativeObjectSurvivesVerbatim(t *testing.T) {
	payload := runtimeManifestPayload(t, "    target Pad: action = suppress\n    target Rail: action = delete")
	if payload.PartMutations == nil || len(payload.PartMutations.Suppression) != 1 || payload.PartMutations.Suppression[0].Object != "Pad" {
		t.Fatalf("Pad did not survive as a Part suppression: %#v", payload.PartMutations)
	}
	if payload.AssemblyMutations == nil || len(payload.AssemblyMutations.Deletion) != 1 || payload.AssemblyMutations.Deletion[0].Object != "Rail" {
		t.Fatalf("Rail did not survive as an Assembly deletion: %#v", payload.AssemblyMutations)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	for _, semanticID := range []string{"feat.pad", "feat.rail", "cmp.root", "cmp.leg"} {
		if strings.Contains(string(raw), semanticID) {
			t.Fatalf("semantic id %q leaked into the manifest payload: %s", semanticID, raw)
		}
	}
}

// ---------------------------------------------------------------------------
// PART R — determinism
// ---------------------------------------------------------------------------

func TestTask11_CanonicalSchemaAndProjectionIsDeterministic(t *testing.T) {
	body := "    target Pad: action = suppress\n    target Slot: action = hide\n    target Rail: action = delete\n    target Latch: action = unhide"
	first := runtimeManifestPayload(t, body)
	for i := 0; i < 10; i++ {
		got := runtimeManifestPayload(t, body)
		if got.SchemaVersion != "1.0" || got.SchemaVersion != first.SchemaVersion {
			t.Fatalf("iteration %d: schema drift %q vs %q", i, got.SchemaVersion, first.SchemaVersion)
		}
		if !reflect.DeepEqual(got.AssemblyMutations, first.AssemblyMutations) || !reflect.DeepEqual(got.PartMutations, first.PartMutations) {
			t.Fatalf("iteration %d: family projection drift", i)
		}
	}
}

// ---------------------------------------------------------------------------
// PART T — identity boundary (Task 12 stays out of scope)
// ---------------------------------------------------------------------------

// Task 11 introduces no dedicated identity algorithm: the manifest payload
// participates in the existing serialized plan surface, and that is all this
// stage proves. No plan-hash equivalence / canonical-ordering assertion here.
func TestTask11_MutationPayloadParticipatesInSerializedPlan(t *testing.T) {
	_, plan := parseValidateAndPlanCaptureBacked(t, runtimeMutationManifestProduct("    target Pad: action = suppress"), runtimeMutationManifestModel(), testProjectionContract())
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if !strings.Contains(string(raw), `"suppression"`) || !strings.Contains(string(raw), `"Pad"`) {
		t.Fatalf("expected the runtime suppression family to appear in the serialized plan: %s", raw)
	}
	for _, forbidden := range []string{
		"TargetActionMutationCollections", "targetActionMutationCollections",
		"ProjectedTargetMutationRouting", "projectedTargetMutationRouting",
		"TargetActionRouting", "targetActionRouting", "RoutedTargetMutation",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("private Task 9/10 type %q leaked into serialized plan: %s", forbidden, raw)
		}
	}
}
