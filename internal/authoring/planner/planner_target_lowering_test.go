package planner

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/cad"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/table"
)

// Phase 5 Task 8 planner integration: these tests prove the semantic
// MutationIntent lowering step (semantic.LowerTargetActionMutationIntent, run
// per product through lowerResolvedTargetActions) is wired into createPlan at
// the correct pipeline seam, immediately after Task 7 capability validation:
//
//	Task 5 action evaluation
//	    -> Task 6 semantic target resolution
//	        -> Task 7 captured-Targetability capability validation
//	            -> Task 8 semantic MutationIntent lowering
//
// The lowered intents are retained only on the private productDraft
// .targetActionMutations seam inside createPlan. Task 8 performs no runtime
// projection, adds no ExecutionPlan field, and never appears in public plan
// JSON or the export manifest. Feature and Component share the exact same
// lowering helper; keep is filtered without disturbing authored order.

// lowerTargetActionsForTest composes the real (unexported) Task 5 -> 6 -> 7 ->
// 8 seams in the exact order createPlan runs them internally:
// evaluateProductTargetActions, resolveEvaluatedTargetActions,
// validateResolvedTargetActionCapabilities, then lowerResolvedTargetActions.
// lowerResolvedTargetActions is the identical function createPlan invokes to
// populate productDraft.targetActionMutations, so a non-empty result here
// proves the Task 8 seam is reachable production code, not dead code. A full
// CreatePlanWithTablesAndSemanticModel success path additionally requires the
// unrelated capture-backed export-manifest projection, so the composed seam is
// the preferred entry point for success-path Task 8 assertions (matching the
// Task 6/7 planner-test precedent).
func lowerTargetActionsForTest(t *testing.T, dslContent string, overrides map[string]string, tables map[string]*table.Table, model *semantic.Model) ([]semantic.MutationIntent, error) {
	t.Helper()

	resolved, err := resolveTargetActionsForTest(t, dslContent, overrides, tables, model)
	if err != nil {
		return nil, err
	}
	if err := validateResolvedTargetActionCapabilities(resolved); err != nil {
		return nil, err
	}
	return lowerResolvedTargetActions(resolved)
}

// lrAction builds a resolved target action for direct lowerResolvedTargetActions
// list tests, bypassing DSL/model plumbing.
func lrAction(name, id, action, targetEntityKind, scope string) resolvedTargetAction {
	return resolvedTargetAction{
		SemanticTarget: name,
		Action:         action,
		Target: semantic.ResolvedSemanticTarget{
			SemanticEntityKind: "feature",
			TargetEntityKind:   targetEntityKind,
			SemanticID:         id,
			Scope:              scope,
		},
	}
}

func allBitsTrue() cad.Targetability {
	return cad.Targetability{Suppress: true, Unsuppress: true, Hide: true, Unhide: true, Delete: true}
}

// 23. Shared list lowerer, keep only: a single keep action produces a
// zero-length intent slice with no nil pointer entry and no OperationKind
// "keep".
func TestLowerResolvedTargetActions_KeepOnlyProducesNoIntent(t *testing.T) {
	got, err := lowerResolvedTargetActions([]resolvedTargetAction{
		lrAction("Pad", "feat.pad", "keep", "feature", "part"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected zero lowered intents for keep-only input, got %#v", got)
	}
}

// 24. Shared list lowerer, all five mutation-producing actions: five intents,
// same order, exact OperationKinds.
func TestLowerResolvedTargetActions_AllFiveMutationProducingActions(t *testing.T) {
	got, err := lowerResolvedTargetActions([]resolvedTargetAction{
		lrAction("Pad", "feat.pad", "suppress", "feature", "part"),
		lrAction("Pocket", "feat.pocket", "unsuppress", "feature", "part"),
		lrAction("Body", "feat.body", "hide", "feature", "part"),
		lrAction("Cover", "cmp.cover", "unhide", "part", "part"),
		lrAction("Chamfer", "feat.chamfer", "delete", "feature", "part"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantKinds := []string{"suppress", "unsuppress", "hide", "unhide", "delete"}
	if len(got) != len(wantKinds) {
		t.Fatalf("expected %d intents, got %d (%#v)", len(wantKinds), len(got), got)
	}
	for i, want := range wantKinds {
		if got[i].OperationKind != want {
			t.Fatalf("intent[%d]: expected OperationKind %q, got %q", i, want, got[i].OperationKind)
		}
		if got[i].ValueSource.Kind != "target_action" {
			t.Fatalf("intent[%d]: expected ValueSource.Kind \"target_action\", got %q", i, got[i].ValueSource.Kind)
		}
		if got[i].ValueSource.BooleanState != nil {
			t.Fatalf("intent[%d]: expected nil BooleanState", i)
		}
	}
}

// 25 / 26. Authored order is preserved and keep actions are filtered without
// reordering the remaining intents.
func TestLowerResolvedTargetActions_AuthoredOrderWithKeepFiltering(t *testing.T) {
	got, err := lowerResolvedTargetActions([]resolvedTargetAction{
		lrAction("A", "feat.a", "suppress", "feature", "part"),
		lrAction("B", "feat.b", "keep", "feature", "part"),
		lrAction("C", "feat.c", "delete", "feature", "part"),
		lrAction("D", "feat.d", "keep", "feature", "part"),
		lrAction("E", "feat.e", "hide", "feature", "part"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantKinds := []string{"suppress", "delete", "hide"}
	wantIDs := []string{"feat.a", "feat.c", "feat.e"}
	if len(got) != 3 {
		t.Fatalf("expected 3 intents after keep filtering, got %d (%#v)", len(got), got)
	}
	for i := range wantKinds {
		if got[i].OperationKind != wantKinds[i] || got[i].TargetSemanticID != wantIDs[i] {
			t.Fatalf("intent[%d]: expected (%q,%q), got (%q,%q)", i, wantKinds[i], wantIDs[i], got[i].OperationKind, got[i].TargetSemanticID)
		}
	}
}

// 27. Every mutation-producing action: one approved target action -> exactly
// one MutationIntent, no duplication.
func TestLowerResolvedTargetActions_OneActionOneIntent(t *testing.T) {
	for _, action := range []string{"suppress", "unsuppress", "hide", "unhide", "delete"} {
		got, err := lowerResolvedTargetActions([]resolvedTargetAction{
			lrAction("Pad", "feat.pad", action, "feature", "part"),
		})
		if err != nil {
			t.Fatalf("action %q: unexpected error: %v", action, err)
		}
		if len(got) != 1 || got[0].OperationKind != action {
			t.Fatalf("action %q: expected exactly one %q intent, got %#v", action, action, got)
		}
	}
}

// 28. No list deduplication: two different resolved target identities carrying
// the same action produce two intents.
func TestLowerResolvedTargetActions_NoDeduplication(t *testing.T) {
	got, err := lowerResolvedTargetActions([]resolvedTargetAction{
		lrAction("Pad", "feat.pad", "suppress", "feature", "part"),
		lrAction("Pocket", "feat.pocket", "suppress", "feature", "part"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 intents (no merge on matching OperationKind), got %d (%#v)", len(got), got)
	}
	if got[0].TargetSemanticID != "feat.pad" || got[1].TargetSemanticID != "feat.pocket" {
		t.Fatalf("expected distinct preserved target identities, got %#v", got)
	}
}

// 29. Feature planner lowering: a resolved+approved Feature action lowers to
// one exact Feature MutationIntent, through the shared helper (no
// Feature-specific path).
func TestPlannerTargetLowering_FeatureExactIntent(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad",
		Targetability: cad.Targetability{Suppress: true},
	}})
	got, err := lowerTargetActionsForTest(t, `
product Demo {
    target Pad: action = suppress
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []semantic.MutationIntent{{
		OperationKind:    "suppress",
		TargetEntityKind: "feature",
		TargetSemanticID: "feat.pad",
		TargetField:      "",
		Scope:            "assembly",
		ValueSource:      semantic.MutationValueSource{Kind: "target_action"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("feature lowering mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

// 30. Component planner lowering: a resolved+approved Component action lowers
// to one exact Component MutationIntent through the same helper.
func TestPlannerTargetLowering_ComponentExactIntent(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", []semantic.Component{{
		ID: "cmp.cover", Kind: "part", Name: "Cover", ChildrenIDs: []string{}, Quantity: 1,
		Targetability: cad.Targetability{Hide: true},
	}}, nil)
	got, err := lowerTargetActionsForTest(t, `
product Demo {
    target Cover: action = hide
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []semantic.MutationIntent{{
		OperationKind:    "hide",
		TargetEntityKind: "part",
		TargetSemanticID: "cmp.cover",
		TargetField:      "",
		Scope:            "part",
		ValueSource:      semantic.MutationValueSource{Kind: "target_action"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("component lowering mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

// 31. assembly Component preservation: an assembly-kind Component target keeps
// TargetEntityKind=assembly and Scope=assembly in the private semantic
// MutationIntent. No assembly-routing assertion (Task 9+ owns destination
// projection).
func TestPlannerTargetLowering_AssemblyComponentPreservation(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", []semantic.Component{{
		ID: "cmp.sub", Kind: "assembly", Name: "Sub", ChildrenIDs: []string{}, Quantity: 1,
		Targetability: cad.Targetability{Delete: true},
	}}, nil)
	got, err := lowerTargetActionsForTest(t, `
product Demo {
    target Sub: action = delete
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one intent, got %#v", got)
	}
	if got[0].TargetEntityKind != "assembly" || got[0].Scope != "assembly" {
		t.Fatalf("expected assembly TargetEntityKind/Scope preserved, got %#v", got[0])
	}
}

// 32. Capability denial (Task 7) occurs before lowering: a required
// Targetability bit that is false produces a Task 7 error and no Task 8 intent
// or diagnostic.
func TestPlannerTargetLowering_CapabilityDenialBeforeLowering(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad",
		Targetability: cad.Targetability{Suppress: false},
	}})
	got, err := lowerTargetActionsForTest(t, `
product Demo {
    target Pad: action = suppress
}`, nil, nil, model)
	if err == nil {
		t.Fatal("expected Task 7 capability denial before lowering")
	}
	if got != nil {
		t.Fatalf("expected no lowered intents on capability denial, got %#v", got)
	}
	if !strings.Contains(err.Error(), "requires captured Targetability.Suppress=true") {
		t.Fatalf("expected Task 7 capability diagnostic, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "semantic mutation lowering") {
		t.Fatalf("expected no Task 8 lowering diagnostic, got %q", err.Error())
	}
}

// 33. Missing target (Task 6) occurs before lowering.
func TestPlannerTargetLowering_MissingTargetBeforeLowering(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, nil)
	_, err := lowerTargetActionsForTest(t, `
product Demo {
    target Missing: action = suppress
}`, nil, nil, model)
	if err == nil {
		t.Fatal("expected Task 6 not-found before lowering")
	}
	if !strings.Contains(err.Error(), "does not resolve to a Feature or Component by exact Name") {
		t.Fatalf("expected Task 6 not-found diagnostic, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "semantic mutation lowering") {
		t.Fatalf("expected no Task 8 lowering diagnostic, got %q", err.Error())
	}
}

// 34 / 35. Invalid action (Task 5) occurs before lowering, through the full
// public entry point; no Task 8 diagnostic appears.
func TestPlannerTargetLowering_InvalidActionBeforeLowering(t *testing.T) {
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target Pad: action = explode
}`)
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad", Targetability: allBitsTrue(),
	}})
	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected Task 5 invalid-action failure before lowering")
	}
	if !strings.Contains(err.Error(), "invalid action value 'explode'") {
		t.Fatalf("expected Task 5 invalid-action diagnostic, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "semantic mutation lowering") || strings.Contains(err.Error(), "lowering target action mutations") {
		t.Fatalf("expected no Task 8 diagnostic, got %q", err.Error())
	}
}

// 36. Literal action integration: the evaluated canonical "suppress" flows
// through to the private Task 8 seam as OperationKind "suppress".
func TestPlannerTargetLowering_LiteralActionIntegration(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad", Targetability: allBitsTrue(),
	}})
	got, err := lowerTargetActionsForTest(t, `
product Demo {
    target Pad: action = suppress
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].OperationKind != "suppress" {
		t.Fatalf("expected one suppress intent, got %#v", got)
	}
}

// 37. Ternary-selected action integration: the lowerer consumes only the
// evaluated branch.
func TestPlannerTargetLowering_TernaryActionIntegration(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad", Targetability: allBitsTrue(),
	}})
	dslContent := `
product Demo {
    param flag: boolean = true
    target Pad: action = flag ? suppress : hide
}`
	gotTrue, err := lowerTargetActionsForTest(t, dslContent, nil, nil, model)
	if err != nil {
		t.Fatalf("flag=true: unexpected error: %v", err)
	}
	if len(gotTrue) != 1 || gotTrue[0].OperationKind != "suppress" {
		t.Fatalf("flag=true: expected suppress, got %#v", gotTrue)
	}
	gotFalse, err := lowerTargetActionsForTest(t, dslContent, map[string]string{"flag": "false"}, nil, model)
	if err != nil {
		t.Fatalf("flag=false: unexpected error: %v", err)
	}
	if len(gotFalse) != 1 || gotFalse[0].OperationKind != "hide" {
		t.Fatalf("flag=false: expected hide, got %#v", gotFalse)
	}
}

// 38 / 39. Table-backed and override-selected action integration: the final
// evaluated action (default row or override-steered row) drives Task 8.
func TestPlannerTargetLowering_TableAndOverrideActionIntegration(t *testing.T) {
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
					"action":  {Type: table.ColumnTypeString, String: "hide"},
				}},
			},
		},
	}
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad", Targetability: allBitsTrue(),
	}})
	dslContent := `
product Demo {
    param variant: string = "A"
    target Pad: action = table_cell("variants", variant, "action")
}`

	def, err := lowerTargetActionsForTest(t, dslContent, nil, tables, model)
	if err != nil {
		t.Fatalf("default row: unexpected error: %v", err)
	}
	if len(def) != 1 || def[0].OperationKind != "suppress" {
		t.Fatalf("default row A: expected suppress, got %#v", def)
	}

	overridden, err := lowerTargetActionsForTest(t, dslContent, map[string]string{"variant": "B"}, tables, model)
	if err != nil {
		t.Fatalf("override row: unexpected error: %v", err)
	}
	if len(overridden) != 1 || overridden[0].OperationKind != "hide" {
		t.Fatalf("override row B: expected hide, got %#v", overridden)
	}
}

// 40. keep through the full Task 5->8 seam produces zero intent. No plan-hash
// equivalence claim.
func TestPlannerTargetLowering_KeepThroughFullSeamProducesNoIntent(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad",
	}})
	got, err := lowerTargetActionsForTest(t, `
product Demo {
    target Pad: action = keep
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected zero intents for keep through the full seam, got %#v", got)
	}
}

// 41. Mixed expression forms preserve authored order: literal, table-backed,
// ternary, and literal actions interleaved with keep lower in authored order.
func TestPlannerTargetLowering_MixedExpressionFormsPreserveAuthoredOrder(t *testing.T) {
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
					"variant": {Type: table.ColumnTypeString, String: "K"},
					"action":  {Type: table.ColumnTypeString, String: "keep"},
				}},
			},
		},
	}
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{
		{ID: "feat.a", ComponentID: "cmp.root", Name: "Achamfer", Targetability: allBitsTrue()},
		{ID: "feat.b", ComponentID: "cmp.root", Name: "Bkeep", Targetability: allBitsTrue()},
		{ID: "feat.c", ComponentID: "cmp.root", Name: "Cdelete", Targetability: allBitsTrue()},
		{ID: "feat.d", ComponentID: "cmp.root", Name: "Dhide", Targetability: allBitsTrue()},
	})
	got, err := lowerTargetActionsForTest(t, `
product Demo {
    param flag: boolean = true
    target Achamfer: action = suppress
    target Bkeep: action = table_cell("variants", "K", "action")
    target Cdelete: action = flag ? delete : keep
    target Dhide: action = hide
}`, nil, tables, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantKinds := []string{"suppress", "delete", "hide"}
	wantIDs := []string{"feat.a", "feat.c", "feat.d"}
	if len(got) != len(wantKinds) {
		t.Fatalf("expected %d intents, got %d (%#v)", len(wantKinds), len(got), got)
	}
	for i := range wantKinds {
		if got[i].OperationKind != wantKinds[i] || got[i].TargetSemanticID != wantIDs[i] {
			t.Fatalf("intent[%d]: expected (%q,%q), got (%q,%q)", i, wantKinds[i], wantIDs[i], got[i].OperationKind, got[i].TargetSemanticID)
		}
	}
}

// 42 / 43. Model-free CreatePlan and CreatePlanWithTables never lower target
// actions into semantic MutationIntent and never raise a Task 8-specific
// failure, even for hide/delete target-action syntax.
func TestPlannerTargetLowering_ModelFreePlanningProducesNoSemanticTargetIntent(t *testing.T) {
	dslContent := `
product Demo {
    target Pad: action = delete
    target Cover: action = hide
}`
	ast := parseAndValidateWithTables(t, dslContent, nil)

	plan, err := CreatePlan(ast, nil)
	if err != nil {
		t.Fatalf("model-free CreatePlan: unexpected error: %v", err)
	}
	assertNoTargetActionLeakInPlanJSON(t, plan)

	ast2 := parseAndValidateWithTables(t, dslContent, nil)
	plan2, err := CreatePlanWithTables(ast2, nil, nil)
	if err != nil {
		t.Fatalf("model-free CreatePlanWithTables: unexpected error: %v", err)
	}
	assertNoTargetActionLeakInPlanJSON(t, plan2)
}

// 44. Private-seam reachability: lowerResolvedTargetActions (the exact function
// createPlan calls to fill productDraft.targetActionMutations) returns the
// lowered intents for approved mutation-producing actions. A non-empty result
// proves the Task 8 seam is live production code.
func TestPlannerTargetLowering_PrivateSeamIsReachableAndPopulated(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad", Targetability: allBitsTrue(),
	}})
	got, err := lowerTargetActionsForTest(t, `
product Demo {
    target Pad: action = delete
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].OperationKind != "delete" || got[0].TargetSemanticID != "feat.pad" {
		t.Fatalf("expected the private seam to carry one delete intent for feat.pad, got %#v", got)
	}
}

// 45. Target-action lowering does not append to the semantic model's existing
// ProductIntent.Mutations (the current projection-owned semantic-intent
// surface). Task 8 intents stay on the separate private seam.
func TestPlannerTargetLowering_DoesNotAppendToSemanticIntentMutations(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad", Targetability: allBitsTrue(),
	}})
	before := len(model.ProductIntents[0].Mutations)

	got, err := lowerTargetActionsForTest(t, `
product Demo {
    target Pad: action = suppress
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one lowered intent on the private return value, got %#v", got)
	}
	if after := len(model.ProductIntents[0].Mutations); after != before {
		t.Fatalf("target-action lowering mutated model.ProductIntents[0].Mutations: before=%d after=%d", before, after)
	}
}

// 46 - 50. No current semantic-map / runtime-manifest / ExecutionPlan / public
// plan JSON surface is created by Task 8, even for hide/delete actions the
// current projection does not own. The private lowered intents exist (proven
// by the composed seam), while the model-free and denial-safe plan surfaces
// stay clean.
func TestPlannerTargetLowering_NoRuntimeOrPlanSurfaceForHideDelete(t *testing.T) {
	// Private intents exist for hide + delete.
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{
		{ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad", Targetability: allBitsTrue()},
		{ID: "feat.body", ComponentID: "cmp.root", Name: "Body", Targetability: allBitsTrue()},
	})
	private, err := lowerTargetActionsForTest(t, `
product Demo {
    target Pad: action = delete
    target Body: action = hide
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("composed seam error: %v", err)
	}
	if len(private) != 2 {
		t.Fatalf("expected 2 private lowered intents, got %#v", private)
	}

	// Public plan surface (model-free planning of the same target actions)
	// carries no target-action / visibility / deletion surface.
	ast := parseAndValidateWithTables(t, `
product Demo {
    target Pad: action = delete
    target Body: action = hide
}`, nil)
	plan, err := CreatePlanWithTables(ast, nil, nil)
	if err != nil {
		t.Fatalf("CreatePlanWithTables: unexpected error: %v", err)
	}
	assertNoTargetActionLeakInPlanJSON(t, plan)

	// The manifest payload structurally has no target-action collection.
	assertNoTargetActionFieldInType(t, reflect.TypeOf(WriteExportManifestPayload{}))
	assertNoTargetActionFieldInType(t, reflect.TypeOf(ExecutionPlan{}))
}

// assertNoTargetActionLeakInPlanJSON marshals every step payload and asserts no
// target-action / visibility / deletion / targetActionMutations surface leaked
// into serialized plan output.
func assertNoTargetActionLeakInPlanJSON(t *testing.T, plan *ExecutionPlan) {
	t.Helper()
	forbidden := []string{
		"targetActionMutations",
		"targetActions",
		"semanticTargetActions",
		"target_action",
		"\"unsuppress\"",
		"\"unhide\"",
		"visibility",
		"deletion",
		"feat.pad",
		"feat.body",
	}
	for i, step := range plan.Steps {
		raw, err := json.Marshal(step.Payload)
		if err != nil {
			t.Fatalf("step %d (%s): marshal failed: %v", i, step.Type, err)
		}
		text := string(raw)
		for _, needle := range forbidden {
			if strings.Contains(text, needle) {
				t.Fatalf("step %d (%s): plan JSON leaked %q: %s", i, step.Type, needle, text)
			}
		}
	}
}

// assertNoTargetActionFieldInType recursively walks a struct type and fails if
// any field name or JSON tag names a target-action surface.
func assertNoTargetActionFieldInType(t *testing.T, typ reflect.Type) {
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
			for _, needle := range []string{"targetaction", "target_action", "semantictarget"} {
				if strings.Contains(lower, needle) {
					t.Fatalf("type %s field %q exposes a target-action surface (tag %q)", rt.Name(), f.Name, f.Tag.Get("json"))
				}
			}
			walk(f.Type)
		}
	}
	walk(typ)
}

// 22 (planner determinism). The composed Task 5->8 seam is deterministic for a
// representative authored-order + keep-filter input across repeats.
func TestPlannerTargetLowering_AuthoredOrderDeterminism(t *testing.T) {
	input := []resolvedTargetAction{
		lrAction("A", "feat.a", "suppress", "feature", "part"),
		lrAction("B", "feat.b", "keep", "feature", "part"),
		lrAction("C", "feat.c", "delete", "feature", "part"),
		lrAction("D", "feat.d", "hide", "feature", "part"),
	}
	first, err := lowerResolvedTargetActions(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 10; i++ {
		got, err := lowerResolvedTargetActions(input)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("iteration %d: non-deterministic lowered sequence\nfirst: %#v\ngot:   %#v", i, first, got)
		}
	}
}
