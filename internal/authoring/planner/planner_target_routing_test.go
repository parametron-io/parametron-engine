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

// Phase 5 Task 9 planner integration: these tests prove the generic
// part/assembly target-mutation routing step (semanticmap.RouteTargetMutations,
// invoked by projectSemanticManifestIntent — the exact function createPlan calls
// to build the capture-backed manifest projection) is wired into the planner at
// the correct pipeline seam, immediately after Task 8 semantic lowering:
//
//	Task 5 action evaluation
//	    -> Task 6 semantic target resolution
//	        -> Task 7 captured-Targetability capability validation
//	            -> Task 8 semantic MutationIntent lowering
//	                -> Task 9 destination + native-object routing
//
// The routed result lives only on the private
// semanticManifestProjectionResult.TargetActionRouting seam. Task 9 performs no
// runtime projection, adds no ExecutionPlan / WriteExportManifestPayload field,
// never leaks into public plan JSON, and never enters the existing public
// PartMutations / AssemblyMutations / Suppression surfaces.

// routeViaProjection runs the real production Task 9 seam: it calls
// projectSemanticManifestIntent (the function createPlan invokes for
// capture-backed products) with the lowered Task 8 mutations and returns the
// private routed result. An empty Intent keeps the unrelated output / parameter
// / property projection inert so the assertion is about routing only.
func routeViaProjection(t *testing.T, model *semantic.Model, mutations []semantic.MutationIntent) *semanticmap.TargetMutationRouting {
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
	return result.TargetActionRouting
}

// routeTargetActionsForTest composes the real Task 5 -> 6 -> 7 -> 8 planner
// seams (lowerTargetActionsForTest) with the real Task 9 seam
// (projectSemanticManifestIntent). It is the full capture-backed preparation
// path without forcing public runtime emission.
func routeTargetActionsForTest(t *testing.T, dslContent string, overrides map[string]string, tables map[string]*table.Table, model *semantic.Model) (*semanticmap.TargetMutationRouting, error) {
	t.Helper()
	mutations, err := lowerTargetActionsForTest(t, dslContent, overrides, tables, model)
	if err != nil {
		return nil, err
	}
	return routeViaProjection(t, model, mutations), nil
}

// plannerRoutingModel builds a model rooted at cmp.root (assembly) with a
// top-level part "Leg" and a top-level assembly "Sub", plus features attached to
// each so that Feature routing can be exercised in both the part-scope and
// assembly-scope fallback directions.
func plannerRoutingModel(t *testing.T) *semantic.Model {
	t.Helper()
	return plannerSemanticModelWithProductIntent(t, "Demo",
		[]semantic.Component{
			{ID: "cmp.leg", Kind: "part", Name: "Leg", ChildrenIDs: []string{}, Quantity: 1, Targetability: allBitsTrue()},
			{ID: "cmp.sub", Kind: "assembly", Name: "Sub", ChildrenIDs: []string{}, Quantity: 1, Targetability: allBitsTrue()},
		},
		[]semantic.Feature{
			{ID: "feat.pad", ComponentID: "cmp.leg", Name: "Pad", Targetability: allBitsTrue()},
			{ID: "feat.bracket", ComponentID: "cmp.root", Name: "Bracket", Targetability: allBitsTrue()},
		})
}

// 49 / 66. The private Task 9 route is live production state derived from the
// Task 8 mutations: the full capture-backed Task 5 -> 9 path yields a non-empty
// routed result.
func TestPlannerTargetRouting_CaptureBackedHappyPathPopulatesPrivateRoute(t *testing.T) {
	model := plannerRoutingModel(t)
	routing, err := routeTargetActionsForTest(t, `
product Demo {
    target Pad: action = suppress
    target Sub: action = delete
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if routing == nil {
		t.Fatal("expected a populated private Task 9 route")
	}
	if len(routing.Part) != 1 || len(routing.Assembly) != 1 {
		t.Fatalf("expected one part + one assembly route, got %#v", routing)
	}
	if routing.Part[0] != (semanticmap.RoutedTargetMutation{OperationKind: "suppress", Object: "Pad"}) {
		t.Fatalf("unexpected part route: %#v", routing.Part[0])
	}
	if routing.Assembly[0] != (semanticmap.RoutedTargetMutation{OperationKind: "delete", Object: "Sub"}) {
		t.Fatalf("unexpected assembly route: %#v", routing.Assembly[0])
	}
}

// 50. Feature with part Scope routes to Part with the native Feature.Name.
func TestPlannerTargetRouting_FeaturePartScopeRoutesToPart(t *testing.T) {
	model := plannerRoutingModel(t)
	routing, err := routeTargetActionsForTest(t, `
product Demo {
    target Pad: action = hide
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routing.Part) != 1 || len(routing.Assembly) != 0 {
		t.Fatalf("expected Feature part-scope route into Part, got %#v", routing)
	}
	if routing.Part[0] != (semanticmap.RoutedTargetMutation{OperationKind: "hide", Object: "Pad"}) {
		t.Fatalf("unexpected part route: %#v", routing.Part[0])
	}
}

// 51. Feature with assembly Scope routes to Assembly (Feature attached to the
// assembly root component).
func TestPlannerTargetRouting_FeatureAssemblyScopeRoutesToAssembly(t *testing.T) {
	model := plannerRoutingModel(t)
	routing, err := routeTargetActionsForTest(t, `
product Demo {
    target Bracket: action = unsuppress
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routing.Assembly) != 1 || len(routing.Part) != 0 {
		t.Fatalf("expected Feature assembly-scope route into Assembly, got %#v", routing)
	}
	if routing.Assembly[0] != (semanticmap.RoutedTargetMutation{OperationKind: "unsuppress", Object: "Bracket"}) {
		t.Fatalf("unexpected assembly route: %#v", routing.Assembly[0])
	}
}

// 52. part Component routes to Part with the native Component.Name through the
// mandatory identity linkage.
func TestPlannerTargetRouting_PartComponentRoutesToPart(t *testing.T) {
	model := plannerRoutingModel(t)
	routing, err := routeTargetActionsForTest(t, `
product Demo {
    target Leg: action = suppress
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routing.Part) != 1 || len(routing.Assembly) != 0 {
		t.Fatalf("expected part Component route into Part, got %#v", routing)
	}
	if routing.Part[0] != (semanticmap.RoutedTargetMutation{OperationKind: "suppress", Object: "Leg"}) {
		t.Fatalf("unexpected part route: %#v", routing.Part[0])
	}
}

// 53. assembly Component routes to Assembly with the native Component.Name.
func TestPlannerTargetRouting_AssemblyComponentRoutesToAssembly(t *testing.T) {
	model := plannerRoutingModel(t)
	routing, err := routeTargetActionsForTest(t, `
product Demo {
    target Sub: action = delete
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routing.Assembly) != 1 || len(routing.Part) != 0 {
		t.Fatalf("expected assembly Component route into Assembly, got %#v", routing)
	}
	if routing.Assembly[0] != (semanticmap.RoutedTargetMutation{OperationKind: "delete", Object: "Sub"}) {
		t.Fatalf("unexpected assembly route: %#v", routing.Assembly[0])
	}
}

// 59. Literal action integration: the evaluated canonical action reaches Task 9
// as the exact routed OperationKind.
func TestPlannerTargetRouting_LiteralActionIntegration(t *testing.T) {
	model := plannerRoutingModel(t)
	routing, err := routeTargetActionsForTest(t, `
product Demo {
    target Pad: action = suppress
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if routing.Part[0].OperationKind != "suppress" {
		t.Fatalf("expected suppress, got %#v", routing.Part[0])
	}
}

// 60. Ternary integration: Task 9 routes only the final selected OperationKind.
func TestPlannerTargetRouting_TernaryActionIntegration(t *testing.T) {
	model := plannerRoutingModel(t)
	dslContent := `
product Demo {
    param flag: boolean = true
    target Pad: action = flag ? suppress : hide
}`
	gotTrue, err := routeTargetActionsForTest(t, dslContent, nil, nil, model)
	if err != nil {
		t.Fatalf("flag=true: unexpected error: %v", err)
	}
	if gotTrue.Part[0].OperationKind != "suppress" {
		t.Fatalf("flag=true: expected suppress, got %#v", gotTrue.Part[0])
	}
	gotFalse, err := routeTargetActionsForTest(t, dslContent, map[string]string{"flag": "false"}, nil, model)
	if err != nil {
		t.Fatalf("flag=false: unexpected error: %v", err)
	}
	if gotFalse.Part[0].OperationKind != "hide" {
		t.Fatalf("flag=false: expected hide, got %#v", gotFalse.Part[0])
	}
}

// 61 / 62. Table-backed and override-selected integration: Task 9 sees only the
// final Task 8 result, never an AST default.
func TestPlannerTargetRouting_TableAndOverrideActionIntegration(t *testing.T) {
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
					"action":  {Type: table.ColumnTypeString, String: "suppress"},
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
	def, err := routeTargetActionsForTest(t, dslContent, nil, tables, model)
	if err != nil {
		t.Fatalf("default row: unexpected error: %v", err)
	}
	if def.Part[0].OperationKind != "suppress" {
		t.Fatalf("default row A: expected suppress, got %#v", def.Part[0])
	}
	overridden, err := routeTargetActionsForTest(t, dslContent, map[string]string{"variant": "B"}, tables, model)
	if err != nil {
		t.Fatalf("override row: unexpected error: %v", err)
	}
	if overridden.Part[0].OperationKind != "delete" {
		t.Fatalf("override row B: expected delete, got %#v", overridden.Part[0])
	}
}

// 63 / 64. Mixed Feature/Component product split across part and assembly:
// both private buckets carry the native names in authored-relative order.
func TestPlannerTargetRouting_MixedFeatureComponentProductPreservesAuthoredOrder(t *testing.T) {
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
	routing, err := routeTargetActionsForTest(t, `
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
	wantPart := []semanticmap.RoutedTargetMutation{
		{OperationKind: "suppress", Object: "Pad"},
		{OperationKind: "delete", Object: "Cover"},
		{OperationKind: "unhide", Object: "Leg"},
	}
	wantAssembly := []semanticmap.RoutedTargetMutation{
		{OperationKind: "hide", Object: "Sub"},
		{OperationKind: "unsuppress", Object: "Bracket"},
	}
	if !reflect.DeepEqual(routing.Part, wantPart) {
		t.Fatalf("part bucket\nwant: %#v\ngot:  %#v", wantPart, routing.Part)
	}
	if !reflect.DeepEqual(routing.Assembly, wantAssembly) {
		t.Fatalf("assembly bucket\nwant: %#v\ngot:  %#v", wantAssembly, routing.Assembly)
	}
}

// 54. Task 5 invalid action fails before Task 9; no route is produced.
func TestPlannerTargetRouting_InvalidActionPreventsRouting(t *testing.T) {
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target Pad: action = explode
}`)
	model := plannerRoutingModel(t)
	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected Task 5 invalid-action failure")
	}
	if !strings.Contains(err.Error(), "invalid action value 'explode'") {
		t.Fatalf("expected Task 5 diagnostic, got %q", err.Error())
	}
}

// 55 / 56. Task 6 missing / ambiguous target fails before Task 9.
func TestPlannerTargetRouting_MissingTargetPreventsRouting(t *testing.T) {
	model := plannerRoutingModel(t)
	_, err := routeTargetActionsForTest(t, `
product Demo {
    target Missing: action = suppress
}`, nil, nil, model)
	if err == nil {
		t.Fatal("expected Task 6 not-found before routing")
	}
	if !strings.Contains(err.Error(), "does not resolve to a Feature or Component by exact Name") {
		t.Fatalf("expected Task 6 diagnostic, got %q", err.Error())
	}
}

func TestPlannerTargetRouting_AmbiguousTargetPreventsRouting(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo",
		[]semantic.Component{
			{ID: "cmp.dup", Kind: "part", Name: "Dup", ChildrenIDs: []string{}, Quantity: 1, Targetability: allBitsTrue()},
		},
		[]semantic.Feature{
			{ID: "feat.dup", ComponentID: "cmp.root", Name: "Dup", Targetability: allBitsTrue()},
		})
	_, err := routeTargetActionsForTest(t, `
product Demo {
    target Dup: action = suppress
}`, nil, nil, model)
	if err == nil {
		t.Fatal("expected Task 6 ambiguity before routing")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected Task 6 ambiguity diagnostic, got %q", err.Error())
	}
}

// 57. Task 7 capability denial fails before Task 9; no route.
func TestPlannerTargetRouting_CapabilityDenialPreventsRouting(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad",
		Targetability: cad.Targetability{Suppress: false},
	}})
	_, err := routeTargetActionsForTest(t, `
product Demo {
    target Pad: action = suppress
}`, nil, nil, model)
	if err == nil {
		t.Fatal("expected Task 7 capability denial before routing")
	}
	if !strings.Contains(err.Error(), "requires captured Targetability.Suppress=true") {
		t.Fatalf("expected Task 7 diagnostic, got %q", err.Error())
	}
}

// 58. keep produces no Task 9 route: zero lowered intents leave
// TargetActionRouting nil (the committed zero-input compatibility shape).
func TestPlannerTargetRouting_KeepProducesNoRoute(t *testing.T) {
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
	routing := routeViaProjection(t, model, mutations)
	if routing != nil {
		t.Fatalf("expected nil Task 9 route for zero mutations, got %#v", routing)
	}
}

// 67 - 80. Full capture-backed planning with authored target actions never
// leaks the private Task 9 route into the public plan, the export manifest
// payload, the existing PartMutations / AssemblyMutations / Suppression
// surfaces, the ExecutionPlan type, or the manifest schema version.
func TestPlannerTargetRouting_PrivateRouteNeverLeaksIntoPublicPlan(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step", "csv"]

    target Keyway: action = suppress
    target Cover: action = hide
    target Rail: action = delete
}
`
	contract := testProjectionContract()
	model := &semantic.Model{
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
		},
	}

	_, plan := parseValidateAndPlanCaptureBacked(t, dslContent, model, contract)

	// No private routing markers anywhere in serialized plan output.
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	for _, forbidden := range []string{
		"targetActionRouting", "TargetActionRouting",
		"routedTargetMutations", "RoutedTargetMutation",
		"targetMutationRouting", "TargetMutationRouting",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("plan JSON leaked %q: %s", forbidden, string(raw))
		}
	}

	// The routed type is not a field of any public plan / manifest payload type.
	assertNoRoutingFieldInType(t, reflect.TypeOf(ExecutionPlan{}))
	assertNoRoutingFieldInType(t, reflect.TypeOf(WriteExportManifestPayload{}))
}

// assertNoRoutingFieldInType recursively walks a struct type and fails if any
// field name or JSON tag names a Task 9 routing surface.
func assertNoRoutingFieldInType(t *testing.T, typ reflect.Type) {
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
			for _, needle := range []string{"routedtarget", "targetmutationrouting", "targetactionrouting", "routedmutation"} {
				if strings.Contains(lower, needle) {
					t.Fatalf("type %s field %q exposes a Task 9 routing surface (tag %q)", rt.Name(), f.Name, f.Tag.Get("json"))
				}
			}
			walk(f.Type)
		}
	}
	walk(typ)
}
