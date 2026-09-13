package ir

import (
	"os"
	"testing"

	"parametron/internal/authoring/dsl"
)

func TestConvertAST_SimpleProductAndParam(t *testing.T) {
	dslContent := `
product Test {
    param width: number = 100
    param height: number = 200
}`

	ast := parseAndValidate(t, dslContent)
	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	if program.Version != "1.0" {
		t.Errorf("Expected version '1.0', got '%s'", program.Version)
	}

	if len(program.Products) != 1 {
		t.Fatalf("Expected 1 product, got %d", len(program.Products))
	}

	product := program.Products[0]
	if product.Name != "Test" {
		t.Errorf("Expected product name 'Test', got '%s'", product.Name)
	}

	if len(product.Parameters) != 2 {
		t.Fatalf("Expected 2 parameters, got %d", len(product.Parameters))
	}

	// Check first parameter
	if product.Parameters[0].Name != "width" {
		t.Errorf("Expected first parameter 'width', got '%s'", product.Parameters[0].Name)
	}
	if product.Parameters[0].Type.Kind != IRTypeNumber {
		t.Errorf("Expected type number, got %s", product.Parameters[0].Type.Kind)
	}
	if product.Parameters[0].DefaultValue.Kind != ExprKindLiteral {
		t.Errorf("Expected literal expression, got %s", product.Parameters[0].DefaultValue.Kind)
	}
	if product.Parameters[0].DefaultValue.LiteralValue != 100.0 {
		t.Errorf("Expected value 100, got %v", product.Parameters[0].DefaultValue.LiteralValue)
	}

	// Check second parameter
	if product.Parameters[1].Name != "height" {
		t.Errorf("Expected second parameter 'height', got '%s'", product.Parameters[1].Name)
	}
	if product.Parameters[1].DefaultValue.LiteralValue != 200.0 {
		t.Errorf("Expected value 200, got %v", product.Parameters[1].DefaultValue.LiteralValue)
	}
}

func TestConvertAST_ConstantFolding(t *testing.T) {
	dslContent := `
const BASE_WIDTH = 1200
const SCALE = 0.75

product Table {
    param width: number = BASE_WIDTH
    param height: number = width * SCALE
}`

	ast := parseAndValidate(t, dslContent)
	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	// Check constants are resolved
	if len(program.Constants) != 2 {
		t.Fatalf("Expected 2 constants, got %d", len(program.Constants))
	}

	baseWidth, ok := program.Constants["BASE_WIDTH"]
	if !ok {
		t.Fatal("BASE_WIDTH constant not found")
	}
	if baseWidth.Value != 1200.0 {
		t.Errorf("Expected BASE_WIDTH value 1200, got %v", baseWidth.Value)
	}
	if baseWidth.Type.Kind != IRTypeNumber {
		t.Errorf("Expected BASE_WIDTH type number, got %s", baseWidth.Type.Kind)
	}

	scale, ok := program.Constants["SCALE"]
	if !ok {
		t.Fatal("SCALE constant not found")
	}
	if scale.Value != 0.75 {
		t.Errorf("Expected SCALE value 0.75, got %v", scale.Value)
	}

	// Check parameter references constant
	product := program.Products[0]
	widthParam := product.Parameters[0]
	if widthParam.DefaultValue.Kind != ExprKindReference {
		t.Errorf("Expected reference expression for width, got %s", widthParam.DefaultValue.Kind)
	}
	if widthParam.DefaultValue.ReferenceName != "BASE_WIDTH" {
		t.Errorf("Expected reference to BASE_WIDTH, got %s", widthParam.DefaultValue.ReferenceName)
	}
	if widthParam.DefaultValue.ReferenceType != RefTypeConstant {
		t.Errorf("Expected reference type constant, got %s", widthParam.DefaultValue.ReferenceType)
	}
}

func TestConvertAST_ComplexExpressionTree(t *testing.T) {
	dslContent := `
const BASE = 100

product Test {
    param a: number = 10
    param b: number = 20
    param c: number = a + b * 2
    param d: number = (a + b) * 2
    param e: boolean = a < b && b > 5
    param f: number = -a
    param g: boolean = !(a > b && a < 100)
    param h: number = a > b ? 1 : 0
    param i: number = max(a, b)
}`

	ast := parseAndValidate(t, dslContent)
	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	product := program.Products[0]

	// Test binary expression with precedence
	cParam := findParam(t, product.Parameters, "c")
	if cParam.DefaultValue.Kind != ExprKindBinary {
		t.Errorf("Expected binary expression for c, got %s", cParam.DefaultValue.Kind)
	}
	if cParam.DefaultValue.BinaryOp != "+" {
		t.Errorf("Expected operator '+', got '%s'", cParam.DefaultValue.BinaryOp)
	}
	// Right operand should be multiplication
	if cParam.DefaultValue.BinaryRight.Kind != ExprKindBinary {
		t.Errorf("Expected right side to be binary expression")
	}
	if cParam.DefaultValue.BinaryRight.BinaryOp != "*" {
		t.Errorf("Expected right side operator '*', got '%s'", cParam.DefaultValue.BinaryRight.BinaryOp)
	}

	// Test parenthesized expression
	dParam := findParam(t, product.Parameters, "d")
	if dParam.DefaultValue.Kind != ExprKindBinary {
		t.Errorf("Expected binary expression for d, got %s", dParam.DefaultValue.Kind)
	}
	if dParam.DefaultValue.BinaryOp != "*" {
		t.Errorf("Expected operator '*', got '%s'", dParam.DefaultValue.BinaryOp)
	}
	// Left operand should be addition (parenthesized)
	if dParam.DefaultValue.BinaryLeft.Kind != ExprKindBinary {
		t.Errorf("Expected left side to be binary expression")
	}
	if dParam.DefaultValue.BinaryLeft.BinaryOp != "+" {
		t.Errorf("Expected left side operator '+', got '%s'", dParam.DefaultValue.BinaryLeft.BinaryOp)
	}

	// Test logical expression with short-circuit
	eParam := findParam(t, product.Parameters, "e")
	if eParam.DefaultValue.Kind != ExprKindBinary {
		t.Errorf("Expected binary expression for e, got %s", eParam.DefaultValue.Kind)
	}
	if eParam.DefaultValue.BinaryOp != "&&" {
		t.Errorf("Expected operator '&&', got '%s'", eParam.DefaultValue.BinaryOp)
	}

	// Test unary negation
	fParam := findParam(t, product.Parameters, "f")
	if fParam.DefaultValue.Kind != ExprKindUnary {
		t.Errorf("Expected unary expression for f, got %s", fParam.DefaultValue.Kind)
	}
	if fParam.DefaultValue.UnaryOp != "-" {
		t.Errorf("Expected unary operator '-', got '%s'", fParam.DefaultValue.UnaryOp)
	}

	// Test unary not
	gParam := findParam(t, product.Parameters, "g")
	if gParam.DefaultValue.Kind != ExprKindUnary {
		t.Errorf("Expected unary expression for g, got %s", gParam.DefaultValue.Kind)
	}
	if gParam.DefaultValue.UnaryOp != "!" {
		t.Errorf("Expected unary operator '!', got '%s'", gParam.DefaultValue.UnaryOp)
	}

	// Test ternary expression
	hParam := findParam(t, product.Parameters, "h")
	if hParam.DefaultValue.Kind != ExprKindTernary {
		t.Errorf("Expected ternary expression for h, got %s", hParam.DefaultValue.Kind)
	}
	if hParam.DefaultValue.TernaryCond.Kind != ExprKindBinary {
		t.Errorf("Expected binary condition, got %s", hParam.DefaultValue.TernaryCond.Kind)
	}

	// Test function call
	iParam := findParam(t, product.Parameters, "i")
	if iParam.DefaultValue.Kind != ExprKindCall {
		t.Errorf("Expected call expression for i, got %s", iParam.DefaultValue.Kind)
	}
	if iParam.DefaultValue.FuncName != "max" {
		t.Errorf("Expected function 'max', got '%s'", iParam.DefaultValue.FuncName)
	}
	if len(iParam.DefaultValue.FuncArgs) != 2 {
		t.Errorf("Expected 2 arguments, got %d", len(iParam.DefaultValue.FuncArgs))
	}
}

func TestConvertAST_ProductLets(t *testing.T) {
	dslContent := `
product Test {
    let base = 5
    param material: enum { Oak, Pine } = Oak
    let finish = material
    param total: number = base + 1
}`

	ast := parseAndValidate(t, dslContent)
	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	product := program.Products[0]
	if len(product.Lets) != 2 {
		t.Fatalf("expected 2 lets, got %d", len(product.Lets))
	}
	if product.Lets[0].Name != "base" {
		t.Fatalf("expected first let to be base, got %q", product.Lets[0].Name)
	}
	if product.Lets[1].Name != "finish" {
		t.Fatalf("expected second let to be finish, got %q", product.Lets[1].Name)
	}
	if product.Lets[1].Value.Kind != ExprKindReference {
		t.Fatalf("expected enum let to be reference, got %s", product.Lets[1].Value.Kind)
	}
	if product.Lets[1].Value.ReferenceType != RefTypeParam {
		t.Fatalf("expected let finish to reference param material, got %s", product.Lets[1].Value.ReferenceType)
	}
	material := findParam(t, product.Parameters, "material")
	if material.DefaultValue.ReferenceType != RefTypeEnumValue {
		t.Fatalf("expected material to use enum default, got %s", material.DefaultValue.ReferenceType)
	}
	total := findParam(t, product.Parameters, "total")
	if total.DefaultValue.BinaryLeft.ReferenceType != RefTypeLet {
		t.Fatalf("expected total to reference let, got %s", total.DefaultValue.BinaryLeft.ReferenceType)
	}
}

func TestConvertAST_ProfileConversion(t *testing.T) {
	dslContent := `
profile Dev {
    output_dir = "test"
    file_pattern = "{product}.csv"
}

profile Prod {
    output_dir = "production"
}

use profile Dev

product Test {
    param x: number = 1
}`

	ast := parseAndValidate(t, dslContent)
	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	if len(program.Profiles) != 2 {
		t.Fatalf("Expected 2 profiles, got %d", len(program.Profiles))
	}

	// Check first profile
	devProfile := program.Profiles[0]
	if devProfile.Name != "Dev" {
		t.Errorf("Expected profile name 'Dev', got '%s'", devProfile.Name)
	}
	if len(devProfile.Settings) != 2 {
		t.Errorf("Expected 2 settings, got %d", len(devProfile.Settings))
	}

	outputDir, ok := devProfile.Settings["output_dir"]
	if !ok {
		t.Fatal("output_dir setting not found in Dev profile")
	}
	if outputDir.Kind != ExprKindLiteral {
		t.Errorf("Expected literal expression for output_dir, got %s", outputDir.Kind)
	}
	if outputDir.LiteralValue != "test" {
		t.Errorf("Expected output_dir value 'test', got %v", outputDir.LiteralValue)
	}

	// Check active profile
	if program.ActiveProfileName == nil {
		t.Fatal("ActiveProfileName should not be nil")
	}
	if *program.ActiveProfileName != "Dev" {
		t.Errorf("Expected active profile 'Dev', got '%s'", *program.ActiveProfileName)
	}
}

func TestConvertAST_StringConcatenationExpression(t *testing.T) {
	dslContent := `
const PREFIX = "pre"

product Test {
    param left: string = "L"
    param right: string = "R"
    param joined: string = left + right
    param from_const: string = PREFIX + "_fix"
}`

	ast := parseAndValidate(t, dslContent)
	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	if got := program.Constants["PREFIX"].Type.Kind; got != IRTypeString {
		t.Fatalf("expected PREFIX constant type string, got %s", got)
	}

	product := program.Products[0]
	joined := findParam(t, product.Parameters, "joined")
	if joined.DefaultValue.Kind != ExprKindBinary {
		t.Fatalf("expected joined to be binary expression, got %s", joined.DefaultValue.Kind)
	}
	if joined.DefaultValue.BinaryOp != "+" {
		t.Fatalf("expected joined operator '+', got %q", joined.DefaultValue.BinaryOp)
	}
	if joined.DefaultValue.BinaryLeft.Kind != ExprKindReference || joined.DefaultValue.BinaryLeft.ReferenceType != RefTypeParam {
		t.Fatalf("expected joined left operand to be parameter reference, got kind=%s type=%s", joined.DefaultValue.BinaryLeft.Kind, joined.DefaultValue.BinaryLeft.ReferenceType)
	}
	if joined.DefaultValue.BinaryRight.Kind != ExprKindReference || joined.DefaultValue.BinaryRight.ReferenceType != RefTypeParam {
		t.Fatalf("expected joined right operand to be parameter reference, got kind=%s type=%s", joined.DefaultValue.BinaryRight.Kind, joined.DefaultValue.BinaryRight.ReferenceType)
	}
}

func TestConvertAST_InterpolatedStringExpression(t *testing.T) {
	dslContent := `
product Test {
    param first: string = "A"
    param last: string = "B"
    param full: string = "{param:first}-{param:last}"
}`

	ast := parseAndValidate(t, dslContent)
	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	product := program.Products[0]
	full := findParam(t, product.Parameters, "full")
	if full.DefaultValue.Kind != ExprKindInterpolatedString {
		t.Fatalf("expected interpolated string expression, got %s", full.DefaultValue.Kind)
	}
	if len(full.DefaultValue.InterpolatedSegments) != 3 {
		t.Fatalf("expected 3 interpolation segments, got %d", len(full.DefaultValue.InterpolatedSegments))
	}
	if full.DefaultValue.InterpolatedSegments[0].ParamName != "first" {
		t.Fatalf("unexpected first segment: %+v", full.DefaultValue.InterpolatedSegments[0])
	}
	if full.DefaultValue.InterpolatedSegments[1].Text != "-" {
		t.Fatalf("unexpected middle segment: %+v", full.DefaultValue.InterpolatedSegments[1])
	}
	if full.DefaultValue.InterpolatedSegments[2].ParamName != "last" {
		t.Fatalf("unexpected last segment: %+v", full.DefaultValue.InterpolatedSegments[2])
	}
}

func TestConvertAST_EnumTypePreservation(t *testing.T) {
	dslContent := `
product Test {
    param material: enum { Oak, Pine, Maple } = Oak
    param finish: enum { Glossy, Matte } = Glossy
    param is_oak: boolean = material == Oak
}`

	ast := parseAndValidate(t, dslContent)
	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	product := program.Products[0]

	// Check enum parameter
	materialParam := findParam(t, product.Parameters, "material")
	if materialParam.Type.Kind != IRTypeEnum {
		t.Errorf("Expected enum type, got %s", materialParam.Type.Kind)
	}
	expectedEnumValues := []string{"Oak", "Pine", "Maple"}
	if len(materialParam.Type.EnumValues) != len(expectedEnumValues) {
		t.Errorf("Expected %d enum values, got %d", len(expectedEnumValues), len(materialParam.Type.EnumValues))
	}
	for i, v := range expectedEnumValues {
		if materialParam.Type.EnumValues[i] != v {
			t.Errorf("Expected enum value '%s', got '%s'", v, materialParam.Type.EnumValues[i])
		}
	}

	// Check enum default value is a reference with enum type
	if materialParam.DefaultValue.Kind != ExprKindReference {
		t.Errorf("Expected reference expression for enum default, got %s", materialParam.DefaultValue.Kind)
	}
	if materialParam.DefaultValue.ReferenceName != "Oak" {
		t.Errorf("Expected reference to 'Oak', got '%s'", materialParam.DefaultValue.ReferenceName)
	}
	if materialParam.DefaultValue.ReferenceType != RefTypeEnumValue {
		t.Errorf("Expected reference type enum, got %s", materialParam.DefaultValue.ReferenceType)
	}
}

func TestConvertAST_NilAST(t *testing.T) {
	_, err := ConvertAST(nil)
	if err == nil {
		t.Fatal("Expected error for nil AST")
	}
	if err.Error() != "cannot convert nil AST" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestConvertAST_UnvalidatedAST(t *testing.T) {
	dslContent := `
const X = 1
product Test {
    param x: number = X
}`

	// Parse without validating
	tmpFile, err := os.CreateTemp("", "test_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse DSL: %v", err)
	}

	// Don't validate - try to convert directly
	_, err = ConvertAST(ast)
	if err == nil {
		t.Fatal("Expected error for unvalidated AST")
	}
	if err.Error() != "AST must be validated before conversion (ResolvedConstants is nil)" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestConvertExpression_UndefinedIdentifierWithoutEnumContext(t *testing.T) {
	_, err := convertExpression(
		&dsl.IdentifierExpression{Name: "UNKNOWN"},
		map[string]*dsl.ParameterNode{},
		map[string]dsl.ConstantValue{},
		nil,
	)
	if err == nil {
		t.Fatal("expected undefined identifier error")
	}
	if err.Error() != "undefined identifier 'UNKNOWN'" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConvertAST_StringParameter(t *testing.T) {
	dslContent := `
const PREFIX = "Item"

product Test {
    param name: string = "TestItem"
    param label: string = PREFIX
}`

	ast := parseAndValidate(t, dslContent)
	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	product := program.Products[0]

	// Check string literal parameter
	nameParam := findParam(t, product.Parameters, "name")
	if nameParam.Type.Kind != IRTypeString {
		t.Errorf("Expected string type, got %s", nameParam.Type.Kind)
	}
	if nameParam.DefaultValue.LiteralValue != "TestItem" {
		t.Errorf("Expected string value 'TestItem', got %v", nameParam.DefaultValue.LiteralValue)
	}

	// Check string reference parameter
	labelParam := findParam(t, product.Parameters, "label")
	if labelParam.DefaultValue.Kind != ExprKindReference {
		t.Errorf("Expected reference expression, got %s", labelParam.DefaultValue.Kind)
	}
	if labelParam.DefaultValue.ReferenceType != RefTypeConstant {
		t.Errorf("Expected reference type constant, got %s", labelParam.DefaultValue.ReferenceType)
	}
}

func TestConvertAST_BooleanParameter(t *testing.T) {
	dslContent := `
product Test {
    param flag: boolean = true
    param other: boolean = false
    param combined: boolean = flag && other
    param comparison: boolean = 1 < 2
}`

	ast := parseAndValidate(t, dslContent)
	program, err := ConvertAST(ast)
	if err != nil {
		t.Fatalf("ConvertAST failed: %v", err)
	}

	product := program.Products[0]

	// Check boolean literal
	flagParam := findParam(t, product.Parameters, "flag")
	if flagParam.Type.Kind != IRTypeBoolean {
		t.Errorf("Expected boolean type, got %s", flagParam.Type.Kind)
	}
	if flagParam.DefaultValue.LiteralValue != true {
		t.Errorf("Expected boolean value true, got %v", flagParam.DefaultValue.LiteralValue)
	}

	// Check logical AND
	combinedParam := findParam(t, product.Parameters, "combined")
	if combinedParam.DefaultValue.Kind != ExprKindBinary {
		t.Errorf("Expected binary expression, got %s", combinedParam.DefaultValue.Kind)
	}
	if combinedParam.DefaultValue.BinaryOp != "&&" {
		t.Errorf("Expected operator '&&', got '%s'", combinedParam.DefaultValue.BinaryOp)
	}

	// Check comparison expression
	comparisonParam := findParam(t, product.Parameters, "comparison")
	if comparisonParam.DefaultValue.Kind != ExprKindBinary {
		t.Errorf("Expected binary expression, got %s", comparisonParam.DefaultValue.Kind)
	}
	if comparisonParam.DefaultValue.BinaryOp != "<" {
		t.Errorf("Expected operator '<', got '%s'", comparisonParam.DefaultValue.BinaryOp)
	}
}

// Helper function to parse and validate DSL content
func parseAndValidate(t *testing.T, content string) *dsl.AST {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "test_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(content))); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse DSL: %v", err)
	}

	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("Failed to validate AST: %v", err)
	}

	return ast
}

// Helper function to find a parameter by name
func findParam(t *testing.T, params []IRParameter, name string) IRParameter {
	t.Helper()
	for _, p := range params {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("Parameter '%s' not found", name)
	return IRParameter{}
}
