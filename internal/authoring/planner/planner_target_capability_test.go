package planner

import (
	"strings"
	"testing"

	"parametron/internal/engine/cad"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/table"
)

// Phase 5 Task 7 planner integration: these tests prove the shared capability
// gate (semantic.ValidateTargetActionCapability) is inserted at the correct
// pipeline seam inside createPlan, immediately after Task 6 semantic target
// resolution:
//
//	Task 5 action evaluation
//	    -> Task 6 semantic target resolution
//	        -> Task 7 capability validation
//
// The gate runs only on semantic-model planning paths; model-free planning
// never fabricates an all-false Targetability denial.

// validateTargetActionCapabilitiesForTest composes the real (unexported)
// Task 5 -> Task 6 -> Task 7 seams in the exact order createPlan uses them:
// evaluateProductTargetActions, then resolveEvaluatedTargetActions, then
// validateResolvedTargetActionCapabilities. It is the preferred entry point
// for permanent Task 7 planner integration tests because a successful
// capability check in a full CreatePlanWithTablesAndSemanticModel call would
// additionally require the unrelated capture-backed export-manifest
// projection. Capability denials short-circuit createPlan before that point
// and so are additionally proven through the full public entry point.
func validateTargetActionCapabilitiesForTest(t *testing.T, dslContent string, overrides map[string]string, tables map[string]*table.Table, model *semantic.Model) error {
	t.Helper()

	resolved, err := resolveTargetActionsForTest(t, dslContent, overrides, tables, model)
	if err != nil {
		return err
	}
	return validateResolvedTargetActionCapabilities(resolved)
}

func featureModelWithTargetability(t *testing.T, name, id string, tb cad.Targetability) *semantic.Model {
	t.Helper()
	return plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID:            id,
		ComponentID:   "cmp.root",
		Name:          name,
		Targetability: tb,
	}})
}

func componentModelWithTargetability(t *testing.T, name, id string, tb cad.Targetability) *semantic.Model {
	t.Helper()
	return plannerSemanticModelWithProductIntent(t, "Demo", []semantic.Component{{
		ID:            id,
		Kind:          "part",
		Name:          name,
		ChildrenIDs:   []string{},
		Quantity:      1,
		Targetability: tb,
	}}, nil)
}

// 20 / 21. Unique Feature: capability allow, then deny.
func TestPlannerTargetCapability_UniqueFeatureAllowThenDeny(t *testing.T) {
	dslContent := `
product Demo {
    target Pad: action = suppress
}`

	allow := featureModelWithTargetability(t, "Pad", "feat.pad", cad.Targetability{Suppress: true})
	if err := validateTargetActionCapabilitiesForTest(t, dslContent, nil, nil, allow); err != nil {
		t.Fatalf("allow: expected Task 5->6->7 to pass, got %v", err)
	}

	deny := featureModelWithTargetability(t, "Pad", "feat.pad", cad.Targetability{Suppress: false})
	err := validateTargetActionCapabilitiesForTest(t, dslContent, nil, nil, deny)
	if err == nil {
		t.Fatal("deny: expected Task 7 capability denial")
	}
	if !strings.Contains(err.Error(), `requires captured Targetability.Suppress=true`) {
		t.Fatalf("deny: expected capability denial, got %q", err.Error())
	}
	for _, wrong := range []string{
		"does not resolve to a Feature or Component",
		"is ambiguous across candidates",
		"invalid action value",
	} {
		if strings.Contains(err.Error(), wrong) {
			t.Fatalf("deny: capability denial contaminated by %q: %q", wrong, err.Error())
		}
	}
}

// 22 / 23. Unique Component: capability allow, then deny, through the same
// shared gate (there is no Component-specific branch).
func TestPlannerTargetCapability_UniqueComponentAllowThenDeny(t *testing.T) {
	dslContent := `
product Demo {
    target Cover: action = hide
}`

	allow := componentModelWithTargetability(t, "Cover", "cmp.cover", cad.Targetability{Hide: true})
	if err := validateTargetActionCapabilitiesForTest(t, dslContent, nil, nil, allow); err != nil {
		t.Fatalf("allow: expected Task 5->6->7 to pass, got %v", err)
	}

	deny := componentModelWithTargetability(t, "Cover", "cmp.cover", cad.Targetability{Hide: false})
	err := validateTargetActionCapabilitiesForTest(t, dslContent, nil, nil, deny)
	if err == nil {
		t.Fatal("deny: expected Task 7 capability denial for Component")
	}
	if !strings.Contains(err.Error(), `requires captured Targetability.Hide=true`) {
		t.Fatalf("deny: expected capability denial naming Targetability.Hide, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), `component "cmp.cover"`) {
		t.Fatalf("deny: expected diagnostic to name the component, got %q", err.Error())
	}
}

// 24. Planner all-six allow matrix for a unique Feature with every capability
// bit true.
func TestPlannerTargetCapability_AllSixActionsAllow(t *testing.T) {
	allTrue := cad.Targetability{Suppress: true, Unsuppress: true, Hide: true, Unhide: true, Delete: true}
	model := featureModelWithTargetability(t, "Pad", "feat.pad", allTrue)

	for _, action := range []string{"keep", "suppress", "unsuppress", "hide", "unhide", "delete"} {
		t.Run(action, func(t *testing.T) {
			dslContent := `
product Demo {
    target Pad: action = ` + action + `
}`
			if err := validateTargetActionCapabilitiesForTest(t, dslContent, nil, nil, model); err != nil {
				t.Fatalf("action %q: expected Task 7 gate to pass, got %v", action, err)
			}
		})
	}
}

// 25. Planner all-five deny matrix: for each capability-bearing action, only
// its required bit is false, and the planner-wrapped error names that exact
// Targetability field.
func TestPlannerTargetCapability_AllFiveActionsDeny(t *testing.T) {
	rows := []struct {
		action string
		tb     cad.Targetability
		field  string
	}{
		{"suppress", cad.Targetability{Suppress: false, Unsuppress: true, Hide: true, Unhide: true, Delete: true}, "Targetability.Suppress"},
		{"unsuppress", cad.Targetability{Suppress: true, Unsuppress: false, Hide: true, Unhide: true, Delete: true}, "Targetability.Unsuppress"},
		{"hide", cad.Targetability{Suppress: true, Unsuppress: true, Hide: false, Unhide: true, Delete: true}, "Targetability.Hide"},
		{"unhide", cad.Targetability{Suppress: true, Unsuppress: true, Hide: true, Unhide: false, Delete: true}, "Targetability.Unhide"},
		{"delete", cad.Targetability{Suppress: true, Unsuppress: true, Hide: true, Unhide: true, Delete: false}, "Targetability.Delete"},
	}

	for _, row := range rows {
		t.Run(row.action, func(t *testing.T) {
			model := featureModelWithTargetability(t, "Pad", "feat.pad", row.tb)
			dslContent := `
product Demo {
    target Pad: action = ` + row.action + `
}`
			// internal seam
			err := validateTargetActionCapabilitiesForTest(t, dslContent, nil, nil, model)
			if err == nil || !strings.Contains(err.Error(), row.field+"=true") {
				t.Fatalf("internal seam: expected denial naming %s, got %v", row.field, err)
			}
			if !strings.Contains(err.Error(), "target action 'Pad'") {
				t.Fatalf("internal seam: expected planner wrapping to name authored target, got %q", err.Error())
			}

			// full public entry point (denial short-circuits before export)
			ast := parseDSLForPlannerTest(t, dslContent)
			_, planErr := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
			if planErr == nil || !strings.Contains(planErr.Error(), row.field+"=true") {
				t.Fatalf("full path: expected denial naming %s, got %v", row.field, planErr)
			}
			if !strings.Contains(planErr.Error(), "error validating target action capabilities in product 'Demo'") {
				t.Fatalf("full path: expected product-context wrapping, got %q", planErr.Error())
			}
		})
	}
}

// 26. Full planner capability error context, locked byte-for-byte, with all
// required context: product name, authored target, action, required
// Targetability field, semantic kind, semantic ID.
func TestPlannerTargetCapability_FullDiagnosticExact(t *testing.T) {
	model := featureModelWithTargetability(t, "Pad", "feat.pad", cad.Targetability{Suppress: false})
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target Pad: action = suppress
}`)

	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected full-path capability denial")
	}
	want := `error validating target action capabilities in product 'Demo': target action 'Pad': action "suppress" requires captured Targetability.Suppress=true for semantic feature "feat.pad"`
	if err.Error() != want {
		t.Fatalf("expected exact diagnostic:\n%q\ngot:\n%q", want, err.Error())
	}
	for _, fragment := range []string{
		"product 'Demo'",
		"target action 'Pad'",
		`action "suppress"`,
		"Targetability.Suppress=true",
		"feature",
		`"feat.pad"`,
	} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("expected diagnostic to contain %q, got %q", fragment, err.Error())
		}
	}
}

// 27. Error precedence: an invalid action (Task 5) wins over any Task 6/7
// failure.
func TestPlannerTargetCapability_InvalidActionBeatsResolutionAndCapability(t *testing.T) {
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target DoesNotExist: action = explode
}`)
	model := featureModelWithTargetability(t, "Pad", "feat.pad", cad.Targetability{})

	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected invalid-action failure")
	}
	if !strings.Contains(err.Error(), "invalid action value 'explode'") {
		t.Fatalf("expected Task 5 invalid-action diagnostic, got %q", err.Error())
	}
	for _, wrong := range []string{"does not resolve to a Feature or Component", "requires captured Targetability"} {
		if strings.Contains(err.Error(), wrong) {
			t.Fatalf("expected no Task 6/7 diagnostic, got %q", err.Error())
		}
	}
}

// 28. Error precedence: a missing target (Task 6) wins over Task 7; no
// capability denial appears because no resolved target exists.
func TestPlannerTargetCapability_MissingTargetBeatsCapability(t *testing.T) {
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target Missing: action = suppress
}`)
	model := plannerSemanticModelWithProductIntent(t, "Demo", nil, nil)

	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected Task 6 not-found failure")
	}
	if !strings.Contains(err.Error(), "does not resolve to a Feature or Component by exact Name") {
		t.Fatalf("expected Task 6 not-found diagnostic, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "requires captured Targetability") {
		t.Fatalf("expected no Task 7 capability diagnostic, got %q", err.Error())
	}
}

// 29. Error precedence: Task 7 denial fires only after a successful Task 6
// resolution.
func TestPlannerTargetCapability_CapabilityDenialAfterSuccessfulResolution(t *testing.T) {
	model := featureModelWithTargetability(t, "Pad", "feat.pad", cad.Targetability{Suppress: false})
	ast := parseDSLForPlannerTest(t, `
product Demo {
    target Pad: action = suppress
}`)

	_, err := CreatePlanWithTablesAndSemanticModel(ast, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected Task 7 denial after Pad uniquely resolves")
	}
	if !strings.Contains(err.Error(), "requires captured Targetability.Suppress=true") {
		t.Fatalf("expected Task 7 denial, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "does not resolve") || strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected clean Task 7 denial with no Task 6 error, got %q", err.Error())
	}
}

// 30 / 31. keep present target with all-false Targetability passes Task 7;
// keep missing target still fails at Task 6 (no keep bypass around existence).
func TestPlannerTargetCapability_KeepBoundary(t *testing.T) {
	present := featureModelWithTargetability(t, "Pad", "feat.pad", cad.Targetability{})
	if err := validateTargetActionCapabilitiesForTest(t, `
product Demo {
    target Pad: action = keep
}`, nil, nil, present); err != nil {
		t.Fatalf("keep present target: expected Task 7 pass with all bits false, got %v", err)
	}

	missingAST := parseDSLForPlannerTest(t, `
product Demo {
    target DoesNotExist: action = keep
}`)
	missingModel := plannerSemanticModelWithProductIntent(t, "Demo", nil, nil)
	_, err := CreatePlanWithTablesAndSemanticModel(missingAST, nil, nil, missingModel, nil)
	if err == nil || !strings.Contains(err.Error(), "does not resolve to a Feature or Component by exact Name") {
		t.Fatalf("keep missing target: expected Task 6 failure, got %v", err)
	}
}

// 32. Authored-order first denial: with two resolvable, both-denied targets the
// first authored target produces the error; reversing DSL order reverses it.
func TestPlannerTargetCapability_AuthoredOrderFirstDenial(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", []semantic.Component{{
		ID: "cmp.cover", Kind: "part", Name: "Cover", ChildrenIDs: []string{}, Quantity: 1,
		Targetability: cad.Targetability{Hide: false},
	}}, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad",
		Targetability: cad.Targetability{Suppress: false},
	}})

	padFirst := `
product Demo {
    target Pad: action = suppress
    target Cover: action = hide
}`
	err := validateTargetActionCapabilitiesForTest(t, padFirst, nil, nil, model)
	if err == nil || !strings.Contains(err.Error(), "target action 'Pad'") {
		t.Fatalf("Pad first: expected first denial on Pad, got %v", err)
	}

	coverFirst := `
product Demo {
    target Cover: action = hide
    target Pad: action = suppress
}`
	err = validateTargetActionCapabilitiesForTest(t, coverFirst, nil, nil, model)
	if err == nil || !strings.Contains(err.Error(), "target action 'Cover'") {
		t.Fatalf("Cover first: expected first denial on Cover, got %v", err)
	}
}

// 33. Authored-order first denial is deterministic across repeats.
func TestPlannerTargetCapability_AuthoredOrderDeterminism(t *testing.T) {
	model := plannerSemanticModelWithProductIntent(t, "Demo", []semantic.Component{{
		ID: "cmp.cover", Kind: "part", Name: "Cover", ChildrenIDs: []string{}, Quantity: 1,
		Targetability: cad.Targetability{Hide: false},
	}}, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad",
		Targetability: cad.Targetability{Suppress: false},
	}})
	dslContent := `
product Demo {
    target Pad: action = suppress
    target Cover: action = hide
}`
	want := `target action 'Pad': action "suppress" requires captured Targetability.Suppress=true for semantic feature "feat.pad"`
	for i := 0; i < 10; i++ {
		err := validateTargetActionCapabilitiesForTest(t, dslContent, nil, nil, model)
		if err == nil || err.Error() != want {
			t.Fatalf("iteration %d: expected stable first denial %q, got %v", i, want, err)
		}
	}
}

// 34. Literal action expression flows through the shared gate: the evaluated
// canonical "suppress" selects Targetability.Suppress.
func TestPlannerTargetCapability_LiteralExpressionIntegration(t *testing.T) {
	dslContent := `
product Demo {
    target Pad: action = suppress
}`
	if err := validateTargetActionCapabilitiesForTest(t, dslContent, nil, nil,
		featureModelWithTargetability(t, "Pad", "feat.pad", cad.Targetability{Suppress: true})); err != nil {
		t.Fatalf("literal allow: got %v", err)
	}
	err := validateTargetActionCapabilitiesForTest(t, dslContent, nil, nil,
		featureModelWithTargetability(t, "Pad", "feat.pad", cad.Targetability{Suppress: false}))
	if err == nil || !strings.Contains(err.Error(), "Targetability.Suppress=true") {
		t.Fatalf("literal deny: expected Targetability.Suppress denial, got %v", err)
	}
}

// 35. Ternary: the selected/evaluated branch drives the gate.
func TestPlannerTargetCapability_TernaryExpressionIntegration(t *testing.T) {
	dslContent := `
product Demo {
    param flag: boolean = true
    target Pad: action = flag ? suppress : hide
}`
	model := featureModelWithTargetability(t, "Pad", "feat.pad", cad.Targetability{Suppress: true, Hide: false})

	if err := validateTargetActionCapabilitiesForTest(t, dslContent, nil, nil, model); err != nil {
		t.Fatalf("flag=true: expected suppress branch to pass, got %v", err)
	}

	err := validateTargetActionCapabilitiesForTest(t, dslContent, map[string]string{"flag": "false"}, nil, model)
	if err == nil || !strings.Contains(err.Error(), "Targetability.Hide=true") {
		t.Fatalf("flag=false: expected hide branch denial, got %v", err)
	}
}

// 36. Table-backed: the selected row's canonical action drives the gate, with
// no table-specific capability code.
func TestPlannerTargetCapability_TableBackedIntegration(t *testing.T) {
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
	dslContent := `
product Demo {
    param variant: string = "A"
    target Pad: action = table_cell("variants", variant, "action")
}`
	model := featureModelWithTargetability(t, "Pad", "feat.pad", cad.Targetability{Suppress: true, Hide: false})

	if err := validateTargetActionCapabilitiesForTest(t, dslContent, nil, tables, model); err != nil {
		t.Fatalf("variant=A: expected suppress row to pass, got %v", err)
	}

	err := validateTargetActionCapabilitiesForTest(t, dslContent, map[string]string{"variant": "B"}, tables, model)
	if err == nil || !strings.Contains(err.Error(), "Targetability.Hide=true") {
		t.Fatalf("variant=B: expected hide row denial, got %v", err)
	}
}

// 37. A parameter override that steers table-backed action selection composes
// Task 5 override evaluation with the Task 7 gate.
func TestPlannerTargetCapability_OverrideSelectedActionParticipates(t *testing.T) {
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
	dslContent := `
product Demo {
    param variant: string = "A"
    target Pad: action = table_cell("variants", variant, "action")
}`
	model := featureModelWithTargetability(t, "Pad", "feat.pad", cad.Targetability{Suppress: true, Hide: false})

	if err := validateTargetActionCapabilitiesForTest(t, dslContent, nil, tables, model); err != nil {
		t.Fatalf("default variant=A: expected pass, got %v", err)
	}
	err := validateTargetActionCapabilitiesForTest(t, dslContent, map[string]string{"variant": "B"}, tables, model)
	if err == nil || !strings.Contains(err.Error(), "Targetability.Hide=true") {
		t.Fatalf("override variant=B: expected hide denial via Task 5 override composition, got %v", err)
	}
}

// 38. A capability-disabled target still resolves under Task 6 first; the
// failure is a Task 7 capability denial, not a resolution error.
func TestPlannerTargetCapability_DisabledTargetResolvesThenFailsCapability(t *testing.T) {
	model := featureModelWithTargetability(t, "Pad", "feat.pad", cad.Targetability{Suppress: false})

	resolved, err := resolveTargetActionsForTest(t, `
product Demo {
    target Pad: action = suppress
}`, nil, nil, model)
	if err != nil {
		t.Fatalf("Task 6 resolution should succeed for a capability-disabled target, got %v", err)
	}
	if resolved[0].Target.SemanticID != "feat.pad" {
		t.Fatalf("unexpected resolved target: %#v", resolved[0].Target)
	}
	if capErr := validateResolvedTargetActionCapabilities(resolved); capErr == nil {
		t.Fatal("expected Task 7 capability denial after successful resolution")
	}
}

// 39 / 40. Model-free planning never fabricates a capability denial: both
// public model-free entry points succeed even for a target action whose
// captured Targetability would be all-false if a model existed.
func TestPlannerTargetCapability_ModelFreeDoesNotFabricateDenial(t *testing.T) {
	dslContent := `
product Demo {
    target Pad: action = suppress
}`
	ast := parseAndValidateWithTables(t, dslContent, nil)
	if _, err := CreatePlan(ast, nil); err != nil {
		t.Fatalf("model-free CreatePlan: expected no capability error, got %v", err)
	}
	if _, err := CreatePlanWithTables(ast, nil, nil); err != nil {
		t.Fatalf("model-free CreatePlanWithTables: expected no capability error, got %v", err)
	}
}

// 41. The semantic-model planning path activates the Task 7 gate: identical
// DSL, model-free succeeds, semantic-model with a denied Targetability fails.
func TestPlannerTargetCapability_SemanticModelPathActivatesGate(t *testing.T) {
	dslContent := `
product Demo {
    target Pad: action = suppress
}`
	modelFreeAST := parseAndValidateWithTables(t, dslContent, nil)
	if _, err := CreatePlanWithTables(modelFreeAST, nil, nil); err != nil {
		t.Fatalf("model-free planning should succeed, got %v", err)
	}

	semanticAST := parseDSLForPlannerTest(t, dslContent)
	model := featureModelWithTargetability(t, "Pad", "feat.pad", cad.Targetability{Suppress: false})
	_, err := CreatePlanWithTablesAndSemanticModel(semanticAST, nil, nil, model, nil)
	if err == nil {
		t.Fatal("expected semantic-model planning to activate the Task 7 gate and deny")
	}
	if !strings.Contains(err.Error(), "error validating target action capabilities in product 'Demo'") {
		t.Fatalf("expected Task 7 capability denial via the semantic-model path, got %q", err.Error())
	}
}

// 42. Feature / Component parity through the planner: same Targetability
// pattern and same action produce the same pass/fail semantics; only the
// diagnostic's semantic kind/ID differs.
func TestPlannerTargetCapability_FeatureComponentParityThroughPlanner(t *testing.T) {
	for _, tb := range []cad.Targetability{
		{Suppress: true},
		{Suppress: false},
	} {
		featureModel := featureModelWithTargetability(t, "Thing", "feat.thing", tb)
		componentModel := componentModelWithTargetability(t, "Thing", "cmp.thing", tb)
		dslContent := `
product Demo {
    target Thing: action = suppress
}`
		featErr := validateTargetActionCapabilitiesForTest(t, dslContent, nil, nil, featureModel)
		compErr := validateTargetActionCapabilitiesForTest(t, dslContent, nil, nil, componentModel)
		if (featErr == nil) != (compErr == nil) {
			t.Fatalf("parity break for %#v: feature err=%v component err=%v", tb, featErr, compErr)
		}
	}
}

// 43. Scope parity through the planner helper: a feature whose scope resolves
// to "part" (under a part component) and one whose scope resolves to
// "assembly" (under the root) get the same gate result for the same
// Targetability. Task 9 routing stays irrelevant.
func TestPlannerTargetCapability_ScopeParityThroughPlanner(t *testing.T) {
	tb := cad.Targetability{Suppress: false}

	assemblyScopedModel := plannerSemanticModelWithProductIntent(t, "Demo", nil, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad", Targetability: tb,
	}})
	partScopedModel := plannerSemanticModelWithProductIntent(t, "Demo", []semantic.Component{{
		ID: "cmp.part", Kind: "part", Name: "Part", ChildrenIDs: []string{}, Quantity: 1,
	}}, []semantic.Feature{{
		ID: "feat.pad", ComponentID: "cmp.part", Name: "Pad", Targetability: tb,
	}})

	dslContent := `
product Demo {
    target Pad: action = suppress
}`

	assemblyResolved, err := resolveTargetActionsForTest(t, dslContent, nil, nil, assemblyScopedModel)
	if err != nil {
		t.Fatalf("assembly-scoped resolution: %v", err)
	}
	partResolved, err := resolveTargetActionsForTest(t, dslContent, nil, nil, partScopedModel)
	if err != nil {
		t.Fatalf("part-scoped resolution: %v", err)
	}
	if assemblyResolved[0].Target.Scope != "assembly" || partResolved[0].Target.Scope != "part" {
		t.Fatalf("fixture scope mismatch: assembly=%q part=%q",
			assemblyResolved[0].Target.Scope, partResolved[0].Target.Scope)
	}

	assemblyErr := validateResolvedTargetActionCapabilities(assemblyResolved)
	partErr := validateResolvedTargetActionCapabilities(partResolved)
	if (assemblyErr == nil) != (partErr == nil) {
		t.Fatalf("scope changed the gate result: assembly err=%v part err=%v", assemblyErr, partErr)
	}
}
