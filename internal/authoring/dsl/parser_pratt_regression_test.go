package dsl

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var updatePrattGolden = flag.Bool("update", false, "update Pratt golden files")

func parseExprFromSnippet(t *testing.T, expr string) (ExpressionNode, error) {
	t.Helper()
	input := "product P {\n    param x: number = " + expr + "\n}"
	p := &Parser{
		input: []rune(input),
		line:  1,
		col:   1,
	}
	ast, err := p.parse()
	if err != nil {
		return nil, err
	}
	return ast.Products[0].Parameters[0].DefaultValue, nil
}

func exprJSON(t *testing.T, expr ExpressionNode) string {
	t.Helper()
	b, err := json.MarshalIndent(expr, "", "  ")
	if err != nil {
		t.Fatalf("marshal expression to JSON: %v", err)
	}
	return string(b) + "\n"
}

func assertGoldenExpr(t *testing.T, name, expr string) {
	t.Helper()
	gotExpr, err := parseExprFromSnippet(t, expr)
	if err != nil {
		t.Fatalf("parse failed for %q: %v", expr, err)
	}
	gotJSON := exprJSON(t, gotExpr)

	goldenPath := filepath.Join("testdata", "pratt_"+name+".golden.json")
	if *updatePrattGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir for golden: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(gotJSON), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if string(want) != gotJSON {
		t.Fatalf("golden mismatch for %s\nexpr: %s\nwant:\n%s\ngot:\n%s", name, expr, string(want), gotJSON)
	}
}

func TestPratt_PrecedenceAndAssociativity(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want ExpressionNode
	}{
		{
			name: "mul_binds_tighter_than_add",
			expr: "a + b * c",
			want: &BinaryExpression{
				Left:     &IdentifierExpression{Name: "a"},
				Operator: "+",
				Right: &BinaryExpression{
					Left:     &IdentifierExpression{Name: "b"},
					Operator: "*",
					Right:    &IdentifierExpression{Name: "c"},
				},
			},
		},
		{
			name: "grouping_overrides",
			expr: "(a + b) * c",
			want: &BinaryExpression{
				Left: &BinaryExpression{
					Left:     &IdentifierExpression{Name: "a"},
					Operator: "+",
					Right:    &IdentifierExpression{Name: "b"},
				},
				Operator: "*",
				Right:    &IdentifierExpression{Name: "c"},
			},
		},
		{
			name: "comparison_then_and",
			expr: "a == b && c",
			want: &BinaryExpression{
				Left: &BinaryExpression{
					Left:     &IdentifierExpression{Name: "a"},
					Operator: "==",
					Right:    &IdentifierExpression{Name: "b"},
				},
				Operator: "&&",
				Right:    &IdentifierExpression{Name: "c"},
			},
		},
		{
			name: "and_then_or",
			expr: "a && b || c",
			want: &BinaryExpression{
				Left: &BinaryExpression{
					Left:     &IdentifierExpression{Name: "a"},
					Operator: "&&",
					Right:    &IdentifierExpression{Name: "b"},
				},
				Operator: "||",
				Right:    &IdentifierExpression{Name: "c"},
			},
		},
		{
			name: "unary_not_binds_tight",
			expr: "!a && b",
			want: &BinaryExpression{
				Left: &UnaryExpression{
					Operator: "!",
					Operand:  &IdentifierExpression{Name: "a"},
				},
				Operator: "&&",
				Right:    &IdentifierExpression{Name: "b"},
			},
		},
		{
			name: "double_unary_not_right_associative",
			expr: "!!a",
			want: &UnaryExpression{
				Operator: "!",
				Operand: &UnaryExpression{
					Operator: "!",
					Operand:  &IdentifierExpression{Name: "a"},
				},
			},
		},
		{
			name: "unary_not_on_or_rhs",
			expr: "a || !b",
			want: &BinaryExpression{
				Left:     &IdentifierExpression{Name: "a"},
				Operator: "||",
				Right: &UnaryExpression{
					Operator: "!",
					Operand:  &IdentifierExpression{Name: "b"},
				},
			},
		},
		{
			name: "grouped_not_changes_precedence",
			expr: "!(a && b)",
			want: &UnaryExpression{
				Operator: "!",
				Operand: &BinaryExpression{
					Left:     &IdentifierExpression{Name: "a"},
					Operator: "&&",
					Right:    &IdentifierExpression{Name: "b"},
				},
			},
		},
		{
			name: "unary_minus_in_mul_left",
			expr: "-a * b",
			want: &BinaryExpression{
				Left: &UnaryExpression{
					Operator: "-",
					Operand:  &IdentifierExpression{Name: "a"},
				},
				Operator: "*",
				Right:    &IdentifierExpression{Name: "b"},
			},
		},
		{
			name: "unary_minus_in_mul_right",
			expr: "a * -b",
			want: &BinaryExpression{
				Left:     &IdentifierExpression{Name: "a"},
				Operator: "*",
				Right: &UnaryExpression{
					Operator: "-",
					Operand:  &IdentifierExpression{Name: "b"},
				},
			},
		},
		{
			name: "minus_minus_parses_as_sub_then_unary_minus",
			expr: "a--b",
			want: &BinaryExpression{
				Left:     &IdentifierExpression{Name: "a"},
				Operator: "-",
				Right: &UnaryExpression{
					Operator: "-",
					Operand:  &IdentifierExpression{Name: "b"},
				},
			},
		},
		{
			name: "ternary_right_assoc",
			expr: "a ? b : c ? d : e",
			want: &TernaryExpression{
				Condition: &IdentifierExpression{Name: "a"},
				TrueExpr:  &IdentifierExpression{Name: "b"},
				FalseExpr: &TernaryExpression{
					Condition: &IdentifierExpression{Name: "c"},
					TrueExpr:  &IdentifierExpression{Name: "d"},
					FalseExpr: &IdentifierExpression{Name: "e"},
				},
			},
		},
		{
			name: "call_precedence",
			expr: "a + foo(1, 2) * 2",
			want: &BinaryExpression{
				Left:     &IdentifierExpression{Name: "a"},
				Operator: "+",
				Right: &BinaryExpression{
					Left: &FunctionCallExpression{
						Name: "foo",
						Args: []ExpressionNode{
							&LiteralExpression{Value: 1.0},
							&LiteralExpression{Value: 2.0},
						},
					},
					Operator: "*",
					Right:    &LiteralExpression{Value: 2.0},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseExprFromSnippet(t, tt.expr)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("AST mismatch for %q\nwant: %#v\ngot:  %#v", tt.expr, tt.want, got)
			}
		})
	}
}

func TestPratt_CallIsIdentifierOnly(t *testing.T) {
	goodExpr := "foo(1,2)"
	if _, err := parseExprFromSnippet(t, goodExpr); err != nil {
		t.Fatalf("expected call expression to parse, got error: %v", err)
	}

	bad := []string{
		"(foo)(1)",
		"foo(1)(2)",
	}
	for _, expr := range bad {
		t.Run(expr, func(t *testing.T) {
			_, err := parseExprFromSnippet(t, expr)
			if err == nil {
				t.Fatalf("expected parse failure for %q", expr)
			}
		})
	}
}

func TestPratt_LexerNotAndNotEqual(t *testing.T) {
	tests := []struct {
		name      string
		expr      string
		expectErr bool
		wantType  interface{}
	}{
		{
			name:      "unary_not",
			expr:      "!a",
			expectErr: false,
			wantType:  &UnaryExpression{},
		},
		{
			name:      "not_equal",
			expr:      "a!=b",
			expectErr: false,
			wantType:  &BinaryExpression{},
		},
		{
			name:      "bad_spacing_not_then_assign",
			expr:      "a! = b",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseExprFromSnippet(t, tt.expr)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected parse error for %q", tt.expr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected parse error for %q: %v", tt.expr, err)
			}
			if tt.wantType != nil {
				if reflect.TypeOf(got) != reflect.TypeOf(tt.wantType) {
					t.Fatalf("unexpected node type for %q: got %T want %T", tt.expr, got, tt.wantType)
				}
			}
			if tt.expr == "a!=b" {
				bin, ok := got.(*BinaryExpression)
				if !ok {
					t.Fatalf("expected binary expression for %q, got %T", tt.expr, got)
				}
				if bin.Operator != "!=" {
					t.Fatalf("expected operator '!=', got %q", bin.Operator)
				}
			}
		})
	}
}

func TestPratt_GoldenAST(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{name: "mixed_ops", expr: "a + b * c == d && !e"},
		{name: "ternary_nested", expr: "a ? b + c : d ? e : f"},
		{name: "call_and_unary", expr: "foo(1, 2 + 3) * -x"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertGoldenExpr(t, tc.name, tc.expr)
		})
	}
}

func TestPratt_CallRejectsGroupedIdentifierWithClearError(t *testing.T) {
	_, err := parseExprFromSnippet(t, "(foo)(1)")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "unexpected token") && !strings.Contains(err.Error(), "expected") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
