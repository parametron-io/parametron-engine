package planner

import (
	"errors"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/engine/table"
)

// actionPlannerTestTables returns the permanent Task 5 in-memory "variants"
// table used by planner-layer target-action tests: an ordinary
// string-backed table with no action-specific schema or metadata.
func actionPlannerTestTables() map[string]*table.Table {
	return map[string]*table.Table{
		"variants": {
			SchemaVersion: table.SchemaVersion,
			Name:          "variants",
			KeyColumn:     "variant",
			Columns: []table.Column{
				{Name: "variant", Type: table.ColumnTypeString, Required: true},
				{Name: "chamferAction", Type: table.ColumnTypeString, Required: true},
			},
			Rows: []table.Row{
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "A"},
					"chamferAction": {Type: table.ColumnTypeString, String: "suppress"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "B"},
					"chamferAction": {Type: table.ColumnTypeString, String: "hide"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "C"},
					"chamferAction": {Type: table.ColumnTypeString, String: "delete"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "D"},
					"chamferAction": {Type: table.ColumnTypeString, String: "keep"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "E"},
					"chamferAction": {Type: table.ColumnTypeString, String: "unsuppress"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "F"},
					"chamferAction": {Type: table.ColumnTypeString, String: "unhide"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "BAD"},
					"chamferAction": {Type: table.ColumnTypeString, String: "explode"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "CASE"},
					"chamferAction": {Type: table.ColumnTypeString, String: "Suppress"},
				}},
				{Values: map[string]table.Value{
					"variant":       {Type: table.ColumnTypeString, String: "SPACE"},
					"chamferAction": {Type: table.ColumnTypeString, String: "suppress "},
				}},
			},
		},
	}
}

// resolvePlannerBindingsForTest replicates the exact binding-resolution seam
// createPlan runs before calling evaluateProductTargetActions: applying
// parameter overrides, then iteratively resolving lets/params via the same
// unexported evaluateExpressionWithContext used by production. It exists so
// permanent tests can drive real target-action evaluation (including
// override participation) without a production export, per Task 5 Stage 2
// scope.
func resolvePlannerBindingsForTest(
	t *testing.T,
	ast *dsl.AST,
	product *dsl.ProductNode,
	overrides map[string]string,
	tables map[string]*table.Table,
) (map[string]interface{}, map[string]struct{}, map[string]*dsl.ParameterNode) {
	t.Helper()

	productBindingScope, err := dsl.BuildProductBindingScopeForPlanning(product, ast.ResolvedConstants, tables)
	if err != nil {
		t.Fatalf("BuildProductBindingScopeForPlanning failed: %v", err)
	}

	productBindingNames := make(map[string]struct{}, len(product.Lets)+len(product.Parameters))
	for _, let := range product.Lets {
		productBindingNames[let.Name] = struct{}{}
	}
	for _, param := range product.Parameters {
		productBindingNames[param.Name] = struct{}{}
	}

	resolvedValues := make(map[string]interface{})
	for _, param := range product.Parameters {
		if overrideStr, ok := overrides[param.Name]; ok {
			val, err := convertOverrideValue(overrideStr, param.Type)
			if err != nil {
				t.Fatalf("failed to apply override for '%s': %v", param.Name, err)
			}
			resolvedValues[param.Name] = val
		}
	}

	type pendingBinding struct {
		name         string
		expr         dsl.ExpressionNode
		expectedType *dsl.ParameterType
	}
	unresolved := make([]pendingBinding, 0, len(product.Lets)+len(product.Parameters))
	for _, let := range product.Lets {
		letBinding := productBindingScope[let.Name]
		unresolved = append(unresolved, pendingBinding{name: let.Name, expr: let.Value, expectedType: &letBinding.Type})
	}
	for _, p := range product.Parameters {
		if _, isResolved := resolvedValues[p.Name]; !isResolved {
			paramType := p.Type
			unresolved = append(unresolved, pendingBinding{name: p.Name, expr: p.DefaultValue, expectedType: &paramType})
		}
	}

	progress := true
	for len(unresolved) > 0 && progress {
		progress = false
		remaining := make([]pendingBinding, 0, len(unresolved))
		for _, binding := range unresolved {
			val, err := evaluateExpressionWithContext(binding.expr, resolvedValues, productBindingNames, productBindingScope, ast.ResolvedConstants, binding.expectedType, plannerEvalContext{tables: tables})
			if err == nil {
				resolvedValues[binding.name] = val
				progress = true
			} else if !errors.Is(err, errUnresolvedIdentifier) {
				t.Fatalf("error evaluating binding '%s': %v", binding.name, err)
			} else {
				remaining = append(remaining, binding)
			}
		}
		unresolved = remaining
	}
	if len(unresolved) > 0 {
		t.Fatalf("unresolved bindings remain in test fixture: %v", unresolved)
	}

	return resolvedValues, productBindingNames, productBindingScope
}

// evaluateTargetActionsForTest parses and validates dslContent (expecting
// exactly one product), resolves its bindings through the real production
// binding-resolution seam, and evaluates its target actions through the
// real (unexported) evaluateProductTargetActions seam, returning its result
// or error for direct assertion.
func evaluateTargetActionsForTest(t *testing.T, dslContent string, overrides map[string]string, tables map[string]*table.Table) ([]evaluatedTargetAction, error) {
	t.Helper()

	ast := parseDSLForPlannerTest(t, dslContent)
	if err := dsl.ValidateWithTables(ast, tables); err != nil {
		t.Fatalf("ValidateWithTables failed: %v", err)
	}
	if len(ast.Products) != 1 {
		t.Fatalf("expected exactly one product, got %d", len(ast.Products))
	}
	product := ast.Products[0]

	resolvedValues, productBindingNames, productBindingScope := resolvePlannerBindingsForTest(t, ast, product, overrides, tables)
	return evaluateProductTargetActions(product, resolvedValues, productBindingNames, productBindingScope, ast.ResolvedConstants, tables)
}

// TestEvaluateProductTargetActions_LiteralAction proves a literal action
// expression resolves internally to the canonical action string, paired
// with its semantic target name.
func TestEvaluateProductTargetActions_LiteralAction(t *testing.T) {
	evaluated, err := evaluateTargetActionsForTest(t, `
product Demo {
    target Pad: action = suppress
}`, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evaluated) != 1 {
		t.Fatalf("expected exactly one evaluated target action, got %d", len(evaluated))
	}
	if evaluated[0].SemanticTarget != "Pad" {
		t.Fatalf("expected SemanticTarget 'Pad', got %q", evaluated[0].SemanticTarget)
	}
	if evaluated[0].Action != "suppress" {
		t.Fatalf("expected Action 'suppress', got %q", evaluated[0].Action)
	}
}

// TestEvaluateProductTargetActions_TableSixValueMatrix permanently proves
// each of the six canonical selected table-backed action values evaluates
// to its exact canonical string, with no enum wrapper, alternate
// representation, or normalization.
func TestEvaluateProductTargetActions_TableSixValueMatrix(t *testing.T) {
	tables := actionPlannerTestTables()
	matrix := []struct {
		row    string
		action string
	}{
		{"A", "suppress"},
		{"B", "hide"},
		{"C", "delete"},
		{"D", "keep"},
		{"E", "unsuppress"},
		{"F", "unhide"},
	}

	for _, tt := range matrix {
		t.Run(tt.row+"_"+tt.action, func(t *testing.T) {
			evaluated, err := evaluateTargetActionsForTest(t, `
product Demo {
    target Chamfer: action = table_cell("variants", "`+tt.row+`", "chamferAction")
}`, nil, tables)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(evaluated) != 1 || evaluated[0].Action != tt.action {
				t.Fatalf("expected Action %q, got %+v", tt.action, evaluated)
			}
		})
	}
}

// TestEvaluateProductTargetActions_LiteralTernaryTableParity is the core
// Task 5 exit-criterion test: literal, ternary, and table-backed action
// expressions all resolve to the exact same canonical string ("hide")
// through the same evaluation seam.
func TestEvaluateProductTargetActions_LiteralTernaryTableParity(t *testing.T) {
	literal, err := evaluateTargetActionsForTest(t, `
product Demo {
    target Pad: action = hide
}`, nil, nil)
	if err != nil {
		t.Fatalf("literal: unexpected error: %v", err)
	}

	ternary, err := evaluateTargetActionsForTest(t, `
product Demo {
    param flag: boolean = false
    target Pad: action = flag ? suppress : hide
}`, nil, nil)
	if err != nil {
		t.Fatalf("ternary: unexpected error: %v", err)
	}

	tableBacked, err := evaluateTargetActionsForTest(t, `
product Demo {
    param variant: string = "B"
    target Pad: action = table_cell("variants", variant, "chamferAction")
}`, nil, actionPlannerTestTables())
	if err != nil {
		t.Fatalf("table: unexpected error: %v", err)
	}

	if literal[0].Action != "hide" {
		t.Fatalf("literal: expected 'hide', got %q", literal[0].Action)
	}
	if ternary[0].Action != "hide" {
		t.Fatalf("ternary: expected 'hide', got %q", ternary[0].Action)
	}
	if tableBacked[0].Action != "hide" {
		t.Fatalf("table: expected 'hide', got %q", tableBacked[0].Action)
	}
}

// TestEvaluateProductTargetActions_TernaryTableComposition proves a
// ternary that selects between a table-backed action branch and a literal
// action branch evaluates correctly for both branches, with no special
// target-action ternary implementation (the existing generic ternary
// evaluation seam handles it).
func TestEvaluateProductTargetActions_TernaryTableComposition(t *testing.T) {
	dslContent := `
product Demo {
    param useTable: boolean = true
    param variant: string = "A"

    target Pad:
        action =
            useTable
                ? table_cell("variants", variant, "chamferAction")
                : keep
}`
	tables := actionPlannerTestTables()

	useTableTrue, err := evaluateTargetActionsForTest(t, dslContent, nil, tables)
	if err != nil {
		t.Fatalf("useTable=true: unexpected error: %v", err)
	}
	if useTableTrue[0].Action != "suppress" {
		t.Fatalf("useTable=true: expected 'suppress', got %q", useTableTrue[0].Action)
	}

	useTableFalse, err := evaluateTargetActionsForTest(t, dslContent, map[string]string{"useTable": "false"}, tables)
	if err != nil {
		t.Fatalf("useTable=false: unexpected error: %v", err)
	}
	if useTableFalse[0].Action != "keep" {
		t.Fatalf("useTable=false: expected 'keep', got %q", useTableFalse[0].Action)
	}
}

// TestEvaluateProductTargetActions_OverrideParticipatesInSelection is the
// critical Task 5 override proof: table-backed action evaluation occurs
// after parameter override resolution, so overriding the row-key parameter
// changes the selected row and thus the evaluated action.
func TestEvaluateProductTargetActions_OverrideParticipatesInSelection(t *testing.T) {
	dslContent := `
product Demo {
    param variant: string = "A"

    target Chamfer:
        action = table_cell("variants", variant, "chamferAction")
}`
	tables := actionPlannerTestTables()

	noOverride, err := evaluateTargetActionsForTest(t, dslContent, nil, tables)
	if err != nil {
		t.Fatalf("no override: unexpected error: %v", err)
	}
	if noOverride[0].Action != "suppress" {
		t.Fatalf("no override: expected default row A -> 'suppress', got %q", noOverride[0].Action)
	}

	overridden, err := evaluateTargetActionsForTest(t, dslContent, map[string]string{"variant": "B"}, tables)
	if err != nil {
		t.Fatalf("override variant=B: unexpected error: %v", err)
	}
	if overridden[0].Action != "hide" {
		t.Fatalf("override variant=B: expected row B -> 'hide', got %q", overridden[0].Action)
	}
}

// TestCreatePlanWithTables_TargetAction_OverrideToInvalidSelectionFails
// proves, through the real public CreatePlanWithTables entry point, that a
// parameter override may steer table-backed action selection onto an
// invalid row even though the DSL's default row is valid: the validator
// does not reject the DSL merely because an invalid row exists somewhere in
// the table, but planner evaluation of the actual overridden selection
// fails deterministically.
func TestCreatePlanWithTables_TargetAction_OverrideToInvalidSelectionFails(t *testing.T) {
	dslContent := `
product Demo {
    param variant: string = "A"

    target Chamfer:
        action = table_cell("variants", variant, "chamferAction")
}`
	tables := actionPlannerTestTables()

	ast := parseAndValidateWithTables(t, dslContent, tables)

	if _, err := CreatePlanWithTables(ast, nil, tables); err != nil {
		t.Fatalf("no override: expected CreatePlanWithTables to succeed, got: %v", err)
	}

	_, err := CreatePlanWithTables(ast, map[string]string{"variant": "BAD"}, tables)
	if err == nil {
		t.Fatal("override variant=BAD: expected CreatePlanWithTables to fail")
	}
	want := `error evaluating target actions in product 'Demo': target action 'Chamfer': function 'table_cell' selected invalid action value 'explode' from table 'variants' row "BAD" column 'chamferAction'; allowed values are [keep suppress unsuppress hide unhide delete]`
	if err.Error() != want {
		t.Fatalf("expected exact error:\n%q\ngot:\n%q", want, err.Error())
	}
}

// TestEvaluateProductTargetActions_InvalidSelectedValueDiagnosticExact locks
// the exact diagnostic produced by the internal evaluateProductTargetActions
// seam for an invalid selected table-backed action value, matching
// production output verbatim.
func TestEvaluateProductTargetActions_InvalidSelectedValueDiagnosticExact(t *testing.T) {
	_, err := evaluateTargetActionsForTest(t, `
product Demo {
    param variant: string = "A"

    target Chamfer:
        action = table_cell("variants", variant, "chamferAction")
}`, map[string]string{"variant": "BAD"}, actionPlannerTestTables())
	if err == nil {
		t.Fatal("expected error for selected invalid action value")
	}
	want := `target action 'Chamfer': function 'table_cell' selected invalid action value 'explode' from table 'variants' row "BAD" column 'chamferAction'; allowed values are [keep suppress unsuppress hide unhide delete]`
	if err.Error() != want {
		t.Fatalf("expected exact error:\n%q\ngot:\n%q", want, err.Error())
	}

	for _, want := range []string{
		"target action 'Chamfer'",
		"table 'variants'",
		`row "BAD"`,
		"column 'chamferAction'",
		"selected invalid action value 'explode'",
		"allowed values are [keep suppress unsuppress hide unhide delete]",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected diagnostic to contain %q, got %q", want, err.Error())
		}
	}
}

// TestCreatePlanWithTables_TargetAction_UnusedInvalidRowDoesNotFailValidSelection
// proves that an unused invalid row elsewhere in the same generic table
// column does not fail a product whose actual selected row is valid: only
// the selected value is evaluated, never the whole column.
func TestCreatePlanWithTables_TargetAction_UnusedInvalidRowDoesNotFailValidSelection(t *testing.T) {
	dslContent := `
product Demo {
    param variant: string = "A"

    target Chamfer:
        action = table_cell("variants", variant, "chamferAction")
}`
	tables := actionPlannerTestTables()

	ast := parseAndValidateWithTables(t, dslContent, tables)
	if _, err := CreatePlanWithTables(ast, nil, tables); err != nil {
		t.Fatalf("expected CreatePlanWithTables to succeed despite unused invalid 'BAD' row, got: %v", err)
	}

	evaluated, err := evaluateTargetActionsForTest(t, dslContent, nil, tables)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evaluated[0].Action != "suppress" {
		t.Fatalf("expected 'suppress', got %q", evaluated[0].Action)
	}
}

// TestEvaluateProductTargetActions_CaseSensitiveSelectedValue proves a
// case-mismatched selected table value ("Suppress") fails planner
// evaluation: no case folding is performed. The row key is dynamic
// (parameter-bound, selected via override) because a statically known row
// key of "CASE" would already be caught at DSL validation time; this test
// isolates the planner's own selected-value check.
func TestEvaluateProductTargetActions_CaseSensitiveSelectedValue(t *testing.T) {
	_, err := evaluateTargetActionsForTest(t, `
product Demo {
    param variant: string = "A"

    target Chamfer:
        action = table_cell("variants", variant, "chamferAction")
}`, map[string]string{"variant": "CASE"}, actionPlannerTestTables())
	if err == nil {
		t.Fatal("expected error for case-mismatched selected action value")
	}
	if !strings.Contains(err.Error(), "selected invalid action value 'Suppress'") {
		t.Fatalf("expected case-sensitive rejection diagnostic, got %q", err.Error())
	}
}

// TestEvaluateProductTargetActions_WhitespaceSelectedValue proves a
// whitespace-noncanonical selected table value ("suppress ") fails planner
// evaluation: no trimming is performed. The row key is dynamic
// (parameter-bound, selected via override) for the same reason as the
// case-sensitivity test above.
func TestEvaluateProductTargetActions_WhitespaceSelectedValue(t *testing.T) {
	_, err := evaluateTargetActionsForTest(t, `
product Demo {
    param variant: string = "A"

    target Chamfer:
        action = table_cell("variants", variant, "chamferAction")
}`, map[string]string{"variant": "SPACE"}, actionPlannerTestTables())
	if err == nil {
		t.Fatal("expected error for whitespace-noncanonical selected action value")
	}
	if !strings.Contains(err.Error(), "selected invalid action value 'suppress '") {
		t.Fatalf("expected whitespace-sensitive rejection diagnostic, got %q", err.Error())
	}
}

// TestEvaluateProductTargetActions_CanonicalLiteralWinsOverSameNamedBinding
// proves the canonical action literal wins over a same-named ordinary
// binding in planner evaluation: the evaluated action is the string
// 'suppress', not the boolean true carried by the same-named parameter.
func TestEvaluateProductTargetActions_CanonicalLiteralWinsOverSameNamedBinding(t *testing.T) {
	evaluated, err := evaluateTargetActionsForTest(t, `
product Demo {
    param suppress: boolean = true
    target Pad: action = suppress
}`, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evaluated[0].Action != "suppress" {
		t.Fatalf("expected canonical action literal 'suppress' to win over same-named boolean binding, got %q (%T)", evaluated[0].Action, evaluated[0].Action)
	}
}

// TestCreatePlanWithTables_TargetAction_GenericStringColumnUnaffected proves
// the same string-backed table column used for actions elsewhere remains
// usable as an ordinary string through the full CreatePlanWithTables entry
// point: Task 5 does not globally reinterpret string columns as actions.
func TestCreatePlanWithTables_TargetAction_GenericStringColumnUnaffected(t *testing.T) {
	ast := parseAndValidateWithTables(t, `
product Demo {
    param chosen: string = table_cell("variants", "A", "chamferAction")
}`, actionPlannerTestTables())

	plan, err := CreatePlanWithTables(ast, nil, actionPlannerTestTables())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	payload := plan.Steps[0].Payload.(WriteCSVPayload)
	if len(payload.Values) != 1 || payload.Values[0] != "suppress" {
		t.Fatalf("expected ordinary string lookup to remain unaffected, got %v", payload.Values)
	}
}

// TestEvaluateProductTargetActions_MissingSemanticTargetBoundary proves
// Task 5 performs no semantic target existence resolution: a target action
// naming a semantic target with no corresponding model still validates and
// evaluates successfully. Semantic target resolution is Task 6 scope.
func TestEvaluateProductTargetActions_MissingSemanticTargetBoundary(t *testing.T) {
	dslContent := `
product Demo {
    target DoesNotExist:
        action = table_cell("variants", "A", "chamferAction")
}`
	tables := actionPlannerTestTables()

	ast := parseDSLForPlannerTest(t, dslContent)
	if err := dsl.ValidateWithTables(ast, tables); err != nil {
		t.Fatalf("DSL validation: unexpected error for missing semantic target: %v", err)
	}

	evaluated, err := evaluateTargetActionsForTest(t, dslContent, nil, tables)
	if err != nil {
		t.Fatalf("planner evaluation: unexpected error for missing semantic target: %v", err)
	}
	if evaluated[0].SemanticTarget != "DoesNotExist" || evaluated[0].Action != "suppress" {
		t.Fatalf("expected SemanticTarget 'DoesNotExist' Action 'suppress', got %+v", evaluated[0])
	}
}
