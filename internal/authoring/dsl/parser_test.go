package dsl

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParser(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedAST   *AST
		expectErr     bool
		errorContains string
	}{
		{
			name: "Profile Block",
			input: `profile Build {
    output_dir = "out"
    metadata_enabled = true
    retries = 2
}`,
			expectedAST: &AST{
				Profiles: []*ProfileDeclarationNode{
					{
						Name: "Build",
						Settings: map[string]ExpressionNode{
							"output_dir":       &LiteralExpression{Value: "out"},
							"metadata_enabled": &LiteralExpression{Value: true},
							"retries":          &LiteralExpression{Value: 2.0},
						},
						SettingOrder: []string{"output_dir", "metadata_enabled", "retries"},
					},
				},
			},
		},
		{
			name: "Product Block With Execution Declarations",
			input: `product Widget {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step"]

    param width: number = 1200
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name:        "Widget",
						Adapter:     &LiteralExpression{Value: "freecad"},
						SourceModel: &LiteralExpression{Value: "input/box.FCStd"},
						Outputs: &ArrayExpression{
							Elements: []ExpressionNode{
								&LiteralExpression{Value: "step"},
							},
						},
						ExecutionDeclarationOrder: []string{"adapter", "source_model", "outputs"},
						Parameters: []*ParameterNode{
							{
								Name:         "width",
								Type:         ParameterType{Kind: ParamTypeNumber},
								DefaultValue: &LiteralExpression{Value: 1200.0},
							},
						},
					},
				},
			},
		},
		{
			name: "Product Block With Mixed Execution And Params",
			input: `product Widget {
    param width: number = 1200
    outputs = ["step", "pdf"]
    param height: number = 800
    adapter = "freecad"
    source_model = "input/box.FCStd"
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name:        "Widget",
						Adapter:     &LiteralExpression{Value: "freecad"},
						SourceModel: &LiteralExpression{Value: "input/box.FCStd"},
						Outputs: &ArrayExpression{
							Elements: []ExpressionNode{
								&LiteralExpression{Value: "step"},
								&LiteralExpression{Value: "pdf"},
							},
						},
						ExecutionDeclarationOrder: []string{"outputs", "adapter", "source_model"},
						Parameters: []*ParameterNode{
							{
								Name:         "width",
								Type:         ParameterType{Kind: ParamTypeNumber},
								DefaultValue: &LiteralExpression{Value: 1200.0},
							},
							{
								Name:         "height",
								Type:         ParameterType{Kind: ParamTypeNumber},
								DefaultValue: &LiteralExpression{Value: 800.0},
							},
						},
					},
				},
			},
		},
		{
			name: "Product Block Duplicate Execution Declaration Fails",
			input: `product Widget {
    adapter = "freecad"
    adapter = "none"
}`,
			expectErr:     true,
			errorContains: "duplicate product execution declaration key 'adapter' in product 'Widget'",
		},
		{
			name: "Use Profile Statement",
			input: `profile Dev {
    output_dir = "out/dev"
}
profile Prod {
    output_dir = "out/prod"
}
use profile Prod`,
			expectedAST: &AST{
				Profiles: []*ProfileDeclarationNode{
					{
						Name: "Dev",
						Settings: map[string]ExpressionNode{
							"output_dir": &LiteralExpression{Value: "out/dev"},
						},
						SettingOrder: []string{"output_dir"},
					},
					{
						Name: "Prod",
						Settings: map[string]ExpressionNode{
							"output_dir": &LiteralExpression{Value: "out/prod"},
						},
						SettingOrder: []string{"output_dir"},
					},
				},
				ActiveProfileName: ptrString("Prod"),
			},
		},
		{
			name: "Simple Product and Parameter",
			input: `product Desk {
    param width: number = 1200
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name: "Desk",
						Parameters: []*ParameterNode{
							{
								Name:         "width",
								Type:         ParameterType{Kind: ParamTypeNumber},
								DefaultValue: &LiteralExpression{Value: 1200.0},
							},
						},
					},
				},
			},
		},
		{
			name: "Global Constant",
			input: `const BASE = 1200
product Desk {
    param width: number = BASE
}`,
			expectedAST: &AST{
				Constants: []*ConstDeclarationNode{
					{
						Name:  "BASE",
						Value: &LiteralExpression{Value: 1200.0},
					},
				},
				Products: []*ProductNode{
					{
						Name: "Desk",
						Parameters: []*ParameterNode{
							{
								Name:         "width",
								Type:         ParameterType{Kind: ParamTypeNumber},
								DefaultValue: &IdentifierExpression{Name: "BASE"},
							},
						},
					},
				},
			},
		},
		{
			name: "Arithmetic Precedence",
			input: `product Calc {
    param res: number = 1 + 2 * 3
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name: "Calc",
						Parameters: []*ParameterNode{
							{
								Name: "res",
								Type: ParameterType{Kind: ParamTypeNumber},
								DefaultValue: &BinaryExpression{
									Left:     &LiteralExpression{Value: 1.0},
									Operator: "+",
									Right: &BinaryExpression{
										Left:     &LiteralExpression{Value: 2.0},
										Operator: "*",
										Right:    &LiteralExpression{Value: 3.0},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Parentheses Precedence",
			input: `product Calc {
    param res: number = (1 + 2) * 3
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name: "Calc",
						Parameters: []*ParameterNode{
							{
								Name: "res",
								Type: ParameterType{Kind: ParamTypeNumber},
								DefaultValue: &BinaryExpression{
									Left: &BinaryExpression{
										Left:     &LiteralExpression{Value: 1.0},
										Operator: "+",
										Right:    &LiteralExpression{Value: 2.0},
									},
									Operator: "*",
									Right:    &LiteralExpression{Value: 3.0},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Comparison and Logical Operators",
			input: `product Logic {
    param check: boolean = 5 > 3 && 2 < 4
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name: "Logic",
						Parameters: []*ParameterNode{
							{
								Name: "check",
								Type: ParameterType{Kind: ParamTypeBoolean},
								DefaultValue: &BinaryExpression{
									Left: &BinaryExpression{
										Left:     &LiteralExpression{Value: 5.0},
										Operator: ">",
										Right:    &LiteralExpression{Value: 3.0},
									},
									Operator: "&&",
									Right: &BinaryExpression{
										Left:     &LiteralExpression{Value: 2.0},
										Operator: "<",
										Right:    &LiteralExpression{Value: 4.0},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Unary Not Expressions",
			input: `product UnaryNot {
    param a: boolean = true
    param b: boolean = !a
    param c: boolean = !!a
    param d: boolean = !a && true
    param e: boolean = a || !true
    param f: boolean = !(a && true)
    param g: boolean = a != b
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name: "UnaryNot",
						Parameters: []*ParameterNode{
							{
								Name:         "a",
								Type:         ParameterType{Kind: ParamTypeBoolean},
								DefaultValue: &LiteralExpression{Value: true},
							},
							{
								Name: "b",
								Type: ParameterType{Kind: ParamTypeBoolean},
								DefaultValue: &UnaryExpression{
									Operator: "!",
									Operand:  &IdentifierExpression{Name: "a"},
								},
							},
							{
								Name: "c",
								Type: ParameterType{Kind: ParamTypeBoolean},
								DefaultValue: &UnaryExpression{
									Operator: "!",
									Operand: &UnaryExpression{
										Operator: "!",
										Operand:  &IdentifierExpression{Name: "a"},
									},
								},
							},
							{
								Name: "d",
								Type: ParameterType{Kind: ParamTypeBoolean},
								DefaultValue: &BinaryExpression{
									Left: &UnaryExpression{
										Operator: "!",
										Operand:  &IdentifierExpression{Name: "a"},
									},
									Operator: "&&",
									Right:    &LiteralExpression{Value: true},
								},
							},
							{
								Name: "e",
								Type: ParameterType{Kind: ParamTypeBoolean},
								DefaultValue: &BinaryExpression{
									Left:     &IdentifierExpression{Name: "a"},
									Operator: "||",
									Right: &UnaryExpression{
										Operator: "!",
										Operand:  &LiteralExpression{Value: true},
									},
								},
							},
							{
								Name: "f",
								Type: ParameterType{Kind: ParamTypeBoolean},
								DefaultValue: &UnaryExpression{
									Operator: "!",
									Operand: &BinaryExpression{
										Left:     &IdentifierExpression{Name: "a"},
										Operator: "&&",
										Right:    &LiteralExpression{Value: true},
									},
								},
							},
							{
								Name: "g",
								Type: ParameterType{Kind: ParamTypeBoolean},
								DefaultValue: &BinaryExpression{
									Left:     &IdentifierExpression{Name: "a"},
									Operator: "!=",
									Right:    &IdentifierExpression{Name: "b"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Ternary Expression",
			input: `product Cond {
    param val: string = true ? "yes" : "no"
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name: "Cond",
						Parameters: []*ParameterNode{
							{
								Name: "val",
								Type: ParameterType{Kind: ParamTypeString},
								DefaultValue: &TernaryExpression{
									Condition: &LiteralExpression{Value: true},
									TrueExpr:  &LiteralExpression{Value: "yes"},
									FalseExpr: &LiteralExpression{Value: "no"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Syntax Error - Missing Closing Brace",
			input: `product Desk {
    param width: number = 1200`,
			expectErr: true,
		},
		{
			name: "Enum Type - Single Value",
			input: `product Desk {
    param finish: enum { Oak } = Oak
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name: "Desk",
						Parameters: []*ParameterNode{
							{
								Name: "finish",
								Type: ParameterType{
									Kind:       ParamTypeEnum,
									EnumValues: []string{"Oak"},
								},
								DefaultValue: &IdentifierExpression{Name: "Oak"},
							},
						},
					},
				},
			},
		},
		{
			name: "Enum Type - Multiple Values",
			input: `product Desk {
    param finish: enum { Oak, Pine } = Oak
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name: "Desk",
						Parameters: []*ParameterNode{
							{
								Name: "finish",
								Type: ParameterType{
									Kind:       ParamTypeEnum,
									EnumValues: []string{"Oak", "Pine"},
								},
								DefaultValue: &IdentifierExpression{Name: "Oak"},
							},
						},
					},
				},
			},
		},
		{
			name: "Enum Type - Whitespace Variations",
			input: `product Desk {
    param finish: enum    {   Oak  ,   Pine   } = Oak
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name: "Desk",
						Parameters: []*ParameterNode{
							{
								Name: "finish",
								Type: ParameterType{
									Kind:       ParamTypeEnum,
									EnumValues: []string{"Oak", "Pine"},
								},
								DefaultValue: &IdentifierExpression{Name: "Oak"},
							},
						},
					},
				},
			},
		},
		{
			name: "Enum Syntax Error - Empty Body",
			input: `product Desk {
    param finish: enum { } = Oak
}`,
			expectErr:     true,
			errorContains: "enum type must contain at least one identifier",
		},
		{
			name: "Enum Syntax Error - Trailing Comma",
			input: `product Desk {
    param finish: enum { Oak, } = Oak
}`,
			expectErr:     true,
			errorContains: "expected identifier after ',' in enum type",
		},
		{
			name: "Enum Syntax Error - Invalid Token in Enum",
			input: `product Desk {
    param finish: enum { Oak, 123 } = Oak
}`,
			expectErr:     true,
			errorContains: "invalid identifier '123'",
		},
		{
			name: "Enum Syntax Error - Missing Closing Brace",
			input: `product Desk {
    param finish: enum { Oak, Pine = Oak
}`,
			expectErr:     true,
			errorContains: "expected ',' or '}' in enum type, got '='",
		},
		{
			name: "Simple Function Call",
			input: `product Func {
    param val: number = max(10, 20)
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name: "Func",
						Parameters: []*ParameterNode{
							{
								Name: "val",
								Type: ParameterType{Kind: ParamTypeNumber},
								DefaultValue: &FunctionCallExpression{
									Name: "max",
									Args: []ExpressionNode{
										&LiteralExpression{Value: 10.0},
										&LiteralExpression{Value: 20.0},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Nested Function Call",
			input: `product Func {
    param val: number = max(min(10, 20), 5)
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name: "Func",
						Parameters: []*ParameterNode{
							{
								Name: "val",
								Type: ParameterType{Kind: ParamTypeNumber},
								DefaultValue: &FunctionCallExpression{
									Name: "max",
									Args: []ExpressionNode{
										&FunctionCallExpression{
											Name: "min",
											Args: []ExpressionNode{
												&LiteralExpression{Value: 10.0},
												&LiteralExpression{Value: 20.0},
											},
										},
										&LiteralExpression{Value: 5.0},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Function Call with Expression",
			input: `product Func {
    param val: number = abs(10 - 20)
}`,
			expectedAST: &AST{
				Products: []*ProductNode{
					{
						Name: "Func",
						Parameters: []*ParameterNode{
							{
								Name: "val",
								Type: ParameterType{Kind: ParamTypeNumber},
								DefaultValue: &FunctionCallExpression{
									Name: "abs",
									Args: []ExpressionNode{
										&BinaryExpression{
											Left:     &LiteralExpression{Value: 10.0},
											Operator: "-",
											Right:    &LiteralExpression{Value: 20.0},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Syntax Error - Invalid Token",
			input: `product Desk {
    param width: number = @
}`,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize parser directly with string input to avoid file I/O.
			// Since we are in package dsl, we can access the unexported fields and methods.
			p := &Parser{
				input: []rune(tt.input),
				line:  1,
				col:   1,
			}
			ast, err := p.parse()

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error, got nil")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !reflect.DeepEqual(ast, tt.expectedAST) {
				t.Errorf("AST mismatch.\nGot:  %+v\nWant: %+v", ast, tt.expectedAST)
			}
		})
	}
}

func ptrString(v string) *string {
	return &v
}

func TestParser_Comments(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectErr     bool
		errorContains string
	}{
		{
			name: "line comment in product body",
			input: `product A { // comment
    param x: number = 1
}`,
		},
		{
			name:  "block comment after param",
			input: `product A { param x: number = 1 /*comment*/ }`,
		},
		{
			name:  "block comment in expression",
			input: `product A { param x: number = 1/*c*/+2 }`,
		},
		{
			name:  "line comment after use profile",
			input: "profile Dev { output_dir = \"out\" }\nuse profile Dev // trailing\nproduct A { param x: number = 1 }",
		},
		{
			name:  "line comment at EOF",
			input: "product A { param x: number = 1 }\n// comment",
		},
		{
			name:          "unclosed block comment is lex error",
			input:         "product A { /*",
			expectErr:     true,
			errorContains: "line 1:13: lex error: unclosed block comment",
		},
		{
			name:  "string with // is not comment",
			input: `product A { param s: string = "http://x" }`,
		},
		{
			name:  "string with block markers is not comment",
			input: `product A { param s: string = "a/*b*/c" }`,
		},
		{
			name:  "division still works",
			input: `product A { param x: number = 10 / 2 }`,
		},
		{
			name:          "double slash after number starts comment",
			input:         `product A { param x: number = 10//2 }`,
			expectErr:     true,
			errorContains: "unexpected EOF inside product 'A'",
		},
		{
			name: "comments across scopes",
			input: `// top
const BASE = 1 /* base */
profile Dev {
  output_dir = "out" // setting
}
use profile Dev
product A {
  param x: number = (BASE /*mid*/ + 2) // tail
}`,
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
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Fatalf("expected error containing %q, got %v", tt.errorContains, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestParser_CommentLineColWithWindowsNewline(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		errorContains string
	}{
		{
			name: "line comment with CRLF keeps line-col",
			input: "product A { // comment\r\n" +
				"  param x: number = 1\r\n" +
				"  @\r\n" +
				"}",
			errorContains: "line 3:3:",
		},
		{
			name: "block comment with CRLF keeps line-col",
			input: "product A {\r\n" +
				"  /* block\r\n" +
				"     comment */\r\n" +
				"  @\r\n" +
				"}",
			errorContains: "line 4:3:",
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
				t.Fatalf("expected parse error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got %v", tt.errorContains, err)
			}
		})
	}
}

func TestParser_BlockCommentClosingAndTokenBoundaries(t *testing.T) {
	t.Run("block comment star density closes at first */", func(t *testing.T) {
		input := `product A {
  param x: number = 1 /***/ + 2
  param y: number = 3 /* ** */ + 4
}`
		p := &Parser{
			input: []rune(input),
			line:  1,
			col:   1,
		}
		_, err := p.parse()
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}
	})

	t.Run("comment between number tokens yields parse error with line-col", func(t *testing.T) {
		input := `product A { param x: number = 1/*c*/2 }`
		p := &Parser{
			input: []rune(input),
			line:  1,
			col:   1,
		}
		_, err := p.parse()
		if err == nil {
			t.Fatalf("expected parse error, got nil")
		}
		if !strings.Contains(err.Error(), "line 1:") || !strings.Contains(err.Error(), "unexpected token '2'") {
			t.Fatalf("expected line/col parse error around token '2', got: %v", err)
		}
	})

	t.Run("use keyword split by comment behaves as whitespace", func(t *testing.T) {
		input := `use/*c*/profile Dev`
		p := &Parser{
			input: []rune(input),
			line:  1,
			col:   1,
		}
		ast, err := p.parse()
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}
		if ast.ActiveProfileName == nil || *ast.ActiveProfileName != "Dev" {
			t.Fatalf("expected active profile Dev, got %+v", ast.ActiveProfileName)
		}
	})
}

func TestParse_VersionDirective(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectErr     bool
		errorContains string
		expectedVer   string
	}{
		{
			name: "accepts dsl v1.0",
			content: `dsl v1.0
product X { param a: number = 1 }`,
			expectedVer: SupportedDSLVersion,
		},
		{
			name: "accepts dsl 1.0",
			content: `dsl 1.0
product X { param a: number = 1 }`,
			expectedVer: SupportedDSLVersion,
		},
		{
			name:          "missing directive",
			content:       `product X { param a: number = 1 }`,
			expectErr:     true,
			errorContains: "missing version directive",
		},
		{
			name: "unsupported version",
			content: `dsl v2.0
product X { param a: number = 1 }`,
			expectErr:     true,
			errorContains: "unsupported DSL version",
		},
		{
			name: "rejects shortened version token",
			content: `dsl v1
product X { param a: number = 1 }`,
			expectErr:     true,
			errorContains: "unsupported DSL version",
		},
		{
			name: "rejects semantic version token",
			content: `dsl v1.0.0
product X { param a: number = 1 }`,
			expectErr:     true,
			errorContains: "unsupported DSL version",
		},
		{
			name: "rejects missing separator",
			content: `dslv1.0
product X { param a: number = 1 }`,
			expectErr:     true,
			errorContains: "missing version directive",
		},
		{
			name: "directive not at top",
			content: `product X { param a: number = 1 }
dsl v1.0`,
			expectErr:     true,
			errorContains: "version directive must appear at top of file",
		},
		{
			name: "comments before directive",
			content: `// banner
/* block */
dsl v1.0
product X { param a: number = 1 }`,
			expectedVer: SupportedDSLVersion,
		},
		{
			name: "line comment then newline then directive",
			content: `// comment
dsl v1.0
product X { param a: number = 1 }`,
			expectedVer: SupportedDSLVersion,
		},
		{
			name: "block comment then directive on same line",
			content: `/* comment */ dsl v1.0
product X { param a: number = 1 }`,
			expectedVer: SupportedDSLVersion,
		},
		{
			name:          "only line comment then EOF",
			content:       `// comment`,
			expectErr:     true,
			errorContains: "missing version directive",
		},
		{
			name:          "only block comment then EOF",
			content:       `/* comment */`,
			expectErr:     true,
			errorContains: "missing version directive",
		},
		{
			name:        "header and product on same line",
			content:     `dsl v1.0 product X { param a: number = 1 }`,
			expectedVer: SupportedDSLVersion,
		},
		{
			name:          "directive keyword is case-sensitive",
			content:       `DSL v1.0 product X { param a: number = 1 }`,
			expectErr:     true,
			errorContains: "missing version directive",
		},
		{
			name:          "version token is case-sensitive",
			content:       `dsl V1.0 product X { param a: number = 1 }`,
			expectErr:     true,
			errorContains: "unsupported DSL version",
		},
		{
			name:        "directive with tab spacing",
			content:     "dsl\tv1.0\nproduct X { param a: number = 1 }",
			expectedVer: SupportedDSLVersion,
		},
		{
			name: "directive duplicated",
			content: `dsl v1.0
product X { param a: number = 1 }
dsl v1.0`,
			expectErr:     true,
			errorContains: "version directive must appear at top of file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "parse_version_*.dsl")
			if err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.Write([]byte(tt.content)); err != nil {
				t.Fatalf("failed to write temp file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("failed to close temp file: %v", err)
			}

			ast, err := Parse(tmpFile.Name())
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected parse error, got nil")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Fatalf("expected error containing %q, got: %v", tt.errorContains, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if ast.DSLVersion != tt.expectedVer {
				t.Fatalf("expected DSLVersion=%q, got %q", tt.expectedVer, ast.DSLVersion)
			}
		})
	}
}
