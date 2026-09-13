package dsl

import (
	"strings"
	"testing"
)

func TestParser_ProductLets(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		assertions func(t *testing.T, ast *AST)
	}{
		{
			name: "single let",
			input: `product Widget {
    let width_mm = 42
}`,
			assertions: func(t *testing.T, ast *AST) {
				t.Helper()
				if len(ast.Products) != 1 {
					t.Fatalf("expected 1 product, got %d", len(ast.Products))
				}

				prod := ast.Products[0]
				if len(prod.Lets) != 1 {
					t.Fatalf("expected 1 let, got %d", len(prod.Lets))
				}
				if prod.Lets[0].Name != "width_mm" {
					t.Fatalf("expected let name width_mm, got %q", prod.Lets[0].Name)
				}
				lit, ok := prod.Lets[0].Value.(*LiteralExpression)
				if !ok {
					t.Fatalf("expected literal let value, got %T", prod.Lets[0].Value)
				}
				if lit.Value != 42.0 {
					t.Fatalf("expected let value 42, got %#v", lit.Value)
				}
				if len(prod.Parameters) != 0 {
					t.Fatalf("expected no params, got %d", len(prod.Parameters))
				}
			},
		},
		{
			name: "multiple lets",
			input: `product Widget {
    let width_mm = 42
    let height_mm = width_mm + 8
}`,
			assertions: func(t *testing.T, ast *AST) {
				t.Helper()
				prod := ast.Products[0]
				if len(prod.Lets) != 2 {
					t.Fatalf("expected 2 lets, got %d", len(prod.Lets))
				}
				if prod.Lets[0].Name != "width_mm" || prod.Lets[1].Name != "height_mm" {
					t.Fatalf("unexpected let names: %q, %q", prod.Lets[0].Name, prod.Lets[1].Name)
				}
				expr, ok := prod.Lets[1].Value.(*BinaryExpression)
				if !ok {
					t.Fatalf("expected binary expression, got %T", prod.Lets[1].Value)
				}
				if expr.Operator != "+" {
					t.Fatalf("expected + operator, got %q", expr.Operator)
				}
			},
		},
		{
			name: "mixed lets and params",
			input: `product Widget {
    let base = 5
    param width: number = base + 1
    let label = "w"
    param name: string = label
}`,
			assertions: func(t *testing.T, ast *AST) {
				t.Helper()
				prod := ast.Products[0]
				if len(prod.Lets) != 2 {
					t.Fatalf("expected 2 lets, got %d", len(prod.Lets))
				}
				if len(prod.Parameters) != 2 {
					t.Fatalf("expected 2 params, got %d", len(prod.Parameters))
				}
				if prod.Parameters[0].Name != "width" || prod.Parameters[1].Name != "name" {
					t.Fatalf("unexpected parameter names: %q, %q", prod.Parameters[0].Name, prod.Parameters[1].Name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parser{
				input: []rune(tt.input),
				line:  1,
				col:   1,
			}

			ast, err := p.parse()
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			tt.assertions(t, ast)
		})
	}
}

func TestParser_ProductLetErrors(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		errorContains string
	}{
		{
			name:          "missing identifier",
			input:         `product Widget { let = 5 }`,
			errorContains: "invalid identifier '='",
		},
		{
			name:          "missing equals",
			input:         `product Widget { let x }`,
			errorContains: "expected '=', got '}'",
		},
		{
			name:          "missing expression",
			input:         `product Widget { let x = }`,
			errorContains: "unexpected token in expression: '}'",
		},
		{
			name:          "typed let rejected",
			input:         `product Widget { let x: number = 5 }`,
			errorContains: "expected '=', got ':'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parser{
				input: []rune(tt.input),
				line:  1,
				col:   1,
			}

			_, err := p.parse()
			if err == nil {
				t.Fatalf("expected parse error containing %q, got nil", tt.errorContains)
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected parse error containing %q, got %v", tt.errorContains, err)
			}
		})
	}
}

func TestParser_TopLevelLetRejected(t *testing.T) {
	p := &Parser{
		input: []rune(`let x = 1

product P {
  param a: number = 1
}`),
		line: 1,
		col:  1,
	}

	_, err := p.parse()
	if err == nil {
		t.Fatal("expected parse error for top-level let, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected token 'let'") {
		t.Fatalf("expected top-level let parse error, got %v", err)
	}
}
