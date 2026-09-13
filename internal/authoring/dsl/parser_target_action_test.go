package dsl

import (
	"strings"
	"testing"
)

// parseTargetActionSnippet parses raw DSL source without requiring the
// version directive, matching the direct-parser convention used by the
// other Task 3 parser test files (parser_let_test.go, parser_reserved_test.go).
func parseTargetActionSnippet(t *testing.T, input string) (*AST, error) {
	t.Helper()
	p := &Parser{
		input: []rune(input),
		line:  1,
		col:   1,
	}
	return p.parse()
}

func TestParser_TargetAction_Canonical(t *testing.T) {
	ast, err := parseTargetActionSnippet(t, `product Demo {
    target Pad: action = suppress
}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if len(ast.Products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(ast.Products))
	}

	prod := ast.Products[0]
	if len(prod.TargetActions) != 1 {
		t.Fatalf("expected 1 target action, got %d", len(prod.TargetActions))
	}

	ta := prod.TargetActions[0]
	if ta.SemanticTarget != "Pad" {
		t.Fatalf("expected SemanticTarget 'Pad', got %q", ta.SemanticTarget)
	}

	ident, ok := ta.Action.(*IdentifierExpression)
	if !ok {
		t.Fatalf("expected *IdentifierExpression action, got %T", ta.Action)
	}
	if ident.Name != "suppress" {
		t.Fatalf("expected action identifier 'suppress', got %q", ident.Name)
	}
}

func TestParser_TargetAction_NoDeclarationsNilCompatible(t *testing.T) {
	ast, err := parseTargetActionSnippet(t, `product Widget {
    param width: number = 1200
}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	prod := ast.Products[0]
	if prod.TargetActions != nil {
		t.Fatalf("expected nil TargetActions for product with no target declarations, got %#v", prod.TargetActions)
	}
}

func TestParser_TargetAction_SourceOrderPreserved(t *testing.T) {
	ast, err := parseTargetActionSnippet(t, `product Demo {
    target Chamfer: action = keep
    target Pad: action = suppress
    target Body: action = hide
}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	prod := ast.Products[0]
	if len(prod.TargetActions) != 3 {
		t.Fatalf("expected 3 target actions, got %d", len(prod.TargetActions))
	}

	wantTargets := []string{"Chamfer", "Pad", "Body"}
	wantActions := []string{"keep", "suppress", "hide"}
	for i, ta := range prod.TargetActions {
		if ta.SemanticTarget != wantTargets[i] {
			t.Fatalf("index %d: expected SemanticTarget %q, got %q", i, wantTargets[i], ta.SemanticTarget)
		}
		ident, ok := ta.Action.(*IdentifierExpression)
		if !ok {
			t.Fatalf("index %d: expected *IdentifierExpression action, got %T", i, ta.Action)
		}
		if ident.Name != wantActions[i] {
			t.Fatalf("index %d: expected action identifier %q, got %q", i, wantActions[i], ident.Name)
		}
	}
}

func TestParser_TargetAction_ExpressionParserReuse(t *testing.T) {
	t.Run("identifier RHS", func(t *testing.T) {
		ast, err := parseTargetActionSnippet(t, `product Demo {
    target Pad: action = requestedAction
}`)
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}
		ta := ast.Products[0].TargetActions[0]
		ident, ok := ta.Action.(*IdentifierExpression)
		if !ok {
			t.Fatalf("expected *IdentifierExpression, got %T", ta.Action)
		}
		if ident.Name != "requestedAction" {
			t.Fatalf("expected identifier 'requestedAction', got %q", ident.Name)
		}
	})

	t.Run("ternary RHS is structural only", func(t *testing.T) {
		ast, err := parseTargetActionSnippet(t, `product Demo {
    target Pocket: action = removeHole ? suppress : unsuppress
}`)
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}
		ta := ast.Products[0].TargetActions[0]
		ternary, ok := ta.Action.(*TernaryExpression)
		if !ok {
			t.Fatalf("expected *TernaryExpression, got %T", ta.Action)
		}
		cond, ok := ternary.Condition.(*IdentifierExpression)
		if !ok || cond.Name != "removeHole" {
			t.Fatalf("expected condition identifier 'removeHole', got %#v", ternary.Condition)
		}
		trueExpr, ok := ternary.TrueExpr.(*IdentifierExpression)
		if !ok || trueExpr.Name != "suppress" {
			t.Fatalf("expected true-branch identifier 'suppress', got %#v", ternary.TrueExpr)
		}
		falseExpr, ok := ternary.FalseExpr.(*IdentifierExpression)
		if !ok || falseExpr.Name != "unsuppress" {
			t.Fatalf("expected false-branch identifier 'unsuppress', got %#v", ternary.FalseExpr)
		}
	})

	t.Run("function-call RHS is structural only", func(t *testing.T) {
		ast, err := parseTargetActionSnippet(t, `product Demo {
    target Chamfer: action = table_cell("variants", variant, "chamferAction")
}`)
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}
		ta := ast.Products[0].TargetActions[0]
		call, ok := ta.Action.(*FunctionCallExpression)
		if !ok {
			t.Fatalf("expected *FunctionCallExpression, got %T", ta.Action)
		}
		if call.Name != "table_cell" {
			t.Fatalf("expected function name 'table_cell', got %q", call.Name)
		}
		if len(call.Args) != 3 {
			t.Fatalf("expected 3 args, got %d", len(call.Args))
		}
		lit0, ok := call.Args[0].(*LiteralExpression)
		if !ok || lit0.Value != "variants" {
			t.Fatalf("expected first arg literal 'variants', got %#v", call.Args[0])
		}
		ident1, ok := call.Args[1].(*IdentifierExpression)
		if !ok || ident1.Name != "variant" {
			t.Fatalf("expected second arg identifier 'variant', got %#v", call.Args[1])
		}
		lit2, ok := call.Args[2].(*LiteralExpression)
		if !ok || lit2.Value != "chamferAction" {
			t.Fatalf("expected third arg literal 'chamferAction', got %#v", call.Args[2])
		}
	})
}

// TestParser_TargetAction_UnsupportedActionIdentifierParses permanently protects the
// Task 3 / Task 4 boundary: any structurally valid identifier parses as an action
// expression here. Task 4 owns the strict six-action domain and unknown-action
// rejection; Task 3 must not reject 'explode' at the parser level.
func TestParser_TargetAction_UnsupportedActionIdentifierParses(t *testing.T) {
	ast, err := parseTargetActionSnippet(t, `product Demo {
    target Pad: action = explode
}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	ta := ast.Products[0].TargetActions[0]
	ident, ok := ta.Action.(*IdentifierExpression)
	if !ok {
		t.Fatalf("expected *IdentifierExpression, got %T", ta.Action)
	}
	if ident.Name != "explode" {
		t.Fatalf("expected identifier 'explode', got %q", ident.Name)
	}
}

func TestParser_TargetAction_DuplicateDeclarations(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectErr     bool
		errorContains string
		assertions    func(t *testing.T, ast *AST)
	}{
		{
			name: "exact duplicate fails",
			input: `product Demo {
    target Pad: action = suppress
    target Pad: action = unsuppress
}`,
			expectErr:     true,
			errorContains: "duplicate target action declaration for semantic target 'Pad' in product 'Demo'",
		},
		{
			name: "duplicate with identical action expressions still fails",
			input: `product Demo {
    target Pad: action = suppress
    target Pad: action = suppress
}`,
			expectErr:     true,
			errorContains: "duplicate target action declaration for semantic target 'Pad' in product 'Demo'",
		},
		{
			name: "case variant is not a parser duplicate",
			input: `product Demo {
    target Pad: action = suppress
    target pad: action = unsuppress
}`,
			assertions: func(t *testing.T, ast *AST) {
				t.Helper()
				prod := ast.Products[0]
				if len(prod.TargetActions) != 2 {
					t.Fatalf("expected 2 target actions, got %d", len(prod.TargetActions))
				}
				if prod.TargetActions[0].SemanticTarget != "Pad" || prod.TargetActions[1].SemanticTarget != "pad" {
					t.Fatalf("unexpected semantic targets: %q, %q", prod.TargetActions[0].SemanticTarget, prod.TargetActions[1].SemanticTarget)
				}
			},
		},
		{
			name: "duplicate scope is per product",
			input: `product A {
    target Pad: action = suppress
}

product B {
    target Pad: action = unsuppress
}`,
			assertions: func(t *testing.T, ast *AST) {
				t.Helper()
				if len(ast.Products) != 2 {
					t.Fatalf("expected 2 products, got %d", len(ast.Products))
				}
				if len(ast.Products[0].TargetActions) != 1 || len(ast.Products[1].TargetActions) != 1 {
					t.Fatalf("expected 1 target action per product, got %d and %d",
						len(ast.Products[0].TargetActions), len(ast.Products[1].TargetActions))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parseTargetActionSnippet(t, tt.input)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected parse error containing %q, got nil", tt.errorContains)
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Fatalf("expected parse error containing %q, got %v", tt.errorContains, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if tt.assertions != nil {
				tt.assertions(t, ast)
			}
		})
	}
}

func TestParser_TargetAction_MalformedGrammar(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		errorContains string
	}{
		{
			name: "missing colon",
			input: `product Demo {
    target Pad action = suppress
}`,
			errorContains: "expected ':', got 'action'",
		},
		{
			name: "missing action keyword",
			input: `product Demo {
    target Pad: suppress = true
}`,
			errorContains: "expected 'action', got 'suppress'",
		},
		{
			name: "missing equals",
			input: `product Demo {
    target Pad: action suppress
}`,
			errorContains: "expected '=', got 'suppress'",
		},
		{
			name: "missing RHS expression",
			input: `product Demo {
    target Pad: action =
}`,
			errorContains: "unexpected token in expression: '}'",
		},
		{
			name: "missing target identifier",
			input: `product Demo {
    target : action = suppress
}`,
			errorContains: "invalid identifier ':'",
		},
		{
			name: "target-kind syntax 'feature' rejected",
			input: `product Demo {
    target feature Pad: action = suppress
}`,
			errorContains: "expected ':', got 'Pad'",
		},
		{
			name: "target-kind syntax 'component' rejected",
			input: `product Demo {
    target component Pad: action = suppress
}`,
			errorContains: "expected ':', got 'Pad'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseTargetActionSnippet(t, tt.input)
			if err == nil {
				t.Fatalf("expected parse error containing %q, got nil", tt.errorContains)
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected parse error containing %q, got %v", tt.errorContains, err)
			}
		})
	}
}

// TestParser_TargetAction_ExecutionDeclarationOrderUnaffected proves target-action
// declarations are tracked only in ProductNode.TargetActions and never enter
// ExecutionDeclarationOrder, which remains reserved for adapter/source_model/outputs.
func TestParser_TargetAction_ExecutionDeclarationOrderUnaffected(t *testing.T) {
	ast, err := parseTargetActionSnippet(t, `product Demo {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    target Pad: action = suppress
    outputs = ["step"]
}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	prod := ast.Products[0]
	wantOrder := []string{"adapter", "source_model", "outputs"}
	if len(prod.ExecutionDeclarationOrder) != len(wantOrder) {
		t.Fatalf("expected ExecutionDeclarationOrder %v, got %v", wantOrder, prod.ExecutionDeclarationOrder)
	}
	for i, key := range wantOrder {
		if prod.ExecutionDeclarationOrder[i] != key {
			t.Fatalf("expected ExecutionDeclarationOrder %v, got %v", wantOrder, prod.ExecutionDeclarationOrder)
		}
	}

	if len(prod.TargetActions) != 1 || prod.TargetActions[0].SemanticTarget != "Pad" {
		t.Fatalf("expected 1 target action for 'Pad', got %#v", prod.TargetActions)
	}
}

// TestParser_TargetAction_MixedDeclarationsCompatibility proves target-action
// declarations coexist with the existing let/param/adapter/source_model/outputs
// declarations without disturbing their independent AST population.
func TestParser_TargetAction_MixedDeclarationsCompatibility(t *testing.T) {
	ast, err := parseTargetActionSnippet(t, `product Demo {
    let base = 5
    param width: number = base + 1
    adapter = "freecad"
    source_model = "input/box.FCStd"
    target Pad: action = suppress
    target Body: action = hide
    outputs = ["step"]
}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	prod := ast.Products[0]

	if len(prod.Lets) != 1 || prod.Lets[0].Name != "base" {
		t.Fatalf("expected 1 let 'base', got %#v", prod.Lets)
	}
	if len(prod.Parameters) != 1 || prod.Parameters[0].Name != "width" {
		t.Fatalf("expected 1 param 'width', got %#v", prod.Parameters)
	}
	if lit, ok := prod.Adapter.(*LiteralExpression); !ok || lit.Value != "freecad" {
		t.Fatalf("expected adapter literal 'freecad', got %#v", prod.Adapter)
	}
	if lit, ok := prod.SourceModel.(*LiteralExpression); !ok || lit.Value != "input/box.FCStd" {
		t.Fatalf("expected source_model literal 'input/box.FCStd', got %#v", prod.SourceModel)
	}
	arr, ok := prod.Outputs.(*ArrayExpression)
	if !ok || len(arr.Elements) != 1 {
		t.Fatalf("expected outputs array with 1 element, got %#v", prod.Outputs)
	}

	if len(prod.TargetActions) != 2 {
		t.Fatalf("expected 2 target actions, got %d", len(prod.TargetActions))
	}
	if prod.TargetActions[0].SemanticTarget != "Pad" || prod.TargetActions[1].SemanticTarget != "Body" {
		t.Fatalf("unexpected target action order: %q, %q", prod.TargetActions[0].SemanticTarget, prod.TargetActions[1].SemanticTarget)
	}
}

// TestParser_FutureActionValuesNotGloballyReserved permanently protects the
// Task 3/Task 4 boundary: the six future canonical action values must not be
// added to reservedIdentifiers by this task. Task 4 owns the strict typed
// action domain; it must not be conflated with parser-keyword reservation.
func TestParser_FutureActionValuesNotGloballyReserved(t *testing.T) {
	futureActionValues := []string{"keep", "suppress", "unsuppress", "hide", "unhide", "delete"}

	for _, word := range futureActionValues {
		word := word
		t.Run(word+" not in reservedIdentifiers map", func(t *testing.T) {
			if reservedIdentifiers[word] {
				t.Fatalf("expected %q to not be globally reserved by Task 3, but it is", word)
			}
		})

		t.Run(word+" usable as product name", func(t *testing.T) {
			ast, err := parseTargetActionSnippet(t, `product `+word+` {}`)
			if err != nil {
				t.Fatalf("unexpected parse error using %q as product name: %v", word, err)
			}
			if ast.Products[0].Name != word {
				t.Fatalf("expected product name %q, got %q", word, ast.Products[0].Name)
			}
		})

		t.Run(word+" usable as let name", func(t *testing.T) {
			ast, err := parseTargetActionSnippet(t, `product X { let `+word+` = 1 }`)
			if err != nil {
				t.Fatalf("unexpected parse error using %q as let name: %v", word, err)
			}
			if ast.Products[0].Lets[0].Name != word {
				t.Fatalf("expected let name %q, got %q", word, ast.Products[0].Lets[0].Name)
			}
		})
	}
}
