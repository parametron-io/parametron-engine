package planner

import (
	"strings"
	"testing"

	"parametron/internal/engine/cad"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/table"
)

// plannerSemanticModelWithProductIntent builds a minimal valid semantic
// model rooted at cmp.root, with a matching ProductIntent for productName so
// that resolveSemanticProductIntent skips capture-backed manifest
// resolution (the model's ProductIntent adapter is left empty, which is not
// "freecad", so shouldRunCADAdapter is false and createPlan proceeds to
// ordinary Task 5/6 evaluation without requiring semanticmap.SemanticMap
// projection). This lets these permanent Task 6 tests exercise the real
// createPlan target-resolution seam without dragging in unrelated Task 7+
// capture-backed manifest machinery.
func plannerSemanticModelWithProductIntent(t *testing.T, productName string, extraComponents []semantic.Component, features []semantic.Feature) *semantic.Model {
	t.Helper()

	model := &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "target_resolution_planner_model",
		RootComponentID:         "cmp.root",
		Components: append([]semantic.Component{{
			ID:          "cmp.root",
			Kind:        "assembly",
			Name:        "RootAssembly",
			DisplayName: "Root Assembly",
			ChildrenIDs: []string{},
			Quantity:    1,
		}}, extraComponents...),
		Features: features,
		ProductIntents: []semantic.ProductIntent{{
			Name:                   productName,
			RequestedOutputFormats: []string{},
			Outputs:                []semantic.OutputIntent{},
			ExportedParameters:     []semantic.ExportedParameterIntent{},
			Mutations:              []semantic.MutationIntent{},
		}},
	}
	if err := semantic.Validate(model); err != nil {
		t.Fatalf("plannerSemanticModelWithProductIntent: invalid fixture: %v", err)
	}
	return model
}

// resolveTargetActionsForTest composes the real (unexported) Task 5
// evaluateProductTargetActions seam with the real (unexported) Task 6
// resolveEvaluatedTargetActions seam, in the exact order createPlan uses
// them internally. It is the preferred entry point for permanent
// success-path planner integration tests: it proves the true production
// integration precisely without requiring the unrelated capture-backed
// export-manifest projection that a full CreatePlanWithTablesAndSemanticModel
// call would additionally require once target resolution succeeds.
func resolveTargetActionsForTest(t *testing.T, dslContent string, overrides map[string]string, tables map[string]*table.Table, model *semantic.Model) ([]resolvedTargetAction, error) {
	t.Helper()

	evaluated, err := evaluateTargetActionsForTest(t, dslContent, overrides, tables)
	if err != nil {
		return nil, err
	}
	return resolveEvaluatedTargetActions(model, evaluated)
}

// 19. Planner unique Feature target.
func TestResolveEvaluatedTargetActions_UniqueFeatureTarget(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID:          "feat.pad",
		ComponentID: "cmp.root",
		Name:        "Pad",
	}})

	resolved, err := resolveTargetActionsForTest(t, `
product Demo {
    target Pad: action = suppress
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected exactly one resolved target action, got %d", len(resolved))
	}
	if resolved[0].Target.SemanticEntityKind != "feature" || resolved[0].Target.SemanticID != "feat.pad" {
		t.Fatalf("unexpected resolved target: %#v", resolved[0].Target)
	}
	if resolved[0].Action != "suppress" {
		t.Fatalf("expected action 'suppress', got %q", resolved[0].Action)
	}
}

// 20. Planner unique Component target. Feature and Component resolution go
// through the exact same resolveTargetActionsForTest -> resolveEvaluatedTargetActions
// -> semantic.ResolveSemanticTargetByExactName call chain; there is no
// separate Component-specific branch in production.
func TestResolveEvaluatedTargetActions_UniqueComponentTarget(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", []semantic.Component{{
		ID:          "cmp.cover",
		Kind:        "part",
		Name:        "Cover",
		ChildrenIDs: []string{},
		Quantity:    1,
	}}, nil)

	resolved, err := resolveTargetActionsForTest(t, `
product Demo {
    target Cover: action = hide
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected exactly one resolved target action, got %d", len(resolved))
	}
	if resolved[0].Target.SemanticEntityKind != "component" || resolved[0].Target.SemanticID != "cmp.cover" {
		t.Fatalf("unexpected resolved target: %#v", resolved[0].Target)
	}
}

// 21. Planner preserves authored target-action declaration order: candidate
// sorting only applies inside one target's ambiguity set, never across
// distinct authored targets.
func TestResolveEvaluatedTargetActions_PreservesAuthoredOrder(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{
		{ID: "feat.chamfer", ComponentID: "cmp.root", Name: "Chamfer"},
		{ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad"},
		{ID: "feat.body", ComponentID: "cmp.root", Name: "Body"},
	})

	resolved, err := resolveTargetActionsForTest(t, `
product Demo {
    target Chamfer: action = hide
    target Pad: action = suppress
    target Body: action = keep
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantOrder := []string{"Chamfer", "Pad", "Body"}
	if len(resolved) != len(wantOrder) {
		t.Fatalf("expected %d resolved target actions, got %d (%#v)", len(wantOrder), len(resolved), resolved)
	}
	for i, name := range wantOrder {
		if resolved[i].SemanticTarget != name {
			t.Fatalf("expected resolved[%d].SemanticTarget = %q, got %q (full: %#v)", i, name, resolved[i].SemanticTarget, resolved)
		}
	}
}

// 22. Planner missing target failure through the real public
// CreatePlanWithTablesAndSemanticModel entry point. Since target resolution
// failure short-circuits createPlan before it ever reaches export-manifest
// generation, this scenario can safely use the full public entry point.
func TestCreatePlanWithTablesAndSemanticModel_MissingTargetFails(t *testing.T) {
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target DoesNotExist:
        action = suppress
}`)
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, nil)

	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected missing semantic target to fail through the semantic-model planning path")
	}
	if !strings.Contains(err.Error(), `does not resolve to a Feature or Component by exact Name`) {
		t.Fatalf("expected Task 6 not-found diagnostic, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "DoesNotExist") {
		t.Fatalf("expected diagnostic to reference the missing target name, got %q", err.Error())
	}
}

// 23. Planner cross-kind ambiguity failure: neither Feature nor Component
// wins by kind precedence, and the planner-wrapped error carries the
// semantic candidate evidence for both kinds.
func TestCreatePlanWithTablesAndSemanticModel_CrossKindAmbiguityFails(t *testing.T) {
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target Keyway:
        action = suppress
}`)
	model := plannerSemanticModelWithProductIntent(t, "Demo",
		[]semantic.Component{{ID: "cmp.keyway", Kind: "part", Name: "Keyway", ChildrenIDs: []string{}, Quantity: 1}},
		[]semantic.Feature{{ID: "feat.keyway", ComponentID: "cmp.root", Name: "Keyway"}},
	)

	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected Feature+Component cross-kind collision to fail as ambiguous")
	}
	if !strings.Contains(err.Error(), "component:cmp.keyway") {
		t.Fatalf("expected planner-wrapped error to include the Component candidate, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "feature:feat.keyway") {
		t.Fatalf("expected planner-wrapped error to include the Feature candidate, got %q", err.Error())
	}
}

// 24. Action-error precedence: an invalid action value fails DSL validation
// (Task 5), which runs before createPlan ever touches the semantic model, so
// the Task 6 not-found diagnostic must never appear for this input.
func TestCreatePlanWithTablesAndSemanticModel_InvalidActionBeatsMissingTarget(t *testing.T) {
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target DoesNotExist:
        action = explode
}`)
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, nil)

	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected invalid action value to fail DSL validation")
	}
	if !strings.Contains(err.Error(), "invalid action value 'explode'") {
		t.Fatalf("expected Task 5 invalid-action diagnostic, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "does not resolve to a Feature or Component") {
		t.Fatalf("expected no Task 6 target-resolution diagnostic to appear, got %q", err.Error())
	}
}

// 25. All six canonical actions resolve to the identical semantic target
// identity: Task 6 resolution is action-independent, with no capability
// enforcement.
func TestResolveEvaluatedTargetActions_ActionIndependence(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID:          "feat.pad",
		ComponentID: "cmp.root",
		Name:        "Pad",
	}})

	for _, action := range []string{"keep", "suppress", "unsuppress", "hide", "unhide", "delete"} {
		t.Run(action, func(t *testing.T) {
			resolved, err := resolveTargetActionsForTest(t, `
product Demo {
    target Pad: action = `+action+`
}`, nil, nil, model)
			if err != nil {
				t.Fatalf("unexpected error for action %q: %v", action, err)
			}
			if resolved[0].Target.SemanticEntityKind != "feature" || resolved[0].Target.SemanticID != "feat.pad" {
				t.Fatalf("expected identical resolved target identity for action %q, got %#v", action, resolved[0].Target)
			}
		})
	}
}

// 26. `keep` still requires target existence: it does not bypass Task 6
// resolution. The present-target half is proven here; the missing-target
// half is TestCreatePlanWithTablesAndSemanticModel_KeepStillRequiresTargetExistence.
func TestResolveEvaluatedTargetActions_KeepResolvesExistingTarget(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID:          "feat.pad",
		ComponentID: "cmp.root",
		Name:        "Pad",
	}})

	resolved, err := resolveTargetActionsForTest(t, `
product Demo {
    target Pad: action = keep
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved[0].Target.SemanticID != "feat.pad" {
		t.Fatalf("unexpected resolved target: %#v", resolved[0].Target)
	}
}

func TestCreatePlanWithTablesAndSemanticModel_KeepStillRequiresTargetExistence(t *testing.T) {
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target DoesNotExist:
        action = keep
}`)
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, nil)

	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected 'keep' targeting a missing semantic target to fail resolution, not bypass it")
	}
	if !strings.Contains(err.Error(), "does not resolve to a Feature or Component by exact Name") {
		t.Fatalf("expected Task 6 not-found diagnostic, got %q", err.Error())
	}
}

// 27. A capability-disabled action (Targetability.Suppress=false) must still
// resolve under Task 6: capability enforcement is Task 7+ scope.
func TestResolveEvaluatedTargetActions_CapabilityDisabledActionStillResolves(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID:          "feat.pad",
		ComponentID: "cmp.root",
		Name:        "Pad",
		Targetability: cad.Targetability{
			Suppress: false,
		},
	}})

	resolved, err := resolveTargetActionsForTest(t, `
product Demo {
    target Pad: action = suppress
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("expected Targetability.Suppress=false to not block Task 6 resolution, got error: %v", err)
	}
	if resolved[0].Target.Targetability.Suppress {
		t.Fatalf("expected fixture Targetability.Suppress=false to be preserved, got %#v", resolved[0].Target.Targetability)
	}
}

// 28. Model-free planner compatibility: neither public entry point performs
// Task 6 semantic target lookup when no semantic model is supplied, so a
// missing target name causes no failure at all.
func TestCreatePlan_ModelFreeDoesNotPerformSemanticTargetLookup(t *testing.T) {
	ast := parseAndValidateWithTables(t, `
product Demo {
    target DoesNotExist:
        action = suppress
}`, nil)

	if _, err := CreatePlan(ast, nil); err != nil {
		t.Fatalf("expected model-free CreatePlan to succeed despite a missing semantic target, got: %v", err)
	}
}

func TestCreatePlanWithTables_ModelFreeDoesNotPerformSemanticTargetLookup(t *testing.T) {
	ast := parseAndValidateWithTables(t, `
product Demo {
    target DoesNotExist:
        action = suppress
}`, nil)

	if _, err := CreatePlanWithTables(ast, nil, nil); err != nil {
		t.Fatalf("expected model-free CreatePlanWithTables to succeed despite a missing semantic target, got: %v", err)
	}
}

// 29. The semantic-model planning path actively invokes Task 6 resolution,
// distinguishing it from the model-free path above: identical DSL content,
// model-free succeeds and semantic-model fails.
func TestCreatePlanWithTablesAndSemanticModel_SemanticModelPathInvokesResolution(t *testing.T) {
	dslContent := `
product Demo {
    target DoesNotExist:
        action = suppress
}`

	modelFreeAST := parseAndValidateWithTables(t, dslContent, nil)
	if _, err := CreatePlanWithTables(modelFreeAST, nil, nil); err != nil {
		t.Fatalf("expected model-free planning to succeed, got: %v", err)
	}

	semanticModelAST := parseDSLForPlannerTest(t, dslContent)
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, nil)
	_, err := CreatePlanWithTablesAndSemanticModel(semanticModelAST, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected semantic-model planning to fail resolving the missing target")
	}
	if !strings.Contains(err.Error(), "does not resolve to a Feature or Component by exact Name") {
		t.Fatalf("expected Task 6 not-found diagnostic, got %q", err.Error())
	}
}

// 30. A table-backed action expression resolves its target after Task 5
// table evaluation completes, with no table-aware resolver logic.
func TestResolveEvaluatedTargetActions_TableBackedActionResolves(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID:          "feat.pad",
		ComponentID: "cmp.root",
		Name:        "Pad",
	}})
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
			},
		},
	}

	resolved, err := resolveTargetActionsForTest(t, `
product Demo {
    param variant: string = "A"

    target Pad:
        action = table_cell("variants", variant, "action")
}`, nil, tables, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved[0].Action != "suppress" {
		t.Fatalf("expected table-backed action 'suppress', got %q", resolved[0].Action)
	}
	if resolved[0].Target.SemanticID != "feat.pad" {
		t.Fatalf("unexpected resolved target: %#v", resolved[0].Target)
	}
}

// 31. A ternary action expression resolves its target after ternary
// evaluation selects the canonical action string.
func TestResolveEvaluatedTargetActions_TernaryActionResolves(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID:          "feat.pad",
		ComponentID: "cmp.root",
		Name:        "Pad",
	}})

	resolved, err := resolveTargetActionsForTest(t, `
product Demo {
    param flag: boolean = true

    target Pad:
        action = flag ? suppress : hide
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved[0].Action != "suppress" {
		t.Fatalf("expected ternary-evaluated action 'suppress', got %q", resolved[0].Action)
	}
	if resolved[0].Target.SemanticID != "feat.pad" {
		t.Fatalf("unexpected resolved target: %#v", resolved[0].Target)
	}
}

// 32. A Parameter carrying the same textual name as the authored target must
// not become a target candidate: the semantic target search space excludes
// Parameter/Metadata/ParameterGroup entirely.
func TestCreatePlanWithTablesAndSemanticModel_ParameterNameCollisionDoesNotBecomeTarget(t *testing.T) {
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target Pad:
        action = suppress
}`)
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, nil)
	model.Parameters = []semantic.Parameter{{
		ID:          "par.pad",
		OwnerKind:   semantic.OwnerKindComponent,
		OwnerID:     "cmp.root",
		ComponentID: "cmp.root",
		Name:        "Pad",
		ValueType:   "number",
		NativeType:  "App::PropertyFloat",
		Observable:  true,
		Writable:    true,
	}}
	if err := semantic.Validate(model); err != nil {
		t.Fatalf("invalid fixture: %v", err)
	}

	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected a Parameter-name collision to not resolve as a target")
	}
	if !strings.Contains(err.Error(), "does not resolve to a Feature or Component by exact Name") {
		t.Fatalf("expected Task 6 not-found diagnostic, got %q", err.Error())
	}
}

// 33. The planner adds no DisplayName fallback of its own around the
// semantic resolver.
func TestCreatePlanWithTablesAndSemanticModel_NoDisplayNameFallback(t *testing.T) {
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target Keyway:
        action = suppress
}`)
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID:          "feat.pocket",
		ComponentID: "cmp.root",
		Name:        "Pocket001",
		DisplayName: "Keyway",
	}})

	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected DisplayName-only match to fail through the planner")
	}
	if !strings.Contains(err.Error(), "does not resolve to a Feature or Component by exact Name") {
		t.Fatalf("expected Task 6 not-found diagnostic, got %q", err.Error())
	}
}

// 34. The planner adds no case folding of its own around the semantic
// resolver.
func TestCreatePlanWithTablesAndSemanticModel_CaseMismatchFails(t *testing.T) {
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target pad:
        action = suppress
}`)
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID:          "feat.pad",
		ComponentID: "cmp.root",
		Name:        "Pad",
	}})

	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected a case-mismatched target name to fail through the planner")
	}
	if !strings.Contains(err.Error(), "does not resolve to a Feature or Component by exact Name") {
		t.Fatalf("expected Task 6 not-found diagnostic, got %q", err.Error())
	}
}
