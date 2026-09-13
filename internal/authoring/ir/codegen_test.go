package ir

import (
	"os"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
)

func TestGenerateDSL_SimpleRoundtrip(t *testing.T) {
	originalDSL := `product Test {
    param width: number = 100
    param height: number = 200
}
`

	ast := parseAndValidate(t, originalDSL)
	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	generatedDSL := GenerateDSL(program)

	// Parse the generated DSL
	tmpFile, err := os.CreateTemp("", "roundtrip_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(generatedDSL)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast2, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse generated DSL: %v\nGenerated DSL:\n%s", err, generatedDSL)
	}

	if err := dsl.Validate(ast2); err != nil {
		t.Fatalf("Failed to validate generated DSL: %v\nGenerated DSL:\n%s", err, generatedDSL)
	}

	program2, err := ConvertAST(ast2)
	if err != nil {
		t.Fatalf("ConvertAST on generated DSL failed: %v", err)
	}

	// Compare the two programs
	if !programsEqual(program, program2) {
		t.Errorf("Programs are not equal after roundtrip\nOriginal:\n%s\nGenerated:\n%s", originalDSL, generatedDSL)
	}
}

func TestGenerateDSL_CompleteRoundtrip(t *testing.T) {
	originalDSL := `const BASE_WIDTH = 1200
const SCALE = 0.75

profile Dev {
    output_dir = "test"
}

use profile Dev

product Table {
    param width: number = BASE_WIDTH
    param height: number = width * SCALE
    param material: enum { Oak, Pine } = Oak
    param is_large: boolean = width > 1000
}
`

	ast := parseAndValidate(t, originalDSL)
	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	generatedDSL := GenerateDSL(program)

	// Parse the generated DSL
	tmpFile, err := os.CreateTemp("", "roundtrip_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(generatedDSL)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast2, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse generated DSL: %v\nGenerated DSL:\n%s", err, generatedDSL)
	}

	if err := dsl.Validate(ast2); err != nil {
		t.Fatalf("Failed to validate generated DSL: %v\nGenerated DSL:\n%s", err, generatedDSL)
	}

	program2, err := ConvertAST(ast2)
	if err != nil {
		t.Fatalf("ConvertAST on generated DSL failed: %v", err)
	}

	// Compare the two programs
	if !programsEqual(program, program2) {
		t.Errorf("Programs are not equal after roundtrip\nOriginal:\n%s\nGenerated:\n%s", originalDSL, generatedDSL)
	}
}

func TestGenerateDSL_Formatting(t *testing.T) {
	program := &IRProgram{
		Version: "1.0",
		Constants: map[string]IRConstant{
			"CONST_A": {Name: "CONST_A", Type: IRType{Kind: IRTypeNumber}, Value: 100.0},
			"CONST_B": {Name: "CONST_B", Type: IRType{Kind: IRTypeString}, Value: "test"},
		},
		Profiles: []IRProfile{
			{
				Name: "Dev",
				Settings: map[string]IRExpression{
					"output_dir": {Kind: ExprKindLiteral, LiteralValue: "output"},
				},
			},
		},
		ActiveProfileName: strPtr("Dev"),
		Products: []IRProduct{
			{
				Name: "Product1",
				Parameters: []IRParameter{
					{Name: "width", Type: IRType{Kind: IRTypeNumber}, DefaultValue: IRExpression{Kind: ExprKindLiteral, LiteralValue: 100.0}},
					{Name: "height", Type: IRType{Kind: IRTypeNumber}, DefaultValue: IRExpression{Kind: ExprKindLiteral, LiteralValue: 200.0}},
				},
			},
		},
	}

	generated := GenerateDSL(program)

	// Check formatting
	lines := strings.Split(generated, "\n")

	if lines[0] != "dsl v1.0" {
		t.Errorf("Expected DSL version header, got: %s", lines[0])
	}

	// Constants should be first, alphabetically sorted
	if !strings.HasPrefix(lines[1], "const CONST_A") {
		t.Errorf("Expected CONST_A first, got: %s", lines[1])
	}
	if !strings.HasPrefix(lines[2], "const CONST_B") {
		t.Errorf("Expected CONST_B second, got: %s", lines[2])
	}

	// Empty line after constants
	if lines[3] != "" {
		t.Errorf("Expected empty line after constants, got: %s", lines[3])
	}

	// Profile block
	if !strings.Contains(generated, "profile Dev {") {
		t.Errorf("Expected profile block")
	}
	if !strings.Contains(generated, "    output_dir = \"output\"") {
		t.Errorf("Expected indented setting")
	}

	// Use profile statement
	if !strings.Contains(generated, "use profile Dev") {
		t.Errorf("Expected use profile statement")
	}

	// Product block with 4-space indentation
	if !strings.Contains(generated, "product Product1 {") {
		t.Errorf("Expected product block")
	}
	if !strings.Contains(generated, "    param width: number = 100") {
		t.Errorf("Expected indented param declaration")
	}
}

func TestGenerateDSL_StringEscaping(t *testing.T) {
	program := &IRProgram{
		Version: "1.0",
		Constants: map[string]IRConstant{
			"WITH_QUOTES":    {Name: "WITH_QUOTES", Type: IRType{Kind: IRTypeString}, Value: `say "hello"`},
			"WITH_BACKSLASH": {Name: "WITH_BACKSLASH", Type: IRType{Kind: IRTypeString}, Value: `path\to\file`},
			"WITH_NEWLINE":   {Name: "WITH_NEWLINE", Type: IRType{Kind: IRTypeString}, Value: "line1\nline2"},
		},
		Products: []IRProduct{
			{
				Name: "Test",
				Parameters: []IRParameter{
					{Name: "escaped", Type: IRType{Kind: IRTypeString}, DefaultValue: IRExpression{Kind: ExprKindLiteral, LiteralValue: `value with "quotes"`}},
				},
			},
		},
	}

	generated := GenerateDSL(program)

	// Check that special characters are escaped
	if !strings.Contains(generated, `\"`) {
		t.Errorf("Expected escaped quotes in generated DSL")
	}
	if !strings.Contains(generated, `\\`) {
		t.Errorf("Expected escaped backslashes in generated DSL")
	}
	if !strings.Contains(generated, `\n`) {
		t.Errorf("Expected escaped newlines in generated DSL")
	}
}

func TestGenerateDSL_OperatorPrecedence(t *testing.T) {
	// Test that we correctly add parentheses when needed
	program := &IRProgram{
		Version: "1.0",
		Products: []IRProduct{
			{
				Name: "Test",
				Parameters: []IRParameter{
					// a + b * c should NOT have parentheses around b * c
					{
						Name: "no_parens_needed",
						Type: IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{
							Kind:     ExprKindBinary,
							BinaryOp: "+",
							BinaryLeft: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: 1.0,
							},
							BinaryRight: &IRExpression{
								Kind:     ExprKindBinary,
								BinaryOp: "*",
								BinaryLeft: &IRExpression{
									Kind:         ExprKindLiteral,
									LiteralValue: 2.0,
								},
								BinaryRight: &IRExpression{
									Kind:         ExprKindLiteral,
									LiteralValue: 3.0,
								},
							},
						},
					},
					// (a + b) * c SHOULD have parentheses around a + b
					{
						Name: "parens_needed",
						Type: IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{
							Kind:     ExprKindBinary,
							BinaryOp: "*",
							BinaryLeft: &IRExpression{
								Kind:     ExprKindBinary,
								BinaryOp: "+",
								BinaryLeft: &IRExpression{
									Kind:         ExprKindLiteral,
									LiteralValue: 1.0,
								},
								BinaryRight: &IRExpression{
									Kind:         ExprKindLiteral,
									LiteralValue: 2.0,
								},
							},
							BinaryRight: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: 3.0,
							},
						},
					},
				},
			},
		},
	}

	generated := GenerateDSL(program)

	// Check that parentheses are added where needed
	lines := strings.Split(generated, "\n")

	// Find the lines with our parameters
	var noParensLine, parensLine string
	for _, line := range lines {
		if strings.Contains(line, "no_parens_needed") {
			noParensLine = line
		}
		if strings.Contains(line, "parens_needed") {
			parensLine = line
		}
	}

	// no_parens_needed should be: 1 + 2 * 3 (no parentheses)
	if strings.Contains(noParensLine, "(2 * 3)") || strings.Contains(noParensLine, "(2)") {
		t.Errorf("no_parens_needed should not have unnecessary parentheses: %s", noParensLine)
	}

	// parens_needed should be: (1 + 2) * 3 (with parentheses)
	if !strings.Contains(parensLine, "(1 + 2)") {
		t.Errorf("parens_needed should have parentheses around '1 + 2': %s", parensLine)
	}
}

func TestGenerateDSL_EnumValuesNotQuoted(t *testing.T) {
	program := &IRProgram{
		Version: "1.0",
		Products: []IRProduct{
			{
				Name: "Test",
				Parameters: []IRParameter{
					{
						Name: "material",
						Type: IRType{Kind: IRTypeEnum, EnumValues: []string{"Oak", "Pine"}},
						DefaultValue: IRExpression{
							Kind:          ExprKindReference,
							ReferenceName: "Oak",
							ReferenceType: RefTypeEnumValue,
						},
					},
				},
			},
		},
	}

	generated := GenerateDSL(program)

	// Enum value should NOT be quoted
	if strings.Contains(generated, `"Oak"`) {
		t.Errorf("Enum value should not be quoted in generated DSL: %s", generated)
	}
	if !strings.Contains(generated, "= Oak") {
		t.Errorf("Enum value should be unquoted: %s", generated)
	}
}

func TestGenerateDSL_NilProgram(t *testing.T) {
	generated := GenerateDSL(nil)
	if generated != "" {
		t.Errorf("Expected empty string for nil program, got: %s", generated)
	}
}

func TestGenerateDSL_EmptyProgram(t *testing.T) {
	program := &IRProgram{
		Version: "1.0",
	}

	generated := GenerateDSL(program)
	if generated != "dsl v1.0\n" {
		t.Errorf("Expected version header only for empty program, got: %q", generated)
	}
}

func TestGenerateDSL_NumberFormatting(t *testing.T) {
	program := &IRProgram{
		Version: "1.0",
		Constants: map[string]IRConstant{
			"WHOLE":    {Name: "WHOLE", Type: IRType{Kind: IRTypeNumber}, Value: 100.0},
			"DECIMAL":  {Name: "DECIMAL", Type: IRType{Kind: IRTypeNumber}, Value: 3.14159},
			"NEGATIVE": {Name: "NEGATIVE", Type: IRType{Kind: IRTypeNumber}, Value: -50.0},
		},
		Products: []IRProduct{},
	}

	generated := GenerateDSL(program)

	// Check number formatting
	if !strings.Contains(generated, "= 100") {
		t.Errorf("Whole number should not have decimal: %s", generated)
	}
	if !strings.Contains(generated, "3.14159") && !strings.Contains(generated, "3.1416") {
		t.Errorf("Decimal should be preserved: %s", generated)
	}
}

func TestGenerateDSL_UnaryExpression(t *testing.T) {
	program := &IRProgram{
		Version: "1.0",
		Products: []IRProduct{
			{
				Name: "Test",
				Parameters: []IRParameter{
					{
						Name: "negated",
						Type: IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{
							Kind:    ExprKindUnary,
							UnaryOp: "-",
							UnaryOperand: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: 42.0,
							},
						},
					},
					{
						Name: "inverted",
						Type: IRType{Kind: IRTypeBoolean},
						DefaultValue: IRExpression{
							Kind:    ExprKindUnary,
							UnaryOp: "!",
							UnaryOperand: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: true,
							},
						},
					},
				},
			},
		},
	}

	generated := GenerateDSL(program)

	if !strings.Contains(generated, "-42") {
		t.Errorf("Expected unary negation '-42': %s", generated)
	}
	if !strings.Contains(generated, "!true") {
		t.Errorf("Expected unary not '!true': %s", generated)
	}
}

func TestGenerateDSL_TernaryExpression(t *testing.T) {
	program := &IRProgram{
		Version: "1.0",
		Products: []IRProduct{
			{
				Name: "Test",
				Parameters: []IRParameter{
					{
						Name: "conditional",
						Type: IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{
							Kind: ExprKindTernary,
							TernaryCond: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: true,
							},
							TernaryTrue: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: 1.0,
							},
							TernaryFalse: &IRExpression{
								Kind:         ExprKindLiteral,
								LiteralValue: 0.0,
							},
						},
					},
				},
			},
		},
	}

	generated := GenerateDSL(program)

	if !strings.Contains(generated, "true ? 1 : 0") {
		t.Errorf("Expected ternary expression 'true ? 1 : 0': %s", generated)
	}
}

func TestGenerateDSL_FunctionCall(t *testing.T) {
	program := &IRProgram{
		Version: "1.0",
		Products: []IRProduct{
			{
				Name: "Test",
				Parameters: []IRParameter{
					{
						Name: "max_value",
						Type: IRType{Kind: IRTypeNumber},
						DefaultValue: IRExpression{
							Kind:     ExprKindCall,
							FuncName: "max",
							FuncArgs: []IRExpression{
								{Kind: ExprKindLiteral, LiteralValue: 1.0},
								{Kind: ExprKindLiteral, LiteralValue: 2.0},
							},
						},
					},
				},
			},
		},
	}

	generated := GenerateDSL(program)

	if !strings.Contains(generated, "max(1, 2)") {
		t.Errorf("Expected function call 'max(1, 2)': %s", generated)
	}
}

// Helper function to compare two programs for equality
func programsEqual(p1, p2 *IRProgram) bool {
	if p1 == nil || p2 == nil {
		return p1 == p2
	}

	if p1.Version != p2.Version {
		return false
	}

	if len(p1.Constants) != len(p2.Constants) {
		return false
	}
	for name, c1 := range p1.Constants {
		c2, ok := p2.Constants[name]
		if !ok {
			return false
		}
		if !constantsEqual(c1, c2) {
			return false
		}
	}

	if len(p1.Profiles) != len(p2.Profiles) {
		return false
	}
	for i := range p1.Profiles {
		if !profilesEqual(p1.Profiles[i], p2.Profiles[i]) {
			return false
		}
	}

	if (p1.ActiveProfileName == nil) != (p2.ActiveProfileName == nil) {
		return false
	}
	if p1.ActiveProfileName != nil && *p1.ActiveProfileName != *p2.ActiveProfileName {
		return false
	}

	if len(p1.Products) != len(p2.Products) {
		return false
	}
	for i := range p1.Products {
		if !productsEqual(p1.Products[i], p2.Products[i]) {
			return false
		}
	}

	return true
}

func constantsEqual(c1, c2 IRConstant) bool {
	if c1.Name != c2.Name {
		return false
	}
	if c1.Type.Kind != c2.Type.Kind {
		return false
	}
	if len(c1.Type.EnumValues) != len(c2.Type.EnumValues) {
		return false
	}
	for i := range c1.Type.EnumValues {
		if c1.Type.EnumValues[i] != c2.Type.EnumValues[i] {
			return false
		}
	}
	return c1.Value == c2.Value
}

func profilesEqual(p1, p2 IRProfile) bool {
	if p1.Name != p2.Name {
		return false
	}
	if len(p1.Settings) != len(p2.Settings) {
		return false
	}
	for key, s1 := range p1.Settings {
		s2, ok := p2.Settings[key]
		if !ok {
			return false
		}
		if !expressionsEqual(s1, s2) {
			return false
		}
	}
	return true
}

func productsEqual(p1, p2 IRProduct) bool {
	if p1.Name != p2.Name {
		return false
	}
	if len(p1.Lets) != len(p2.Lets) {
		return false
	}
	for i := range p1.Lets {
		if !letsEqual(p1.Lets[i], p2.Lets[i]) {
			return false
		}
	}
	if len(p1.Parameters) != len(p2.Parameters) {
		return false
	}
	for i := range p1.Parameters {
		if !parametersEqual(p1.Parameters[i], p2.Parameters[i]) {
			return false
		}
	}
	return true
}

func letsEqual(l1, l2 IRLet) bool {
	if l1.Name != l2.Name {
		return false
	}
	return expressionsEqual(l1.Value, l2.Value)
}

func parametersEqual(p1, p2 IRParameter) bool {
	if p1.Name != p2.Name {
		return false
	}
	if p1.Type.Kind != p2.Type.Kind {
		return false
	}
	return expressionsEqual(p1.DefaultValue, p2.DefaultValue)
}

func expressionsEqual(e1, e2 IRExpression) bool {
	if e1.Kind != e2.Kind {
		return false
	}

	switch e1.Kind {
	case ExprKindLiteral:
		return e1.LiteralValue == e2.LiteralValue
	case ExprKindReference:
		return e1.ReferenceName == e2.ReferenceName && e1.ReferenceType == e2.ReferenceType
	case ExprKindBinary:
		return e1.BinaryOp == e2.BinaryOp &&
			expressionsEqual(*e1.BinaryLeft, *e2.BinaryLeft) &&
			expressionsEqual(*e1.BinaryRight, *e2.BinaryRight)
	case ExprKindUnary:
		return e1.UnaryOp == e2.UnaryOp &&
			expressionsEqual(*e1.UnaryOperand, *e2.UnaryOperand)
	case ExprKindTernary:
		return expressionsEqual(*e1.TernaryCond, *e2.TernaryCond) &&
			expressionsEqual(*e1.TernaryTrue, *e2.TernaryTrue) &&
			expressionsEqual(*e1.TernaryFalse, *e2.TernaryFalse)
	case ExprKindCall:
		if e1.FuncName != e2.FuncName {
			return false
		}
		if len(e1.FuncArgs) != len(e2.FuncArgs) {
			return false
		}
		for i := range e1.FuncArgs {
			if !expressionsEqual(e1.FuncArgs[i], e2.FuncArgs[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func strPtr(s string) *string {
	return &s
}
