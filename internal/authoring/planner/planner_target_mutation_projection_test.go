package planner

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/cad"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
	"parametron/internal/engine/table"
)

// Phase 5 Task 10 planner integration: these tests prove the canonical
// mutation-family projection step (semanticmap.ProjectTargetMutationRouting,
// invoked by projectSemanticManifestIntent immediately after the Task 9
// RouteTargetMutations call) is wired into the capture-backed planner at the
// correct pipeline seam:
//
//	Task 5 action evaluation
//	    -> Task 6 semantic target resolution
//	        -> Task 7 captured-Targetability capability validation
//	            -> Task 8 semantic MutationIntent lowering
//	                -> Task 9 destination + native-object routing
//	                    -> Task 10 canonical mutation-family projection
//
// The projected families live only on the private
// semanticManifestProjectionResult.TargetActionMutationCollections seam. Task 10
// adds no ExecutionPlan / WriteExportManifestPayload field, never leaks into
// public plan JSON, never enters the pre-existing public PartMutations /
// AssemblyMutations / Suppression surfaces, and does not activate schema 2.0.

// projectFamiliesViaProjection runs the real production Task 9 -> 10 seam:
// projectSemanticManifestIntent (the function createPlan invokes for
// capture-backed products) routes then projects the lowered Task 8 mutations and
// returns the private Task 10 family collections. An empty Intent keeps the
// unrelated output / parameter / property projection inert.
func projectFamiliesViaProjection(t *testing.T, model *semantic.Model, mutations []semantic.MutationIntent) *semanticmap.ProjectedTargetMutationRouting {
	t.Helper()
	result := projectSemanticManifestResultForTest(t, model, mutations)
	return result.TargetActionMutationCollections
}

func projectSemanticManifestResultForTest(t *testing.T, model *semantic.Model, mutations []semantic.MutationIntent) *semanticManifestProjectionResult {
	t.Helper()
	result, err := projectSemanticManifestIntent(&semanticManifestProjectionRequest{
		BaseName:      "Demo",
		CaptureBacked: true,
		Model:         model,
		Intent: &semantic.ProductIntent{
			Name:                   "Demo",
			RequestedOutputFormats: []string{},
			Outputs:                []semantic.OutputIntent{},
			ExportedParameters:     []semantic.ExportedParameterIntent{},
			Mutations:              []semantic.MutationIntent{},
		},
		TargetActionMutations: mutations,
		ResolvedValues:        map[string]any{},
		Contract:              testProjectionContract(),
	})
	if err != nil {
		t.Fatalf("projectSemanticManifestIntent returned error: %v", err)
	}
	return result
}

// projectTargetActionFamiliesForTest composes the real Task 5 -> 8 planner seams
// (lowerTargetActionsForTest) with the real Task 9 -> 10 seam.
func projectTargetActionFamiliesForTest(t *testing.T, dslContent string, overrides map[string]string, tables map[string]*table.Table, model *semantic.Model) (*semanticmap.ProjectedTargetMutationRouting, error) {
	t.Helper()
	mutations, err := lowerTargetActionsForTest(t, dslContent, overrides, tables, model)
	if err != nil {
		return nil, err
	}
	return projectFamiliesViaProjection(t, model, mutations), nil
}

// ---------------------------------------------------------------------------
// PART H — private planner integration
// ---------------------------------------------------------------------------

// 55. The Task 10 family projection consumes the actual Task 9 private route:
// one projectSemanticManifestIntent call yields a routed result and a family
// result whose entries correspond 1:1, in the same buckets and order.
func TestPlannerTargetMutationProjection_ConsumesTask9Route(t *testing.T) {
	model := plannerRoutingModel(t)
	mutations, err := lowerTargetActionsForTest(t, `
product Demo {
    target Pad: action = suppress
    target Sub: action = hide
    target Leg: action = delete
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := projectSemanticManifestResultForTest(t, model, mutations)
	routing := result.TargetActionRouting
	families := result.TargetActionMutationCollections
	if routing == nil || families == nil {
		t.Fatalf("expected both a Task 9 route and a Task 10 family projection, got routing=%#v families=%#v", routing, families)
	}
	assertFamiliesMatchRoute(t, "part", routing.Part, families.Part)
	assertFamiliesMatchRoute(t, "assembly", routing.Assembly, families.Assembly)
}

func assertFamiliesMatchRoute(t *testing.T, bucket string, routed []semanticmap.RoutedTargetMutation, families *semanticmap.ManifestMutationCollection) {
	t.Helper()
	type famEntry struct {
		op     string
		object string
	}
	var got []famEntry
	if families != nil {
		for _, s := range families.Suppression {
			op := "suppress"
			if !s.Suppressed {
				op = "unsuppress"
			}
			got = append(got, famEntry{op, s.Object})
		}
		for _, v := range families.Visibility {
			op := "hide"
			if v.Visible {
				op = "unhide"
			}
			got = append(got, famEntry{op, v.Object})
		}
		for _, d := range families.Deletion {
			got = append(got, famEntry{"delete", d.Object})
		}
	}
	// Task 10 appends family-by-family, so compare as a set keyed by (op,object).
	want := map[famEntry]int{}
	for _, r := range routed {
		want[famEntry{r.OperationKind, r.Object}]++
	}
	have := map[famEntry]int{}
	for _, g := range got {
		have[famEntry{g.op, g.object}]++
	}
	if !reflect.DeepEqual(want, have) {
		t.Fatalf("%s bucket: Task 10 families do not correspond to the Task 9 route\nroute:    %#v\nfamilies: %#v", bucket, routed, families)
	}
}

// 56. Canonical authored suppress -> private Part.Suppression / Pad / true.
func TestPlannerTargetMutationProjection_PrivateSuppressProjection(t *testing.T) {
	model := plannerRoutingModel(t)
	families, err := projectTargetActionFamiliesForTest(t, `
product Demo {
    target Pad: action = suppress
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if families == nil || families.Part == nil {
		t.Fatalf("expected a populated private Part family collection, got %#v", families)
	}
	if !reflect.DeepEqual(families.Part.Suppression, []semanticmap.ManifestSuppressionMutation{{Object: "Pad", Suppressed: true}}) {
		t.Fatalf("unexpected suppression projection: %#v", families.Part)
	}
	if families.Assembly != nil {
		t.Fatalf("expected no assembly families, got %#v", families.Assembly)
	}
}

// 57. Canonical authored hide -> private Visibility / false.
func TestPlannerTargetMutationProjection_PrivateHideProjection(t *testing.T) {
	model := plannerRoutingModel(t)
	families, err := projectTargetActionFamiliesForTest(t, `
product Demo {
    target Pad: action = hide
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(families.Part.Visibility, []semanticmap.ManifestVisibilityMutation{{Object: "Pad", Visible: false}}) {
		t.Fatalf("unexpected visibility projection: %#v", families.Part)
	}
	if len(families.Part.Suppression) != 0 {
		t.Fatalf("hide must not populate the private suppression family: %#v", families.Part.Suppression)
	}
}

// 58. Canonical authored unhide -> private Visibility / true.
func TestPlannerTargetMutationProjection_PrivateUnhideProjection(t *testing.T) {
	model := plannerRoutingModel(t)
	families, err := projectTargetActionFamiliesForTest(t, `
product Demo {
    target Bracket: action = unhide
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(families.Assembly.Visibility, []semanticmap.ManifestVisibilityMutation{{Object: "Bracket", Visible: true}}) {
		t.Fatalf("unexpected visibility projection: %#v", families.Assembly)
	}
	if len(families.Assembly.Suppression) != 0 {
		t.Fatalf("unhide must not populate the private suppression family: %#v", families.Assembly.Suppression)
	}
}

// 59. Canonical authored delete -> private Deletion entry.
func TestPlannerTargetMutationProjection_PrivateDeleteProjection(t *testing.T) {
	model := plannerRoutingModel(t)
	families, err := projectTargetActionFamiliesForTest(t, `
product Demo {
    target Sub: action = delete
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(families.Assembly.Deletion, []semanticmap.ManifestDeletionMutation{{Object: "Sub"}}) {
		t.Fatalf("unexpected deletion projection: %#v", families.Assembly)
	}
}

// 60. Mixed Feature/Component product across Part and Assembly with multiple
// families: the private Task 10 collections carry the native names in each
// destination bucket.
func TestPlannerTargetMutationProjection_MixedFeatureComponentProduct(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo",
		[]semantic.Component{
			{ID: "cmp.leg", Kind: "part", Name: "Leg", ChildrenIDs: []string{}, Quantity: 1, Targetability: allBitsTrue()},
			{ID: "cmp.cover", Kind: "part", Name: "Cover", ChildrenIDs: []string{}, Quantity: 1, Targetability: allBitsTrue()},
			{ID: "cmp.sub", Kind: "assembly", Name: "Sub", ChildrenIDs: []string{}, Quantity: 1, Targetability: allBitsTrue()},
		},
		[]semantic.Feature{
			{ID: "feat.pad", ComponentID: "cmp.leg", Name: "Pad", Targetability: allBitsTrue()},
			{ID: "feat.bracket", ComponentID: "cmp.root", Name: "Bracket", Targetability: allBitsTrue()},
		})
	families, err := projectTargetActionFamiliesForTest(t, `
product Demo {
    target Pad: action = suppress
    target Sub: action = hide
    target Cover: action = delete
    target Bracket: action = unsuppress
    target Leg: action = unhide
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantPart := &semanticmap.ManifestMutationCollection{
		Suppression: []semanticmap.ManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
		Visibility:  []semanticmap.ManifestVisibilityMutation{{Object: "Leg", Visible: true}},
		Deletion:    []semanticmap.ManifestDeletionMutation{{Object: "Cover"}},
	}
	wantAssembly := &semanticmap.ManifestMutationCollection{
		Suppression: []semanticmap.ManifestSuppressionMutation{{Object: "Bracket", Suppressed: false}},
		Visibility:  []semanticmap.ManifestVisibilityMutation{{Object: "Sub", Visible: false}},
	}
	if !reflect.DeepEqual(families.Part, wantPart) {
		t.Fatalf("part families\nwant: %#v\ngot:  %#v", wantPart, families.Part)
	}
	if !reflect.DeepEqual(families.Assembly, wantAssembly) {
		t.Fatalf("assembly families\nwant: %#v\ngot:  %#v", wantAssembly, families.Assembly)
	}
}

// 61. Literal integration: the final Task 8 literal action drives the family.
func TestPlannerTargetMutationProjection_LiteralIntegration(t *testing.T) {
	model := plannerRoutingModel(t)
	families, err := projectTargetActionFamiliesForTest(t, `
product Demo {
    target Pad: action = delete
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(families.Part.Deletion) != 1 || families.Part.Deletion[0].Object != "Pad" {
		t.Fatalf("expected a single Pad deletion, got %#v", families.Part)
	}
}

// 62. Ternary integration: family follows the selected branch; Task 10 never
// inspects the AST expression form.
func TestPlannerTargetMutationProjection_TernaryIntegration(t *testing.T) {
	model := plannerRoutingModel(t)
	dslContent := `
product Demo {
    param flag: boolean = true
    target Pad: action = flag ? suppress : hide
}`
	whenTrue, err := projectTargetActionFamiliesForTest(t, dslContent, nil, nil, model)
	if err != nil {
		t.Fatalf("flag=true: unexpected error: %v", err)
	}
	if len(whenTrue.Part.Suppression) != 1 || len(whenTrue.Part.Visibility) != 0 {
		t.Fatalf("flag=true: expected suppression family, got %#v", whenTrue.Part)
	}
	whenFalse, err := projectTargetActionFamiliesForTest(t, dslContent, map[string]string{"flag": "false"}, nil, model)
	if err != nil {
		t.Fatalf("flag=false: unexpected error: %v", err)
	}
	if len(whenFalse.Part.Visibility) != 1 || len(whenFalse.Part.Suppression) != 0 {
		t.Fatalf("flag=false: expected visibility family, got %#v", whenFalse.Part)
	}
	if whenFalse.Part.Visibility[0].Visible {
		t.Fatalf("flag=false: hide must project Visible=false, got %#v", whenFalse.Part.Visibility[0])
	}
}

// 63. Table-backed integration: selected row action drives the family.
func TestPlannerTargetMutationProjection_TableBackedIntegration(t *testing.T) {
	tables := map[string]*table.Table{
		"variants": {
			SchemaVersion: table.SchemaVersion,
			Name:          "variants",
			KeyColumn:     "variant",
			Columns: []table.Column{
				{Name: "variant", Type: table.ColumnTypeString, Required: true},
				{Name: "action", Type: table.ColumnTypeString, Required: true},
			},
			Rows: []table.Row{
				{Values: map[string]table.Value{
					"variant": {Type: table.ColumnTypeString, String: "A"},
					"action":  {Type: table.ColumnTypeString, String: "unhide"},
				}},
				{Values: map[string]table.Value{
					"variant": {Type: table.ColumnTypeString, String: "B"},
					"action":  {Type: table.ColumnTypeString, String: "delete"},
				}},
			},
		},
	}
	model := plannerRoutingModel(t)
	dslContent := `
product Demo {
    param variant: string = "A"
    target Pad: action = table_cell("variants", variant, "action")
}`
	rowA, err := projectTargetActionFamiliesForTest(t, dslContent, nil, tables, model)
	if err != nil {
		t.Fatalf("row A: unexpected error: %v", err)
	}
	if len(rowA.Part.Visibility) != 1 || !rowA.Part.Visibility[0].Visible {
		t.Fatalf("row A: expected unhide -> Visible=true, got %#v", rowA.Part)
	}
	rowB, err := projectTargetActionFamiliesForTest(t, dslContent, map[string]string{"variant": "B"}, tables, model)
	if err != nil {
		t.Fatalf("row B: unexpected error: %v", err)
	}
	if len(rowB.Part.Deletion) != 1 {
		t.Fatalf("row B: expected delete -> deletion family, got %#v", rowB.Part)
	}
}

// 64. Override-selected integration: the override changes the final action and
// therefore the projected family; Task 10 sees only the routed canonical result.
func TestPlannerTargetMutationProjection_OverrideSelectedIntegration(t *testing.T) {
	model := plannerRoutingModel(t)
	dslContent := `
product Demo {
    param mode: string = "hide"
    target Pad: action = mode == "hide" ? hide : suppress
}`
	def, err := projectTargetActionFamiliesForTest(t, dslContent, nil, nil, model)
	if err != nil {
		t.Fatalf("default: unexpected error: %v", err)
	}
	if len(def.Part.Visibility) != 1 {
		t.Fatalf("default: expected visibility family, got %#v", def.Part)
	}
	overridden, err := projectTargetActionFamiliesForTest(t, dslContent, map[string]string{"mode": "off"}, nil, model)
	if err != nil {
		t.Fatalf("override: unexpected error: %v", err)
	}
	if len(overridden.Part.Suppression) != 1 || len(overridden.Part.Visibility) != 0 {
		t.Fatalf("override: expected suppression family, got %#v", overridden.Part)
	}
}

// 65. Task 7 capability denial fails before Task 10; no family projection.
func TestPlannerTargetMutationProjection_CapabilityDenialPreventsProjection(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad",
		Targetability: cad.Targetability{Hide: false},
	}})
	_, err := projectTargetActionFamiliesForTest(t, `
product Demo {
    target Pad: action = hide
}`, nil, nil, model)
	if err == nil {
		t.Fatal("expected Task 7 capability denial before Task 10")
	}
	if !strings.Contains(err.Error(), "requires captured Targetability.Hide=true") {
		t.Fatalf("expected Task 7 diagnostic, got %q", err.Error())
	}
}

// 66. Task 6 resolution failure fails before Task 10; no family projection.
func TestPlannerTargetMutationProjection_ResolutionFailurePreventsProjection(t *testing.T) {
	model := plannerRoutingModel(t)
	_, err := projectTargetActionFamiliesForTest(t, `
product Demo {
    target Missing: action = suppress
}`, nil, nil, model)
	if err == nil {
		t.Fatal("expected Task 6 resolution failure before Task 10")
	}
	if !strings.Contains(err.Error(), "does not resolve to a Feature or Component by exact Name") {
		t.Fatalf("expected Task 6 diagnostic, got %q", err.Error())
	}
}

// 67. Task 5 invalid action fails before Task 10; no family projection.
func TestPlannerTargetMutationProjection_InvalidActionPreventsProjection(t *testing.T) {
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target Pad: action = explode
}`)
	model := plannerRoutingModel(t)
	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected Task 5 invalid-action failure before Task 10")
	}
	if !strings.Contains(err.Error(), "invalid action value 'explode'") {
		t.Fatalf("expected Task 5 diagnostic, got %q", err.Error())
	}
}

// 53. A product with no target actions retains the committed zero
// representation: no private Task 10 collection is allocated.
func TestPlannerTargetMutationProjection_ZeroTargetActionsProducesNil(t *testing.T) {
	model := plannerRoutingModel(t)
	families := projectFamiliesViaProjection(t, model, nil)
	if families != nil {
		t.Fatalf("expected nil private Task 10 collections for zero target actions, got %#v", families)
	}
}

// 54. keep-only authoring produces no Task 8 intent, no Task 9 route, and no
// Task 10 family projection. No identity-equivalence claim vs declaration
// omission.
func TestPlannerTargetMutationProjection_KeepOnlyProducesNoProjection(t *testing.T) {
	model := plannerRoutingModel(t)
	mutations, err := lowerTargetActionsForTest(t, `
product Demo {
    target Pad: action = keep
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mutations) != 0 {
		t.Fatalf("expected zero lowered intents for keep, got %#v", mutations)
	}
	result := projectSemanticManifestResultForTest(t, model, mutations)
	if result.TargetActionRouting != nil {
		t.Fatalf("expected nil Task 9 route for keep-only, got %#v", result.TargetActionRouting)
	}
	if result.TargetActionMutationCollections != nil {
		t.Fatalf("expected nil Task 10 collections for keep-only, got %#v", result.TargetActionMutationCollections)
	}
}

// ---------------------------------------------------------------------------
// PART I — public / runtime non-leakage
// ---------------------------------------------------------------------------

func task10LeakDSL() string {
	return `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step", "csv"]

    target Keyway: action = suppress
    target Cover: action = hide
    target Rail: action = delete
    target Latch: action = unhide
}
`
}

func task10LeakModel() *semantic.Model {
	return &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly"},
			{ID: "cmp.part", Kind: "part", Name: "Leg", ParentID: "cmp.root"},
			{ID: "cmp.cover", Kind: "part", Name: "Cover", ParentID: "cmp.root", Targetability: allBitsTrue()},
		},
		Features: []semantic.Feature{
			{ID: "feat.keyway", ComponentID: "cmp.part", Name: "Keyway", NativeType: "PartDesign::Pocket", Targetability: allBitsTrue()},
			{ID: "feat.rail", ComponentID: "cmp.root", Name: "Rail", NativeType: "PartDesign::Pad", Targetability: allBitsTrue()},
			{ID: "feat.latch", ComponentID: "cmp.root", Name: "Latch", NativeType: "PartDesign::Pad", Targetability: allBitsTrue()},
		},
	}
}

// 68 - 72. Private suppress / visibility / deletion families exist for the
// authored target actions, but the full capture-backed plan exposes none of
// them: the public export-manifest mutation collections are untouched and no
// Task 10 family data appears anywhere in serialized plan output.
func TestPlannerTargetMutationProjection_PrivateFamiliesNeverLeakIntoPublicPlan(t *testing.T) {
	contract := testProjectionContract()
	model := task10LeakModel()

	_, plan := parseValidateAndPlanCaptureBacked(t, task10LeakDSL(), model, contract)
	if ExportManifestSchemaVersion != "1.0" {
		t.Fatalf("schema-1 compatibility constant changed: %q", ExportManifestSchemaVersion)
	}

	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	for _, forbidden := range []string{
		"targetActionMutationCollections", "TargetActionMutationCollections",
		"ProjectedTargetMutationRouting", "projectedTargetMutationRouting",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("plan JSON leaked Task 10 surface %q: %s", forbidden, string(raw))
		}
	}
}

// 73. No public plan / manifest payload type carries a Task 10 mutation-family
// field, and the public export mutation collection still has exactly the three
// pre-Task-10 families.
func TestPlannerTargetMutationProjection_NoPublicTask10Field(t *testing.T) {
	assertNoTask10FieldInType(t, reflect.TypeOf(ExecutionPlan{}))
	assertNoTask10FieldInType(t, reflect.TypeOf(WriteExportManifestPayload{}))

	rt := reflect.TypeOf(ExportManifestMutationCollection{})
	want := []string{"Parameters", "Properties", "Suppression", "Visibility", "Deletion"}
	if rt.NumField() != len(want) {
		t.Fatalf("public ExportManifestMutationCollection field count changed: want %d, got %d", len(want), rt.NumField())
	}
	for i, name := range want {
		if rt.Field(i).Name != name {
			t.Fatalf("public ExportManifestMutationCollection field %d: want %q, got %q", i, name, rt.Field(i).Name)
		}
	}
}

func assertNoTask10FieldInType(t *testing.T, typ reflect.Type) {
	t.Helper()
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			lower := strings.ToLower(f.Name + " " + f.Tag.Get("json"))
			for _, needle := range []string{
				"targetactionmutationcollection", "projectedtargetmutation", "mutationfamil",
			} {
				if strings.Contains(lower, needle) {
					t.Fatalf("type %s field %q exposes a Task 10 mutation-family surface (tag %q)", rt.Name(), f.Name, f.Tag.Get("json"))
				}
			}
			walk(f.Type)
		}
	}
	walk(typ)
}

// ---------------------------------------------------------------------------
// determinism
// ---------------------------------------------------------------------------

// 108. The private Task 9 -> 10 seam is deterministic for a representative mixed
// Part + Assembly five-operation product across repeats.
func TestPlannerTargetMutationProjection_MixedProjectionDeterminism(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo",
		[]semantic.Component{
			{ID: "cmp.leg", Kind: "part", Name: "Leg", ChildrenIDs: []string{}, Quantity: 1, Targetability: allBitsTrue()},
			{ID: "cmp.cover", Kind: "part", Name: "Cover", ChildrenIDs: []string{}, Quantity: 1, Targetability: allBitsTrue()},
			{ID: "cmp.sub", Kind: "assembly", Name: "Sub", ChildrenIDs: []string{}, Quantity: 1, Targetability: allBitsTrue()},
		},
		[]semantic.Feature{
			{ID: "feat.pad", ComponentID: "cmp.leg", Name: "Pad", Targetability: allBitsTrue()},
			{ID: "feat.bracket", ComponentID: "cmp.root", Name: "Bracket", Targetability: allBitsTrue()},
		})
	dslContent := `
product Demo {
    target Pad: action = suppress
    target Sub: action = hide
    target Cover: action = delete
    target Bracket: action = unsuppress
    target Leg: action = unhide
}`
	first, err := projectTargetActionFamiliesForTest(t, dslContent, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 10; i++ {
		got, err := projectTargetActionFamiliesForTest(t, dslContent, nil, nil, model)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("iteration %d: non-deterministic Task 10 projection\nfirst: %#v\ngot:   %#v", i, first, got)
		}
	}
}
