package dsl

import (
	"os"
	"strings"
	"testing"
)

// parseDSLForValidatorActionTest parses DSL content through the public Parse
// entry point, matching the temp-file convention used by validator_test.go
// and validator_table_test.go, so parser and validator behavior are both
// exercised through their real production entry points.
func parseDSLForValidatorActionTest(t *testing.T, content string) (*AST, error) {
	t.Helper()
	return parseDSLContentForActionTest(t, withDSLVersionHeader(content))
}

func parseDSLContentForActionTest(t *testing.T, content string) (*AST, error) {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "validator_action_test_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(content)); err != nil {
		t.Fatalf("failed to write temp DSL: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp DSL: %v", err)
	}

	return Parse(tmpFile.Name())
}

// TestValidate_TargetAction_CanonicalDomain permanently locks that exactly
// the six canonical action identifiers Parse PASS and Validate PASS in
// target action context.
func TestValidate_TargetAction_CanonicalDomain(t *testing.T) {
	for _, action := range []string{"keep", "suppress", "unsuppress", "hide", "unhide", "delete"} {
		t.Run(action, func(t *testing.T) {
			ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    target Pad: action = `+action+`
}`)
			if err != nil {
				t.Fatalf("Parse: unexpected error for canonical action %q: %v", action, err)
			}
			if err := Validate(ast); err != nil {
				t.Fatalf("Validate: unexpected error for canonical action %q: %v", action, err)
			}
		})
	}
}

// TestValidate_TargetAction_DomainExactness proves the canonical action
// domain is exactly the six values, in order, via the exported diagnostic
// text produced for an invalid action value (the package does not export
// the canonical slice, so the domain is locked through observable
// validation behavior rather than direct slice access).
func TestValidate_TargetAction_DomainExactness(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    target Pad: action = explode
}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	err = Validate(ast)
	if err == nil {
		t.Fatal("expected validation error for 'explode'")
	}
	wantDomain := "[keep suppress unsuppress hide unhide delete]"
	if !strings.Contains(err.Error(), wantDomain) {
		t.Fatalf("expected error to contain domain %q, got %q", wantDomain, err.Error())
	}
}

// TestValidate_TargetAction_UnsupportedIdentifiers permanently locks that
// unsupported, typo, and case-mismatched action identifiers parse but fail
// validation deterministically.
func TestValidate_TargetAction_UnsupportedIdentifiers(t *testing.T) {
	for _, action := range []string{"explode", "supress", "Suppress", "SUPPRESS", "remove", "deleted"} {
		t.Run(action, func(t *testing.T) {
			ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    target Pad: action = `+action+`
}`)
			if err != nil {
				t.Fatalf("Parse: unexpected error for %q: %v", action, err)
			}
			err = Validate(ast)
			if err == nil {
				t.Fatalf("Validate: expected error for unsupported action %q", action)
			}
			wantMsg := "invalid action value '" + action + "', allowed values are [keep suppress unsuppress hide unhide delete]"
			if !strings.Contains(err.Error(), wantMsg) {
				t.Fatalf("expected error containing %q, got %q", wantMsg, err.Error())
			}
		})
	}
}

// TestValidate_TargetAction_UnsupportedDiagnosticExact locks the exact,
// full diagnostic shape produced for an invalid action value, matching
// production output verbatim.
func TestValidate_TargetAction_UnsupportedDiagnosticExact(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    target Pad: action = explode
}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	err = Validate(ast)
	if err == nil {
		t.Fatal("expected validation error")
	}
	want := "validation error in product 'Demo': in target action 'Pad': invalid action value 'explode', allowed values are [keep suppress unsuppress hide unhide delete]"
	if err.Error() != want {
		t.Fatalf("expected exact error:\n%q\ngot:\n%q", want, err.Error())
	}
}

// TestValidate_TargetAction_WrongLiteralType permanently locks that no
// literal type (string, boolean, number) implicitly coerces to action.
func TestValidate_TargetAction_WrongLiteralType(t *testing.T) {
	tests := []struct {
		name          string
		literal       string
		errorContains string
	}{
		{"string literal", `"suppress"`, "expected action literal, but got string literal"},
		{"boolean literal true", `true`, "expected action literal, but got boolean literal"},
		{"boolean literal false", `false`, "expected action literal, but got boolean literal"},
		{"number literal integer", `1`, "expected action literal, but got number literal"},
		{"number literal decimal", `1.5`, "expected action literal, but got number literal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    target Pad: action = `+tt.literal+`
}`)
			if err != nil {
				t.Fatalf("Parse: unexpected error: %v", err)
			}
			err = Validate(ast)
			if err == nil {
				t.Fatalf("Validate: expected error for literal %s", tt.literal)
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got %q", tt.errorContains, err.Error())
			}
		})
	}
}

// TestValidate_TargetAction_InterpolatedStringRejected proves interpolated
// strings remain strings and do not become actions, even when their
// literal content matches a canonical action word.
func TestValidate_TargetAction_InterpolatedStringRejected(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    param desired: string = "suppress"
    target Pad: action = "{param:desired}"
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	err = Validate(ast)
	if err == nil {
		t.Fatal("Validate: expected error for interpolated string in action context")
	}
	if !strings.Contains(err.Error(), "expected type is 'action'") {
		t.Fatalf("expected error about expected type 'action', got %q", err.Error())
	}
}

// TestValidate_TargetAction_ProductBindingWrongType permanently locks that
// boolean, number, and string product bindings do not coerce to action.
func TestValidate_TargetAction_ProductBindingWrongType(t *testing.T) {
	tests := []struct {
		name          string
		dslContent    string
		errorContains string
	}{
		{
			name: "boolean parameter",
			dslContent: `
product Demo {
    param enabled: boolean = true
    target Pad: action = enabled
}`,
			errorContains: "binding reference 'enabled' has type 'boolean', but expected 'action'",
		},
		{
			name: "number parameter",
			dslContent: `
product Demo {
    param count: number = 1
    target Pad: action = count
}`,
			errorContains: "binding reference 'count' has type 'number', but expected 'action'",
		},
		{
			name: "string let",
			dslContent: `
product Demo {
    let desired = "suppress"
    target Pad: action = desired
}`,
			errorContains: "binding reference 'desired' has type 'string', but expected 'action'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parseDSLForValidatorActionTest(t, tt.dslContent)
			if err != nil {
				t.Fatalf("Parse: unexpected error: %v", err)
			}
			err = Validate(ast)
			if err == nil {
				t.Fatal("Validate: expected error")
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got %q", tt.errorContains, err.Error())
			}
		})
	}
}

// TestValidate_TargetAction_EnumBindingRejected proves enum bindings do not
// coerce to action even when their enum value set overlaps the canonical
// action domain.
func TestValidate_TargetAction_EnumBindingRejected(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    param desired: enum {keep, suppress} = suppress
    target Pad: action = desired
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	err = Validate(ast)
	if err == nil {
		t.Fatal("Validate: expected error for enum binding in action context")
	}
	if !strings.Contains(err.Error(), "binding reference 'desired' has type 'enum', but expected 'action'") {
		t.Fatalf("expected enum-vs-action type mismatch error, got %q", err.Error())
	}
}

// TestValidate_TargetAction_ConstantDoesNotCoerce proves ordinary constants
// do not become actions, even when their value matches a canonical action
// word. Unknown identifiers in constant expressions are evaluated as enum
// literals by the existing constant model (see evaluateConstantExpression),
// so the resulting constant type is 'enum', not 'action'.
func TestValidate_TargetAction_ConstantDoesNotCoerce(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
const desired = suppress
product Demo {
    target Pad: action = desired
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	err = Validate(ast)
	if err == nil {
		t.Fatal("Validate: expected error for constant reference in action context")
	}
	if !strings.Contains(err.Error(), "constant reference 'desired' has type 'enum', but expected 'action'") {
		t.Fatalf("expected constant-vs-action type mismatch error, got %q", err.Error())
	}
}

// TestValidate_TargetAction_CanonicalLiteralPrecedence proves a same-named
// ordinary binding cannot shadow a canonical action literal in action
// context: canonical action literal lookup occurs before ordinary binding
// lookup.
func TestValidate_TargetAction_CanonicalLiteralPrecedence(t *testing.T) {
	for _, action := range []string{"keep", "suppress", "unsuppress", "hide", "unhide", "delete"} {
		t.Run(action, func(t *testing.T) {
			ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    param `+action+`: boolean = true
    target Pad: action = `+action+`
}`)
			if err != nil {
				t.Fatalf("Parse: unexpected error: %v", err)
			}
			if err := Validate(ast); err != nil {
				t.Fatalf("Validate: expected canonical action literal %q to win over same-named boolean binding, got error: %v", action, err)
			}
		})
	}
}

// TestValidate_TargetAction_TernaryPositive proves a boolean-conditioned
// ternary with canonical action branches validates.
func TestValidate_TargetAction_TernaryPositive(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    param removeHole: boolean = true
    target Pocket: action = removeHole ? suppress : unsuppress
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if err := Validate(ast); err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
}

// TestValidate_TargetAction_NestedTernary proves the expected 'action' type
// propagates recursively through nested ternary branches.
func TestValidate_TargetAction_NestedTernary(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    param a: boolean = true
    param b: boolean = false
    target Pocket: action = a ? suppress : (b ? hide : keep)
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if err := Validate(ast); err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
}

// TestValidate_TargetAction_TernaryConditionWrongType proves the ternary
// condition must remain boolean, even in action context.
func TestValidate_TargetAction_TernaryConditionWrongType(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    param count: number = 1
    target Pocket: action = count ? suppress : unsuppress
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	err = Validate(ast)
	if err == nil {
		t.Fatal("Validate: expected error for non-boolean ternary condition")
	}
	if !strings.Contains(err.Error(), "ternary condition must be a boolean expression") {
		t.Fatalf("expected ternary condition error, got %q", err.Error())
	}
}

// TestValidate_TargetAction_TernaryInvalidTrueBranch proves the true branch
// of an action ternary is validated in action context, preserving the
// existing true-branch diagnostic wrapping.
func TestValidate_TargetAction_TernaryInvalidTrueBranch(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    param enabled: boolean = true
    target Pocket: action = enabled ? explode : suppress
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	err = Validate(ast)
	if err == nil {
		t.Fatal("Validate: expected error for invalid true branch")
	}
	if !strings.Contains(err.Error(), "ternary 'true' branch error") {
		t.Fatalf("expected ternary true-branch wrapping, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "invalid action value 'explode'") {
		t.Fatalf("expected invalid action value diagnostic, got %q", err.Error())
	}
}

// TestValidate_TargetAction_TernaryInvalidFalseBranch proves the false
// branch of an action ternary is validated in action context, preserving
// the existing false-branch diagnostic wrapping.
func TestValidate_TargetAction_TernaryInvalidFalseBranch(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    param enabled: boolean = true
    target Pocket: action = enabled ? suppress : explode
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	err = Validate(ast)
	if err == nil {
		t.Fatal("Validate: expected error for invalid false branch")
	}
	if !strings.Contains(err.Error(), "ternary 'false' branch error") {
		t.Fatalf("expected ternary false-branch wrapping, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "invalid action value 'explode'") {
		t.Fatalf("expected invalid action value diagnostic, got %q", err.Error())
	}
}

// TestValidate_TargetAction_TernaryWrongTypeBranch proves no string
// coercion occurs even inside ternary branches.
func TestValidate_TargetAction_TernaryWrongTypeBranch(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    param enabled: boolean = true
    target Pocket: action = enabled ? suppress : "unsuppress"
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	err = Validate(ast)
	if err == nil {
		t.Fatal("Validate: expected error for string literal in action ternary branch")
	}
	if !strings.Contains(err.Error(), "ternary 'false' branch error") {
		t.Fatalf("expected ternary false-branch wrapping, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "expected action literal, but got string literal") {
		t.Fatalf("expected string-literal rejection, got %q", err.Error())
	}
}

// TestParser_TargetAction_ExplodeParsesValidationFails permanently locks the
// parser/validator split established in Task 3/Task 4: 'explode' remains
// structurally valid at parse time (parser owns syntax), and is rejected
// only at validation time (validator owns the action domain).
func TestParser_TargetAction_ExplodeParsesValidationFails(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    target Pad: action = explode
}`)
	if err != nil {
		t.Fatalf("Parse: expected PASS for 'explode', got error: %v", err)
	}

	ta := ast.Products[0].TargetActions[0]
	ident, ok := ta.Action.(*IdentifierExpression)
	if !ok {
		t.Fatalf("expected TargetActionNode.Action to remain an *IdentifierExpression, got %T", ta.Action)
	}
	if ident.Name != "explode" {
		t.Fatalf("expected identifier 'explode', got %q", ident.Name)
	}

	if err := Validate(ast); err == nil {
		t.Fatal("Validate: expected FAIL for 'explode'")
	}
}

// TestParser_ParamTypeAction_RemainsParserInvalid proves Task 4's internal
// ParamTypeAction does not expose 'action' as a user-authored parameter
// type: 'param ...: action = ...' must remain parser-invalid.
func TestParser_ParamTypeAction_RemainsParserInvalid(t *testing.T) {
	_, err := parseDSLContentForActionTest(t, withDSLVersionHeader(`
product Demo {
    param desired: action = suppress
}`))
	if err == nil {
		t.Fatal("expected parse error for 'param ...: action' syntax")
	}
	if !strings.Contains(err.Error(), "unknown type 'action'") {
		t.Fatalf("expected 'unknown type' parse error, got %q", err.Error())
	}
}

// TestValidate_TargetAction_LetDoesNotBecomeAction proves Task 4 did not
// introduce action-valued let inference: a let bound to a canonical action
// word infers to a non-action type (enum, per the existing binding-type
// inference model for unresolved bare identifiers), so referencing it in
// action context fails. The exact failure mechanism is not the contract;
// only that it does NOT Validate PASS as an action-valued binding.
func TestValidate_TargetAction_LetDoesNotBecomeAction(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    let desired = suppress
    target Pad: action = desired
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if err := Validate(ast); err == nil {
		t.Fatal("Validate: action-valued let must not validate as an action binding")
	}
}

// TestParser_TargetAction_ParamTypeActionKeywordUnaffectedReservation proves
// introducing the internal ParamTypeAction kind did not change parser
// reservation behavior for 'action' as a keyword; this remains governed by
// Task 3's reservedIdentifiers map, unmodified by Task 4.
func TestParser_TargetAction_ParamTypeActionKeywordUnaffectedReservation(t *testing.T) {
	_, err := parseDSLContentForActionTest(t, withDSLVersionHeader(`
product Demo {
    param action: number = 1
}`))
	if err == nil {
		t.Fatal("expected parse error for 'action' used as a param name")
	}
	if !strings.Contains(err.Error(), "reserved keyword") {
		t.Fatalf("expected reserved keyword error, got %q", err.Error())
	}
}

// TestValidateWithTables_TargetAction_TableCellStringRejected proves
// string-backed table_cell lookups remain action-invalid: Task 4 does not
// introduce an action table column type or string-to-action table
// coercion. Reuses the existing validatorTestTables() "fasteners" fixture
// and its "code" string column rather than introducing a new fixture.
func TestValidateWithTables_TargetAction_TableCellStringRejected(t *testing.T) {
	ast := parseDSLForValidatorTableTest(t, `
product Fastener {
    target Chamfer: action = table_cell("fasteners", "M8x20", "code")
}`)
	err := ValidateWithTables(ast, validatorTestTables())
	if err == nil {
		t.Fatal("ValidateWithTables: expected error for string-backed table_cell in action context")
	}
	if !strings.Contains(err.Error(), "returns a string") {
		t.Fatalf("expected 'returns a string' type-mismatch diagnostic, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "expected type is 'action'") {
		t.Fatalf("expected expected-type 'action' diagnostic, got %q", err.Error())
	}
}

// TestValidate_TargetAction_NumericBuiltinRejected proves no numeric
// builtin returns an action value.
func TestValidate_TargetAction_NumericBuiltinRejected(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    target Pad: action = max(1, 2)
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	err = Validate(ast)
	if err == nil {
		t.Fatal("Validate: expected error for numeric builtin in action context")
	}
	if !strings.Contains(err.Error(), "returns a number, but expected type is 'action'") {
		t.Fatalf("expected number-vs-action diagnostic, got %q", err.Error())
	}
}

// TestValidate_TargetAction_OperatorsRejected permanently locks that no
// arithmetic/logical operator semantics exist for action values.
func TestValidate_TargetAction_OperatorsRejected(t *testing.T) {
	tests := []struct {
		name       string
		dslContent string
	}{
		{
			name: "binary + on actions",
			dslContent: `
product Demo {
    target Pad: action = suppress + unsuppress
}`,
		},
		{
			name: "unary ! on action",
			dslContent: `
product Demo {
    target Pad: action = !suppress
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parseDSLForValidatorActionTest(t, tt.dslContent)
			if err != nil {
				t.Fatalf("Parse: unexpected error: %v", err)
			}
			if err := Validate(ast); err == nil {
				t.Fatal("Validate: expected error for operator applied to action operands")
			}
		})
	}
}

// TestValidate_TargetAction_MissingSemanticTargetDoesNotMatterYet proves
// Task 4 validates action typing only: a target action referencing a
// semantic target name with no corresponding semantic model still
// Validate PASSes at the DSL layer, since Task 4 does not perform semantic
// target resolution.
func TestValidate_TargetAction_MissingSemanticTargetDoesNotMatterYet(t *testing.T) {
	ast, err := parseDSLForValidatorActionTest(t, `
product Demo {
    target DoesNotExist: action = suppress
}`)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if err := Validate(ast); err != nil {
		t.Fatalf("Validate: unexpected error for missing semantic target (Task 4 does not resolve targets): %v", err)
	}
}
